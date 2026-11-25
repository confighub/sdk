// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"testing"
	"time"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// attempt is one publish: nil error for a published Release, otherwise a refusal
// that is or is not waiting on Triggers.
type attempt struct {
	refused          bool
	awaitingTriggers bool
}

// A publish refused while the refusal waits on Triggers is tried again; any
// other refusal is returned at once.
func TestClusterPublishOnceTriggersSettle(t *testing.T) {
	refused := errors.New("outstanding ValidationErrors")
	for _, tc := range []struct {
		name      string
		attempts  []attempt
		wantErr   bool
		wantCalls int
	}{
		{"published", []attempt{{}}, false, 1},
		{"published once the Triggers settle", []attempt{{true, true}, {true, true}, {}}, false, 3},
		{"refused by a gate that is a verdict", []attempt{{true, false}}, true, 1},
		{"refused by a gate once the Triggers settle", []attempt{{true, true}, {true, false}}, true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			publish := func() (bool, error) {
				a := tc.attempts[calls]
				calls++
				if !a.refused {
					return false, nil
				}
				return a.awaitingTriggers, refused
			}
			err := clusterPublishOnceTriggersSettle(publish, time.Minute, time.Millisecond)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want an error: %v", err, tc.wantErr)
			}
			if calls != tc.wantCalls {
				t.Fatalf("publish calls = %d, want %d", calls, tc.wantCalls)
			}
		})
	}

	// A Unit that stays gated ends the retries at the timeout, with the refusal.
	calls := 0
	err := clusterPublishOnceTriggersSettle(
		func() (bool, error) { calls++; return true, refused },
		20*time.Millisecond, 5*time.Millisecond)
	if !errors.Is(err, refused) || calls < 2 {
		t.Fatalf("err = %v after %d calls, want the refusal after retries", err, calls)
	}
}

// The refusal waits on Triggers when its Validation Errors item lists
// awaiting/triggers, alone or with gates the Triggers may yet clear.
func TestRefusalAwaitsTriggers(t *testing.T) {
	refusal := func(items ...goclientnew.ErrorItem) *goclientnew.ResponseError {
		return &goclientnew.ResponseError{
			Message:       "outstanding ValidationErrors",
			ErrorMetadata: &goclientnew.ErrorMetadata{EntityType: "Unit", Items: items},
		}
	}
	gates := func(list string) goclientnew.ErrorItem {
		return goclientnew.ErrorItem{Item: "Validation Errors", Description: list}
	}
	for _, tc := range []struct {
		name    string
		refusal *goclientnew.ResponseError
		want    bool
	}{
		{"no error", nil, false},
		{"no items", &goclientnew.ResponseError{Message: "outstanding ValidationErrors"}, false},
		{"awaiting alone", refusal(gates("awaiting/triggers")), true},
		{"awaiting with another gate", refusal(gates("awaiting/triggers,vet-cel")), true},
		{"another gate alone", refusal(gates("vet-cel")), false},
		{"a gate whose name only contains it", refusal(gates("x-awaiting/triggers")), false},
		{"another item", refusal(goclientnew.ErrorItem{Item: "Unit", Description: "awaiting/triggers"}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := refusalAwaitsTriggers(tc.refusal); got != tc.want {
				t.Fatalf("refusalAwaitsTriggers = %v, want %v", got, tc.want)
			}
		})
	}
}
