// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package reconcile drives a non-clobbering, idempotent sync of a desired
// set of Units into a ConfigHub Space. It is the engine behind the
// installer's "plan"/"update" and cub's "variant upload" refresh: given a
// desired Unit set (each with a stable merge-external-source identity), it
// computes adds/updates/deletes against the live Space and applies them so
// that operator post-install edits survive (3-way merge on update,
// empty-merge instead of delete on removal).
//
// The engine never scans directories, parses manifests, or knows about
// AppConfig — the caller supplies the desired set already expanded, with a
// body (Path or Content) and a source name per Unit. All ConfigHub side
// effects go through a CubRunner, so callers can fork the cub binary or
// invoke the CLI in-process.
//
// This engine was lifted from the installer's internal/diff. The installer
// still carries its own copy; migrating it onto this package is tracked by
// confighub issue #4806.
package reconcile

import (
	"context"
	"io"
)

// CubRunner executes the cub CLI. args excludes the leading "cub". Either
// writer may be nil (discard). Implementations either fork a cub process
// (BinaryRunner) or dispatch to the CLI's commands in-process.
type CubRunner interface {
	Run(ctx context.Context, args []string, stdout, stderr io.Writer) error
}

// Engine reconciles desired Unit sets against ConfigHub via Cub.
type Engine struct {
	Cub CubRunner
}

// New returns an Engine backed by runner.
func New(runner CubRunner) *Engine {
	return &Engine{Cub: runner}
}

// DesiredUnit is one Unit the caller wants present in the Space.
type DesiredUnit struct {
	// Slug is the Unit slug — its stable identity in the Space.
	Slug string
	// SourceName is the merge-external-source identity passed to cub on
	// create/update. The server picks the 3-way-merge base by source type
	// per Unit, so this value only labels the change, but keeping it stable
	// across runs (typically the source file's basename) keeps the audit
	// trail coherent. Defaults to baseName(Path), or Slug when Path is empty.
	SourceName string
	// Path is a file on disk holding the Unit body. Used when Content is nil.
	Path string
	// Content, when non-nil, is the Unit body; the engine stages it to a
	// temp file for cub. Takes precedence over Path — set it for bodies that
	// don't already exist as files (e.g. AppConfig raw env content).
	Content []byte
	// Toolchain, when set, is forwarded to `cub unit create --toolchain`.
	// Empty lets cub infer it. Ignored on update (cub uses the existing Unit).
	Toolchain string
	// Bodyless marks a Unit that should be recognized as present — kept out
	// of delete candidates — but never added or updated by this engine. Used
	// for Units whose body is maintained elsewhere, e.g. an AppConfig
	// placeholder populated by an Upsert link.
	Bodyless bool
}

// SpaceSource is the desired state for one Space: the Unit set plus the
// ownership scoping and stamping the engine applies. Component/variant
// semantics live in the caller; the engine only reconciles Units.
type SpaceSource struct {
	// Space is the target Space slug.
	Space string
	// ListWhere scopes which existing Units this reconcile owns and may
	// therefore delete: the engine lists current Units matching this cub
	// where-clause (e.g. "Labels.Package='web'"). Empty means every Unit in
	// the Space is owned.
	ListWhere string
	// Ignore lists owned Unit slugs to never treat as delete candidates even
	// when absent from Desired (e.g. a bookkeeping record Unit).
	Ignore []string
	// Desired is the target Unit set.
	Desired []DesiredUnit
	// Target is the target ref forwarded to `cub unit create --target` for
	// adds. Empty means no target binding. Existing Units keep their binding.
	Target string
	// CreateLabels / CreateAnnotations are key=value pairs stamped on adds
	// only — never re-applied on update, which would clobber operator edits.
	CreateLabels      []string
	CreateAnnotations []string
	// UpdateAnnotations are key=value pairs re-stamped on every update via a
	// separate --patch (engine-owned bookkeeping the caller wants kept in
	// sync, e.g. a version marker). Empty skips the extra patch.
	UpdateAnnotations []string
	// DisplayName / DisplayVersion are presentation only, echoed in Apply's
	// per-Space banner and the default ChangeSet description.
	DisplayName    string
	DisplayVersion string
}

// Plan is the diff between the desired Unit sets and live ConfigHub state,
// broken down per Space.
type Plan struct {
	Spaces []SpacePlan
}

// SpacePlan is the slice of a Plan in one Space, carrying forward the
// stamping config from its SpaceSource so Apply is self-contained.
type SpacePlan struct {
	Space          string
	DisplayName    string
	DisplayVersion string
	// Adds exist in Desired but not in the live Space.
	Adds []SlugDiff
	// Updates exist in both and cub's dry-run reports non-bookkeeping changes.
	Updates []SlugDiff
	// Deletes are owned Units absent from Desired (Ignore/Bodyless excluded).
	Deletes []SlugDiff

	Target            string
	CreateLabels      []string
	CreateAnnotations []string
	UpdateAnnotations []string
}

// SlugDiff is one entry in Adds/Updates/Deletes. For Deletes only Slug is set.
type SlugDiff struct {
	Slug       string
	SourceName string
	Path       string
	Content    []byte
	Toolchain  string
	// DiffText is the cub -o mutations diff (ANSI-stripped, bookkeeping
	// filtered). Set only for Updates.
	DiffText string
}

// HasChanges reports whether the plan would create, update, or delete anything.
func (p Plan) HasChanges() bool {
	for _, s := range p.Spaces {
		if len(s.Adds) > 0 || len(s.Updates) > 0 || len(s.Deletes) > 0 {
			return true
		}
	}
	return false
}

// Counts returns the totals across all Spaces in the plan.
func (p Plan) Counts() (adds, updates, deletes int) {
	for _, s := range p.Spaces {
		adds += len(s.Adds)
		updates += len(s.Updates)
		deletes += len(s.Deletes)
	}
	return
}

// baseName returns the final path element of path (handling both separators).
func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

// sourceName resolves the merge-external-source identity for u.
func (u DesiredUnit) sourceName() string {
	if u.SourceName != "" {
		return u.SourceName
	}
	if u.Path != "" {
		return baseName(u.Path)
	}
	return u.Slug
}
