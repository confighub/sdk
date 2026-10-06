// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var applyArgs struct {
	filenames        []string
	source           string
	adopt            bool
	dryRun           bool
	prune            bool
	backingUnitSpace string
}

var applyCmd = &cobra.Command{
	Use:   "apply -f <file or directory>...",
	Short: "Create or update ConfigHub entities from their documents",
	Long: getCommandHelp(`Create or update ConfigHub entities from documents describing them, the way kubectl apply
creates or updates Kubernetes objects.

A document is YAML naming the entity's type and slug, with the fields it sets, the shape
"cub <entity> edit" shows and a backing Unit holds:

  EntityType: Trigger
  Slug: standard-vets
  Event: Mutation
  ToolchainType: Kubernetes/YAML
  FunctionName: vet-schemas
  UnitFilter: all-k8s-units

Each document is written to the entity's backing Unit, which is created if the entity has
none, and the entity is then created or patched from the Unit, after the Unit's Triggers
have run. Documents naming each other are written in order: a Trigger after the Filter it
names. A field the document stops stating is removed from the entity; a field changed in
ConfigHub that the document did not change is left alone.

The documents go to the Space --space names, which must exist; a name in a document is in
that Space unless it is qualified as <space>/<slug>. A Space document describes that Space,
and may leave its Slug out; its backing Unit goes in the Space --backing-unit-space names. Kubernetes and AppConfig files given
alongside are uploaded as cub variant upload would upload them, without a component.

Applying writes only the documents given: nothing the source applied before is removed.
With --prune, the documents given are all the source's: the backing Unit of an entity the
source applied before and whose document is not among them is emptied, and the entity is
then deleted, keeping the empty Unit, which revives if the document comes back. A Unit of
the source's whose file is not among them is emptied too.

Documents are owned by their source (--source, cub-apply by default), and a document for
an entity another source owns is refused unless --adopt is given.`, `
  # Apply a directory of entity documents to a Space
  cub apply --space platform -f platform/

  # Show what would change, path by path
  cub apply --space platform -f trigger.yaml --dry-run -o mutations

  # Apply a directory as everything the source owns, deleting what it no longer holds
  cub apply --space platform -f platform/ --prune`),
	Args:              cobra.NoArgs,
	PersistentPreRunE: spacePreRunE,
	RunE:              applyCmdRun,
}

func init() {
	addSpaceFlags(applyCmd)
	enableOutputFlag(applyCmd)
	applyCmd.Flags().StringSliceVarP(&applyArgs.filenames, "filename", "f", nil, "a file or directory of documents to apply, or - for stdin (repeatable)")
	applyCmd.Flags().StringVar(&applyArgs.source, "source", "cub-apply", "the source that owns what this applies")
	applyCmd.Flags().BoolVar(&applyArgs.adopt, "adopt", false, "take over entities another source owns")
	applyCmd.Flags().BoolVar(&applyArgs.dryRun, "dry-run", false, "report what would be created and updated, and change nothing")
	applyCmd.Flags().BoolVar(&applyArgs.prune, "prune", false, "take the documents given as all the source's, and delete the entities it applied before whose documents are not among them")
	applyCmd.Flags().StringVar(&applyArgs.backingUnitSpace, "backing-unit-space", "", "space, by slug, for the backing Unit of a Space document, which is in no space of its own; required with one")
	rootCmd.AddCommand(applyCmd)
}

func applyCmdRun(cmd *cobra.Command, args []string) error {
	if len(applyArgs.filenames) == 0 {
		return errors.New("name the documents to apply with -f")
	}
	if selectedSpaceSlug == "" || selectedSpaceSlug == "*" {
		return errors.New("cub apply needs a space: pass --space <space>")
	}
	files, _, _, err := collectUploadFiles(applyArgs.filenames)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("no YAML or JSON files to apply")
	}
	req := goclientnew.UploadRequest{
		Files:   files,
		Partial: !applyArgs.prune,
		Source: &goclientnew.UploadSourceInfo{
			Ref:           uploadSourceDescription(applyArgs.filenames),
			Client:        "cub",
			ClientVersion: Version,
		},
		Components: []goclientnew.UploadComponentRequest{{
			Space:            selectedSpaceSlug,
			SourceName:       applyArgs.source,
			Adopt:            applyArgs.adopt,
			BackingUnitSpace: applyArgs.backingUnitSpace,
		}},
	}
	var with []func(*goclientnew.UploadParams)
	showMutations := shouldDisplayMutations()
	if showMutations {
		with = append(with, cubapi.WithUploadMutations)
	}
	result, err := cubapi.Upload(ctx, cubClient, req, applyArgs.dryRun, with...)
	if err != nil {
		return err
	}
	failed := 0
	structured := renderPayload(result)
	if !structured {
		failed = reportApplyResult(result, showMutations)
	} else {
		failed = countApplyFailures(result)
	}
	if applyArgs.prune && !result.DryRun {
		failed += pruneApplied(result, structured)
	}
	if failed > 0 {
		return fmt.Errorf("%d document(s) were not applied", failed)
	}
	return nil
}

// reportApplyResult prints what became of each document, and of the Units the files held
// besides, and returns how many failed or were not written.
func reportApplyResult(result *goclientnew.UploadResult, showMutations bool) int {
	if result.DryRun {
		tprint("Dry run: nothing was written.")
	}
	failed := 0
	for _, c := range result.Components {
		for _, s := range c.Spaces {
			for _, u := range s.Units {
				e := u.Entity
				if e == nil {
					if u.Error != nil {
						failed++
						tprint("%-9s Unit %s: %s", "FAILED", u.Slug, errString(u.Error))
					} else if u.Action != "Unchanged" {
						tprint("%-9s Unit %s", u.Action, u.Slug)
					}
					continue
				}
				name := e.EntityType + " " + e.Slug
				switch {
				case u.Error != nil:
					failed++
					tprint("%-9s %s: its backing Unit %s: %s", "FAILED", name, u.Slug, errString(u.Error))
				case e.Error != nil:
					failed++
					action := e.Action
					if action == "" {
						action = "FAILED"
					}
					tprint("%-9s %s: %s", action, name, errString(e.Error))
				default:
					tprint("%-9s %s (backing Unit %s %s)", e.Action, name, u.Slug, strings.ToLower(u.Action))
				}
				if showMutations && u.Mutations != nil && len(*u.Mutations) > 0 {
					displayResourceMutationList(u.Mutations)
				}
			}
		}
	}
	return failed
}

// countApplyFailures counts the documents and Units that failed or were not written.
func countApplyFailures(result *goclientnew.UploadResult) int {
	failed := 0
	for _, c := range result.Components {
		for _, s := range c.Spaces {
			for _, u := range s.Units {
				if u.Error != nil || (u.Entity != nil && u.Entity.Error != nil) {
					failed++
				}
			}
		}
	}
	return failed
}

// pruneApplied deletes the entities whose backing Units the upload left empty, which it reports
// with the action Prune, type by type in the reverse of the order they are applied in, and returns
// how many were not deleted. A prune keeps the empty Units. With structured output, it reports
// only through the count.
func pruneApplied(result *goclientnew.UploadResult, structured bool) int {
	prunable := map[string][]goclientnew.UploadEntityResult{}
	for _, c := range result.Components {
		for _, s := range c.Spaces {
			for _, u := range s.Units {
				if e := u.Entity; e != nil && e.Action == "Prune" && e.EntityID != nil {
					prunable[e.EntityType] = append(prunable[e.EntityType], *e)
				}
			}
		}
	}
	entityTypes := slices.Collect(maps.Keys(prunable))
	cubapi.PruneOrder(entityTypes)
	failed := 0
	for _, entityType := range entityTypes {
		entities := prunable[entityType]
		ids := make([]uuid.UUID, len(entities))
		for i, e := range entities {
			ids[i] = *e.EntityID
		}
		responses, err := cubapi.PruneEntities(ctx, cubClient, entityType, cubapi.WhereEntityIDs(entityType, ids))
		if err != nil {
			failed += len(entities)
			if !structured {
				for _, e := range entities {
					tprint("%-9s %s %s: %v", "FAILED", entityType, e.Slug, err)
				}
			}
			continue
		}
		deleted := 0
		for _, response := range responses {
			if response.Error != nil {
				failed++
				if !structured {
					tprint("%-9s %s: %s", "FAILED", entityType, errString(response.Error))
				}
			} else {
				deleted++
			}
		}
		if structured {
			continue
		}
		// A delete's result names the entity by ID alone, so the slugs are listed when every
		// entity was deleted. One left alone had its backing Unit written in between.
		if deleted == len(entities) {
			for _, e := range entities {
				tprint("%-9s %s %s", "Pruned", entityType, e.Slug)
			}
		} else if deleted > 0 {
			tprint("%-9s %d of %d %ss", "Pruned", deleted, len(entities), entityType)
		}
	}
	return failed
}
