// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"strings"
	"time"

	"github.com/confighub/sdk/cmd/cub/upload"
	"github.com/confighub/sdk/core/reconcile"
)

// A re-upload is "cub variant upload" run again against a Space it already
// populated: the same command, the same inputs (or newer ones), landing changed
// resources on top of what is already there. It is not a second create — the
// Units exist, they may carry post-upload edits, and blindly recreating them
// would either fail or discard those edits.
//
// The work is done by public/core/reconcile, the engine lifted from the
// installer's internal/diff. It classifies each desired Unit against live
// ConfigHub state (add / update / unchanged), applies updates as a 3-way merge
// via "cub unit update --merge-external-source" so local edits survive, and
// records the updates in a ChangeSet so the whole re-upload can be reverted.
// This file only translates an upload.Plan into that engine's vocabulary.

// uploadSpaceHasUnits reports whether spaceSlug already exists and holds at
// least one Unit — the signal that distinguishes a re-upload from a first
// upload. A missing Space is not an error: it is the ordinary first-upload
// case, so the lookup failure is reported as "no units" rather than propagated.
func uploadSpaceHasUnits(spaceSlug string) bool {
	space, err := apiGetSpaceFromSlug(spaceSlug, "SpaceID,Slug")
	if err != nil || space == nil {
		return false
	}
	units, err := apiListUnits(space.SpaceID.String(), "", "UnitID,Slug")
	if err != nil {
		return false
	}
	return len(units) > 0
}

// uploadDesiredUnits translates the upload plan into the engine's desired-set
// vocabulary.
//
// Every Unit carries the upload's source description as its
// merge-external-source identity, the same value the first upload stamped, so
// the server resolves each update against the previous external push rather
// than against an operator's edit.
//
// An AppConfig manifest contributes two entries. The data Unit is a normal
// desired Unit — it holds the rendered file body and is what actually changes
// between uploads. The placeholder is marked Bodyless: its content is produced
// by the Upsert link, not by this command, so the engine must recognize it as
// present (keeping it out of the prune set) without ever writing to it.
func uploadDesiredUnits(plan *upload.Plan, a *variantUploadOptions) []reconcile.DesiredUnit {
	desired := make([]reconcile.DesiredUnit, 0, len(plan.Units))
	for _, u := range plan.Units {
		if u.Kind == upload.UnitAppConfig {
			ac := u.AppConfig
			desired = append(desired,
				reconcile.DesiredUnit{
					Slug:       ac.UnitSlug(),
					SourceName: a.sourceDesc,
					Content:    []byte(u.Content),
					Toolchain:  ac.Toolchain,
				},
				reconcile.DesiredUnit{
					Slug:     ac.PlaceholderSlug(),
					Bodyless: true,
				},
			)
			continue
		}
		desired = append(desired, reconcile.DesiredUnit{
			Slug:       u.Slug,
			SourceName: a.sourceDesc,
			Content:    []byte(u.Content),
			Toolchain:  u.Toolchain,
		})
	}
	return desired
}

// uploadSpaceSource builds the engine's per-Space desired state.
//
// ListWhere is deliberately empty, which tells the engine every Unit in the
// Space is in scope. Scoping it to an ownership label instead would break the
// far more important add/update classification: Spaces populated by an earlier
// upload carry no such label, so every existing Unit would read as absent, and
// the re-upload would try to create Units that are already there. The Space is
// this command's to manage — it creates it and names it after the component and
// variant — so whole-Space scope is also the honest description of ownership.
// The consequence is confined to pruning, which is opt-in for exactly that
// reason.
func uploadSpaceSource(spaceSlug string, plan *upload.Plan, a *variantUploadOptions) reconcile.SpaceSource {
	return reconcile.SpaceSource{
		Space:             spaceSlug,
		Desired:           uploadDesiredUnits(plan, a),
		Target:            a.target,
		CreateLabels:      a.labels,
		CreateAnnotations: a.annotations,
		DisplayName:       a.component,
		DisplayVersion:    a.variant,
	}
}

// uploadEngine returns a reconcile Engine that forks this same binary.
//
// The Bin is set explicitly rather than left to BinaryRunner's default: that
// default resolves "cub" through $PATH, which is the version-skew trap
// cubBinaryPath exists to avoid — a locally built bin/cub-dev would hand every
// reconcile step to whatever release happens to be installed.
func uploadEngine() *reconcile.Engine {
	return reconcile.New(reconcile.BinaryRunner{Bin: cubBinaryPath()})
}

// variantUploadReconcile re-uploads into a Space that already holds Units.
//
// Deletes are dropped from the plan unless --prune: a resource vanishing from
// the rendered input is far more often a narrowed input (a different overlay, a
// partial render) than a deliberate removal, and emptying a Unit stages the
// destruction of what it has deployed. When --prune is set the engine empties
// them by merging empty content — never "cub unit delete" — so only the
// resources this source contributed are withdrawn and any DestroyGate-guarded
// Unit refuses.
func variantUploadReconcile(spaceSlug string, plan *upload.Plan, a *variantUploadOptions) error {
	eng := uploadEngine()
	rp, err := eng.Compute(ctx, []reconcile.SpaceSource{uploadSpaceSource(spaceSlug, plan, a)})
	if err != nil {
		return err
	}

	pruned := dropDeletes(&rp, a.prune)
	if !rp.HasChanges() {
		reportPruneSkipped(pruned)
		tprint("Already up to date: %s matches the input", spaceSlug)
		return nil
	}

	res, err := eng.Apply(ctx, rp, reconcile.ApplyOptions{
		Yes: a.prune,
		// The engine does not name the ChangeSet itself — an empty slug reaches
		// the server verbatim and is rejected. DefaultChangeSetSlug is stable
		// within a second, so retrying a failed re-upload reuses the ChangeSet
		// (opened with --allow-exists) rather than stranding a half-used one.
		ChangeSetSlug:        reconcile.DefaultChangeSetSlug(time.Now()),
		ChangeSetDescription: "cub variant upload from " + a.sourceDesc,
		PostSpaceHook: func(context.Context, reconcile.SpacePlan) error {
			return uploadScaffolding(spaceSlug, plan, a)
		},
	})
	if err != nil {
		return err
	}

	reportPruneSkipped(pruned)
	tprint("Re-uploaded %s: %d created, %d updated, %d emptied",
		spaceSlug, res.Created, res.Updated, res.Deleted)
	for _, cs := range res.ChangeSetsOpened {
		tprint("Revert with: %s", reconcile.RestoreCommand(cs.Space, cs.Slug, cs.UpdatedSlugs))
	}
	return nil
}

// dropDeletes strips the plan's deletes unless prune is set, returning the
// slugs it removed so they can be reported rather than silently ignored.
func dropDeletes(rp *reconcile.Plan, prune bool) []string {
	if prune {
		return nil
	}
	var skipped []string
	for i := range rp.Spaces {
		for _, d := range rp.Spaces[i].Deletes {
			skipped = append(skipped, d.Slug)
		}
		rp.Spaces[i].Deletes = nil
	}
	return skipped
}

func reportPruneSkipped(slugs []string) {
	if len(slugs) == 0 {
		return
	}
	tprint("")
	tprint("Note: %d Unit(s) are in the Space but not in this input. Re-run with --prune to", len(slugs))
	tprint("empty them (their deployed resources are removed on the next apply):")
	for _, s := range slugs {
		tprint("  - %s", s)
	}
}

// uploadScaffolding creates the parts of an AppConfig expansion the reconcile
// engine does not own — the render-configmap Invocation, the placeholder Unit,
// and the Upsert link that renders one into the other — plus the inferred
// Unit→Unit links. Everything here is created with --allow-exists because a
// re-upload re-asserts the same scaffolding: it must be a no-op for the
// manifests that were already uploaded and a create for the ones that are new.
func uploadScaffolding(spaceSlug string, plan *upload.Plan, a *variantUploadOptions) error {
	for _, u := range plan.Units {
		if u.Kind != upload.UnitAppConfig {
			continue
		}
		if err := createAppConfigInvocation(spaceSlug, u.AppConfig, true); err != nil {
			return err
		}
		if err := createAppConfigPlaceholder(spaceSlug, u.AppConfig, a, true); err != nil {
			return err
		}
		if err := linkAppConfigPlaceholder(spaceSlug, u.AppConfig, true); err != nil {
			return err
		}
		if err := setPlaceholderNamespace(spaceSlug, u.AppConfig, a); err != nil {
			return err
		}
	}
	existing, err := existingLinkPairs(spaceSlug)
	if err != nil {
		return err
	}
	return createInferredLinks(spaceSlug, plan, true, existing)
}

// existingLinkPairs returns the from→to Unit slug pairs already linked in the
// Space, so a re-upload re-asserts only the links that are genuinely missing.
func existingLinkPairs(spaceSlug string) (map[string]bool, error) {
	space, err := apiGetSpaceFromSlug(spaceSlug, "SpaceID,Slug")
	if err != nil {
		return nil, err
	}
	links, err := apiListLinks(space.SpaceID.String(), "", "", "")
	if err != nil {
		return nil, err
	}
	pairs := make(map[string]bool, len(links))
	for _, l := range links {
		if l.FromUnit == nil || l.ToUnit == nil {
			continue
		}
		pairs[linkPairKey(l.FromUnit.Slug, l.ToUnit.Slug)] = true
	}
	return pairs, nil
}

// reportUploadDryRun prints what a re-upload would do without touching
// anything. The per-Unit diff text is the engine's own dry-run mutations
// output, so it shows the merge as the server would resolve it rather than a
// textual guess.
func reportUploadDryRun(spaceSlug string, plan *upload.Plan, a *variantUploadOptions) error {
	if !uploadSpaceHasUnits(spaceSlug) {
		tprint("Space %s does not exist yet; a first upload would create %d Unit(s):", spaceSlug, len(plan.Units))
		for _, u := range plan.Units {
			tprint("  + %s", u.Slug)
		}
		reportUploadPlan(plan)
		return nil
	}

	rp, err := uploadEngine().Compute(ctx, []reconcile.SpaceSource{uploadSpaceSource(spaceSlug, plan, a)})
	if err != nil {
		return err
	}
	pruned := dropDeletes(&rp, a.prune)

	adds, updates, deletes := rp.Counts()
	if adds+updates+deletes == 0 {
		reportPruneSkipped(pruned)
		tprint("Already up to date: %s matches the input", spaceSlug)
		reportUploadPlan(plan)
		return nil
	}

	tprint("== Space %s ==", spaceSlug)
	for _, sp := range rp.Spaces {
		for _, add := range sp.Adds {
			tprint("  + %s (create)", add.Slug)
		}
		for _, up := range sp.Updates {
			tprint("  ~ %s (update)", up.Slug)
			for _, line := range splitNonEmptyLines(up.DiffText) {
				tprint("      %s", line)
			}
		}
		for _, del := range sp.Deletes {
			tprint("  - %s (empty)", del.Slug)
		}
	}
	tprint("")
	tprint("%d to create, %d to update, %d to empty", adds, updates, deletes)
	reportPruneSkipped(pruned)
	reportUploadPlan(plan)
	return nil
}

// splitNonEmptyLines splits s on newlines, dropping blank lines so the dry-run
// diff indents cleanly under its Unit.
func splitNonEmptyLines(s string) []string {
	var out []string
	for line := range strings.SplitSeq(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
