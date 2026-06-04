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
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/confighub/sdk/core/function/executor"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/google/uuid"
)

// Long-poll transport. Pairs with the server-side lease/poll/patch handlers.
//
// Wire flow:
//
//	auth once:           POST /auth/worker { worker_id, worker_secret }      → JWT + Worker { space_id, slug }
//	per claim attempt:   POST /api/space/{space_id}/bridge_worker/{id}/lease
//	after first claim:   PATCH /api/space/{space_id}/bridge_worker/{id}      (merge-patch: ProvidedInfo)
//	                     POST/PATCH /api/space/{space_id}/target/...         (one per AvailableTarget with Name)
//	per poll:            POST /api/space/{space_id}/bridge_worker/{id}/queued_operation/poll
//	per result:          PATCH /api/space/{space_id}/bridge_worker/{id}/queued_operation/{op_id}
//	on shutdown:         DELETE /api/space/{space_id}/bridge_worker/{id}/lease
//
// The lease (claim/release) is the leader-election mechanism: a worker holds
// the lease over its operation queue and renews it by polling. Poll and result
// submission act on the queued_operation resource.
//
// ProvidedInfo / Target registration is gated on holding the lease: two
// workers booting against the same bridge_worker would otherwise race each
// other into PATCHing ProvidedInfo, leaving stale data on whichever lost
// the claim race.

const (
	// pollTimeoutSeconds is sent in the POST body of /poll. The server caps
	// this at 60s and uses 30s if 0/negative.
	pollTimeoutSeconds = 30

	// claimAcquireBackoffInitial / Max define the wait between failed claim
	// acquisitions. We back off if another instance already holds the claim.
	claimAcquireBackoffInitial = 2 * time.Second
	claimAcquireBackoffMax     = 30 * time.Second

	// pollRequestTimeout is the HTTP-client deadline for each poll request.
	// It must exceed pollTimeoutSeconds to give the server room to reply on
	// the boundary; we add 15s of slack.
	pollRequestTimeout = (pollTimeoutSeconds + 15) * time.Second

	// shortRequestTimeout bounds non-poll calls (claim, release, patch, /me,
	// /auth/worker). Generous enough for any of these to succeed under load.
	shortRequestTimeout = 30 * time.Second
)

// claimResponse is the JSON body returned by POST /lease.
type claimResponse struct {
	ClaimToken uuid.UUID `json:"claim_token"`
	TTLSeconds int       `json:"ttl_seconds"`
}

// pollRequestBody is the JSON body sent to POST /queued_operation/poll.
type pollRequestBody struct {
	ClaimToken     uuid.UUID `json:"claim_token"`
	TimeoutSeconds int       `json:"timeout_seconds"`
}

// releaseRequestBody is the JSON body sent to DELETE /lease.
type releaseRequestBody struct {
	ClaimToken uuid.UUID `json:"claim_token"`
}

// authWorkerRequest is the JSON body sent to POST /auth/worker.
type authWorkerRequest struct {
	WorkerID     string `json:"worker_id"`
	WorkerSecret string `json:"worker_secret"`
}

// authWorkerResponse picks the fields we need from the POST /auth/worker response.
type authWorkerResponse struct {
	AccessToken string                 `json:"access_token"`
	Worker      *authWorkerResponseSub `json:"worker,omitempty"`
}

// authWorkerResponseSub is the worker-scoping object inside the
// POST /auth/worker response.
type authWorkerResponseSub struct {
	BridgeWorkerID string `json:"bridge_worker_id"`
	Slug           string `json:"slug"`
	SpaceID        string `json:"space_id"`
	SpaceSlug      string `json:"space_slug"`
}

// longPollTransport drives the worker via HTTP long-polling against the main
// API port. Unlike the SSE/h2c stream path (implemented inline in
// workerClient), it does not require h2c and does not switch to a separate
// worker port.
type longPollTransport struct {
	mainURL      string
	workerID     string
	workerSecret string

	bridgeWorker     api.BridgeWorker
	functionExecutor executor.FunctionExecutor

	httpClient *http.Client
	dispatcher eventDispatcher

	// bootstrap state, populated by /auth/worker on first refreshJWT
	spaceID    uuid.UUID
	workerSlug string

	// jwtMu guards jwt — re-auth happens on 401.
	jwtMu sync.RWMutex
	jwt   string

	// claimMu guards claimToken — set/cleared by Run, read by SendResult.
	// SendResult does not strictly need the claim token (PATCH just needs
	// JWT auth), but we keep it for log correlation and future tightening.
	claimMu    sync.RWMutex
	claimToken uuid.UUID
	claimTTL   time.Duration

	// shutdownInProgress is set by BeginShutdown so the outer Run loop
	// doesn't re-acquire the claim after we've released it eagerly.
	shutdownInProgress atomic.Bool
}

func newLongPollTransport(
	serverURL, workerID, workerSecret string,
	bridgeWorker api.BridgeWorker,
	functionExecutor executor.FunctionExecutor,
	dispatcher eventDispatcher,
) *longPollTransport {
	return &longPollTransport{
		mainURL:          serverURL,
		workerID:         workerID,
		workerSecret:     workerSecret,
		bridgeWorker:     bridgeWorker,
		functionExecutor: functionExecutor,
		httpClient:       &http.Client{}, // no special transport — plain HTTP
		dispatcher:       dispatcher,
	}
}

// Run drives the long-poll lifecycle: auth → claim → register info → poll
// loop. Blocks until ctx is canceled. On shutdown, best-effort releases the
// claim.
//
// ProvidedInfo / Target registration runs *after* the first successful claim,
// not at process startup. Otherwise two workers booting against the same
// bridge_worker would both PATCH ProvidedInfo before either had won the
// claim — one's update would silently overwrite the other's, leaving stale
// data on the worker record of whichever instance lost the race for the
// claim. Gating registration on holding the claim makes the worker that
// owns the queue also the worker whose info the server reflects.
//
// Registration is idempotent and only runs once per process; re-acquires
// after a claim death don't re-register.
func (t *longPollTransport) Run(ctx context.Context) error {
	if err := t.refreshJWT(ctx); err != nil {
		return fmt.Errorf("long-poll bootstrap: %w", err)
	}
	if t.spaceID == uuid.Nil {
		return errors.New("long-poll bootstrap: /auth/worker did not return space_id; is the server new enough?")
	}
	log.Printf("[INFO] long-poll auth complete: worker_slug=%s space_id=%s", t.workerSlug, t.spaceID)

	t.dispatcher.SetServing(true)
	defer t.dispatcher.SetServing(false)

	registered := false

	// Outer loop: acquire claim, poll until claim dies, then re-acquire.
	// On ctx cancellation we release the claim before returning so the next
	// run (or a subsequent `cub worker delete`) doesn't wait for TTL expiry.
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if t.shutdownInProgress.Load() {
			// BeginShutdown already released the claim; don't grab a new
			// one. Wait for ctx to cancel.
			<-ctx.Done()
			return ctx.Err()
		}

		token, ttl, err := t.acquireClaimWithBackoff(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return ctx.Err()
			}
			return fmt.Errorf("long-poll: failed to acquire claim: %w", err)
		}

		t.setClaim(token, ttl)
		log.Printf("[INFO] long-poll claim acquired token=%s ttl=%v", token, ttl)

		// First successful claim: publish ProvidedInfo and register
		// targets. Doing this *after* the claim guarantees we are the
		// active worker before mutating shared state.
		if !registered {
			if err := t.registerWorkerInfo(ctx); err != nil {
				return fmt.Errorf("long-poll: register worker info: %w", err)
			}
			if err := t.registerAvailableTargets(ctx); err != nil {
				// Best-effort — operators can create targets out-of-band,
				// and an error here typically means "target already exists
				// with a conflicting shape" which the worker can't auto-
				// resolve. Don't fail Run.
				log.Printf("[WARN] long-poll: target registration had errors (continuing): %v", err)
			}
			registered = true
			log.Printf("[INFO] long-poll registration complete")
		}

		// Inner poll loop. Returns when claim is dead or ctx is done.
		err = t.pollLoop(ctx, token)
		if err != nil && errors.Is(err, context.Canceled) {
			// Graceful shutdown: release before clearing local state so the
			// server marks the worker Disconnected immediately. Use a fresh
			// background ctx with a tight deadline since ctx is already
			// canceled.
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if rerr := t.releaseClaim(releaseCtx, token); rerr != nil {
				log.Printf("[WARN] long-poll: release claim on shutdown: %v", rerr)
			} else {
				log.Printf("[INFO] long-poll claim released on shutdown token=%s", token)
			}
			cancel()
			t.clearClaim()
			return ctx.Err()
		}
		t.clearClaim()
		if err != nil {
			log.Printf("[WARN] long-poll: poll loop ended: %v. Re-acquiring claim.", err)
		}
	}
}

// registerAvailableTargets walks the worker's BridgeWorkerInfo.AvailableTargets
// and POSTs each (or PATCHes if it already exists) to the standard /target
// endpoint. The SSE counterpart registers targets server-side as part of the
// stream handshake; the two paths intentionally diverge on how an existing
// Target is updated (see comment on the PATCH branch in upsertTarget below).
// Auto-target updates are slated for removal, so we are not chasing byte-exact
// parity.
//
// This is best-effort: errors on individual targets are logged but don't fail
// bootstrap. AvailableTargets without a Name are skipped (their existence in
// ProvidedInfo is just a way to publish a BridgeHandle).
func (t *longPollTransport) registerAvailableTargets(ctx context.Context) error {
	bridgeWorkerInfo := t.bridgeWorker.Info(api.InfoOptions{WorkerSlug: t.workerSlug})

	type bridgeKey struct {
		compatibleProvider api.ProviderType
		bridgeHandle       string
	}
	compatibleConfigTypes := map[bridgeKey][]api.ConfigType{}
	for _, ct := range bridgeWorkerInfo.SupportedConfigTypes {
		if ct == nil || ct.CompatibleBridge == "" {
			continue
		}
		seen := map[string]bool{}
		for _, at := range ct.AvailableTargets {
			if at.BridgeHandle == "" || seen[at.BridgeHandle] {
				continue
			}
			seen[at.BridgeHandle] = true
			k := bridgeKey{compatibleProvider: ct.CompatibleBridge, bridgeHandle: at.BridgeHandle}
			compatibleConfigTypes[k] = append(compatibleConfigTypes[k], ct.ConfigType)
		}
	}

	var firstErr error
	for _, ct := range bridgeWorkerInfo.SupportedConfigTypes {
		if ct == nil {
			continue
		}
		for _, at := range ct.AvailableTargets {
			if at.Name == "" {
				continue
			}
			k := bridgeKey{compatibleProvider: ct.ProviderType, bridgeHandle: at.BridgeHandle}
			if err := t.upsertTarget(ctx, ct, at, compatibleConfigTypes[k]); err != nil {
				log.Printf("[WARN] long-poll: failed to upsert target %s: %v", at.Name, err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	return firstErr
}

// targetUpsertBody mirrors the fields of goclientnew.Target the server expects
// on POST/PATCH. We use plain map[string]any rather than the generated client
// to keep the long-poll transport self-contained.
type targetUpsertBody struct {
	Slug           string                 `json:"Slug"`
	DisplayName    string                 `json:"DisplayName"`
	BridgeHandle   string                 `json:"BridgeHandle,omitempty"`
	BridgeWorkerID uuid.UUID              `json:"BridgeWorkerID"`
	ProviderType   api.ProviderType       `json:"ProviderType"`
	ToolchainType  any                    `json:"ToolchainType"`
	LiveStateType  any                    `json:"LiveStateType,omitempty"`
	Parameters     string                 `json:"Parameters,omitempty"`
	ConfigTypes    []targetConfigTypeBody `json:"ConfigTypes,omitempty"`
}

type targetConfigTypeBody struct {
	ProviderType  api.ProviderType `json:"ProviderType,omitempty"`
	ToolchainType any              `json:"ToolchainType,omitempty"`
	LiveStateType any              `json:"LiveStateType,omitempty"`
}

func (t *longPollTransport) upsertTarget(
	ctx context.Context,
	ct *api.SupportedConfigType,
	availableTarget api.Target,
	compatibleConfigTypes []api.ConfigType,
) error {
	jsonParams, err := json.Marshal(availableTarget.Params)
	if err != nil {
		return fmt.Errorf("marshal target params: %w", err)
	}
	body := targetUpsertBody{
		Slug:           availableTarget.Name,
		DisplayName:    availableTarget.Name,
		BridgeHandle:   availableTarget.BridgeHandle,
		BridgeWorkerID: t.workerUUID(),
		ProviderType:   ct.ProviderType,
		ToolchainType:  ct.ToolchainType,
		LiveStateType:  ct.LiveStateType,
		Parameters:     string(jsonParams),
	}
	for _, cct := range compatibleConfigTypes {
		body.ConfigTypes = append(body.ConfigTypes, targetConfigTypeBody{
			ProviderType:  cct.ProviderType,
			ToolchainType: cct.ToolchainType,
			LiveStateType: cct.LiveStateType,
		})
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal target body: %w", err)
	}

	// Try POST first; on 409 (slug already exists), fall back to PATCH.
	createURL := fmt.Sprintf("%s/api/space/%s/target", t.mainURL, t.spaceID)
	status, respBody, err := t.doJSON(ctx, http.MethodPost, createURL, "application/json", payload)
	if err != nil {
		return err
	}
	if status == http.StatusOK || status == http.StatusCreated {
		return nil
	}
	if status != http.StatusConflict {
		return fmt.Errorf("POST target status %d: %s", status, string(respBody))
	}

	// Conflict — narrow PATCH so we only refresh the fields that an operator
	// is unlikely to have hand-edited. Specifically: leave Parameters,
	// ConfigTypes, ProviderType, ToolchainType, and LiveStateType alone. We
	// do refresh BridgeHandle and BridgeWorkerID so the existing target keeps
	// pointing at *this* worker, and DisplayName so a slug-only target gains
	// a display name. NOTE: This intentionally diverges from the SSE auto-
	// target merge in HandleStream's updateWorkerData (which mutates more
	// fields with an existing-params-wins merge). We're not aiming for byte-
	// exact parity — auto-target updates are slated for removal, and the
	// narrower PATCH is closer to the desired end-state where target shape
	// is operator-managed and not something a worker rewrites on every
	// reconnect.
	// NOTE: (jesperfj) Claude made these choices when I told it to get as close
	// to the SSE transport behavior as possible but also that worker-created
	// targets will probably go away (or change). We can tweak as needed to fix
	// compatibility issues as we see them.
	patch := map[string]any{
		"BridgeHandle":   availableTarget.BridgeHandle,
		"BridgeWorkerID": t.workerUUID(),
		"DisplayName":    availableTarget.Name,
	}
	patchPayload, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal target patch: %w", err)
	}
	patchURL := fmt.Sprintf("%s/api/space/%s/target/%s", t.mainURL, t.spaceID, availableTarget.Name)
	pstatus, pbody, err := t.doJSON(ctx, http.MethodPatch, patchURL, "application/merge-patch+json", patchPayload)
	if err != nil {
		return err
	}
	if pstatus != http.StatusOK && pstatus != http.StatusNoContent {
		return fmt.Errorf("PATCH target status %d: %s", pstatus, string(pbody))
	}
	return nil
}

// workerUUID parses the worker ID once. Bootstrap will already have populated
// it via /auth/worker, so this is the same id passed on the command line.
func (t *longPollTransport) workerUUID() uuid.UUID {
	id, _ := uuid.Parse(t.workerID)
	return id
}

// doJSON is a thin helper for one-shot JSON requests. It refreshes the JWT on
// 401 and retries once.
func (t *longPollTransport) doJSON(ctx context.Context, method, url, contentType string, payload []byte) (int, []byte, error) {
	doOnce := func() (int, []byte, error) {
		cctx, cancel := context.WithTimeout(ctx, shortRequestTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(cctx, method, url, bytes.NewReader(payload))
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Authorization", "Bearer "+t.currentJWT())
		resp, err := t.httpClient.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, body, nil
	}

	status, body, err := doOnce()
	if err != nil {
		return 0, nil, err
	}
	if status == http.StatusUnauthorized {
		if rerr := t.refreshJWT(ctx); rerr != nil {
			return status, body, fmt.Errorf("refresh JWT after 401: %w", rerr)
		}
		return doOnce()
	}
	return status, body, nil
}

// registerWorkerInfo PATCHes the standard bridge_worker resource with the
// worker's WorkerInfo (as ProvidedInfo). The server uses this to register
// functions (and, for SSE, also auto-create Targets — long-poll creates
// targets explicitly via the standard /target endpoint, see
// registerAvailableTargets).
//
// Uses RFC 7396 application/merge-patch+json so unrelated fields aren't
// touched.
func (t *longPollTransport) registerWorkerInfo(ctx context.Context) error {
	bridgeWorkerInfo := t.bridgeWorker.Info(api.InfoOptions{WorkerSlug: t.workerSlug})

	var functionWorkerInfo api.FunctionWorkerInfo
	if t.functionExecutor != nil {
		functionWorkerInfo = t.functionExecutor.Info()
	}

	patch := map[string]any{
		"ProvidedInfo": api.WorkerInfo{
			BridgeWorkerInfo:   bridgeWorkerInfo,
			FunctionWorkerInfo: functionWorkerInfo,
		},
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal ProvidedInfo patch: %w", err)
	}

	url := fmt.Sprintf("%s/api/space/%s/bridge_worker/%s",
		t.mainURL, t.spaceID, t.workerID)

	cctx, cancel := context.WithTimeout(ctx, shortRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.Header.Set("Authorization", "Bearer "+t.currentJWT())

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// refreshJWT mints a new JWT from /auth/worker using the worker secret. The
// response also carries the worker's space and slug; we populate those on the
// first call. Replaces any existing JWT.
func (t *longPollTransport) refreshJWT(ctx context.Context) error {
	url := t.mainURL + "/auth/worker"
	body, err := json.Marshal(authWorkerRequest{
		WorkerID:     t.workerID,
		WorkerSecret: t.workerSecret,
	})
	if err != nil {
		return err
	}

	cctx, cancel := context.WithTimeout(ctx, shortRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var aresp authWorkerResponse
	if err := json.NewDecoder(resp.Body).Decode(&aresp); err != nil {
		return err
	}
	if aresp.AccessToken == "" {
		return errors.New("/auth/worker returned empty access_token")
	}

	// First call populates space + slug. Subsequent refreshes leave them
	// alone — they don't change for the lifetime of the worker.
	if t.spaceID == uuid.Nil && aresp.Worker != nil {
		spaceID, perr := uuid.Parse(aresp.Worker.SpaceID)
		if perr != nil {
			return fmt.Errorf("/auth/worker returned non-UUID space_id %q: %w", aresp.Worker.SpaceID, perr)
		}
		t.spaceID = spaceID
		t.workerSlug = aresp.Worker.Slug
	}

	t.jwtMu.Lock()
	t.jwt = aresp.AccessToken
	t.jwtMu.Unlock()
	return nil
}

// currentJWT returns the JWT for use in Authorization headers.
func (t *longPollTransport) currentJWT() string {
	t.jwtMu.RLock()
	defer t.jwtMu.RUnlock()
	return t.jwt
}

func (t *longPollTransport) setClaim(token uuid.UUID, ttl time.Duration) {
	t.claimMu.Lock()
	defer t.claimMu.Unlock()
	t.claimToken = token
	t.claimTTL = ttl
}

func (t *longPollTransport) clearClaim() {
	t.claimMu.Lock()
	defer t.claimMu.Unlock()
	t.claimToken = uuid.Nil
	t.claimTTL = 0
}

func (t *longPollTransport) currentClaimToken() uuid.UUID {
	t.claimMu.RLock()
	defer t.claimMu.RUnlock()
	return t.claimToken
}

// acquireClaimWithBackoff retries claim acquisition with exponential backoff.
// Three retryable cases, all loop:
//   - 409 (claim_held): another instance owns the claim — wait for its TTL.
//   - 5xx / network failure: server is restarting or briefly unreachable.
//     The SSE reconnect loop retries forever in this situation; long-poll
//     matches that so workers survive routine deploys without operator
//     intervention.
//   - 401 with successful re-auth: refreshJWT already rotated the token.
//
// 4xx (other than 401/409) and persistent re-auth failures propagate as
// fatal — those indicate misconfiguration the worker can't fix on its own.
func (t *longPollTransport) acquireClaimWithBackoff(ctx context.Context) (uuid.UUID, time.Duration, error) {
	delay := claimAcquireBackoffInitial
	for {
		select {
		case <-ctx.Done():
			return uuid.Nil, 0, ctx.Err()
		default:
		}

		token, ttl, status, err := t.acquireClaim(ctx)
		if err == nil {
			return token, ttl, nil
		}

		retryable := false
		switch {
		case status == http.StatusConflict:
			log.Printf("[INFO] long-poll: claim held by another worker; will retry: %v", err)
			retryable = true
		case status == http.StatusUnauthorized:
			// acquireClaim already attempted a refreshJWT. Retry the
			// acquire so it picks up the new token. If the refresh itself
			// failed, err carries that and we still loop briefly — the
			// next iteration's refreshJWT may succeed if the server is
			// transiently unavailable.
			log.Printf("[INFO] long-poll: 401 on acquire; will retry after JWT refresh: %v", err)
			retryable = true
		case status >= 500 && status < 600:
			log.Printf("[WARN] long-poll: server %d on acquire; will retry: %v", status, err)
			retryable = true
		case status == 0:
			// Network error — no HTTP response at all (server down,
			// connection refused, DNS failure, etc).
			log.Printf("[WARN] long-poll: network error on acquire; will retry: %v", err)
			retryable = true
		}
		if !retryable {
			return uuid.Nil, 0, err
		}

		jitter := time.Duration(rand.Float64() * float64(delay) * 0.2)
		wait := delay + jitter
		select {
		case <-ctx.Done():
			return uuid.Nil, 0, ctx.Err()
		case <-time.After(wait):
		}
		delay *= 2
		if delay > claimAcquireBackoffMax {
			delay = claimAcquireBackoffMax
		}
	}
}

// acquireClaim issues one POST /lease. On success returns
// (token, ttl, 200, nil). On 409 returns (nil, 0, 409, error). On other
// errors returns (nil, 0, status, error).
func (t *longPollTransport) acquireClaim(ctx context.Context) (uuid.UUID, time.Duration, int, error) {
	url := fmt.Sprintf("%s/api/space/%s/bridge_worker/%s/lease",
		t.mainURL, t.spaceID, t.workerID)

	cctx, cancel := context.WithTimeout(ctx, shortRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, url, nil)
	if err != nil {
		return uuid.Nil, 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.currentJWT())

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return uuid.Nil, 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		// JWT expired. Refresh once and let the caller retry on the next
		// outer iteration.
		if rerr := t.refreshJWT(ctx); rerr != nil {
			return uuid.Nil, 0, resp.StatusCode, fmt.Errorf("re-auth after 401: %w", rerr)
		}
		return uuid.Nil, 0, resp.StatusCode, errors.New("JWT expired, refreshed")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return uuid.Nil, 0, resp.StatusCode, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var cresp claimResponse
	if err := json.NewDecoder(resp.Body).Decode(&cresp); err != nil {
		return uuid.Nil, 0, resp.StatusCode, err
	}
	return cresp.ClaimToken, time.Duration(cresp.TTLSeconds) * time.Second, http.StatusOK, nil
}

// releaseClaim issues DELETE /lease, carrying the claim_token in the body so
// the server only releases a lease we actually hold. Best-effort.
func (t *longPollTransport) releaseClaim(ctx context.Context, token uuid.UUID) error {
	url := fmt.Sprintf("%s/api/space/%s/bridge_worker/%s/lease",
		t.mainURL, t.spaceID, t.workerID)
	body, err := json.Marshal(releaseRequestBody{ClaimToken: token})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.currentJWT())

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// pollLoop drives /poll until the claim dies, ctx cancels, or an
// unrecoverable error occurs. Each iteration is one POST that may block on
// the server side for up to pollTimeoutSeconds.
//
// Transient-failure logging: we log the first 5xx/network failure verbosely
// and then suppress identical follow-ups while the streak continues. When a
// poll finally succeeds we emit a single "resumed after N failures" line so
// it's clear the worker recovered. This avoids the wall of identical WARN
// lines during a server bounce.
func (t *longPollTransport) pollLoop(ctx context.Context, token uuid.UUID) error {
	url := fmt.Sprintf("%s/api/space/%s/bridge_worker/%s/queued_operation/poll",
		t.mainURL, t.spaceID, t.workerID)

	// Streak state for the de-duped retry logging described above.
	var transientFailures int
	noteSuccess := func() {
		if transientFailures > 0 {
			log.Printf("[INFO] long-poll: poll resumed after %d failed attempts", transientFailures)
			transientFailures = 0
		}
	}
	noteTransientFailure := func(format string, args ...any) {
		transientFailures++
		if transientFailures == 1 {
			log.Printf("[WARN] long-poll: "+format+"; will retry", args...)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		body, err := json.Marshal(pollRequestBody{
			ClaimToken:     token,
			TimeoutSeconds: pollTimeoutSeconds,
		})
		if err != nil {
			return err
		}

		cctx, cancel := context.WithTimeout(ctx, pollRequestTimeout)
		req, err := http.NewRequestWithContext(cctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			cancel()
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+t.currentJWT())

		resp, err := t.httpClient.Do(req)
		if err != nil {
			cancel()
			if errors.Is(err, context.Canceled) {
				return ctx.Err()
			}
			noteTransientFailure("poll request error: %v", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
			continue
		}

		switch {
		case resp.StatusCode == http.StatusNoContent:
			// Timeout reached without work. Loop again.
			resp.Body.Close()
			cancel()
			noteSuccess()
			continue
		case resp.StatusCode == http.StatusOK:
			var events []*api.EventMessage
			derr := json.NewDecoder(resp.Body).Decode(&events)
			resp.Body.Close()
			cancel()
			if derr != nil {
				return fmt.Errorf("decode poll response: %w", derr)
			}
			noteSuccess()
			t.dispatchEvents(ctx, events)
		case resp.StatusCode == http.StatusConflict:
			// claim_dead — server expired our claim. Caller re-acquires.
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			cancel()
			return fmt.Errorf("claim dead: %s", strings.TrimSpace(string(respBody)))
		case resp.StatusCode == http.StatusUnauthorized:
			// JWT expired. Refresh and retry the same poll.
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			cancel()
			log.Printf("[INFO] long-poll: 401 on poll, refreshing JWT (%s)", strings.TrimSpace(string(respBody)))
			if rerr := t.refreshJWT(ctx); rerr != nil {
				return fmt.Errorf("refresh JWT after 401: %w", rerr)
			}
		case resp.StatusCode >= 500 && resp.StatusCode < 600:
			// Server is restarting / overloaded / behind a transient gateway
			// failure. Don't return — that would unwind to the outer
			// re-acquire loop and risk killing the worker if the transient
			// state spans the next acquire too. Just back off briefly and
			// re-poll. SSE survives server deploys via its reconnect loop;
			// this is the long-poll equivalent.
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			cancel()
			noteTransientFailure("poll status %d (%s)", resp.StatusCode, strings.TrimSpace(string(respBody)))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		default:
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			cancel()
			return fmt.Errorf("poll status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		}
	}
}

// dispatchEvents hands the freshly polled events to the workerClient. Each
// event is JSON-marshaled because handleEventWithLogging takes []byte.
func (t *longPollTransport) dispatchEvents(ctx context.Context, events []*api.EventMessage) {
	for _, event := range events {
		if event == nil {
			continue
		}
		eventData, err := json.Marshal(event.Data)
		if err != nil {
			log.Printf("[ERROR] long-poll: failed to marshal event data: %v", err)
			continue
		}
		t.dispatcher.DispatchEvent(ctx, event.Event, eventData)
	}
}

// SendResult ships an action result via PATCH /queued_operation/{op_id} with
// retry. Mirrors the SSE transport's retry policy: 5min total, exp backoff
// capped at 30s, 15 attempts. 4xx is permanent except 409 (optimistic-lock
// conflict, retryable) and 401 (refresh JWT and retry).
func (t *longPollTransport) SendResult(result *api.ActionResult) error {
	if result.QueuedOperationID == uuid.Nil {
		return errors.New("ActionResult has no QueuedOperationID; long-poll PATCH requires it in the URL")
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %v", err)
	}
	if result.Action != api.ActionHeartbeat {
		logResult := *result
		logResult.Data = []byte(fmt.Sprintf("redacted: %d bytes", len(result.Data)))
		logResult.LiveData = []byte(fmt.Sprintf("redacted: %d bytes", len(result.LiveData)))
		logResult.LiveState = []byte(fmt.Sprintf("redacted: %d bytes", len(result.LiveState)))
		filteredJSON, _ := json.Marshal(logResult)
		log.Printf("Result body (PATCH): %s", string(filteredJSON))
	}

	eb := &backoff.ExponentialBackOff{
		InitialInterval:     1 * time.Second,
		RandomizationFactor: backoff.DefaultRandomizationFactor,
		Multiplier:          2.0,
		MaxInterval:         30 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	url := fmt.Sprintf("%s/api/space/%s/bridge_worker/%s/queued_operation/%s",
		t.mainURL, t.spaceID, t.workerID, result.QueuedOperationID)

	attempt := 0
	op := func() (any, error) {
		attempt++

		req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(resultJSON))
		if err != nil {
			return nil, backoff.Permanent(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+t.currentJWT())

		resp, err := t.httpClient.Do(req)
		if err != nil {
			if result.Action != api.ActionHeartbeat {
				log.Printf("[WARN] long-poll: PATCH result attempt #%d failed: %v - will retry", attempt, err)
			}
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
			if attempt > 1 && result.Action != api.ActionHeartbeat {
				log.Printf("[INFO] long-poll: PATCH result succeeded after %d attempts", attempt)
			}
			return nil, nil
		}

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusUnauthorized {
			// Refresh JWT and let backoff retry pick it up.
			log.Printf("[INFO] long-poll: 401 on PATCH result, refreshing JWT")
			if rerr := t.refreshJWT(ctx); rerr != nil {
				return nil, fmt.Errorf("refresh JWT after 401: %w", rerr)
			}
			return nil, fmt.Errorf("retried after 401")
		}
		// Same policy as SSE transport: 4xx permanent except 409.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusConflict {
			return nil, backoff.Permanent(fmt.Errorf("server returned status %d: %s, body: %s", resp.StatusCode, resp.Status, string(body)))
		}
		if result.Action != api.ActionHeartbeat {
			log.Printf("[WARN] long-poll: PATCH result attempt #%d returned %d - will retry", attempt, resp.StatusCode)
		}
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, resp.Status)
	}

	_, err = backoff.Retry(ctx, op, backoff.WithBackOff(eb), backoff.WithMaxTries(15))
	if err != nil {
		log.Printf("[ERROR] long-poll: PATCH result failed after retries: %v", err)
		return err
	}
	return nil
}

// BeginShutdown releases the claim eagerly so the server marks the worker
// Disconnected before in-flight operations finish draining. Without this,
// a long-running watcher (kubernetes WatchForApply on a resource that
// never becomes ready) would keep WaitForPendingOperations blocked, the
// claim wouldn't be released until the watcher finally exits, and during
// that window any `cub worker delete` from another caller would fail with
// "Worker is connected; shut down the worker first".
//
// After this returns, the transport is no longer in the poll loop — the
// outer Run is racing with ctx cancellation but won't re-acquire because
// shutdownInProgress is set. SendResult/SendResultOnce keep working
// (they only need the JWT, not the claim).
//
// TODO: The reason this BeginShutdown exists is the existence of
// WaitForPendingOperations which is a questionable implementation itself.
// See https://github.com/confighubai/confighub/issues/4359.
// If we can agree to get rid of WaitForPendingOperations, we can perhaps
// also remove this in favor of a more well defined disconnect or shutdown
// mechanism that is meaningful for both long-poll and SSE.
func (t *longPollTransport) BeginShutdown() {
	if t.shutdownInProgress.Swap(true) {
		return // already shutting down
	}
	token := t.currentClaimToken()
	if token == uuid.Nil {
		return
	}
	releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := t.releaseClaim(releaseCtx, token); err != nil {
		log.Printf("[WARN] long-poll: BeginShutdown release failed: %v", err)
		return
	}
	log.Printf("[INFO] long-poll claim released eagerly on shutdown token=%s", token)
}

// SendResultOnce ships a result without retry. The status queue handles its
// own infinite retry.
func (t *longPollTransport) SendResultOnce(result *api.ActionResult) error {
	if result.QueuedOperationID == uuid.Nil {
		return errors.New("ActionResult has no QueuedOperationID; long-poll PATCH requires it in the URL")
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %v", err)
	}
	if result.Action != api.ActionHeartbeat {
		logResult := *result
		logResult.Data = []byte(fmt.Sprintf("redacted: %d bytes", len(result.Data)))
		logResult.LiveData = []byte(fmt.Sprintf("redacted: %d bytes", len(result.LiveData)))
		logResult.LiveState = []byte(fmt.Sprintf("redacted: %d bytes", len(result.LiveState)))
		filteredJSON, _ := json.Marshal(logResult)
		log.Printf("Sending result (PATCH, once): %s", string(filteredJSON))
	}

	url := fmt.Sprintf("%s/api/space/%s/bridge_worker/%s/queued_operation/%s",
		t.mainURL, t.spaceID, t.workerID, result.QueuedOperationID)

	ctx, cancel := context.WithTimeout(context.Background(), shortRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(resultJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.currentJWT())

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("server returned status %d: %s, body: %s",
		resp.StatusCode, resp.Status, string(body))
}
