// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var reviewCommentUpdateCmd = &cobra.Command{
	Use:   "update [<unit> <review-comment-id>] [options...]",
	Short: "Update a review comment or multiple review comments",
	Long: getCommandHelp(`Update the text of a review comment, or of multiple review comments using bulk operations.

Only the text of a review comment can change, and only the user who made it, or a user who manages
its space, can change it.

Single review comment update:
`+"```"+`
  cub review-comment update my-space/my-deployment 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c --text "Why 5 replicas, not 3?"
`+"```"+`

Bulk update with --patch:

Update multiple review comments at once based on search criteria. Requires --patch flag with no
positional arguments.

Examples:
`+"```"+`
  # Replace the text of the comments on one revision of a unit
  cub review-comment update --patch --where "UnitID = '<unit-id>' AND RevisionNum = 4" --text "Superseded by revision 5."
`+"```"+`
`, ""),
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        reviewCommentUpdateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	reviewCommentPatch      bool
	reviewCommentUpdateText string
)

func init() {
	enableDryRunFlag(reviewCommentUpdateCmd)
	enableFromStdinFlag(reviewCommentUpdateCmd)
	enableReplaceFlag(reviewCommentUpdateCmd)
	enableFilenameFlag(reviewCommentUpdateCmd)
	addStandardDisplayFlags(reviewCommentUpdateCmd)
	reviewCommentUpdateCmd.Flags().BoolVar(&reviewCommentPatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(reviewCommentUpdateCmd)
	enableFilterFlag(reviewCommentUpdateCmd)
	reviewCommentUpdateCmd.Flags().StringVar(&reviewCommentUpdateText, "text", "", "text of the comment")
	reviewCommentCmd.AddCommand(reviewCommentUpdateCmd)
}

func checkReviewCommentUpdateConflictingArgs(args []string) bool {
	isBulkPatchMode := len(args) == 0

	if isBulkPatchMode {
		if !reviewCommentPatch {
			failOnError(errors.New("--patch is required in bulk mode"))
		}
	} else {
		if len(args) != 2 {
			failOnError(errors.New("single review comment update requires exactly two arguments: <unit> <review-comment-id>"))
		}
		if filter != "" || where != "" {
			failOnError(fmt.Errorf("--filter or --where can only be specified with --patch and no positional arguments"))
		}
	}

	if reviewCommentPatch && flagReplace {
		failOnError(fmt.Errorf("only one of --patch and --replace should be specified"))
	}
	if err := validateStdinFlags(); err != nil {
		failOnError(err)
	}
	return isBulkPatchMode
}

func reviewCommentPatchEnhancer(patchData map[string]interface{}) {
	if reviewCommentUpdateText != "" {
		patchData["Text"] = reviewCommentUpdateText
	}
}

func runBulkReviewCommentUpdate() error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}
	effectiveWhere := addSpaceIDToWhereClause(where, selectedSpaceID)

	patchJSON, err := BuildPatchData(reviewCommentPatchEnhancer)
	if err != nil {
		return err
	}

	include := "SpaceID,UnitID"
	params := &goclientnew.BulkPatchReviewCommentsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	params.IncludeHidden = includeHiddenParam()
	if filterID != "" {
		params.Filter = &filterID
	}
	params.DryRun = dryRunParam()
	bulkRes, err := cubClientNew.BulkPatchReviewCommentsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}
	return handleBulkReviewCommentCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "update", effectiveWhere)
}

func reviewCommentUpdateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkPatchMode := checkReviewCommentUpdateConflictingArgs(args)
	if isBulkPatchMode {
		return runBulkReviewCommentUpdate()
	}

	unit, reviewCommentID, err := resolveReviewCommentArgs(args)
	if err != nil {
		return err
	}

	if reviewCommentPatch {
		patchData, err := BuildPatchData(reviewCommentPatchEnhancer)
		if err != nil {
			return fmt.Errorf("failed to build patch data: %w", err)
		}
		res, err := cubClientNew.PatchReviewCommentWithBodyWithResponse(
			ctx,
			unit.SpaceID,
			unit.UnitID,
			reviewCommentID,
			&goclientnew.PatchReviewCommentParams{DryRun: dryRunParam()},
			"application/merge-patch+json",
			bytes.NewReader(patchData),
		)
		if cubapi.IsAPIError(err, res) {
			return cubapi.InterpretErrorGeneric(err, res)
		}
		displayUpdateResults(res.JSON200, "review comment", args[1], res.JSON200.ReviewCommentID.String(), displayReviewCommentDetails)
		return nil
	}

	// Read-modify-write
	current, err := apiGetReviewComment(unit.SpaceID, unit.UnitID, reviewCommentID, "*")
	if err != nil {
		return err
	}
	reviewComment := current.ReviewComment
	if flagPopulateModelFromStdin || flagFilename != "" {
		existing := reviewComment
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			reviewComment = new(goclientnew.ReviewComment)
			reviewComment.Version = existing.Version
		}
		if err := populateModelFromFlags(reviewComment); err != nil {
			return err
		}
		// Ensure essential fields can't be clobbered
		reviewComment.OrganizationID = existing.OrganizationID
		reviewComment.SpaceID = existing.SpaceID
		reviewComment.UnitID = existing.UnitID
		reviewComment.ReviewCommentID = existing.ReviewCommentID
	}
	if reviewCommentUpdateText != "" {
		reviewComment.Text = reviewCommentUpdateText
	}

	res, err := cubClientNew.UpdateReviewCommentWithResponse(ctx, unit.SpaceID, unit.UnitID, reviewCommentID,
		&goclientnew.UpdateReviewCommentParams{DryRun: dryRunParam()}, *reviewComment)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	displayUpdateResults(res.JSON200, "review comment", args[1], res.JSON200.ReviewCommentID.String(), displayReviewCommentDetails)
	return nil
}

func handleBulkReviewCommentCreateOrUpdateResponse(responses200 *[]goclientnew.ReviewCommentCreateOrUpdateResponse, responses207 *[]goclientnew.ReviewCommentCreateOrUpdateResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkGenericCreateOrUpdateResults(
		responses200, responses207, statusCode, "review comment", operationName, contextInfo,
		func(r *goclientnew.ReviewCommentCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.ReviewCommentCreateOrUpdateResponse) string {
			if r.ReviewComment != nil {
				return r.ReviewComment.ReviewCommentID.String()
			}
			return ""
		},
	)
}
