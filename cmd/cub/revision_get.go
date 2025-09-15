// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var revisionGetCmd = &cobra.Command{
	Use:   "get <unit-slug> <revision-num>",
	Short: "Get details about a revision",
	Args:  cobra.ExactArgs(2),
	Long: `Get detailed information about a specific revision of a unit including its configuration data and metadata.

Examples:
  # Get details about a specific revision in JSON format
  cub revision get --space my-space --json my-deployment 3

  # Get only the configuration data of a revision
  cub revision get --space my-space --data-only my-ns 2

`,
	RunE: revisionGetCmdRun,
}

func init() {
	addStandardGetFlags(revisionGetCmd)
	revisionGetCmd.Flags().BoolVar(&dataOnly, "data-only", false, "show config data without other response details")
	revisionCmd.AddCommand(revisionGetCmd)
}

func revisionGetCmdRun(cmd *cobra.Command, args []string) error {
	unit, err := apiGetUnitFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	num, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return err
	}
	rev, err := apiGetExtendedRevisionFromNumber(num, unit.UnitID.String(), selectFields)
	if err != nil {
		return err
	}

	displayGetResults(rev, displayExtendedRevisionDetails)
	return nil
}

func displayRevisionDetails(rev *goclientnew.Revision) {
	// Create an ExtendedRevision wrapper with just the Revision set
	extendedRevision := &goclientnew.ExtendedRevision{
		Revision: rev,
		// All other fields (Unit, Space, etc.) will be nil, causing Extended display to show IDs
	}
	displayExtendedRevisionDetails(extendedRevision)
}

func displayExtendedRevisionDetails(extendedRev *goclientnew.ExtendedRevision) {
	rev := extendedRev.Revision
	if !dataOnly {
		view := tableView()
		view.Append([]string{"ID", rev.RevisionID.String()})

		// Show Unit slug instead of Unit ID when available
		if extendedRev.Unit != nil {
			view.Append([]string{"Unit", extendedRev.Unit.Slug})
		} else {
			view.Append([]string{"Unit ID", rev.UnitID.String()})
		}

		view.Append([]string{"Revision Num", fmt.Sprintf("%d", rev.RevisionNum)})
		view.Append([]string{"Source", rev.Source})
		view.Append([]string{"Description", rev.Description})
		view.Append([]string{"Created At", rev.CreatedAt.String()})
		view.Append([]string{"Live At", rev.LiveAt.String()})
		view.Append([]string{"User ID", rev.UserID.String()})

		// Show Space slug instead of Space ID when available
		if extendedRev.Space != nil {
			view.Append([]string{"Space", extendedRev.Space.Slug})
		} else {
			view.Append([]string{"Space ID", rev.SpaceID.String()})
		}

		// Show ChangeSet slug if available, or ChangeSetID if not nil and not uuid.Nil
		if extendedRev.ChangeSet != nil {
			view.Append([]string{"ChangeSet", extendedRev.ChangeSet.Slug})
		} else if rev.ChangeSetID != nil && *rev.ChangeSetID != uuid.Nil {
			view.Append([]string{"ChangeSet ID", rev.ChangeSetID.String()})
		}

		view.Append([]string{"Organization ID", rev.OrganizationID.String()})

		if rev.ApplyGates != nil && len(rev.ApplyGates) != 0 {
			gates := ""
			for gate, failed := range rev.ApplyGates {
				if failed {
					gates += gate + " "
				}
			}
			view.Append([]string{"Apply Gates", strings.TrimSpace(gates)})
		}
		if len(rev.ApprovedBy) != 0 {
			approverIDs := ""
			for _, approverID := range rev.ApprovedBy {
				approverIDs += " " + approverID.String()
			}
			view.Append([]string{"Approved By", strings.TrimSpace(approverIDs)})
		}
		// Display tags if present
		if extendedRev.Tags != nil && len(extendedRev.Tags) > 0 {
			var tagSlugs []string
			for _, tag := range extendedRev.Tags {
				if tag.Slug != "" {
					tagSlugs = append(tagSlugs, tag.Slug)
				}
			}
			if len(tagSlugs) > 0 {
				view.Append([]string{"Tags", strings.Join(tagSlugs, ", ")})
			}
		} else if rev.Tags != nil && len(rev.Tags) > 0 {
			tagSlugs := resolveTagSlugs(rev.Tags, rev.SpaceID.String())
			view.Append([]string{"Tags", strings.Join(tagSlugs, ", ")})
		}
		view.Render()
		tprintRaw("---")
		if len(*rev.MutationSources) != 0 {
			tprintRaw("Mutation Sources:")
			// TODO: Make this prettier
			displayJSON(rev.MutationSources)
			tprintRaw("---")
		}
	}
	data, err := base64.StdEncoding.DecodeString(rev.Data)
	failOnError(err)
	tprintRaw(string(data))
}

func apiGetRevision(revisionID string, unitID string, selectParam string) (*goclientnew.Revision, error) {
	extendedRev, err := apiGetExtendedRevision(revisionID, unitID, selectParam)
	if err != nil {
		return nil, err
	}
	return extendedRev.Revision, nil
}

func apiGetExtendedRevision(revisionID string, unitID string, selectParam string) (*goclientnew.ExtendedRevision, error) {
	newParams := &goclientnew.GetExtendedRevisionParams{}
	include := "UnitID,SpaceID,ChangeSetID,Tags"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	revRes, err := cubClientNew.GetExtendedRevisionWithResponse(ctx,
		uuid.MustParse(selectedSpaceID),
		uuid.MustParse(unitID),
		uuid.MustParse(revisionID),
		newParams,
	)
	if cubapi.IsAPIError(err, revRes) {
		return nil, cubapi.InterpretErrorGeneric(err, revRes)
	}
	if revRes.JSON200.Revision.SpaceID.String() != selectedSpaceID {
		return nil, fmt.Errorf("SERVER DIDN'T CHECK: revision %s not found", revisionID)
	}

	return revRes.JSON200, nil
}

func apiGetRevisionFromNumber(revNo int64, unitID string, selectParam string) (*goclientnew.Revision, error) {
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	revisions, err := apiListRevisions(selectedSpaceID, unitID, fmt.Sprintf("RevisionNum = %d", revNo), selectParam, "")
	if err != nil {
		return nil, err
	}
	for _, extendedRev := range revisions {
		if int64(extendedRev.Revision.RevisionNum) == revNo {
			return extendedRev.Revision, nil
		}
	}
	return nil, fmt.Errorf("rev %d of unit %s not found in space %s", revNo, unitID, selectedSpaceSlug)
}

func apiGetExtendedRevisionFromNumber(revNo int64, unitID string, selectParam string) (*goclientnew.ExtendedRevision, error) {
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	revisions, err := apiListRevisions(selectedSpaceID, unitID, fmt.Sprintf("RevisionNum = %d", revNo), selectParam, "")
	if err != nil {
		return nil, err
	}
	for _, extendedRev := range revisions {
		if int64(extendedRev.Revision.RevisionNum) == revNo {
			return extendedRev, nil
		}
	}
	return nil, fmt.Errorf("rev %d of unit %s not found in space %s", revNo, unitID, selectedSpaceSlug)
}

// resolveTagSlugs converts a map of tag IDs to their slugs
func resolveTagSlugs(tags map[string]string, spaceID string) []string {
	if len(tags) == 0 {
		return []string{}
	}

	var tagSlugs []string
	for tagID := range tags {
		// Try to get tag by ID and resolve to slug
		parsedTagID, err := uuid.Parse(tagID)
		if err != nil {
			// If parsing fails, use the tagID as-is
			tagSlugs = append(tagSlugs, tagID)
			continue
		}

		// Search for the tag at organization level using ListAllTags
		whereClause := fmt.Sprintf("TagID = '%s'", parsedTagID.String())
		params := &goclientnew.ListAllTagsParams{
			Where: &whereClause,
		}

		tagRes, err := cubClientNew.ListAllTagsWithResponse(ctx, params)
		if err != nil || tagRes.JSON200 == nil || len(*tagRes.JSON200) == 0 {
			// If we can't resolve the tag, use the UUID
			tagSlugs = append(tagSlugs, tagID)
			continue
		}

		// Get the first (should be only) tag found
		foundTags := *tagRes.JSON200
		if len(foundTags) > 0 && foundTags[0].Tag != nil && foundTags[0].Tag.Slug != "" {
			tagSlugs = append(tagSlugs, foundTags[0].Tag.Slug)
		} else {
			// Fallback to tag ID if no slug
			tagSlugs = append(tagSlugs, tagID)
		}
	}

	return tagSlugs
}
