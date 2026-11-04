// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
)

// testUndo is an undo step that runs f and succeeds.
func testUndo(what string, f func()) clusterUndo {
	return clusterUndo{what: what, fix: []string{"remove " + what}, run: func() error { f(); return nil }}
}

func TestClusterRollbackInterruptCancelsAndRollsBack(t *testing.T) {
	parent := ctx
	t.Cleanup(func() { ctx = parent })

	var out bytes.Buffer
	sigs := make(chan os.Signal)
	stopped := false
	r := clusterBeginRollbackOn(&out, sigs, func() { stopped = true })

	var order []string
	var rollbackCtxErr error
	r.add(testUndo("first", func() { order = append(order, "first") }))
	r.add(testUndo("second", func() {
		order = append(order, "second")
		rollbackCtxErr = ctx.Err()
	}))

	stepCtx := ctx
	sigs <- os.Interrupt
	<-stepCtx.Done()
	err := stepCtx.Err()
	r.finish(false, &err)

	if got := strings.Join(order, ","); got != "second,first" {
		t.Errorf("rollback order = %s, want second,first", got)
	}
	if rollbackCtxErr != nil {
		t.Errorf("rollback ran on a cancelled context: %v", rollbackCtxErr)
	}
	if ctx != parent {
		t.Errorf("ctx not restored to the command's original context")
	}
	if !stopped {
		t.Errorf("signal handling not stopped")
	}
	if err == nil || !strings.Contains(err.Error(), "interrupted") || !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want an interrupted error wrapping context.Canceled", err)
	}
	if !strings.Contains(out.String(), "Rolling back what this command created") {
		t.Errorf("output does not say it is rolling back: %q", out.String())
	}
	if strings.Contains(out.String(), "Left in place") {
		t.Errorf("output lists leftovers after a rollback that removed everything: %q", out.String())
	}
}

func TestClusterRollbackInterruptWithNothingToRollBack(t *testing.T) {
	parent := ctx
	t.Cleanup(func() { ctx = parent })

	var out bytes.Buffer
	sigs := make(chan os.Signal)
	r := clusterBeginRollbackOn(&out, sigs, func() {})

	stepCtx := ctx
	sigs <- os.Interrupt
	<-stepCtx.Done()
	err := stepCtx.Err()
	r.finish(false, &err)

	if strings.Contains(strings.ToLower(out.String()), "rolling back") {
		t.Errorf("output says it is rolling back with nothing to roll back: %q", out.String())
	}
	if err == nil || !strings.Contains(err.Error(), "interrupted") || strings.Contains(err.Error(), "rolled back") {
		t.Errorf("err = %v, want an interrupted error that does not claim a rollback", err)
	}
}

func TestClusterRollbackInterruptDuringFailureRollback(t *testing.T) {
	parent := ctx
	origExit := clusterExit
	t.Cleanup(func() { ctx, clusterExit = parent, origExit })

	exited := make(chan int, 1)
	clusterExit = func(code int) { exited <- code }

	var out bytes.Buffer
	sigs := make(chan os.Signal)
	r := clusterBeginRollbackOn(&out, sigs, func() {})
	var rollbackCtxErr error
	r.add(testUndo("thing", func() {
		sigs <- os.Interrupt
		rollbackCtxErr = ctx.Err()
		// The watcher takes the second signal only once it has handled the
		// first, and the fake exit lets the test wait for both.
		sigs <- os.Interrupt
		<-exited
	}))

	err := errors.New("step failed")
	r.finish(false, &err)

	got := out.String()
	if !strings.Contains(got, "the rollback is already running") {
		t.Errorf("output does not say the rollback was already running: %q", got)
	}
	if strings.Contains(got, "Rolling back what this command created") || strings.Contains(got, "stopping the current step") {
		t.Errorf("output suggests the interrupt caused the rollback: %q", got)
	}
	if rollbackCtxErr != nil {
		t.Errorf("rollback ran on a cancelled context: %v", rollbackCtxErr)
	}
	if err == nil || err.Error() != "step failed" {
		t.Errorf("err = %v, want the command's own error unchanged", err)
	}
}

func TestClusterRollbackCommitRunsNothing(t *testing.T) {
	parent := ctx
	t.Cleanup(func() { ctx = parent })

	r := clusterBeginRollbackOn(&bytes.Buffer{}, make(chan os.Signal), func() {})
	ran := false
	r.add(testUndo("thing", func() { ran = true }))
	var err error
	r.finish(true, &err)

	if ran {
		t.Errorf("a committed command rolled back")
	}
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if ctx != parent {
		t.Errorf("ctx not restored to the command's original context")
	}
}

func TestClusterRollbackSecondInterruptListsWhatIsLeft(t *testing.T) {
	parent := ctx
	origExit := clusterExit
	t.Cleanup(func() { ctx, clusterExit = parent, origExit })

	exited := make(chan int, 1)
	clusterExit = func(code int) { exited <- code }

	var out bytes.Buffer
	sigs := make(chan os.Signal)
	r := clusterBeginRollbackOn(&out, sigs, func() {})
	r.add(testUndo("kubeconfig", func() {}))
	r.add(testUndo("cluster space", func() {}))
	var code int
	r.add(testUndo("apps space", func() {
		sigs <- syscall.SIGTERM
		code = <-exited
	}))
	r.add(testUndo("argobot space", func() {}))

	stepCtx := ctx
	sigs <- os.Interrupt
	<-stepCtx.Done()
	err := stepCtx.Err()
	r.finish(false, &err)

	if code != 128+int(syscall.SIGTERM) {
		t.Errorf("exit code = %d, want %d", code, 128+int(syscall.SIGTERM))
	}
	got := out.String()
	_, list, _ := strings.Cut(got, "Interrupted again")
	// The step in progress and the two after it are left; the one before it
	// finished.
	for _, want := range []string{"- apps space\n      remove apps space", "- cluster space", "- kubeconfig"} {
		if !strings.Contains(list, want) {
			t.Errorf("list of what is left lacks %q: %q", want, list)
		}
	}
	if strings.Contains(list, "argobot space") {
		t.Errorf("list of what is left names a step that finished: %q", list)
	}
}

func TestClusterRollbackReportsFailedSteps(t *testing.T) {
	parent := ctx
	t.Cleanup(func() { ctx = parent })

	var out bytes.Buffer
	r := clusterBeginRollbackOn(&out, make(chan os.Signal), func() {})
	ranAfterFailure := false
	r.add(testUndo("first", func() { ranAfterFailure = true }))
	r.add(clusterUndo{
		what: "Secret argocd/creds in the cluster",
		fix:  []string{"kubectl -n argocd delete secret creds"},
		run:  func() error { return errors.New("connection refused") },
	})
	r.add(testUndo("last", func() {}))

	err := errors.New("step failed")
	r.finish(false, &err)

	got := out.String()
	if !strings.Contains(got, "failed: connection refused") {
		t.Errorf("output does not report the failure: %q", got)
	}
	if !strings.Contains(got, "- Secret argocd/creds in the cluster\n      kubectl -n argocd delete secret creds\n") {
		t.Errorf("output does not list the Secret and how to remove it: %q", got)
	}
	if !ranAfterFailure {
		t.Errorf("the steps after a failed one did not run")
	}
	if strings.Contains(got, "- first") || strings.Contains(got, "- last") {
		t.Errorf("output lists a step that succeeded: %q", got)
	}
	if err == nil || !strings.HasPrefix(err.Error(), "step failed") || !strings.Contains(err.Error(), "left 1 item(s) in place") {
		t.Errorf("err = %v, want the command's error noting what the rollback left", err)
	}
}

func TestClusterRollbackPartialUndoListsOnlyWhatIsLeft(t *testing.T) {
	parent := ctx
	t.Cleanup(func() { ctx = parent })

	var out bytes.Buffer
	r := clusterBeginRollbackOn(&out, make(chan os.Signal), func() {})
	r.add(clusterUndo{
		what: "argobot's Unit",
		fix:  []string{"cub unit delete apps/argobot", "cub release publish apps"},
		run: func() error {
			return &clusterUndoPartial{fix: []string{"cub release publish apps"}, err: errors.New("publish failed")}
		},
	})

	err := errors.New("step failed")
	r.finish(false, &err)

	got := out.String()
	if !strings.Contains(got, "- argobot's Unit\n      cub release publish apps\n") {
		t.Errorf("list does not give the partial step's remaining fix: %q", got)
	}
	if strings.Contains(got, "cub unit delete") {
		t.Errorf("list repeats the half of the step that was undone: %q", got)
	}
}

func TestClusterLastLine(t *testing.T) {
	for in, want := range map[string]string{
		"":            "",
		"only line\n": "only line",
		"E1104 noise\nE1104 noise\nThe connection was refused\n\n": "The connection was refused",
	} {
		if got := clusterLastLine(in); got != want {
			t.Errorf("clusterLastLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClusterRollbackKeepsKubeconfigForAFailedKubectlStep(t *testing.T) {
	for _, tc := range []struct {
		name           string
		usesKubeconfig bool
		wantKept       bool
	}{
		{"kubectl step failed", true, true},
		{"ConfigHub step failed", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := ctx
			t.Cleanup(func() { ctx = parent })

			var out bytes.Buffer
			r := clusterBeginRollbackOn(&out, make(chan os.Signal), func() {})
			removed := false
			r.add(clusterUndo{
				what:              "the local files",
				fix:               []string{"rm -f kubeconfig"},
				removesKubeconfig: true,
				run:               func() error { removed = true; return nil },
			})
			r.add(clusterUndo{
				what:           "the thing",
				fix:            []string{"remove the thing"},
				usesKubeconfig: tc.usesKubeconfig,
				run:            func() error { return errors.New("boom") },
			})

			err := errors.New("step failed")
			r.finish(false, &err)

			if removed == tc.wantKept {
				t.Errorf("local files removed = %v, want %v", removed, !tc.wantKept)
			}
			listed := strings.Contains(out.String(), "- the local files\n      rm -f kubeconfig\n")
			if listed != tc.wantKept {
				t.Errorf("local files listed as left = %v, want %v: %q", listed, tc.wantKept, out.String())
			}
		})
	}
}
