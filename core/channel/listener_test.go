// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/cubapi"
	funcapi "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// fakeChannel is the server side of one Channel, enough of it for the listener's loop: the
// lease, the poll, the result and the release, with a queue of what the next polls deliver.
type fakeChannel struct {
	t       *testing.T
	space   uuid.UUID
	id      uuid.UUID
	channel goclientnew.Channel

	mu       sync.Mutex
	token    uuid.UUID
	holdNext int // polls to answer 409 with, from the start
	heldNext int // acquires to answer 409 with
	queue    [][]goclientnew.ChannelMessage
	polls    []goclientnew.ChannelPollRequest
	results  map[uuid.UUID]goclientnew.CommandRunResult
	released []uuid.UUID
	auths    int
	served   chan struct{}
}

func newFakeChannel(t *testing.T, declared ...string) (*fakeChannel, *httptest.Server) {
	f := &fakeChannel{t: t, space: uuid.New(), id: uuid.New(), results: map[uuid.UUID]goclientnew.CommandRunResult{}, served: make(chan struct{}, 100)}
	commands := map[string]goclientnew.CommandSignature{}
	for _, name := range declared {
		commands[name] = goclientnew.CommandSignature{}
	}
	f.channel = goclientnew.Channel{ChannelID: f.id, SpaceID: f.space, Slug: "bot", Commands: commands}
	mux := http.NewServeMux()
	prefix := fmt.Sprintf("/api/space/%s/channel/%s", f.space, f.id)
	mux.HandleFunc("/api/channel", func(w http.ResponseWriter, r *http.Request) {
		// The resolver lists by where clause; one channel answers every lookup.
		writeJSON(w, []goclientnew.ExtendedChannel{{Channel: &f.channel}})
	})
	mux.HandleFunc("POST "+prefix+"/lease", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.heldNext > 0 {
			f.heldNext--
			http.Error(w, `{"error":"claim_held"}`, http.StatusConflict)
			return
		}
		f.token = uuid.New()
		writeJSON(w, goclientnew.ChannelLease{ClaimToken: f.token, TTLSeconds: 60})
	})
	mux.HandleFunc("DELETE "+prefix+"/lease", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		token, _ := uuid.Parse(r.URL.Query().Get("claim_token"))
		f.released = append(f.released, token)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST "+prefix+"/poll", func(w http.ResponseWriter, r *http.Request) {
		var req goclientnew.ChannelPollRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.polls = append(f.polls, req)
		if req.ClaimToken != f.token {
			f.mu.Unlock()
			http.Error(w, `{"error":"claim_dead"}`, http.StatusConflict)
			return
		}
		if f.holdNext > 0 {
			f.holdNext--
			f.mu.Unlock()
			http.Error(w, "restarting", http.StatusBadGateway)
			return
		}
		if len(f.queue) == 0 {
			f.mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if req.MaxRuns == 0 && onlyRuns(f.queue[0]) {
			// Nothing deliverable to a poll that takes no runs: hold it for the time the
			// client asked, as the server would.
			f.mu.Unlock()
			time.Sleep(time.Duration(req.TimeoutSeconds) * time.Second)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		batch := f.queue[0]
		f.queue = f.queue[1:]
		// A run the listener has no slot for stays Pending for a later poll, as on the server.
		delivered := make([]goclientnew.ChannelMessage, 0, len(batch))
		var kept []goclientnew.ChannelMessage
		runs := 0
		for _, message := range batch {
			if message.Kind == KindCommandRun {
				if runs >= req.MaxRuns {
					kept = append(kept, message)
					continue
				}
				runs++
			}
			delivered = append(delivered, message)
		}
		if len(kept) > 0 {
			f.queue = append([][]goclientnew.ChannelMessage{kept}, f.queue...)
		}
		f.mu.Unlock()
		if len(delivered) == 0 {
			time.Sleep(50 * time.Millisecond)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, delivered)
	})
	mux.HandleFunc("POST "+prefix+"/command_run/{run}/result", func(w http.ResponseWriter, r *http.Request) {
		var result goclientnew.CommandRunResult
		if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		runID := uuid.MustParse(r.PathValue("run"))
		f.mu.Lock()
		f.results[runID] = result
		f.mu.Unlock()
		writeJSON(w, goclientnew.CommandRun{CommandRunID: runID, Status: result.Status})
		f.served <- struct{}{}
	})
	mux.HandleFunc("/auth/worker", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.auths++
		f.mu.Unlock()
		writeJSON(w, cubapi.AuthSession{AccessToken: "token-" + uuid.NewString(), AuthType: cubapi.AuthTypeJWT})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return f, server
}

// writeJSON answers as the server does: the generated client reads a body only under a JSON
// content type.
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (f *fakeChannel) enqueue(messages ...goclientnew.ChannelMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = append(f.queue, messages)
}

func onlyRuns(batch []goclientnew.ChannelMessage) bool {
	for _, message := range batch {
		if message.Kind != KindCommandRun {
			return false
		}
	}
	return len(batch) > 0
}

func runMessage(command string, params map[string]any) goclientnew.ChannelMessage {
	return goclientnew.ChannelMessage{Kind: KindCommandRun, CommandRun: &goclientnew.CommandRun{
		CommandRunID: uuid.New(), Command: command, Params: params, Status: string(StatusDelivered),
		TimeoutAt: time.Now().Add(30 * time.Second),
	}}
}

func eventMessage(eventType string) goclientnew.ChannelMessage {
	return goclientnew.ChannelMessage{Kind: KindEventLog, Event: &goclientnew.EventLog{EventLogID: uuid.New(), EventType: eventType}}
}

func (f *fakeChannel) waitResults(n int) {
	f.t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-f.served:
		case <-time.After(20 * time.Second):
			f.t.Fatalf("only %d of %d results arrived", i, n)
		}
	}
}

func newTestClient(t *testing.T, server *httptest.Server) *cubapi.Client {
	t.Helper()
	client, err := cubapi.NewClient(cubapi.ClientOptions{ServerURL: server.URL, Token: "static"})
	require.NoError(t, err)
	return client
}

func TestListenerRunsHandlersAndReportsOutcomes(t *testing.T) {
	fake, server := newFakeChannel(t, "test.echo", "test.fail", "test.reject", "test.panic")
	client := newTestClient(t, server)

	var events []string
	var eventsMu sync.Mutex
	listener := NewListener(client, cubapi.ParseRef("bot"), Options{Concurrency: 2, Logger: testLogger(t)}).
		Handle("test.echo", func(ctx context.Context, run *Run) (json.RawMessage, error) {
			assert.NotNil(t, run.Client, "the handler gets the listener's client")
			return json.RawMessage(fmt.Sprintf(`{"echo":%q,"n":%d}`, run.String("message"), run.Int("n"))), nil
		}).
		Handle("test.fail", func(ctx context.Context, run *Run) (json.RawMessage, error) {
			return nil, fmt.Errorf("could not")
		}).
		Handle("test.reject", func(ctx context.Context, run *Run) (json.RawMessage, error) {
			return nil, Reject("not for me: %s", run.String("why"))
		}).
		Handle("test.panic", func(ctx context.Context, run *Run) (json.RawMessage, error) {
			panic("boom")
		}).
		OnEvent(func(ctx context.Context, event *goclientnew.EventLog) {
			eventsMu.Lock()
			defer eventsMu.Unlock()
			events = append(events, event.EventType)
		})

	echo := runMessage("test.echo", map[string]any{"message": "hi", "n": 3})
	fail := runMessage("test.fail", nil)
	reject := runMessage("test.reject", map[string]any{"why": "policy"})
	panics := runMessage("test.panic", nil)
	fake.enqueue(eventMessage("release.published"), echo, fail)
	fake.enqueue(reject, panics, eventMessage("unit.created"))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- listener.Run(ctx) }()
	fake.waitResults(4)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)

	fake.mu.Lock()
	defer fake.mu.Unlock()
	assert.Equal(t, string(StatusCompleted), fake.results[echo.CommandRun.CommandRunID].Status)
	assert.Equal(t, map[string]any{"echo": "hi", "n": float64(3)}, fake.results[echo.CommandRun.CommandRunID].Result)
	assert.Equal(t, string(StatusFailed), fake.results[fail.CommandRun.CommandRunID].Status)
	assert.Equal(t, "could not", fake.results[fail.CommandRun.CommandRunID].ErrorMessage)
	assert.Equal(t, string(StatusRejected), fake.results[reject.CommandRun.CommandRunID].Status)
	assert.Equal(t, "not for me: policy", fake.results[reject.CommandRun.CommandRunID].ErrorMessage)
	assert.Equal(t, string(StatusFailed), fake.results[panics.CommandRun.CommandRunID].Status, "a panic is a failure, not a crash")
	assert.Contains(t, fake.results[panics.CommandRun.CommandRunID].ErrorMessage, "boom")
	assert.Equal(t, []string{"release.published", "unit.created"}, events, "events in delivery order")
	require.NotEmpty(t, fake.polls)
	assert.Equal(t, 2, fake.polls[0].MaxRuns, "the first poll asks for every free slot")
	assert.Equal(t, []uuid.UUID{fake.token}, fake.released, "the lease is released on shutdown")

	status := listener.Status()
	assert.Equal(t, "bot", status.Channel)
	assert.False(t, status.LeaseHeld)
	assert.Equal(t, int64(4), status.Runs)
	assert.Equal(t, int64(2), status.Events)
	assert.Zero(t, status.InFlight)
}

// A serialising listener polls for no runs while one is in flight; when it finishes, the next
// run is delivered at once rather than after that poll's full hold.
func TestListenerTakesTheNextRunWhenASlotFrees(t *testing.T) {
	fake, server := newFakeChannel(t, "test.echo")
	listener := NewListener(newTestClient(t, server), cubapi.ParseRef("bot"), Options{Concurrency: 1, Logger: testLogger(t)}).
		Handle("test.echo", func(ctx context.Context, run *Run) (json.RawMessage, error) {
			time.Sleep(200 * time.Millisecond)
			return json.RawMessage(`{}`), nil
		})
	first := runMessage("test.echo", nil)
	second := runMessage("test.echo", nil)
	fake.enqueue(first, second)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- listener.Run(ctx) }()
	started := time.Now()
	fake.waitResults(2)
	elapsed := time.Since(started)
	cancel()
	<-done
	assert.Less(t, elapsed, 5*time.Second, "the second run followed within the saturated poll's short hold, not the full one")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	assert.Equal(t, string(StatusCompleted), fake.results[second.CommandRun.CommandRunID].Status)
}

func TestListenerRefusesAChannelItDoesNotHandle(t *testing.T) {
	fake, server := newFakeChannel(t, "test.echo", "test.other")
	listener := NewListener(newTestClient(t, server), cubapi.ParseRef("bot"), Options{Logger: testLogger(t)}).
		Handle("test.echo", func(ctx context.Context, run *Run) (json.RawMessage, error) { return nil, nil })
	err := listener.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test.other")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	assert.Len(t, fake.released, 1, "the lease taken for the check is given back")
}

func TestListenerWaitsOutAHeldLeaseAndRidesThroughFailures(t *testing.T) {
	fake, server := newFakeChannel(t, "test.echo")
	fake.heldNext = 1
	fake.holdNext = 2
	listener := NewListener(newTestClient(t, server), cubapi.ParseRef("bot"), Options{Logger: testLogger(t)}).
		Handle("test.echo", func(ctx context.Context, run *Run) (json.RawMessage, error) { return json.RawMessage(`{}`), nil })
	run := runMessage("test.echo", nil)
	fake.enqueue(run)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- listener.Run(ctx) }()
	fake.waitResults(1)
	cancel()
	<-done
	fake.mu.Lock()
	defer fake.mu.Unlock()
	assert.Equal(t, string(StatusCompleted), fake.results[run.CommandRun.CommandRunID].Status)
	assert.GreaterOrEqual(t, len(fake.polls), 3, "the two failed polls were retried")
}

func TestListenerReacquiresADeadLease(t *testing.T) {
	fake, server := newFakeChannel(t, "test.echo")
	listener := NewListener(newTestClient(t, server), cubapi.ParseRef("bot"), Options{Logger: testLogger(t)}).
		Handle("test.echo", func(ctx context.Context, run *Run) (json.RawMessage, error) { return json.RawMessage(`{}`), nil })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- listener.Run(ctx) }()

	require.Eventually(t, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return len(fake.polls) > 0
	}, 5*time.Second, 20*time.Millisecond)
	first := func() uuid.UUID {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		// The server loses the lease: the next poll is a dead claim, and the listener claims again.
		token := fake.token
		fake.token = uuid.New()
		return token
	}()
	run := runMessage("test.echo", nil)
	require.Eventually(t, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if fake.token != first && len(fake.polls) > 0 && fake.polls[len(fake.polls)-1].ClaimToken == fake.token {
			fake.queue = append(fake.queue, []goclientnew.ChannelMessage{run})
			return true
		}
		return false
	}, 10*time.Second, 20*time.Millisecond, "the listener polls under the new lease")
	fake.waitResults(1)
	cancel()
	<-done
	fake.mu.Lock()
	defer fake.mu.Unlock()
	assert.Equal(t, string(StatusCompleted), fake.results[run.CommandRun.CommandRunID].Status)
}

func TestClientRenewsTheSessionOnce(t *testing.T) {
	// Every request under the first session is refused; the renewal is one authentication for
	// all of them.
	var mu sync.Mutex
	auths, refused := 0, 0
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/worker", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths++
		token := fmt.Sprintf("session-%d", auths)
		mu.Unlock()
		writeJSON(w, cubapi.AuthSession{AccessToken: token, AuthType: cubapi.AuthTypeJWT})
	})
	mux.HandleFunc("/api/me", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.Header.Get("Authorization"), "session-1") {
			mu.Lock()
			refused++
			mu.Unlock()
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, goclientnew.OrganizationMember{})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client, err := cubapi.NewClient(cubapi.ClientOptions{ServerURL: server.URL, Authenticator: func(ctx context.Context) (*cubapi.AuthSession, error) {
		res, err := http.Post(server.URL+"/auth/worker", "application/json", strings.NewReader("{}"))
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		var session cubapi.AuthSession
		return &session, json.NewDecoder(res.Body).Decode(&session)
	}})
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.VerifyAuth(context.Background())
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 2, auths, "one authentication to start, one renewal for every request the first session refused")
	assert.Equal(t, 8, refused)
}

// fakeInvoker is an executor that records the request and answers with the configuration it
// would have produced.
type fakeInvoker struct {
	mu       sync.Mutex
	requests []funcapi.FunctionInvocationRequest
}

func (f *fakeInvoker) Invoke(_ context.Context, request *funcapi.FunctionInvocationRequest) (*funcapi.FunctionInvocationResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, *request)
	return &funcapi.FunctionInvocationResponse{Success: true, FunctionInvocationSuccessResponse: funcapi.FunctionInvocationSuccessResponse{ConfigData: request.ConfigData + "# done\n"}}, nil
}

// The functions protocol rides on one command: a run of it carries the whole invocation
// request, the executor gets exactly that, and its response is the run's result. A run
// without the request is Rejected rather than executed.
func TestListenerServesFunctions(t *testing.T) {
	fake, server := newFakeChannel(t, FunctionInvokeCommand)
	client := newTestClient(t, server)
	invoker := &fakeInvoker{}
	listener := NewListener(client, cubapi.ParseRef("bot"), Options{Concurrency: 1, Logger: testLogger(t)}).HandleFunctions(invoker)

	request := funcapi.FunctionInvocationRequest{ConfigData: "replicas: 1\n", FunctionInvocations: funcapi.FunctionInvocationList{{FunctionName: "set-replicas"}, {FunctionName: "set-image"}}}
	batch := runMessage(FunctionInvokeCommand, map[string]any{FunctionRequestParam: request})
	empty := runMessage(FunctionInvokeCommand, map[string]any{})
	fake.enqueue(batch, empty)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- listener.Run(ctx) }()
	fake.waitResults(2)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)

	fake.mu.Lock()
	defer fake.mu.Unlock()
	invoker.mu.Lock()
	defer invoker.mu.Unlock()
	require.Len(t, invoker.requests, 1, "the executor got the batch, and nothing for the empty run")
	assert.Equal(t, "replicas: 1\n", invoker.requests[0].ConfigData)
	assert.Len(t, invoker.requests[0].FunctionInvocations, 2, "the whole list, in one request")
	assert.Equal(t, string(StatusCompleted), fake.results[batch.CommandRun.CommandRunID].Status)
	assert.Equal(t, "replicas: 1\n# done\n", fake.results[batch.CommandRun.CommandRunID].Result["ConfigData"])
	assert.Equal(t, string(StatusRejected), fake.results[empty.CommandRun.CommandRunID].Status)
	assert.Contains(t, fake.results[empty.CommandRun.CommandRunID].ErrorMessage, "carries no request")
}
