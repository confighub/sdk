// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/google/uuid"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// The listener's lifecycle: claim the Channel, verify it declares what the handlers handle,
// poll under the lease, dispatch runs and events, report outcomes, and on shutdown drain,
// report and release in that order. Its loop is the worker tier's long-poll loop, carried over
// with its transient-failure policy; what it does on the wire is the generated client.

const (
	// pollTimeoutSeconds is how long each poll asks the server to hold. The server caps it at 60.
	pollTimeoutSeconds = 30
	// saturatedPollSeconds is the hold while every slot is taken: short, so that a slot freed
	// during it is asked for within seconds, and events still arrive. A poll in flight is never
	// cancelled, since its response may already carry a delivered run.
	saturatedPollSeconds = 2
	// pollRequestTimeout bounds one poll request: the server's hold plus room to answer.
	pollRequestTimeout = (pollTimeoutSeconds + 15) * time.Second
	// shortRequestTimeout bounds every other call: claim, release, report, reads.
	shortRequestTimeout = 30 * time.Second
	// claimBackoffInitial and claimBackoffMax bound the wait between claim attempts.
	claimBackoffInitial = 2 * time.Second
	claimBackoffMax     = 30 * time.Second
	// transientBackoff is the pause after a 5xx or a network failure on a poll.
	transientBackoff = 2 * time.Second
	// DefaultDrainTimeout is how long shutdown waits for in-flight handlers.
	DefaultDrainTimeout = 30 * time.Second
	// DefaultConcurrency is how many runs a listener executes at once.
	DefaultConcurrency = 4
)

// Status is where a run is in its life, as the server reports it.
type Status string

const (
	StatusPending   Status = "Pending"
	StatusDelivered Status = "Delivered"
	StatusCompleted Status = "Completed"
	StatusFailed    Status = "Failed"
	StatusRejected  Status = "Rejected"
	StatusCanceled  Status = "Canceled"
	StatusExpired   Status = "Expired"
	StatusTimedOut  Status = "TimedOut"
)

// IsTerminal reports whether a run in this status can no longer change.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusRejected, StatusCanceled, StatusExpired, StatusTimedOut:
		return true
	case StatusPending, StatusDelivered:
		return false
	}
	return false
}

// The kinds of message a poll delivers.
const (
	KindCommandRun = "CommandRun"
	KindEventLog   = "EventLog"
)

// Ref names the Channel to listen on: a slug, a space/slug, or a UUID, as cubapi resolves it.
type Ref = cubapi.Ref

// Options tune a Listener.
type Options struct {
	// Concurrency is how many runs execute at once; each poll asks for exactly the free slots.
	// One is a serialising listener. Zero means DefaultConcurrency.
	Concurrency int
	// DrainTimeout is how long shutdown waits for in-flight handlers before cancelling them.
	// Zero means DefaultDrainTimeout.
	DrainTimeout time.Duration
	// Logger receives the listener's log lines. Nil means the standard logger.
	Logger *log.Logger
}

// Listener serves one Channel. Build it with NewListener, register handlers, then Run it.
type Listener struct {
	client   *cubapi.Client
	ref      Ref
	opts     Options
	handlers map[string]Handler
	onEvent  EventHandler
	log      *log.Logger

	channel *goclientnew.Channel

	mu         sync.Mutex
	claimToken uuid.UUID
	inFlight   map[uuid.UUID]*Run
	wg         sync.WaitGroup
	lastPoll   atomic.Int64
	lastWork   atomic.Int64
	polls      atomic.Int64
	runs       atomic.Int64
	events     atomic.Int64
}

// NewListener builds a listener for the Channel ref names, over the client. The client carries
// the session, and should be built with an Authenticator so the session renews for as long
// as the listener runs.
func NewListener(client *cubapi.Client, ref Ref, opts ...Options) *Listener {
	l := &Listener{
		client:   client,
		ref:      ref,
		handlers: map[string]Handler{},
		inFlight: map[uuid.UUID]*Run{},
	}
	if len(opts) > 0 {
		l.opts = opts[0]
	}
	if l.opts.Concurrency <= 0 {
		l.opts.Concurrency = DefaultConcurrency
	}
	if l.opts.DrainTimeout <= 0 {
		l.opts.DrainTimeout = DefaultDrainTimeout
	}
	l.log = l.opts.Logger
	if l.log == nil {
		l.log = log.Default()
	}
	return l
}

// Handle registers the handler for a command the Channel declares.
func (l *Listener) Handle(command string, handler Handler) *Listener {
	l.handlers[command] = handler
	return l
}

// OnEvent registers the handler for the events the Channel subscribes to.
func (l *Listener) OnEvent(handler EventHandler) *Listener {
	l.onEvent = handler
	return l
}

// Commands lists the commands the listener handles, sorted.
func (l *Listener) Commands() []string {
	names := make([]string, 0, len(l.handlers))
	for name := range l.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Channel is the Channel the listener resolved, once Run has.
func (l *Listener) Channel() *goclientnew.Channel {
	return l.channel
}

// Run drives the listener until ctx is done, or until something it cannot recover from. It
// resolves the Channel, then loops: claim with backoff, verify the declarations once, poll
// until the lease dies, and on shutdown drain, report and release.
func (l *Listener) Run(ctx context.Context) error {
	if err := l.resolve(ctx); err != nil {
		return err
	}
	verified := false
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lease, err := l.acquireWithBackoff(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return ctx.Err()
			}
			return fmt.Errorf("channel %s: acquire lease: %w", l.ref.Name, err)
		}
		l.setClaim(lease.ClaimToken)
		l.log.Printf("[INFO] channel %s: lease acquired token=%s ttl=%ds", l.channel.Slug, lease.ClaimToken, lease.TTLSeconds)

		if !verified {
			if err := l.verify(ctx); err != nil {
				l.releaseOnExit(lease.ClaimToken)
				return err
			}
			verified = true
		}

		err = l.pollLoop(ctx, lease.ClaimToken)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			l.shutdown(lease.ClaimToken)
			return ctx.Err()
		}
		l.clearClaim()
		if err != nil {
			l.log.Printf("[WARN] channel %s: poll loop ended: %v; re-acquiring the lease", l.channel.Slug, err)
		}
	}
}

// resolve finds the Channel the ref names. A listener's identity typically holds Claim on the
// Channel and nothing on its Space, so a space/slug reference is not resolved through the
// Space: the Channels of that slug are listed across the organization, which shows the ones the
// identity may view, and the Space narrows them where it can be seen.
func (l *Listener) resolve(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, shortRequestTimeout)
	defer cancel()
	if l.ref.IsID() {
		resolved, err := cubapi.ResolveChannel(cctx, l.client, l.ref, cubapi.ResolveOpts{})
		if err != nil {
			return fmt.Errorf("resolve channel %s: %w", l.ref.Name, err)
		}
		l.channel = resolved.Channel
		return nil
	}
	candidates, err := cubapi.ListChannels(cctx, l.client, cubapi.NewWhere("").Slug(l.ref.Name), cubapi.ListOpts{Include: "SpaceID"})
	if err != nil {
		return fmt.Errorf("resolve channel %s: %w", l.ref.Name, err)
	}
	if l.ref.Space != "" && l.ref.Space != "*" {
		var inSpace []*goclientnew.ExtendedChannel
		for _, candidate := range candidates {
			if candidate.Channel == nil {
				continue
			}
			if candidate.Channel.SpaceID.String() == l.ref.Space || (candidate.Space != nil && candidate.Space.Slug == l.ref.Space) {
				inSpace = append(inSpace, candidate)
			}
		}
		switch {
		case len(inSpace) > 0:
			candidates = inSpace
		case len(candidates) == 1 && candidates[0].Space == nil:
			// The one channel of that slug, in a space this identity cannot see to confirm.
			l.log.Printf("[WARN] channel %s: space %q is not visible to this identity; taking the one channel of that slug", l.ref.Name, l.ref.Space)
		default:
			return fmt.Errorf("resolve channel %s: no channel of that slug in space %q is visible to this identity", l.ref.Name, l.ref.Space)
		}
	}
	switch len(candidates) {
	case 0:
		return fmt.Errorf("resolve channel %s: no such channel is visible to this identity; it needs Claim on the channel", l.ref.Name)
	case 1:
		l.channel = candidates[0].Channel
		return nil
	}
	return fmt.Errorf("resolve channel %s: %d channels have that slug; name the space as space/slug, or the channel by its ID", l.ref.Name, len(candidates))
}

// verify compares the Channel's declarations to the handlers: a declared command with no
// handler is fatal, since every run of it would be Rejected; a handler for an undeclared
// command is a warning, since nothing resolves to it.
func (l *Listener) verify(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, shortRequestTimeout)
	defer cancel()
	resolved, err := cubapi.ResolveChannel(cctx, l.client, cubapi.ParseRef(l.channel.ChannelID.String()), cubapi.ResolveOpts{})
	if err != nil {
		return fmt.Errorf("read channel %s: %w", l.channel.Slug, err)
	}
	l.channel = resolved.Channel
	declared := map[string]bool{}
	if l.channel.Declarations != nil {
		for name := range l.channel.Declarations.Commands {
			declared[name] = true
		}
	}
	var missing []string
	for name := range declared {
		if _, ok := l.handlers[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return fmt.Errorf("channel %s declares commands this listener does not handle: %s", l.channel.Slug, strings.Join(missing, ", "))
	}
	for _, name := range l.Commands() {
		if !declared[name] {
			l.log.Printf("[WARN] channel %s: handler for %q, which the channel does not declare; nothing will run it", l.channel.Slug, name)
		}
	}
	return nil
}

func (l *Listener) setClaim(token uuid.UUID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.claimToken = token
}

func (l *Listener) clearClaim() {
	l.setClaim(uuid.Nil)
}

// acquireWithBackoff retries the claim with exponential backoff and jitter. A held lease, a
// 5xx and a network failure all wait and retry; anything else is fatal, since it is
// something the listener cannot fix on its own.
func (l *Listener) acquireWithBackoff(ctx context.Context) (*goclientnew.ChannelLease, error) {
	delay := claimBackoffInitial
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		lease, status, err := l.acquire(ctx)
		if err == nil {
			return lease, nil
		}
		switch {
		case status == http.StatusConflict:
			l.log.Printf("[INFO] channel %s: lease held elsewhere; will retry", l.channel.Slug)
		case status >= 500 || status == 0:
			l.log.Printf("[WARN] channel %s: acquire failed (%v); will retry", l.channel.Slug, err)
		default:
			return nil, err
		}
		jitter := time.Duration(rand.Float64() * float64(delay) * 0.2)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay + jitter):
		}
		delay = min(delay*2, claimBackoffMax)
	}
}

func (l *Listener) acquire(ctx context.Context) (*goclientnew.ChannelLease, int, error) {
	cctx, cancel := context.WithTimeout(ctx, shortRequestTimeout)
	defer cancel()
	res, err := l.client.API.AcquireChannelLeaseWithResponse(cctx, l.channel.SpaceID, l.channel.ChannelID)
	if err != nil {
		return nil, 0, err
	}
	if res.JSON200 == nil {
		return nil, res.StatusCode(), cubapi.InterpretErrorGeneric(err, res)
	}
	return res.JSON200, http.StatusOK, nil
}

// release gives the lease back, so the next claimant does not wait for the TTL.
func (l *Listener) release(ctx context.Context, token uuid.UUID) error {
	cctx, cancel := context.WithTimeout(ctx, shortRequestTimeout)
	defer cancel()
	res, err := l.client.API.ReleaseChannelLeaseWithResponse(cctx, l.channel.SpaceID, l.channel.ChannelID, &goclientnew.ReleaseChannelLeaseParams{ClaimToken: token})
	if err != nil {
		return err
	}
	if res.StatusCode() != http.StatusNoContent {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return nil
}

func (l *Listener) releaseOnExit(token uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), shortRequestTimeout)
	defer cancel()
	if err := l.release(ctx, token); err != nil {
		l.log.Printf("[WARN] channel %s: release lease: %v", l.channel.Slug, err)
	} else {
		l.log.Printf("[INFO] channel %s: lease released", l.channel.Slug)
	}
	l.clearClaim()
}

// pollLoop polls under the lease until it dies, ctx ends, or the server answers with
// something the listener does not expect. A 204 polls again; a 200 dispatches; a 409 is a dead
// lease; a 5xx or a network failure backs off and polls again, logging the first failure of a
// streak and one line when it ends.
func (l *Listener) pollLoop(ctx context.Context, token uuid.UUID) error {
	var failures int
	resumed := func() {
		if failures > 0 {
			l.log.Printf("[INFO] channel %s: poll resumed after %d failed attempts", l.channel.Slug, failures)
			failures = 0
		}
	}
	failed := func(format string, args ...any) error {
		failures++
		if failures == 1 {
			l.log.Printf("[WARN] channel %s: "+format+"; will retry", append([]any{l.channel.Slug}, args...)...)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(transientBackoff):
			return nil
		}
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		free := l.freeSlots()
		hold := pollTimeoutSeconds
		if free == 0 {
			hold = saturatedPollSeconds
		}
		cctx, cancel := context.WithTimeout(ctx, pollRequestTimeout)
		res, err := l.client.API.PollChannelWithResponse(cctx, l.channel.SpaceID, l.channel.ChannelID, goclientnew.ChannelPollRequest{
			ClaimToken:     token,
			TimeoutSeconds: hold,
			MaxRuns:        free,
		})
		cancel()
		l.polls.Add(1)
		l.lastPoll.Store(time.Now().UnixNano())
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := failed("poll request error: %v", err); err != nil {
				return err
			}
			continue
		}
		switch {
		case res.StatusCode() == http.StatusNoContent:
			resumed()
		case res.StatusCode() == http.StatusOK:
			resumed()
			if res.JSON200 != nil {
				l.dispatch(ctx, token, *res.JSON200)
			}
		case res.StatusCode() == http.StatusConflict:
			return fmt.Errorf("lease dead: %s", strings.TrimSpace(string(res.Body)))
		case res.StatusCode() >= 500:
			if err := failed("poll status %d (%s)", res.StatusCode(), strings.TrimSpace(string(res.Body))); err != nil {
				return err
			}
		default:
			return fmt.Errorf("poll status %d: %s", res.StatusCode(), strings.TrimSpace(string(res.Body)))
		}
	}
}

func (l *Listener) freeSlots() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return max(0, l.opts.Concurrency-len(l.inFlight))
}

// dispatch hands events to the event handler in order, on this goroutine, and starts a
// goroutine per run.
func (l *Listener) dispatch(ctx context.Context, token uuid.UUID, messages []goclientnew.ChannelMessage) {
	l.lastWork.Store(time.Now().UnixNano())
	for i := range messages {
		message := &messages[i]
		switch message.Kind {
		case KindEventLog:
			if message.Event == nil {
				continue
			}
			l.events.Add(1)
			l.handleEvent(ctx, message.Event)
		case KindCommandRun:
			if message.CommandRun == nil {
				continue
			}
			l.runs.Add(1)
			l.start(ctx, token, message.CommandRun)
		default:
			l.log.Printf("[WARN] channel %s: message of unknown kind %q ignored", l.channel.Slug, message.Kind)
		}
	}
}

func (l *Listener) handleEvent(ctx context.Context, event *goclientnew.EventLog) {
	if l.onEvent == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			l.log.Printf("[ERROR] channel %s: panic handling event %s (%s): %v", l.channel.Slug, event.EventType, event.EventLogID, r)
		}
	}()
	l.onEvent(ctx, event)
}

// start executes one run on its own goroutine, under a context that ends at the run's
// delivered timeout or at shutdown, and reports the outcome.
func (l *Listener) start(ctx context.Context, token uuid.UUID, delivered *goclientnew.CommandRun) {
	run := newRun(l.client, delivered)
	handler, ok := l.handlers[run.Command]
	if !ok {
		// Verify refused a channel declaring what the listener does not handle, so this is a
		// declaration that changed since; the run is rejected with the reason.
		l.report(run, goclientnew.CommandRunResult{Status: string(StatusRejected), ErrorMessage: fmt.Sprintf("this listener does not handle %q", run.Command)})
		return
	}
	l.mu.Lock()
	l.inFlight[run.CommandRunID] = run
	l.mu.Unlock()
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		defer func() {
			l.mu.Lock()
			delete(l.inFlight, run.CommandRunID)
			l.mu.Unlock()
		}()
		deadline := run.TimeoutAt
		if deadline.IsZero() {
			deadline = time.Now().Add(30 * time.Second)
		}
		rctx, cancel := context.WithDeadline(ctx, deadline)
		defer cancel()
		result, err := l.execute(rctx, handler, run)
		if rctx.Err() != nil && err == nil && result == nil {
			// The handler gave up with its context; the server has already closed the run.
			return
		}
		l.report(run, outcome(result, err))
	}()
}

func (l *Listener) execute(ctx context.Context, handler Handler, run *Run) (result json.RawMessage, err error) {
	defer func() {
		if r := recover(); r != nil {
			l.log.Printf("[ERROR] channel %s: panic handling run %s (%s): %v", l.channel.Slug, run.CommandRunID, run.Command, r)
			err = fmt.Errorf("handler panicked: %v", r)
		}
	}()
	return handler(ctx, run)
}

// report sends a run's outcome with the retry policy the worker tier's results had: five
// minutes in all, exponential backoff capped at thirty seconds. A 4xx other than 409 is
// permanent; the server's own answer to a late result is 200 with the run as it is.
func (l *Listener) report(run *Run, result goclientnew.CommandRunResult) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	eb := &backoff.ExponentialBackOff{
		InitialInterval:     time.Second,
		RandomizationFactor: backoff.DefaultRandomizationFactor,
		Multiplier:          2.0,
		MaxInterval:         30 * time.Second,
	}
	attempt := 0
	_, err := backoff.Retry(ctx, func() (any, error) {
		attempt++
		cctx, cancel := context.WithTimeout(ctx, shortRequestTimeout)
		defer cancel()
		res, err := l.client.API.ReportCommandRunResultWithResponse(cctx, l.channel.SpaceID, l.channel.ChannelID, run.CommandRunID, result)
		if err != nil {
			return nil, err
		}
		if res.StatusCode() == http.StatusOK {
			if res.JSON200 != nil && res.JSON200.Status != result.Status {
				l.log.Printf("[INFO] channel %s: run %s was already %s when its result arrived", l.channel.Slug, run.CommandRunID, res.JSON200.Status)
			}
			return nil, nil
		}
		if res.StatusCode() >= 400 && res.StatusCode() < 500 && res.StatusCode() != http.StatusConflict {
			return nil, backoff.Permanent(cubapi.InterpretErrorGeneric(err, res))
		}
		return nil, fmt.Errorf("status %d", res.StatusCode())
	}, backoff.WithBackOff(eb), backoff.WithMaxTries(15))
	if err != nil {
		l.log.Printf("[ERROR] channel %s: report result of run %s failed after %d attempts: %v", l.channel.Slug, run.CommandRunID, attempt, err)
	}
}

// shutdown is the order that keeps a finished run from executing twice: stop polling, which
// the caller has by returning from the poll loop; wait for in-flight handlers up to the drain
// timeout; report what finished; then release the lease, which requeues whatever is still
// Delivered for the next claimant.
func (l *Listener) shutdown(token uuid.UUID) {
	done := make(chan struct{})
	go func() {
		l.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(l.opts.DrainTimeout):
		l.mu.Lock()
		n := len(l.inFlight)
		l.mu.Unlock()
		l.log.Printf("[WARN] channel %s: %d run(s) still in flight after %s; releasing the lease, they will be redelivered", l.channel.Slug, n, l.opts.DrainTimeout)
	}
	l.releaseOnExit(token)
}

// ListenerStatus is what a probe reads: whether the lease is held, when the listener last
// polled and last had work, and what is in flight.
type ListenerStatus struct {
	Channel     string
	LeaseHeld   bool
	LastPoll    time.Time
	LastWork    time.Time
	InFlight    int
	Polls       int64
	Runs        int64
	Events      int64
	Concurrency int
}

// Status reports the listener's state for a liveness or readiness probe.
func (l *Listener) Status() ListenerStatus {
	l.mu.Lock()
	defer l.mu.Unlock()
	status := ListenerStatus{
		LeaseHeld:   l.claimToken != uuid.Nil,
		InFlight:    len(l.inFlight),
		Polls:       l.polls.Load(),
		Runs:        l.runs.Load(),
		Events:      l.events.Load(),
		Concurrency: l.opts.Concurrency,
	}
	if l.channel != nil {
		status.Channel = l.channel.Slug
	}
	if t := l.lastPoll.Load(); t != 0 {
		status.LastPoll = time.Unix(0, t)
	}
	if t := l.lastWork.Load(); t != 0 {
		status.LastWork = time.Unix(0, t)
	}
	return status
}
