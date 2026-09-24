// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"net/http"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// entityMoveParams is what every POST /<entity>/move takes: the selection, and whether it is a dry
// run. Each entity type's generated client has its own params type of the same shape.
type entityMoveParams struct {
	where  string
	filter *string
	dryRun *bool
}

// entityMove describes `cub <entity> move` for an entity type moved by POST /<entity>/move. Units
// have their own command, since a Unit's move carries its Links and its Target.
type entityMove struct {
	parent *cobra.Command
	// entity and plural name the type in help and messages, as the CLI does elsewhere: "filter".
	entity string
	plural string
	// identifierFlag is the flag naming specific entities for a bulk move, as the type's delete
	// command names it.
	identifierFlag string
	// about is the paragraph of help that says what a move of this type carries or refuses.
	about string
	// buildWhere turns slugs or UUIDs into a where clause.
	buildWhere func([]string) (string, error)
	// move calls the type's move API.
	move func(params entityMoveParams, body goclientnew.MoveRequest) (statusCode int, body200, body207 *[]goclientnew.MoveResponse, err error)
}

// addEntityMoveCommand adds `move [<slug>] <to-space>` to the entity's command.
func addEntityMoveCommand(spec entityMove) {
	var identifiers []string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   fmt.Sprintf("move [<%s-slug>] <to-space>", spec.entity),
		Short: fmt.Sprintf("Move %s to another space", spec.plural),
		Long: getCommandHelp(fmt.Sprintf(`Move %[2]s to another space.

A %[1]s keeps its identity: everything that uses it refers to it by ID and keeps using it.
Moving requires Manage permission on each %[1]s and CreateChildren permission on the
destination space.
%[4]s
Every check runs over the whole selection before anything moves, so a move that would
collide with a name in the destination moves nothing. Rename first if a name is taken;
--dry-run reports what a move would do.

Examples:
`+"```"+`
  # Move one %[1]s
  cub %[1]s move --space my-space my-%[1]s other-space

  # Move every %[1]s with a label
  cub %[1]s move --space my-space --where "Labels.team = 'web'" other-space

  # Preview a move across all spaces
  cub %[1]s move --where "Labels.team = 'web'" --dry-run other-space

  # Move specific %[2]s by slug
  cub %[1]s move --space my-space --%[3]s my-%[1]s,another-%[1]s other-space
`+"```"+`
`, spec.entity, spec.plural, spec.identifierFlag, spec.aboutParagraph()), ""),
		Args:        cobra.RangeArgs(1, 2),
		Annotations: map[string]string{"OrgLevel": ""},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEntityMove(spec, args, identifiers, dryRun)
		},
	}
	enableWhereFlag(cmd)
	enableFilterFlag(cmd)
	enableQuietFlag(cmd)
	enableOutputFlag(cmd)
	cmd.Flags().StringSliceVar(&identifiers, spec.identifierFlag, []string{},
		fmt.Sprintf("move specific %s by slug or UUID (can be repeated or comma-separated)", spec.plural))
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what the move would do without moving anything")
	spec.parent.AddCommand(cmd)
}

func runEntityMove(spec entityMove, args []string, identifiers []string, dryRun bool) error {
	destinationRef := args[len(args)-1]
	isBulkMoveMode := len(args) == 1
	if isBulkMoveMode {
		if len(identifiers) > 0 && where != "" {
			return fmt.Errorf("--%s and --where flags are mutually exclusive", spec.identifierFlag)
		}
	} else {
		if filter != "" || where != "" || len(identifiers) > 0 {
			return fmt.Errorf("--filter, --where, or --%s can only be specified without a %s argument",
				spec.identifierFlag, spec.entity)
		}
		identifiers = []string{args[0]}
	}
	if err := validateSpaceFlag(isBulkMoveMode); err != nil {
		return err
	}

	destination, err := resolveSpace(destinationRef, "SpaceID,Slug")
	if err != nil {
		return err
	}
	if destination == nil || destination.Space == nil {
		return fmt.Errorf("destination space %s not found", destinationRef)
	}
	destinationID, err := uuid.Parse(destination.Space.SpaceID.String())
	if err != nil {
		return err
	}

	params := entityMoveParams{}
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}
	if filterID != "" {
		params.filter = &filterID
	}
	if len(identifiers) > 0 {
		params.where, err = spec.buildWhere(identifiers)
		if err != nil {
			return err
		}
	} else {
		params.where = where
	}
	params.where = addSpaceIDToWhereClause(params.where, selectedSpaceID)
	if dryRun {
		params.dryRun = &dryRun
	}

	statusCode, body200, body207, err := spec.move(params, goclientnew.MoveRequest{ToSpaceID: destinationID})
	if err != nil {
		return err
	}
	var responses *[]goclientnew.MoveResponse
	switch statusCode {
	case http.StatusOK:
		responses = body200
	case http.StatusMultiStatus:
		responses = body207
	}
	if responses == nil {
		return errors.New("unexpected response from the move API")
	}
	return handleEntityMoveResponse(spec, responses, destination.Space.Slug, dryRun)
}

// handleEntityMoveResponse prints one line per entity and fails if any of them did not move.
func handleEntityMoveResponse(spec entityMove, results *[]goclientnew.MoveResponse, destinationSlug string, isDryRun bool) error {
	if len(*results) == 0 {
		if !quiet && !isAlternativeOutput() {
			tprint("No %s found matching the filter", spec.plural)
		}
		renderPayload(results)
		return nil
	}
	if isAlternativeOutput() {
		renderPayload(results)
		return nil
	}

	failures := 0
	for _, result := range *results {
		if result.Error != nil {
			failures++
			tprint("%s: %s", result.Slug, result.Error.Message)
			continue
		}
		if quiet {
			continue
		}
		if isDryRun {
			tprint("%s can move to %s", result.Slug, destinationSlug)
		} else {
			tprint("%s moved to %s", result.Slug, destinationSlug)
		}
		if len(result.MovedTagSlugs) > 0 {
			tprint("  tags moved: %v", result.MovedTagSlugs)
		}
	}
	if failures > 0 {
		if isDryRun {
			return fmt.Errorf("%d %s cannot move", failures, spec.entityCount(failures))
		}
		return fmt.Errorf("%d %s did not move", failures, spec.entityCount(failures))
	}
	return nil
}

func (spec entityMove) entityCount(n int) string {
	if n == 1 {
		return spec.entity
	}
	return spec.plural
}

// aboutParagraph is the type's own paragraph of help, set off from the rest when there is one.
func (spec entityMove) aboutParagraph() string {
	if spec.about == "" {
		return ""
	}
	return "\n" + spec.about + "\n"
}
