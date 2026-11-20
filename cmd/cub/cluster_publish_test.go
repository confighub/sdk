// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

// A publish refused while a Unit of the Space awaits its Triggers is tried again;
// a refusal with nothing awaiting, or one that is not a 422, is returned at once.
func TestClusterPublishOnceTriggersSettle(t *testing.T) {
	refused := errors.New("outstanding ValidationErrors")
	for _, tc := range []struct {
		name      string
		statuses  []int
		awaiting  []bool
		wantErr   bool
		wantCalls int
	}{
		{"published", []int{http.StatusOK}, nil, false, 1},
		{"published once the Triggers settle", []int{422, 422, http.StatusOK}, []bool{true, true}, false, 3},
		{"refused with nothing awaiting", []int{422}, []bool{false}, true, 1},
		{"another failure", []int{http.StatusConflict}, nil, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, checks := 0, 0
			publish := func() (int, error) {
				status := tc.statuses[calls]
				calls++
				if status == http.StatusOK {
					return status, nil
				}
				return status, refused
			}
			awaiting := func() (bool, error) {
				pending := tc.awaiting[checks]
				checks++
				return pending, nil
			}
			err := clusterPublishOnceTriggersSettle(publish, awaiting, time.Minute, time.Millisecond)
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
		func() (int, error) { calls++; return 422, refused },
		func() (bool, error) { return true, nil },
		20*time.Millisecond, 5*time.Millisecond)
	if !errors.Is(err, refused) || calls < 2 {
		t.Fatalf("err = %v after %d calls, want the refusal after retries", err, calls)
	}
}
