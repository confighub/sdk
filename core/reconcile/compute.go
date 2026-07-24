// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package reconcile

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Compute produces a Plan by querying ConfigHub for the current owned Unit
// set (per SpaceSource.ListWhere) and diffing it against the desired set.
// ConfigHub state is the single source of truth: an add is a desired Unit
// absent from the Space, an update is a desired Unit whose dry-run merge
// reports non-bookkeeping changes, a delete is an owned Unit absent from the
// desired set (excluding Ignore and Bodyless Units). Compute mutates nothing.
func (e *Engine) Compute(ctx context.Context, sources []SpaceSource) (Plan, error) {
	plan := Plan{Spaces: make([]SpacePlan, 0, len(sources))}
	for _, src := range sources {
		sp, err := e.computeOne(ctx, src)
		if err != nil {
			return plan, fmt.Errorf("space %s: %w", src.Space, err)
		}
		plan.Spaces = append(plan.Spaces, sp)
	}
	return plan, nil
}

func (e *Engine) computeOne(ctx context.Context, src SpaceSource) (SpacePlan, error) {
	out := SpacePlan{
		Space:             src.Space,
		DisplayName:       src.DisplayName,
		DisplayVersion:    src.DisplayVersion,
		Target:            src.Target,
		CreateLabels:      src.CreateLabels,
		CreateAnnotations: src.CreateAnnotations,
		UpdateAnnotations: src.UpdateAnnotations,
	}

	current, err := e.listCurrentSlugs(ctx, src.Space, src.ListWhere)
	if err != nil {
		return out, err
	}

	// Slugs the caller declared off-limits for deletion even when absent
	// from the desired set (bookkeeping records).
	ignore := map[string]struct{}{}
	for _, s := range src.Ignore {
		ignore[s] = struct{}{}
	}
	currentSet := map[string]struct{}{}
	for _, s := range current {
		if _, skip := ignore[s]; skip {
			continue
		}
		currentSet[s] = struct{}{}
	}

	// Desired set keyed by slug (last write wins on duplicate slugs).
	desired := map[string]DesiredUnit{}
	for _, u := range src.Desired {
		desired[u.Slug] = u
	}

	for _, slug := range sortedDesired(src.Desired) {
		u := desired[slug]
		// Bodyless Units are recognized as present (kept out of deletes) but
		// never added or updated by the engine.
		if u.Bodyless {
			continue
		}
		if _, exists := currentSet[slug]; !exists {
			out.Adds = append(out.Adds, slugDiff(u))
			continue
		}
		path, cleanup, err := bodyPath(u.Slug, u.Path, u.Content)
		if err != nil {
			return out, fmt.Errorf("slug %s: %w", slug, err)
		}
		diff, err := e.dryRunMutations(ctx, src.Space, slug, u.sourceName(), path)
		cleanup()
		if err != nil {
			return out, fmt.Errorf("slug %s: %w", slug, err)
		}
		if diff != "" {
			d := slugDiff(u)
			d.DiffText = diff
			out.Updates = append(out.Updates, d)
		}
	}

	// Deletes: owned Units with no matching desired Unit.
	desiredSet := map[string]struct{}{}
	for _, u := range src.Desired {
		desiredSet[u.Slug] = struct{}{}
	}
	for slug := range currentSet {
		if _, kept := desiredSet[slug]; !kept {
			out.Deletes = append(out.Deletes, SlugDiff{Slug: slug})
		}
	}
	sort.Slice(out.Deletes, func(i, j int) bool { return out.Deletes[i].Slug < out.Deletes[j].Slug })

	return out, nil
}

func slugDiff(u DesiredUnit) SlugDiff {
	return SlugDiff{
		Slug:       u.Slug,
		SourceName: u.sourceName(),
		Path:       u.Path,
		Content:    u.Content,
		Toolchain:  u.Toolchain,
	}
}

// listCurrentSlugs returns the slugs of Units in space matching where (empty
// where lists every Unit in the Space).
func (e *Engine) listCurrentSlugs(ctx context.Context, space, where string) ([]string, error) {
	args := []string{"unit", "list", "--space", space}
	if where != "" {
		args = append(args, "--where", where)
	}
	args = append(args, "-o", "jq=.[].Unit.Slug")
	stdout, stderr, err := e.capture(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("cub unit list: %w\n%s", err, stderr)
	}
	var slugs []string
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		s := strings.Trim(strings.TrimSpace(line), "\"")
		if s != "" {
			slugs = append(slugs, s)
		}
	}
	return slugs, nil
}

// dryRunMutations runs `cub unit update --merge-external-source ... --dry-run
// -o mutations` and returns the cleaned diff text. Empty string means no
// effective change (after ANSI stripping and bookkeeping filtering).
func (e *Engine) dryRunMutations(ctx context.Context, space, slug, sourceName, path string) (string, error) {
	args := []string{"unit", "update",
		"--space", space,
		"--merge-external-source", sourceName,
		"--dry-run", "-o", "mutations",
		slug, path}
	stdout, stderr, err := e.capture(ctx, args)
	if err != nil {
		return "", fmt.Errorf("cub unit update --dry-run: %w\n%s", err, stderr)
	}
	clean := stripANSI(stdout)
	if isNoChange(clean) {
		return "", nil
	}
	filtered := filterBookkeepingMutations(clean)
	if isNoChange(filtered) {
		return "", nil
	}
	return filtered, nil
}

// sortedDesired returns the desired slugs in stable order.
func sortedDesired(units []DesiredUnit) []string {
	keys := make([]string, 0, len(units))
	seen := map[string]struct{}{}
	for _, u := range units {
		if _, dup := seen[u.Slug]; dup {
			continue
		}
		seen[u.Slug] = struct{}{}
		keys = append(keys, u.Slug)
	}
	sort.Strings(keys)
	return keys
}
