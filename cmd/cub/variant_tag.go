// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var variantTagArgs struct {
	revision    string
	move        bool
	whereSpace  string
	changeOrder string
	stage       string
}

var variantTagCmd = &cobra.Command{
	Use:         "tag [<variant>] <tag>",
	Short:       "Add, move or remove a tag on the units of one or more variants",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Add a tag to a revision of each unit in one or more variant spaces, move it, or remove it.

The spaces are the variant named as an argument, or those --where-space selects, or those
--change-order names -- its own space and the spaces it is headed for -- narrowed to one
stage of its change workflow with --stage. These combine as an intersection. --where
narrows the units further.

The tag is a slug, looked up in the variant's space when one is named, a space/slug, or a
UUID. ChangeOrder:<change-order> is that change order's release tag, the revision a release
of the change will publish in each space. Naming it selects the change order's spaces, so
--change-order is needed only to name a different one; --stage narrows them.

--revision says which revision of each unit to tag, as for "cub unit tag": HeadRevisionNum
(the default), LastReleasedRevisionNum, a number, Tag:<tag>, ChangeSet:<changeset>,
ChangeOrder:<change-order>, any of those prefixed with Before:, or - to remove the tag.

--move moves the tag from the revision it marks on each unit to the one --revision names.
A move, or a removal, acts on the units the tag marks in the selected spaces and leaves the
others alone. Tags a changeset, a change order or a release made cannot be added or
removed; a change order's release tag can be moved, to adopt a revision made after the
promotion -- a fix made in review -- before the change is released. It cannot move to a
revision before the one the change order's end tag marks or the unit's last released
revision, to one with validation errors, or at all in a space where a release has been
published at it.

Examples:
`+"```"+`
  # Tag the head revision of every unit of a variant.
  cub variant tag apptique/prod pre-tls-change

  # Remove that tag again.
  cub variant tag apptique/prod pre-tls-change --revision -

  # A fix made in review in staging: move the change's release tag to the fixed revisions.
  cub variant tag ChangeOrder:apptique-base/bump-image --move --stage staging

  # The same, in one space, for the workloads only.
  cub variant tag apptique-staging ChangeOrder:apptique-base/bump-image --move --where "Slug LIKE 'deployment-%'"
`+"```"+`
`, ""),
	Args: cobra.RangeArgs(1, 2),
	RunE: variantTagCmdRun,
}

func init() {
	enableWhereFlag(variantTagCmd)
	variantTagCmd.Flags().StringVar(&variantTagArgs.revision, "revision", "HeadRevisionNum",
		"which revision of each unit to tag: HeadRevisionNum, LastReleasedRevisionNum, a number, Tag:slug, ChangeSet:slug, ChangeOrder:slug, any of those prefixed with Before:, or - to remove the tag")
	variantTagCmd.Flags().BoolVar(&variantTagArgs.move, "move", false,
		"move the tag from the revision it marks on each unit to --revision; the only way to move a change order's release tag (ChangeOrder:slug)")
	variantTagCmd.Flags().StringVar(&variantTagArgs.whereSpace, "where-space", "",
		"select the spaces with a where expression over spaces")
	variantTagCmd.Flags().StringVar(&variantTagArgs.changeOrder, "change-order", "",
		"select this change order's space and the spaces it is headed for; implied by a ChangeOrder:<slug> tag")
	variantTagCmd.Flags().StringVar(&variantTagArgs.stage, "stage", "",
		"select the spaces of this stage of the change order's workflow")
	variantCmd.AddCommand(variantTagCmd)
}

// variantTagRequest is the tag request for a variant tag command line: the spaces, from the
// variant or --space, --where-space, and the change order, which a release tag names itself.
func variantTagRequest(tagID uuid.UUID, revision string, move bool, spaceID, whereSpace string,
	changeOrderID *uuid.UUID, stage string) (goclientnew.UnitTagRequest, error) {
	if move && revision == "Remove" {
		return goclientnew.UnitTagRequest{}, errors.New("--move names the revision to move the tag to, so it cannot be combined with --revision -")
	}
	if stage != "" && changeOrderID == nil {
		return goclientnew.UnitTagRequest{}, errors.New("--stage names a stage of a change order's workflow, so it needs --change-order or a ChangeOrder:<slug> tag")
	}
	var spaceClauses []string
	if spaceID != "" {
		spaceClauses = append(spaceClauses, fmt.Sprintf("SpaceID = '%s'", spaceID))
	}
	if whereSpace != "" {
		spaceClauses = append(spaceClauses, whereSpace)
	}
	if len(spaceClauses) == 0 && changeOrderID == nil {
		return goclientnew.UnitTagRequest{}, errors.New("tag needs the spaces to tag in: name a variant, or pass --where-space or --change-order")
	}
	return goclientnew.UnitTagRequest{
		TagID:         tagID,
		Revision:      revision,
		Move:          move,
		WhereSpace:    strings.Join(spaceClauses, " AND "),
		ChangeOrderID: changeOrderID,
		TargetStage:   stage,
	}, nil
}

func variantTagCmdRun(cmd *cobra.Command, args []string) error {
	// A space named on the command line, as the variant argument or --space, is where a bare tag
	// slug is looked up, and selects the units.
	spaceID := ""
	if selectedSpaceID != "" && selectedSpaceID != "*" {
		spaceID = selectedSpaceID
	}
	tagRef := args[len(args)-1]
	if len(args) == 2 {
		space, err := resolveVariantOrSpace(args[0], "SpaceID,Slug")
		if err != nil {
			return err
		}
		spaceID = space.Space.SpaceID.String()
	}

	// A change order lives in its base's space rather than in the variant's, so a bare change
	// order slug is looked up as any other reference is, not in the variant.
	lookupSpace := spaceID
	if lookupSpace == "" || strings.HasPrefix(tagRef, "ChangeOrder:") {
		lookupSpace = defaultSpaceID()
	}
	tagID, releaseTagOf, err := resolveTagToPlace(tagRef, lookupSpace)
	if err != nil {
		return err
	}
	revision, err := tagRevisionParameter(variantTagArgs.revision)
	if err != nil {
		return err
	}

	changeOrder := releaseTagOf
	if variantTagArgs.changeOrder != "" {
		resolved, err := resolveChangeOrder(variantTagArgs.changeOrder, defaultSpaceID(), "*")
		if err != nil {
			return fmt.Errorf("failed to get change order: %w", err)
		}
		changeOrder = resolved.ChangeOrder
	}
	var changeOrderID *uuid.UUID
	if changeOrder != nil {
		id := changeOrder.ChangeOrderID
		changeOrderID = &id
	}

	request, err := variantTagRequest(tagID, revision, variantTagArgs.move, spaceID, variantTagArgs.whereSpace,
		changeOrderID, variantTagArgs.stage)
	if err != nil {
		return err
	}

	// The request's Space selection is new. A server that predates it ignores the fields it does
	// not know, and would tag every unit --where selects -- with no --where, every unit in the
	// organization. So the unit selection is narrowed here as well, to the spaces cub can work out
	// itself: at worst an older server then acts on more of those spaces than --stage asked for,
	// never outside them.
	var whereSpaceIDs []uuid.UUID
	if variantTagArgs.whereSpace != "" {
		spaces, err := cubapi.ListSpaces(ctx, cubClient, cubapi.NewWhere(variantTagArgs.whereSpace), cubapi.ListOpts{Select: "SpaceID,Slug"})
		if err != nil {
			return err
		}
		if len(spaces) == 0 {
			return errors.New("--where-space selects no space")
		}
		for _, space := range spaces {
			whereSpaceIDs = append(whereSpaceIDs, space.Space.SpaceID)
		}
	}
	var changeOrderSpaceIDs []uuid.UUID
	if changeOrder != nil {
		changeOrderSpaceIDs = append([]uuid.UUID{changeOrder.SpaceID}, changeOrder.InScopeSpaceIDs...)
	}
	unitWhere := variantTagUnitWhere(where, spaceID, whereSpaceIDs, changeOrderSpaceIDs)

	params := &goclientnew.BulkTagUnitsParams{IncludeHidden: includeHiddenParam(), Where: &unitWhere}
	res, err := cubClientNew.BulkTagUnitsWithResponse(ctx, params, request)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 != nil && len(*res.JSON200) == 0 {
		if !quiet && !isAlternativeOutput() {
			tprint("No units to tag: the selected spaces have no units that match%s.", variantTagNothingReason(request))
		}
		return nil
	}
	return handleBulkUnitTagResponse(res.JSON200, res.JSON207, res.StatusCode(), revision, tagRef, request.WhereSpace)
}

// variantTagUnitWhere is the unit selection a variant tag sends alongside the request's Space
// selection: --where, narrowed to the spaces cub can name itself -- the variant or --space, those
// --where-space lists, and the change order's own space and the spaces it is headed for.
func variantTagUnitWhere(userWhere, spaceID string, whereSpaceIDs, changeOrderSpaceIDs []uuid.UUID) string {
	clauses := []string{}
	if userWhere != "" {
		clauses = append(clauses, userWhere)
	}
	if spaceID != "" {
		clauses = append(clauses, fmt.Sprintf("SpaceID = '%s'", spaceID))
	}
	for _, ids := range [][]uuid.UUID{whereSpaceIDs, changeOrderSpaceIDs} {
		if len(ids) == 0 {
			continue
		}
		quoted := make([]string, len(ids))
		for i, id := range ids {
			quoted[i] = "'" + id.String() + "'"
		}
		clauses = append(clauses, fmt.Sprintf("SpaceID IN (%s)", strings.Join(quoted, ", ")))
	}
	return strings.Join(clauses, " AND ")
}

// variantTagNothingReason finishes the sentence saying nothing was tagged: a move or a removal
// acts only on the units the tag marks.
func variantTagNothingReason(request goclientnew.UnitTagRequest) string {
	if request.Move || request.Revision == "Remove" {
		return " and that the tag marks"
	}
	return ""
}
