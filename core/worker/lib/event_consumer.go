// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/confighub/sdk/core/worker/api"
	"github.com/google/uuid"
)

// EventConsumer reads the ConfigHub event log over HTTP long-poll and invokes a
// handler for each delivered fact.
//
// It is a pure consumer, deliberately separate from the bridge/function worker
// connection (lib.Worker). It authenticates as a worker (bot) identity — which
// keys its server-held delivery cursor and authorizes what it can observe — but
// it holds no lease and never dequeues operations. Nothing it does contends
// with a Target's directed work: it only reads the append-only log by cursor.
//
// The subscription's Name is the cursor name; it goes in the request path. The
// filter travels in the body each poll — the server stores only the cursor
// position, so a restarted consumer resumes from its bookmark without keeping
// any local state.
type EventConsumer struct {
	mainURL      string
	workerID     string
	workerSecret string
	subscription api.EventSubscription
	handler      EventHandler
	httpClient   *http.Client

	// spaceID is discovered from /auth/worker on the first auth; the event-poll
	// URL is space-scoped like the rest of the worker API.
	spaceID uuid.UUID

	jwtMu sync.RWMutex
	jwt   string
}

// eventCursorPollRequestBody is the JSON body sent to POST
// /event_cursor/{name}/poll. Field names match the server's
// EventCursorPollRequest. The cursor name is in the path, not here.
type eventCursorPollRequestBody struct {
	EventTypes     []string `json:"event_types,omitempty"`
	SpaceID        string   `json:"space_id,omitempty"`
	TargetID       string   `json:"target_id,omitempty"`
	FromCursor     *int64   `json:"from_cursor,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

// NewEventConsumer builds a consumer for the given worker identity and one
// subscription (its Name is the cursor name). handler is invoked once per
// delivered event-log fact. Run several consumers for several cursors.
func NewEventConsumer(serverURL, workerID, workerSecret string, subscription api.EventSubscription, handler EventHandler) *EventConsumer {
	return &EventConsumer{
		mainURL:      strings.TrimRight(serverURL, "/"),
		workerID:     workerID,
		workerSecret: workerSecret,
		subscription: subscription,
		handler:      handler,
		httpClient:   &http.Client{},
	}
}

// Run authenticates, then long-polls the event log until ctx is canceled. It
// survives transient network/5xx failures with a brief backoff and refreshes
// its JWT on 401, mirroring the queued_operation poll's resilience.
func (ec *EventConsumer) Run(ctx context.Context) error {
	if ec.subscription.Name == "" {
		return errors.New("event consumer requires a subscription name (the cursor name)")
	}
	if err := ec.refreshJWT(ctx); err != nil {
		return fmt.Errorf("initial auth: %w", err)
	}

	pollURL := fmt.Sprintf("%s/api/space/%s/bridge_worker/%s/event_cursor/%s/poll",
		ec.mainURL, ec.spaceID, ec.workerID, url.PathEscape(ec.subscription.Name))
	body, err := json.Marshal(eventCursorPollRequestBody{
		EventTypes:     ec.subscription.EventTypes,
		SpaceID:        ec.subscription.SpaceID,
		TargetID:       ec.subscription.TargetID,
		FromCursor:     ec.subscription.FromCursor,
		TimeoutSeconds: pollTimeoutSeconds,
	})
	if err != nil {
		return err
	}

	var transientFailures int
	noteSuccess := func() {
		if transientFailures > 0 {
			log.Printf("[INFO] event-poll: resumed after %d failed attempts", transientFailures)
			transientFailures = 0
		}
	}
	noteTransientFailure := func(format string, args ...any) {
		transientFailures++
		if transientFailures == 1 {
			log.Printf("[WARN] event-poll: "+format+"; will retry", args...)
		}
	}
	backoff := func() bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
			return true
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		cctx, cancel := context.WithTimeout(ctx, pollRequestTimeout)
		req, rerr := http.NewRequestWithContext(cctx, http.MethodPost, pollURL, bytes.NewReader(body))
		if rerr != nil {
			cancel()
			return rerr
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+ec.currentJWT())

		resp, derr := ec.httpClient.Do(req)
		if derr != nil {
			cancel()
			if errors.Is(derr, context.Canceled) {
				return ctx.Err()
			}
			noteTransientFailure("request error: %v", derr)
			if !backoff() {
				return ctx.Err()
			}
			continue
		}

		switch {
		case resp.StatusCode == http.StatusNoContent:
			// Long-poll timed out with no new facts. Poll again.
			resp.Body.Close()
			cancel()
			noteSuccess()
		case resp.StatusCode == http.StatusOK:
			var msgs []*api.EventMessage
			decErr := json.NewDecoder(resp.Body).Decode(&msgs)
			resp.Body.Close()
			cancel()
			if decErr != nil {
				return fmt.Errorf("decode event-poll response: %w", decErr)
			}
			noteSuccess()
			ec.dispatch(ctx, msgs)
		case resp.StatusCode == http.StatusUnauthorized:
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			cancel()
			log.Printf("[INFO] event-poll: 401, refreshing JWT (%s)", strings.TrimSpace(string(respBody)))
			if refErr := ec.refreshJWT(ctx); refErr != nil {
				return fmt.Errorf("refresh JWT after 401: %w", refErr)
			}
		case resp.StatusCode >= 500 && resp.StatusCode < 600:
			// Server restarting / transient gateway failure. Back off and re-poll
			// rather than exiting, so the consumer rides through a deploy.
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			cancel()
			noteTransientFailure("status %d (%s)", resp.StatusCode, strings.TrimSpace(string(respBody)))
			if !backoff() {
				return ctx.Err()
			}
		default:
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			cancel()
			return fmt.Errorf("event-poll status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		}
	}
}

// dispatch decodes each delivered EventLog message into an EventLogEntry and
// hands it to the handler. Non-event messages are ignored — this endpoint only
// ever returns event-log facts, but the envelope is shared with the op poll.
func (ec *EventConsumer) dispatch(ctx context.Context, msgs []*api.EventMessage) {
	for _, m := range msgs {
		if m == nil || m.Event != api.EventLog {
			continue
		}
		raw, err := json.Marshal(m.Data)
		if err != nil {
			log.Printf("[ERROR] event-poll: failed to marshal event data: %v", err)
			continue
		}
		var entry api.EventLogEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			log.Printf("[ERROR] event-poll: failed to decode event-log entry: %v", err)
			continue
		}
		if ec.handler != nil {
			ec.handler(ctx, entry)
		}
	}
}

// refreshJWT mints a JWT from /auth/worker using the worker secret and, on the
// first call, records the worker's space so the event-poll URL can be built.
func (ec *EventConsumer) refreshJWT(ctx context.Context) error {
	authURL := ec.mainURL + "/auth/worker"
	body, err := json.Marshal(authWorkerRequest{
		WorkerID:     ec.workerID,
		WorkerSecret: ec.workerSecret,
	})
	if err != nil {
		return err
	}

	cctx, cancel := context.WithTimeout(ctx, shortRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, authURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ec.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("/auth/worker returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var aresp authWorkerResponse
	if err := json.NewDecoder(resp.Body).Decode(&aresp); err != nil {
		return err
	}
	if aresp.AccessToken == "" {
		return errors.New("/auth/worker returned empty access_token")
	}
	if ec.spaceID == uuid.Nil && aresp.Worker != nil {
		spaceID, perr := uuid.Parse(aresp.Worker.SpaceID)
		if perr != nil {
			return fmt.Errorf("/auth/worker returned non-UUID space_id %q: %w", aresp.Worker.SpaceID, perr)
		}
		ec.spaceID = spaceID
	}

	ec.jwtMu.Lock()
	ec.jwt = aresp.AccessToken
	ec.jwtMu.Unlock()
	return nil
}

func (ec *EventConsumer) currentJWT() string {
	ec.jwtMu.RLock()
	defer ec.jwtMu.RUnlock()
	return ec.jwt
}
