// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var reviewCommentGetCmd = &cobra.Command{
	Use:   "get <unit> <review-comment-id>",
	Short: "Get details about a review comment",
	Args:  cobra.ExactArgs(2),
	Long: getCommandHelp(`Get detailed information about a review comment on a unit.

Examples:
`+"```"+`
  # Get details about a review comment
  cub review-comment get my-space/my-deployment 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c

  # Get details about a review comment in JSON format
  cub review-comment get --space my-space -o json my-deployment 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c
`+"```"+`
`, ""),
	RunE: reviewCommentGetCmdRun,
}

func init() {
	addStandardGetFlags(reviewCommentGetCmd)
	enableOptionalSpace(reviewCommentGetCmd)
	reviewCommentCmd.AddCommand(reviewCommentGetCmd)
}

func reviewCommentGetCmdRun(cmd *cobra.Command, args []string) error {
	unit, reviewCommentID, err := resolveReviewCommentArgs(args)
	if err != nil {
		return err
	}
	extendedReviewComment, err := apiGetReviewComment(unit.SpaceID, unit.UnitID, reviewCommentID, selectFields)
	if err != nil {
		return err
	}
	displayGetResults(extendedReviewComment, displayExtendedReviewCommentDetails)
	return nil
}

// resolveReviewCommentArgs resolves the <unit> <review-comment-id> operands of a command. The
// comment is then addressed in the unit's space, not the selected one.
func resolveReviewCommentArgs(args []string) (*goclientnew.Unit, uuid.UUID, error) {
	resolved, err := resolveUnit(args[0], selectedSpaceID, "*")
	if err != nil {
		return nil, uuid.Nil, err
	}
	reviewCommentID, err := uuid.Parse(args[1])
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("review comment ID %q is not a UUID: %w", args[1], err)
	}
	return resolved.Unit, reviewCommentID, nil
}

func displayReviewCommentDetails(reviewComment *goclientnew.ReviewComment) {
	displayExtendedReviewCommentDetails(&goclientnew.ExtendedReviewComment{ReviewComment: reviewComment})
}

func displayExtendedReviewCommentDetails(extendedReviewComment *goclientnew.ExtendedReviewComment) {
	reviewComment := extendedReviewComment.ReviewComment
	view := tableView()
	view.Append([]string{"ID", reviewComment.ReviewCommentID.String()})

	if extendedReviewComment.Unit != nil {
		view.Append([]string{"Unit", extendedReviewComment.Unit.Slug})
	} else if reviewComment.UnitSlug != "" {
		view.Append([]string{"Unit", reviewComment.UnitSlug})
	} else {
		view.Append([]string{"Unit ID", reviewComment.UnitID.String()})
	}

	if extendedReviewComment.Space != nil {
		view.Append([]string{"Space", extendedReviewComment.Space.Slug})
	} else if reviewComment.SpaceSlug != "" {
		view.Append([]string{"Space", reviewComment.SpaceSlug})
	} else {
		view.Append([]string{"Space ID", reviewComment.SpaceID.String()})
	}

	view.Append([]string{"Revision Num", fmt.Sprintf("%d", reviewComment.RevisionNum)})
	if resource := reviewComment.Resource; resource != nil {
		if resource.ResourceType != "" {
			view.Append([]string{"Resource Type", resource.ResourceType})
		}
		if resource.ResourceName != "" {
			view.Append([]string{"Resource Name", resource.ResourceName})
		}
	}
	if reviewComment.Path != "" {
		view.Append([]string{"Path", reviewComment.Path})
	}
	if isSetUUID(reviewComment.ReplyToID) {
		view.Append([]string{"Reply To", reviewComment.ReplyToID.String()})
	}
	if reviewComment.UserID != uuid.Nil {
		view.Append([]string{"User ID", reviewComment.UserID.String()})
	}
	if reviewComment.UserAgent != "" {
		view.Append([]string{"User Agent", reviewComment.UserAgent})
	}
	view.Append([]string{"Created At", reviewComment.CreatedAt.String()})
	view.Append([]string{"Updated At", reviewComment.UpdatedAt.String()})
	view.Append([]string{"Organization ID", reviewComment.OrganizationID.String()})
	view.Render()

	tprintRaw("")
	tprintRaw(reviewComment.Text)
}

func apiGetReviewComment(spaceID, unitID, reviewCommentID uuid.UUID, selectParam string) (*goclientnew.ExtendedReviewComment, error) {
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	include := "SpaceID,UnitID"
	params := &goclientnew.GetReviewCommentParams{Include: &include}
	if selectParam != "*" {
		params.Select = &selectParam
	}
	res, err := cubClientNew.GetReviewCommentWithResponse(ctx, spaceID, unitID, reviewCommentID, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}
