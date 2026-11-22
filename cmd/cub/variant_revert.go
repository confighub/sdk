// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var variantRevertCmd = &cobra.Command{
	Use:         "revert [<variant>] <revisions>",
	Short:       "Take back a change across the units it made, keeping what came after it",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Take back a change in every unit it made, with a new revision of each, keeping the changes
made since.

`+revertRevisionsHelp+`

The units are those the change marks: the units a changeset, tag or release marks -- the end
of a range, or its start when the end is HeadRevisionNum or LastReleasedRevisionNum -- in the
variant when one is named, and wherever they are otherwise. A range with nothing but those two
at its ends, such as LastReleasedRevisionNum..HeadRevisionNum, marks no units, so it needs a
variant, and reverts every unit of it. --where narrows the units. Revision numbers differ from
unit to unit, so they are not accepted here; "cub unit revert" takes them.

A unit the change marks but did not change gets no new revision.

Without --change-desc, each new revision is described by what it reverted in that unit, as
"Reverted revisions 6-7", or for a changeset by the changeset and its description.

Publishing is separate: "cub release publish" takes the new revisions to a cluster.

Examples:
`+"```"+`
  # Revert a changeset everywhere it made changes, showing what that would do first
  cub variant revert ChangeSet:web-base/tls-rollout --dry-run -o mutations
  cub variant revert ChangeSet:web-base/tls-rollout

  # Revert what was done between two tags in one variant
  cub variant revert web/prod Tag:web-prod/pre-tls..Tag:web-prod/post-tls

  # Revert everything since a tag, in the workloads only
  cub variant revert web/prod Tag:web-prod/pre-tls..HeadRevisionNum --where "Slug LIKE 'deployment-%'"

  # Revert every unreleased change in a variant
  cub variant revert web/dev LastReleasedRevisionNum..HeadRevisionNum
`+"```"+`
`, ""),
	Args: cobra.RangeArgs(1, 2),
	RunE: variantRevertCmdRun,
}

func init() {
	addRevertFlags(variantRevertCmd)
	enableWhereFlag(variantRevertCmd)
	enableWaitFlag(variantRevertCmd)
	addStandardDisplayFlags(variantRevertCmd)
	variantCmd.AddCommand(variantRevertCmd)
}

// revertUnitChunk is how many units one bulk revert names, to keep the where clause within the
// length of a query string.
const revertUnitChunk = 100

func variantRevertCmdRun(cmd *cobra.Command, args []string) error {
	revisions := args[len(args)-1]
	r, err := parseRevertRange(revisions)
	if err != nil {
		return err
	}
	for _, end := range []string{r.From, r.To} {
		if entityType, identifier := revisionSpecEntity(end); entityType == "Revision" || (entityType == "" && !isNamedRevision(identifier)) {
			return fmt.Errorf(`%s names a revision of one unit, and each unit numbers its own; name a changeset, tag or release, or use "cub unit revert"`, end)
		}
	}

	spaceID := ""
	if selectedSpaceID != "" && selectedSpaceID != "*" {
		spaceID = selectedSpaceID
	}
	if len(args) == 2 {
		space, err := resolveVariantOrSpace(args[0], "SpaceID,Slug")
		if err != nil {
			return err
		}
		spaceID = space.Space.SpaceID.String()
	}
	// A bare slug is looked up in the variant named, or as any other reference is.
	lookupSpace := spaceID
	if lookupSpace == "" {
		lookupSpace = defaultSpaceID()
	}

	base, end, err := revertMergeRange(r, lookupSpace, 0)
	if err != nil {
		return err
	}
	unitWheres, err := revertUnitWheres(base, end, spaceID)
	if err != nil {
		return err
	}
	if len(unitWheres) == 0 {
		if showProgress() {
			tprint("No units to revert: %s marks no unit%s", revisions, revertScopeSuffix(spaceID))
		}
		return nil
	}

	changeDescription, err := revertChangeDescription(r, lookupSpace)
	if err != nil {
		return err
	}
	body, changeSetID, err := revertPatchBody(changeDescription)
	if err != nil {
		return err
	}
	clearance, err := clearanceJSON(true)
	if err != nil {
		return err
	}

	var responses []goclientnew.UnitCreateOrUpdateResponse
	failed := false
	for _, unitWhere := range unitWheres {
		effectiveWhere := cubapi.NewWhere(unitWhere).And(where).String()
		// Squash is implied by revert, and sent as well for a server that predates revert.
		self, squash, revert, includeHidden := "Self", true, true, "*"
		params := &goclientnew.BulkPatchUnitsParams{
			Where:         &effectiveWhere,
			MergeSource:   &self,
			MergeBase:     &base,
			MergeEnd:      &end,
			Squash:        &squash,
			Revert:        &revert,
			DryRun:        dryRunParam(),
			ChangeSetId:   changeSetID,
			IncludeHidden: &includeHidden,
		}
		if clearance != "" {
			params.Clearance = &clearance
		}
		if shouldDisplayMutations() {
			params.Include = includeWriteDiff()
		}
		res, err := cubClientNew.BulkPatchUnitsWithBodyWithResponse(ctx, params,
			"application/merge-patch+json", bytes.NewReader(body))
		if cubapi.IsAPIError(err, res) {
			return withClearanceFlags(cubapi.InterpretErrorGeneric(err, res))
		}
		switch {
		case res.JSON200 != nil:
			responses = append(responses, *res.JSON200...)
		case res.JSON207 != nil:
			responses = append(responses, *res.JSON207...)
			failed = true
		default:
			return errors.New("unexpected response from the bulk patch API")
		}
	}

	statusCode := 200
	if failed {
		statusCode = 207
	}
	bulkErr := handleBulkCreateOrUpdateResponse(&responses, statusCode, "revert", "")
	for i := range responses {
		if response := &responses[i]; response.Error == nil && response.Unit != nil {
			displayWriteConflicts(response.Unit.SpaceID.String(), response.Unit.Slug, response.Conflicts, dryRun)
		}
	}
	if shouldDisplayMutations() {
		displayDiffsForUnitWrites(&responses, "revert "+revisions)
	}
	return bulkErr
}

// revertUnitWheres selects the units a revert acts on, as where clauses over units of at most
// revertUnitChunk units each: those the changeset, tag or release that ends the range marks, or
// that starts it when the end names none, within the space when one is given. A range naming
// none of them at either end is every unit of the space. base and end are the merge range as
// revertMergeRange formats it, so the range's end is base.
func revertUnitWheres(base, end, spaceID string) ([]string, error) {
	markWhere, err := revisionMarkWhere(base)
	if err != nil {
		return nil, err
	}
	if markWhere == "" {
		if markWhere, err = revisionMarkWhere(end); err != nil {
			return nil, err
		}
	}
	spaceClause := ""
	if spaceID != "" {
		spaceClause = fmt.Sprintf("SpaceID = '%s'", spaceID)
	}
	if markWhere == "" {
		if spaceClause == "" {
			return nil, errors.New("name a variant: the range marks no units to find them by, since neither end names a changeset, tag or release")
		}
		return []string{spaceClause}, nil
	}

	unitIDs, err := unitsMarked(markWhere, spaceClause)
	if err != nil {
		return nil, err
	}
	var wheres []string
	for chunk := range slices.Chunk(unitIDs, revertUnitChunk) {
		wheres = append(wheres, cubapi.NewWhere("").In("UnitID", chunk).String())
	}
	return wheres, nil
}

// unitsMarked is the units with a revision markWhere selects, in the space spaceClause names when
// it names one.
func unitsMarked(markWhere, spaceClause string) ([]goclientnew.UUID, error) {
	marked, err := cubapi.ListRevisions(ctx, cubClient, cubapi.NewWhere(markWhere).And(spaceClause), cubapi.ListOpts{
		Select:        "RevisionID,UnitID",
		IncludeHidden: "*",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find the units the change marks: %w", err)
	}
	var unitIDs []goclientnew.UUID
	for _, revision := range marked {
		if revision.Revision != nil && !slices.Contains(unitIDs, revision.Revision.UnitID) {
			unitIDs = append(unitIDs, revision.Revision.UnitID)
		}
	}
	return unitIDs, nil
}

// revisionMarkWhere is a where expression over revisions selecting those the entity a formatted
// revision specification names marks on each unit: a Tag's, a Release's, or a changeset's end tag,
// or its start tag while it is open. It is "" for a specification naming no entity.
func revisionMarkWhere(spec string) (string, error) {
	entityType, identifier, ok := strings.Cut(strings.TrimPrefix(spec, "Before:"), ":")
	if !ok {
		return "", nil
	}
	id, err := uuid.Parse(identifier)
	if err != nil {
		return "", fmt.Errorf("%s does not name an entity by ID", spec)
	}
	switch entityType {
	case "Tag":
		return fmt.Sprintf("Tags.%s IS NOT NULL", id), nil
	case "Release":
		return fmt.Sprintf("Releases.%s IS NOT NULL", id), nil
	case "ChangeSet":
		changeSets, err := cubapi.ListChangeSets(ctx, cubClient, cubapi.NewWhere(fmt.Sprintf("ChangeSetID = '%s'", id)),
			cubapi.ListOpts{Select: "ChangeSetID,Slug,State,StartTagID,EndTagID"})
		if err != nil {
			return "", fmt.Errorf("failed to get changeset: %w", err)
		}
		if len(changeSets) == 0 || changeSets[0].ChangeSet == nil {
			return "", fmt.Errorf("changeset %s not found", id)
		}
		// Both tags exist from the start, but the end tag marks nothing until the changeset closes.
		changeSet := changeSets[0].ChangeSet
		switch changeSet.State {
		case "Closed":
			return fmt.Sprintf("Tags.%s IS NOT NULL", changeSet.EndTagID), nil
		case "Open":
			return fmt.Sprintf("Tags.%s IS NOT NULL", changeSet.StartTagID), nil
		}
		return "", fmt.Errorf("changeset %s has no revisions to revert: it has not been opened", changeSet.Slug)
	}
	return "", nil
}

func revertScopeSuffix(spaceID string) string {
	if spaceID == "" {
		return ""
	}
	return " in the variant"
}
