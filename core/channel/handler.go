// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package channel is the listener side of a ConfigHub Channel: a process that claims a Channel,
// receives the runs of the commands it declares and the events it subscribes to, and reports
// what it did. Everything it does on the wire is a cubapi.Client call; what this package adds
// is the lifecycle.
package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Handler executes one run of a command. It returns the result to report, a JSON value in the
// shape the command's signature declares, or nil for none; or an error, which reports the run
// Failed, or Rejected when it is a Rejection. The context ends at the run's delivered timeout,
// or at shutdown; a handler that is still working then has its result dropped and the run
// redelivered later, so handlers are idempotent or refuse a re-delivery.
type Handler func(ctx context.Context, run *Run) (json.RawMessage, error)

// EventHandler reacts to one event the Channel subscribes to. Events are handled in delivery
// order on the poll goroutine, so a slow handler delays the next poll, which is the
// backpressure a bookmark that advances on delivery wants.
type EventHandler func(ctx context.Context, event *goclientnew.EventLog)

// Rejection is the error a Handler returns to report a run Rejected rather than Failed: the
// handler did not try, because the command or its params are not ones it accepts.
type Rejection struct {
	Reason string
}

func (r *Rejection) Error() string { return r.Reason }

// Reject builds the error that reports a run Rejected.
func Reject(format string, args ...any) error {
	return &Rejection{Reason: fmt.Sprintf(format, args...)}
}

// Run is one delivered run, with its params decoded for the handler.
type Run struct {
	*goclientnew.CommandRun
	// Client is the listener's client, so that what a handler does back against ConfigHub
	// goes over the same session.
	Client *cubapi.Client

	params map[string]json.RawMessage
}

func newRun(client *cubapi.Client, run *goclientnew.CommandRun) *Run {
	r := &Run{CommandRun: run, Client: client, params: map[string]json.RawMessage{}}
	for name, value := range run.Params {
		if raw, err := json.Marshal(value); err == nil {
			r.params[name] = raw
		}
	}
	return r
}

// Param decodes the named option into target. It reports whether the option was given; a
// value that does not decode is an error.
func (r *Run) Param(name string, target any) (bool, error) {
	raw, ok := r.params[name]
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return true, fmt.Errorf("option %q: %w", name, err)
	}
	return true, nil
}

// String returns the named string option, or "" when it was not given.
func (r *Run) String(name string) string {
	var value string
	_, _ = r.Param(name, &value)
	return value
}

// Int returns the named integer option, or 0 when it was not given.
func (r *Run) Int(name string) int64 {
	var value int64
	_, _ = r.Param(name, &value)
	return value
}

// Bool returns the named boolean option, or false when it was not given.
func (r *Run) Bool(name string) bool {
	var value bool
	_, _ = r.Param(name, &value)
	return value
}

// Has reports whether the named option was given.
func (r *Run) Has(name string) bool {
	_, ok := r.params[name]
	return ok
}

// outcome is what a handler's return becomes on the wire.
func outcome(result json.RawMessage, err error) goclientnew.CommandRunResult {
	if err == nil {
		out := goclientnew.CommandRunResult{Status: string(StatusCompleted)}
		if len(result) > 0 {
			var value map[string]any
			if json.Unmarshal(result, &value) == nil {
				out.Result = value
			}
		}
		return out
	}
	var rejection *Rejection
	if errors.As(err, &rejection) {
		return goclientnew.CommandRunResult{Status: string(StatusRejected), ErrorMessage: rejection.Reason}
	}
	return goclientnew.CommandRunResult{Status: string(StatusFailed), ErrorMessage: err.Error()}
}
