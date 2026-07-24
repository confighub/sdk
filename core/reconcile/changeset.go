// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package reconcile

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Note on revert scope: only update mutations recorded against a ChangeSet
// are revertable via `cub unit update --restore Before:ChangeSet:<slug>`.
// Creates and deletes are not reverted by ChangeSet restore — to undo a
// create, delete the Unit; to undo a delete (empty-merge), re-run the sync.

// DefaultChangeSetSlug returns the conventional slug for a reconcile ChangeSet
// at the given time. Stable input → stable output (no nondeterminism);
// callers wanting the live timestamp pass time.Now().
//
// Format: reconcile-YYYYMMDD-HHMMSS (UTC). Avoids RFC3339's colons and
// timezone offsets that ConfigHub slug rules disallow.
func DefaultChangeSetSlug(t time.Time) string {
	return "reconcile-" + t.UTC().Format("20060102-150405")
}

// openChangeSet creates a ChangeSet in space with the given slug and
// description. Idempotent via --allow-exists: an existing ChangeSet with the
// same slug is reused without error. The caller is responsible for choosing a
// slug stable across retries (DefaultChangeSetSlug is — within the same second).
func (e *Engine) openChangeSet(ctx context.Context, space, slug, description string) (string, error) {
	args := []string{"changeset", "create",
		"--space", space, "--allow-exists", "--quiet",
		"--description", description,
		slug}
	_, stderr, err := e.capture(ctx, args)
	if err != nil {
		return "", fmt.Errorf("cub changeset create %s/%s: %w\n%s", space, slug, err, stderr)
	}
	return slug, nil
}

// RestoreCommand returns the verbatim shell command an operator can run to
// revert the updates a ChangeSet captured. updatedSlugs scopes the restore to
// exactly the Units modified in the ChangeSet, avoiding partial-failure noise
// when other Units share the ownership label but were not part of it. Uses
// --patch because bulk restore requires it.
func RestoreCommand(space, slug string, updatedSlugs []string) string {
	if len(updatedSlugs) == 0 {
		return fmt.Sprintf("cub unit update --patch --space %s --restore Before:ChangeSet:%s --where \"true\"", space, slug)
	}
	quoted := make([]string, 0, len(updatedSlugs))
	for _, s := range updatedSlugs {
		quoted = append(quoted, "'"+s+"'")
	}
	return fmt.Sprintf("cub unit update --patch --space %s --restore Before:ChangeSet:%s --where \"Slug IN (%s)\"",
		space, slug, strings.Join(quoted, ","))
}
