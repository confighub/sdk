// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// `cub worker key` is a naming shell over `cub user key --worker`. What can
// break is the wiring: a subcommand that forgets to fill in the worker would
// fall through to "name an identity with --user or a worker with --worker",
// and one that forgets to clear --user would resolve the wrong identity when a
// previous command in the same process had set it.
func TestWorkerKeySubcommandsTargetTheNamedWorker(t *testing.T) {
	userKeyWorker, userKeyUser = "", "stale-user"
	t.Cleanup(func() { userKeyWorker, userKeyUser = "", "" })

	selectWorkerKeyTarget("my-worker")

	if userKeyWorker != "my-worker" {
		t.Errorf("userKeyWorker = %q, want %q", userKeyWorker, "my-worker")
	}
	if userKeyUser != "" {
		t.Errorf("userKeyUser = %q, want it cleared", userKeyUser)
	}
}

// The positional arity is the difference between these and their `cub user key`
// counterparts, and getting it wrong turns a worker name into a kid or drops it
// entirely.
func TestWorkerKeyArgCounts(t *testing.T) {
	tests := []struct {
		cmd  *cobra.Command
		ok   []string
		bad  []string
		name string
	}{
		{workerKeyAddCmd, []string{"w"}, []string{}, "add"},
		{workerKeyListCmd, []string{"w"}, []string{"w", "extra"}, "list"},
		{workerKeyDeleteCmd, []string{"w", "kid"}, []string{"w"}, "delete"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cmd.Args(tc.cmd, tc.ok); err != nil {
				t.Errorf("rejected valid args %v: %v", tc.ok, err)
			}
			if err := tc.cmd.Args(tc.cmd, tc.bad); err == nil {
				t.Errorf("accepted invalid args %v", tc.bad)
			}
		})
	}
}

// `cub worker key` must hang off `cub worker`, not off the root: the whole
// point is that an operator finds it where workers are.
func TestWorkerKeyIsUnderWorker(t *testing.T) {
	for _, c := range workerCmd.Commands() {
		if c == workerKeyCmd {
			return
		}
	}
	t.Fatal("workerKeyCmd is not registered under workerCmd")
}
