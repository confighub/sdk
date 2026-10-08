// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// renamedEntity is what a rename needs of the entity it renames.
type renamedEntity struct {
	// spaceID is the Space the entity is in; uuid.Nil for a type in none.
	spaceID     uuid.UUID
	id          uuid.UUID
	slug        string
	displayName string
}

// entityRename describes `cub <entity> rename` for an entity type renamed by patching its Slug.
type entityRename struct {
	parent *cobra.Command
	// entity names the type in help and messages, as the CLI does elsewhere: "filter".
	entity string
	// orgLevel marks a type in no Space, which takes no --space.
	orgLevel bool
	// about is the paragraph of help that says what else a rename of this type changes, or
	// does not.
	about string
	// forceReason, when set, is why a rename of this type is refused without --force.
	forceReason string
	// resolve finds the entity a reference names.
	resolve func(ref string) (*renamedEntity, error)
	// patch sends the merge patch to the type's patch API and returns the entity written.
	patch func(e *renamedEntity, patch []byte) (any, error)
}

var renameArgs struct {
	displayName string
	force       bool
}

// addEntityRenameCommand adds `rename <entity> <new-slug>` to the entity's command.
func addEntityRenameCommand(spec entityRename) {
	command := spec.parent.Name()
	cmd := &cobra.Command{
		Use:   fmt.Sprintf("rename <%s> <new-slug>", command),
		Short: fmt.Sprintf("Rename a %s", spec.entity),
		Long: getCommandHelp(fmt.Sprintf(`Rename a %[1]s: change its slug. A display name that is the same as the slug changes with
it; --display-name sets the display name instead.

A %[1]s keeps its identity: everything that refers to it by ID keeps doing so. A where
expression that names it by slug, such as "Slug = 'old-name'", stops matching it.
%[2]s
--dry-run reports whether the rename would succeed without making it.

Examples:
`+"```"+`
  # Rename a %[1]s
  cub %[3]s rename my-%[3]s my-new-%[3]s%[4]s

  # Rename it and set its display name
  cub %[3]s rename my-%[3]s my-new-%[3]s --display-name "My new %[1]s"%[4]s
`+"```"+`
`, spec.entity, spec.aboutParagraph(), command, spec.forceExample()), ""),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEntityRename(spec, args[0], args[1], cmd.Flags().Changed("display-name"))
		},
	}
	if !spec.orgLevel {
		enableOptionalSpace(cmd)
	}
	enableQuietFlag(cmd)
	enableOutputFlag(cmd)
	enableDryRunFlag(cmd)
	cmd.Flags().StringVar(&renameArgs.displayName, "display-name", "",
		fmt.Sprintf("display name to give the %s, instead of following the slug", spec.entity))
	if spec.forceReason != "" {
		cmd.Flags().BoolVar(&renameArgs.force, "force", false, fmt.Sprintf("rename the %s even though %s", spec.entity, spec.forceReason))
	}
	spec.parent.AddCommand(cmd)
}

func runEntityRename(spec entityRename, ref, newSlug string, displayNameGiven bool) error {
	if spec.forceReason != "" && !renameArgs.force {
		return fmt.Errorf("renaming a %s needs --force: %s", spec.entity, spec.forceReason)
	}
	entity, err := spec.resolve(ref)
	if err != nil {
		return err
	}
	patch := renamePatch(entity, newSlug, renameArgs.displayName, displayNameGiven)
	if len(patch) == 0 {
		if !quiet && !isAlternativeOutput() {
			tprint("%s %s already has that name", spec.entity, entity.slug)
		}
		return nil
	}
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	written, err := spec.patch(entity, patchJSON)
	if err != nil {
		return err
	}
	if renderPayload(written) {
		return nil
	}
	if !quiet {
		would, did := "rename", "Renamed"
		change := fmt.Sprintf("%s %s to %s", spec.entity, entity.slug, newSlug)
		if newSlug == entity.slug {
			would, did = "set the display name of", "Set the display name of"
			change = fmt.Sprintf("%s %s to %q", spec.entity, entity.slug, renameArgs.displayName)
		}
		if dryRun {
			tprint("Dry run: would %s %s (%s)", would, change, entity.id)
		} else {
			tprint("%s %s (%s)", did, change, entity.id)
		}
	}
	return nil
}

// renamePatch is the merge patch that renames the entity: its Slug, and its DisplayName when one
// was given or when it was the same as the Slug, so that a name nobody chose separately keeps
// following the Slug. Only what changes is sent.
func renamePatch(entity *renamedEntity, newSlug, displayName string, displayNameGiven bool) map[string]any {
	patch := map[string]any{}
	if newSlug != entity.slug {
		patch["Slug"] = newSlug
	}
	switch {
	case displayNameGiven:
		if displayName != entity.displayName {
			patch["DisplayName"] = displayName
		}
	case entity.displayName == entity.slug && newSlug != entity.slug:
		patch["DisplayName"] = newSlug
	}
	return patch
}

// patchedEntity interprets the answer to a single patch.
func patchedEntity[R cubapi.APIResponse, E any](res R, err error, written func(R) *E) (any, error) {
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	return written(res), nil
}

// aboutParagraph is the type's own paragraph of help, set off from the rest when there is one.
func (spec entityRename) aboutParagraph() string {
	if spec.about == "" {
		return ""
	}
	return "\n" + spec.about + "\n"
}

func (spec entityRename) forceExample() string {
	if spec.forceReason == "" {
		return ""
	}
	return " --force"
}
