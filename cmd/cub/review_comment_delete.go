// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var reviewCommentDeleteCmd = &cobra.Command{
	Use:   "delete [<unit> <review-comment-id>]",
	Short: "Delete a review comment or multiple review comments",
	Long: getCommandHelp(`Delete a review comment, or multiple review comments using bulk operations.

Only the user who made a review comment, or a user who manages its space, can delete it. Replies to
a deleted comment are kept.

Single review comment delete:
`+"```"+`
  cub review-comment delete my-space/my-deployment 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c
`+"```"+`

Bulk delete with --where:

Examples:
`+"```"+`
  # Delete the review comments on one revision of a unit
  cub review-comment delete --where "UnitID = '<unit-id>' AND RevisionNum = 4"

  # Delete the review comments made before a date, in one space
  cub review-comment delete --space my-space --where "CreatedAt < '2026-01-01'"
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(2), // Allow 0 args for bulk mode
	RunE:        reviewCommentDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	addStandardDeleteFlags(reviewCommentDeleteCmd)
	enableWhereFlag(reviewCommentDeleteCmd)
	enableFilterFlag(reviewCommentDeleteCmd)
	reviewCommentCmd.AddCommand(reviewCommentDeleteCmd)
}

func checkReviewCommentDeleteConflictingArgs(args []string) bool {
	isBulkDeleteMode := len(args) == 0
	if !isBulkDeleteMode {
		if len(args) != 2 {
			failOnError(fmt.Errorf("single review comment delete requires exactly two arguments: <unit> <review-comment-id>"))
		}
		if filter != "" || where != "" {
			failOnError(fmt.Errorf("--filter or --where can only be specified with no positional arguments"))
		}
	}
	return isBulkDeleteMode
}

func runBulkReviewCommentDelete() error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}
	effectiveWhere := addSpaceIDToWhereClause(where, selectedSpaceID)

	include := "SpaceID,UnitID"
	params := &goclientnew.BulkDeleteReviewCommentsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	params.IncludeHidden = includeHiddenParam()
	if filterID != "" {
		params.Filter = &filterID
	}
	bulkRes, err := cubClientNew.BulkDeleteReviewCommentsWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}
	return displayBulkDeleteResults(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "review comment", "delete", effectiveWhere)
}

func reviewCommentDeleteCmdRun(cmd *cobra.Command, args []string) error {
	if checkReviewCommentDeleteConflictingArgs(args) {
		return runBulkReviewCommentDelete()
	}

	unit, reviewCommentID, err := resolveReviewCommentArgs(args)
	if err != nil {
		return err
	}
	res, err := cubClientNew.DeleteReviewCommentWithResponse(ctx, unit.SpaceID, unit.UnitID, reviewCommentID)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	displayDeleteResults("review comment", args[1], reviewCommentID.String(), res)
	return nil
}
