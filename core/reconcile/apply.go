// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package reconcile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// PostSpaceHook runs after a Space's Unit changes (adds, updates, deletes)
// have been applied. Callers use it to reconcile links, refresh bookkeeping
// records, or any per-Space follow-up. Returning an error fails Apply.
type PostSpaceHook func(ctx context.Context, sp SpacePlan) error

// ApplyOptions tunes Apply. Zero value is the safe default: interactive
// refusal to empty Units without Yes, os.Stdout/os.Stderr, no hook.
type ApplyOptions struct {
	// Yes permits emptying Units that dropped out of the desired set.
	// Required when the plan contains deletes.
	Yes bool
	// ChangeSetSlug is the slug used when opening a per-Space ChangeSet for
	// updates. Defaults to DefaultChangeSetSlug(time.Now()) chosen by the
	// caller — pass a non-empty, retry-stable slug for reproducibility.
	ChangeSetSlug string
	// ChangeSetDescription is the description on the opened ChangeSet.
	// Defaults to a generic line derived from the Space's DisplayName.
	ChangeSetDescription string
	// Stdout/Stderr override the I/O streams; nil uses os.Stdout/os.Stderr.
	Stdout io.Writer
	Stderr io.Writer
	// PostSpaceHook runs after each Space's changes. Nil = no-op.
	PostSpaceHook PostSpaceHook
}

// ChangeSetRef names one opened ChangeSet so the caller can render the revert
// command. UpdatedSlugs is the exact set of Units modified inside it.
type ChangeSetRef struct {
	Space        string
	Slug         string
	Component    string
	UpdatedSlugs []string
}

// ApplyResult reports what Apply did.
type ApplyResult struct {
	Created          int
	Updated          int
	Deleted          int
	ChangeSetsOpened []ChangeSetRef
}

// Apply executes the plan. Updates run inside a per-Space ChangeSet (adds and
// deletes are not ChangeSet-revertable). An empty plan is a no-op.
func (e *Engine) Apply(ctx context.Context, plan Plan, opts ApplyOptions) (ApplyResult, error) {
	res := ApplyResult{}
	if !plan.HasChanges() {
		return res, nil
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	for _, sp := range plan.Spaces {
		if len(sp.Adds) == 0 && len(sp.Updates) == 0 && len(sp.Deletes) == 0 {
			continue
		}
		fmt.Fprintf(stdout, "\n== Space %s ==\n", spaceBanner(sp))

		// Open a ChangeSet only if there are updates (creates and deletes are
		// not ChangeSet-revertable).
		var changeSetSlug string
		if len(sp.Updates) > 0 {
			slug, err := e.openChangeSet(ctx, sp.Space, opts.ChangeSetSlug,
				descriptionOrDefault(opts.ChangeSetDescription, sp))
			if err != nil {
				return res, err
			}
			changeSetSlug = slug
			updatedSlugs := make([]string, 0, len(sp.Updates))
			for _, u := range sp.Updates {
				updatedSlugs = append(updatedSlugs, u.Slug)
			}
			res.ChangeSetsOpened = append(res.ChangeSetsOpened, ChangeSetRef{
				Space: sp.Space, Slug: slug, Component: sp.DisplayName, UpdatedSlugs: updatedSlugs,
			})
			fmt.Fprintf(stdout, "ChangeSet: %s/%s (revert: `%s`)\n",
				sp.Space, slug, RestoreCommand(sp.Space, slug, updatedSlugs))
		}

		for _, a := range sp.Adds {
			if err := e.createUnit(ctx, sp, a, stdout, stderr); err != nil {
				return res, err
			}
			res.Created++
		}
		for _, u := range sp.Updates {
			if err := e.updateUnit(ctx, sp, u, changeSetSlug, stdout, stderr); err != nil {
				return res, err
			}
			res.Updated++
		}
		// Detach the updated Units from the ChangeSet. The ChangeSet itself is
		// preserved (for revert); the Units are freed so the next reconcile
		// can open a new ChangeSet on them. Without this the server rejects
		// all subsequent updates against these Units (the ChangeSet acts as a
		// lock).
		if changeSetSlug != "" {
			if err := e.closeChangeSet(ctx, sp.Space, sp.Updates, stdout, stderr); err != nil {
				return res, err
			}
		}
		if len(sp.Deletes) > 0 {
			deleted, err := e.emptyUnits(ctx, sp.Space, sp.Deletes, opts, stdout, stderr)
			if err != nil {
				return res, err
			}
			res.Deleted += deleted
		}
		if opts.PostSpaceHook != nil {
			if err := opts.PostSpaceHook(ctx, sp); err != nil {
				return res, fmt.Errorf("post-space hook for %s: %w", sp.Space, err)
			}
		}
	}
	return res, nil
}

func spaceBanner(sp SpacePlan) string {
	switch {
	case sp.DisplayName != "" && sp.DisplayVersion != "":
		return fmt.Sprintf("%s (%s@%s)", sp.Space, sp.DisplayName, sp.DisplayVersion)
	case sp.DisplayName != "":
		return fmt.Sprintf("%s (%s)", sp.Space, sp.DisplayName)
	default:
		return sp.Space
	}
}

func descriptionOrDefault(d string, sp SpacePlan) string {
	if d != "" {
		return d
	}
	switch {
	case sp.DisplayName != "" && sp.DisplayVersion != "":
		return fmt.Sprintf("reconcile from %s@%s", sp.DisplayName, sp.DisplayVersion)
	case sp.DisplayName != "":
		return fmt.Sprintf("reconcile from %s", sp.DisplayName)
	default:
		return "reconcile update"
	}
}

func (e *Engine) createUnit(ctx context.Context, sp SpacePlan, a SlugDiff, stdout, stderr io.Writer) error {
	path, cleanup, err := bodyPath(a.Slug, a.Path, a.Content)
	if err != nil {
		return err
	}
	defer cleanup()

	args := []string{"unit", "create",
		"--space", sp.Space,
		"--merge-external-source", a.SourceName,
	}
	if a.Toolchain != "" {
		args = append(args, "--toolchain", a.Toolchain)
	}
	for _, l := range sp.CreateLabels {
		args = append(args, "--label", l)
	}
	for _, an := range sp.CreateAnnotations {
		args = append(args, "--annotation", an)
	}
	if sp.Target != "" {
		args = append(args, "--target", sp.Target)
	}
	args = append(args, a.Slug, path)
	if err := e.Cub.Run(ctx, args, stdout, stderr); err != nil {
		return fmt.Errorf("cub unit create %s in %s: %w", a.Slug, sp.Space, err)
	}
	return nil
}

func (e *Engine) updateUnit(ctx context.Context, sp SpacePlan, u SlugDiff, changeSetSlug string, stdout, stderr io.Writer) error {
	path, cleanup, err := bodyPath(u.Slug, u.Path, u.Content)
	if err != nil {
		return err
	}
	defer cleanup()

	args := []string{"unit", "update",
		"--space", sp.Space,
		"--merge-external-source", u.SourceName,
	}
	if changeSetSlug != "" {
		args = append(args, "--changeset", changeSetSlug)
	}
	args = append(args, u.Slug, path)
	if err := e.Cub.Run(ctx, args, stdout, stderr); err != nil {
		return fmt.Errorf("cub unit update %s in %s: %w", u.Slug, sp.Space, err)
	}

	// Re-stamp caller-owned bookkeeping annotations via a separate --patch so
	// they merge into the Unit's annotations without disturbing the data merge
	// above. Must ride the same ChangeSet as the data update — while a Unit is
	// attached to a ChangeSet the server rejects updates that omit it — so the
	// stamp reverts cleanly alongside the data change.
	if len(sp.UpdateAnnotations) > 0 {
		pvArgs := []string{"unit", "update",
			"--space", sp.Space, "--patch", "--quiet",
		}
		for _, an := range sp.UpdateAnnotations {
			pvArgs = append(pvArgs, "--annotation", an)
		}
		if changeSetSlug != "" {
			pvArgs = append(pvArgs, "--changeset", changeSetSlug)
		}
		pvArgs = append(pvArgs, u.Slug)
		if err := e.Cub.Run(ctx, pvArgs, stdout, stderr); err != nil {
			return fmt.Errorf("cub unit update --patch annotations on %s in %s: %w", u.Slug, sp.Space, err)
		}
	}
	return nil
}

// closeChangeSet detaches each updated Unit from its ChangeSet by patching
// --changeset -. The ChangeSet is preserved (start/end tags + recorded
// mutations remain) so a future restore reverts cleanly.
func (e *Engine) closeChangeSet(ctx context.Context, space string, updates []SlugDiff, stdout, stderr io.Writer) error {
	for _, u := range updates {
		args := []string{"unit", "update",
			"--space", space, "--patch", "--quiet",
			"--changeset", "-",
			u.Slug}
		if err := e.Cub.Run(ctx, args, stdout, stderr); err != nil {
			return fmt.Errorf("close changeset on %s/%s: %w", space, u.Slug, err)
		}
	}
	return nil
}

// emptyUnits reconciles Units that exist in ConfigHub (within the owned set)
// but no longer appear in the desired set. It does NOT run `cub unit delete`:
// the sync reconciles Data only, never the deployed live state. Deleting the
// Unit record would orphan whatever it has deployed, discard post-install
// edits, and fail on Units guarded by DeleteGates.
//
// Instead it feeds empty content through `cub unit update
// --merge-external-source`. That is a 3-way merge against the last external
// push, so it removes only the resources this source contributed and leaves
// any post-install additions in place — never a blunt full-replace-to-empty.
// The Unit record, target binding, and metadata survive; the next `cub unit
// apply` removes the now-absent resources through the normal deploy path.
//
// Because applying an emptied Unit destroys its deployed resources, emptying
// is refused for any Unit carrying a DestroyGate — the same protection the
// server enforces on the destroy action itself. The whole batch is scanned
// first so a gated Unit is reported before anything is touched.
func (e *Engine) emptyUnits(ctx context.Context, space string, deletes []SlugDiff, opts ApplyOptions, stdout, stderr io.Writer) (int, error) {
	if !opts.Yes {
		fmt.Fprintf(stdout, "Refusing to empty %d Unit(s) in %s without Yes:\n", len(deletes), space)
		for _, d := range deletes {
			fmt.Fprintf(stdout, "  - %s\n", d.Slug)
		}
		return 0, fmt.Errorf("set ApplyOptions.Yes to empty Units no longer in the desired set")
	}

	var gated []string
	for _, d := range deletes {
		gates, err := e.unitDestroyGates(ctx, space, d.Slug, stderr)
		if err != nil {
			return 0, err
		}
		if len(gates) > 0 {
			gated = append(gated, fmt.Sprintf("  - %s (DestroyGates: %s)", d.Slug, strings.Join(gates, ", ")))
		}
	}
	if len(gated) > 0 {
		fmt.Fprintf(stdout, "Refusing to empty %d Unit(s) in %s guarded by DestroyGates:\n", len(gated), space)
		for _, line := range gated {
			fmt.Fprintln(stdout, line)
		}
		return 0, fmt.Errorf("remove the DestroyGate(s) (e.g. `cub unit update --patch --destroy-gate <key>=-`) before these Units can be emptied")
	}

	// Stage a single empty file to feed every merge-on-update.
	empty, err := os.CreateTemp("", "reconcile-empty-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(empty.Name())
	if err := empty.Close(); err != nil {
		return 0, err
	}

	emptied := 0
	for _, d := range deletes {
		// --merge-external-source makes this a 3-way merge against the last
		// push (the server picks that base by source type, so the identifier
		// value only labels the change). Pass the slug as that label;
		// --change-desc is what actually lands on the revision.
		args := []string{"unit", "update",
			"--space", space, "--quiet",
			"--merge-external-source", d.Slug,
			"--change-desc", fmt.Sprintf("reconcile: emptied %s (no longer in desired set)", d.Slug),
			d.Slug, empty.Name()}
		var combined bytes.Buffer
		if err := e.Cub.Run(ctx, args, &combined, &combined); err != nil {
			io.Copy(stderr, &combined)
			return emptied, fmt.Errorf("cub unit update (empty) %s in %s: %w", d.Slug, space, err)
		}
		fmt.Fprintf(stdout, "Emptied %s/%s (apply to remove its deployed resources)\n", space, d.Slug)
		emptied++
	}
	return emptied, nil
}

// unitDestroyGates returns the DestroyGate keys set on a Unit, or nil if it
// has none.
func (e *Engine) unitDestroyGates(ctx context.Context, space, slug string, stderr io.Writer) ([]string, error) {
	stdout, errb, err := e.capture(ctx, []string{"unit", "get", "--space", space, "-o", "json", slug})
	if err != nil {
		io.Copy(stderr, strings.NewReader(errb))
		return nil, fmt.Errorf("cub unit get %s in %s: %w", slug, space, err)
	}
	var resp struct {
		Unit struct {
			DestroyGates map[string]bool `json:"DestroyGates"`
		} `json:"Unit"`
	}
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		return nil, fmt.Errorf("parse cub unit get %s in %s: %w", slug, space, err)
	}
	if len(resp.Unit.DestroyGates) == 0 {
		return nil, nil
	}
	gates := make([]string, 0, len(resp.Unit.DestroyGates))
	for k := range resp.Unit.DestroyGates {
		gates = append(gates, k)
	}
	sort.Strings(gates)
	return gates, nil
}
