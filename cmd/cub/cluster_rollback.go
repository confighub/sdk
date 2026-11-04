// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/confighub/sdk/core/cubapi"
)

// clusterExit is os.Exit, replaced in tests.
var clusterExit = os.Exit

// clusterUndo is one undo step of a clusterRollback. A step that fails leaves
// what it was removing in place, so it says what that is and how to remove it
// by hand.
type clusterUndo struct {
	// what names what the step removes, as the list of what is left says it.
	what string
	// fix is the equivalent of run as commands a user types, in order: what the
	// list of what is left prints for each item, so it can be removed by hand.
	// It is only printed. run is what undoes the step, in-process.
	fix []string
	// usesKubeconfig is set when fix reads the cluster's local kubeconfig.
	usesKubeconfig bool
	// removesKubeconfig marks the step that removes the cluster's local files,
	// the kubeconfig among them. It is skipped, and listed as left, when a step
	// that failed before it needs the kubeconfig for its fix.
	removesKubeconfig bool
	// run does the undo, through the same in-process API and kubectl helpers
	// the command created things with. It prints its own "Rolling back:" line,
	// rather than the rollback printing one for it, because some steps find
	// nothing to remove when what they undo was never created, and say nothing
	// then. It returns nil when what it removes is gone, already gone included,
	// and an error only when that is still in place, which puts the step on the
	// list of what is left. A step that undoes its change in more than one
	// call, and fails after part of it is undone, returns a
	// *clusterUndoPartial naming what is left to do.
	run func() error
}

// clusterUndoPartial is the error of an undo step that failed after undoing
// part of its change. fix lists the commands for the part still in place, and
// the list of what is left prints them in place of the step's own, which
// would repeat what is already undone.
type clusterUndoPartial struct {
	fix []string
	err error
}

func (e *clusterUndoPartial) Error() string { return e.err.Error() }
func (e *clusterUndoPartial) Unwrap() error { return e.err }

// clusterRollback collects the undo steps of a `cub cluster` command that
// creates things in ConfigHub, the cluster and on disk, and runs them, last
// first, unless the command commits. Whatever the rollback could not remove
// is listed at the end with the commands that remove it.
//
// An attach runs for minutes, through a controller install and an argobot
// install, so while it runs SIGINT and SIGTERM cancel the in-flight step,
// through the package ctx every step uses, and the command returns and rolls
// back instead of dying with everything left behind. A second signal exits at
// once, listing what is left.
type clusterRollback struct {
	out     io.Writer
	parent  context.Context
	cancel  context.CancelFunc
	stopSig func()
	done    chan struct{}

	mu          sync.Mutex
	steps       []clusterUndo
	pending     int           // steps not yet finished, once the rollback has started
	left        []clusterUndo // steps that failed or were skipped, in the order they ran
	rollingBack bool
	interrupted os.Signal
}

// clusterBeginRollback starts collecting undo steps, and catches SIGINT and
// SIGTERM until finish. Every caller defers finish.
func clusterBeginRollback(out io.Writer) *clusterRollback {
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	return clusterBeginRollbackOn(out, sigs, func() { signal.Stop(sigs) })
}

func clusterBeginRollbackOn(out io.Writer, sigs <-chan os.Signal, stopSig func()) *clusterRollback {
	r := &clusterRollback{out: out, parent: ctx, stopSig: stopSig, done: make(chan struct{})}
	ctx, r.cancel = context.WithCancel(r.parent)
	go r.watch(sigs)
	return r
}

func (r *clusterRollback) watch(sigs <-chan os.Signal) {
	for {
		select {
		case <-r.done:
			return
		case sig := <-sigs:
			r.mu.Lock()
			first := r.interrupted == nil
			if first {
				r.interrupted = sig
			}
			rollingBack, registered := r.rollingBack, len(r.steps)
			// What is still in place: what failed so far, then the steps not
			// finished, in the order the rollback would have run them.
			left := append([]clusterUndo(nil), r.left...)
			unfinished := r.pending
			if !rollingBack {
				unfinished = registered
			}
			for i := unfinished - 1; i >= 0; i-- {
				left = append(left, r.steps[i])
			}
			r.mu.Unlock()
			if first {
				// A rollback already running was caused by the command failing,
				// and the steps run on the restored context, so there is nothing
				// to cancel.
				if rollingBack {
					fmt.Fprintf(r.out, "\nInterrupted (%v): the rollback is already running. Interrupt again to exit at once.\n", sig)
				} else {
					fmt.Fprintf(r.out, "\nInterrupted (%v): stopping the current step. Interrupt again to exit at once.\n", sig)
					r.cancel()
				}
				continue
			}
			switch {
			case !rollingBack && registered == 0:
				fmt.Fprintf(r.out, "\nInterrupted again: exiting. Nothing had been created to roll back.\n")
			case !rollingBack:
				clusterPrintLeft(r.out, "Interrupted again: exiting before the rollback ran. Left in place (a step registered before what it undoes was created may have had nothing to remove), and how to remove each:", left)
			default:
				clusterPrintLeft(r.out, "Interrupted again: exiting before the rollback finished. Left in place (a step that had not run may have had nothing to remove), and how to remove each:", left)
			}
			clusterExit(clusterSignalExitCode(sig))
			return
		}
	}
}

// add registers an undo step, to run before the ones registered before it.
func (r *clusterRollback) add(u clusterUndo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, u)
}

// finish stops catching signals and, unless the command committed, rolls back.
// The steps run on the context the command started with, not the cancelled
// one, so an interrupted command can still undo what it did. *errp says when a
// signal stopped the command, and when the rollback left something behind.
func (r *clusterRollback) finish(commit bool, errp *error) {
	ctx = r.parent
	defer func() {
		r.cancel()
		r.stopSig()
		close(r.done)
	}()

	r.mu.Lock()
	sig := r.interrupted
	steps := r.steps
	if !commit {
		r.rollingBack, r.pending = true, len(steps)
	}
	r.mu.Unlock()

	var left []clusterUndo
	if !commit {
		if sig != nil && len(steps) > 0 {
			fmt.Fprintf(r.out, "Rolling back what this command created...\n")
		}
		needKubeconfig := false
		for i := len(steps) - 1; i >= 0; i-- {
			u := steps[i]
			failed := false
			if u.removesKubeconfig && needKubeconfig {
				fmt.Fprintf(r.out, "Rolling back: keeping %s, which the commands below need\n", u.what)
				failed = true
			} else if err := u.run(); err != nil {
				fmt.Fprintf(r.out, "  failed: %v\n", err)
				failed = true
				var partial *clusterUndoPartial
				if errors.As(err, &partial) {
					u.fix = partial.fix
				}
				needKubeconfig = needKubeconfig || u.usesKubeconfig
			}
			r.mu.Lock()
			if failed {
				left = append(left, u)
				r.left = left
			}
			r.pending--
			r.mu.Unlock()
		}
		if len(left) > 0 {
			clusterPrintLeft(r.out, "The rollback did not remove everything. Left in place, and how to remove each:", left)
		}
	}
	if errp == nil || *errp == nil {
		return
	}
	switch {
	case sig != nil && len(left) > 0:
		*errp = fmt.Errorf("interrupted (%v), and the rollback left %d item(s) in place, listed above: %w", sig, len(left), *errp)
	case sig != nil && len(steps) > 0:
		*errp = fmt.Errorf("interrupted (%v), and rolled back: %w", sig, *errp)
	case sig != nil:
		*errp = fmt.Errorf("interrupted (%v): %w", sig, *errp)
	case len(left) > 0:
		*errp = fmt.Errorf("%w (the rollback left %d item(s) in place, listed above)", *errp, len(left))
	}
}

// clusterPrintLeft lists what a rollback left in place, with the commands that
// remove each.
func clusterPrintLeft(out io.Writer, heading string, left []clusterUndo) {
	fmt.Fprintf(out, "\n%s\n", heading)
	for _, u := range left {
		fmt.Fprintf(out, "  - %s\n", u.what)
		for _, c := range u.fix {
			fmt.Fprintf(out, "      %s\n", c)
		}
	}
}

// clusterUndoKubectlDeleteSecret undoes applying a Secret to the cluster. The
// fix names the kubeconfig the command copied, which the rollback keeps when
// this step fails.
func clusterUndoKubectlDeleteSecret(out io.Writer, kubeconfigPath, namespace, name string) clusterUndo {
	return clusterUndo{
		what:           fmt.Sprintf("Secret %s/%s in the cluster", namespace, name),
		fix:            []string{fmt.Sprintf("kubectl --kubeconfig %s -n %s delete secret %s", kubeconfigPath, namespace, name)},
		usesKubeconfig: true,
		run: func() error {
			fmt.Fprintf(out, "Rolling back: kubectl -n %s delete secret %s\n", namespace, name)
			var output bytes.Buffer
			if err := clusterKubectl(ctx, kubeconfigPath, nil, &output, "delete", "secret", name, "-n", namespace, "--ignore-not-found"); err != nil {
				// kubectl's client logs every failed API discovery attempt before
				// the line that says what went wrong, which is its last.
				return fmt.Errorf("%w: %s", err, clusterLastLine(output.String()))
			}
			return nil
		},
	}
}

// clusterLastLine is the last non-blank line of s.
func clusterLastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[i+1:])
	}
	return s
}

// clusterUndoDeleteSpace undoes creating a Space. One already gone is not a
// failure.
func clusterUndoDeleteSpace(out io.Writer, slug string, clearReleaseTarget bool) clusterUndo {
	return clusterUndo{
		what: fmt.Sprintf("Space %q", slug),
		fix:  []string{fmt.Sprintf("cub space delete --recursive %s", slug)},
		run: func() error {
			sp, err := resolveSpace(slug, "SpaceID")
			if cubapi.IsNotFoundError(err) {
				return nil
			} else if err != nil {
				return err
			}
			fmt.Fprintf(out, "Rolling back: cub space delete --recursive %q\n", slug)
			if clearReleaseTarget {
				// The Space's release target is the OCI target in the cluster
				// Space. The reference does not block deleting this Space, but
				// ON DELETE RESTRICT on it blocks deleting the cluster Space,
				// which holds that target, so it is cleared for that later step.
				// A clear that fails shows up when that step does.
				_ = clusterClearReleaseTarget(sp.Space.SpaceID)
			}
			return clusterDeleteSpace(slug, true)
		},
	}
}

// clusterSignalExitCode is the shell's convention for a process a signal ended.
func clusterSignalExitCode(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return 1
}
