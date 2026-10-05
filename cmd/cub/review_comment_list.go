// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var reviewCommentListCmd = &cobra.Command{
	Use:   "list <unit>",
	Short: "List review comments",
	Long: getCommandHelp(`List the review comments on a unit, oldest first.

Examples:
`+"```"+`
  # List the review comments on a unit
  cub review-comment list my-space/my-deployment

  # List the review comments on one revision
  cub review-comment list --space my-space --where 'RevisionNum = 4' my-deployment

  # List the comments that start a thread
  cub review-comment list --space my-space --where 'ReplyToID IS NULL' my-deployment

  # List the replies to a comment
  cub review-comment list --space my-space --where "ReplyToID = '61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c'" my-deployment
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: reviewCommentListCmdRun,
}

// Default columns to display when no custom columns are specified. This is also what drives the
// select list.
var defaultReviewCommentColumns = []string{"ReviewComment.ReviewCommentID", "ReviewComment.RevisionNum", "ReviewComment.ReplyToID", "ReviewComment.Path", "ReviewComment.CreatedAt", "ReviewComment.Text"}

var reviewCommentAliases = map[string]string{
	"Name": "ReviewComment.ReviewCommentID",
	"ID":   "ReviewComment.ReviewCommentID",
}

var reviewCommentCustomColumnDependencies = map[string][]string{}

// maxReviewCommentListText is where the Text column is cut. `cub review-comment get` and -o json
// show the whole text.
const maxReviewCommentListText = 60

func init() {
	addStandardListFlags(reviewCommentListCmd)
	enableListPagingFlags(reviewCommentListCmd)
	enableOptionalSpace(reviewCommentListCmd)
	reviewCommentCmd.AddCommand(reviewCommentListCmd)
}

func reviewCommentListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}
	resolved, err := resolveUnit(args[0], selectedSpaceID, "*")
	if err != nil {
		return err
	}
	unit := resolved.Unit
	reviewComments, err := apiListReviewComments(unit.SpaceID, unit.UnitID, where, selectFields, filterID)
	if err != nil {
		return err
	}
	displayListResults(reviewComments, getReviewCommentIDFromExtended, displayReviewCommentList)
	return nil
}

func getReviewCommentIDFromExtended(extendedReviewComment *goclientnew.ExtendedReviewComment) string {
	return extendedReviewComment.ReviewComment.ReviewCommentID.String()
}

func displayReviewCommentList(extendedReviewComments []*goclientnew.ExtendedReviewComment) {
	if displayRequestedColumns(extendedReviewComments, reviewCommentAliases, nil) {
		return
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"ID", "RevisionNum", "ReplyTo", "Path", "Time", "Text"})
	}
	for _, extendedReviewComment := range extendedReviewComments {
		reviewComment := extendedReviewComment.ReviewComment
		replyTo := ""
		if isSetUUID(reviewComment.ReplyToID) {
			replyTo = reviewComment.ReplyToID.String()
		}
		table.Append([]string{
			reviewComment.ReviewCommentID.String(),
			fmt.Sprintf("%d", reviewComment.RevisionNum),
			replyTo,
			truncateWithEllipsis(reviewComment.Path, maxMutationProvidedPath),
			reviewComment.CreatedAt.Format("2006-01-02 15:04:05"),
			truncateWithEllipsis(reviewComment.Text, maxReviewCommentListText),
		})
	}
	table.Render()
}

func apiListReviewComments(spaceID, unitID uuid.UUID, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedReviewComment, error) {
	params := &goclientnew.ListReviewCommentsParams{}
	if whereFilter != "" {
		params.Where = &whereFilter
	}
	if filterParam != "" {
		params.Filter = &filterParam
	}
	if contains != "" {
		params.Contains = &contains
	}
	if includeHidden != "" {
		params.IncludeHidden = &includeHidden
	}
	include := "SpaceID,UnitID"
	params.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"ReviewCommentID", "UnitID", "SpaceID", "OrganizationID"}
		return buildSelectList("ReviewComment", listColumnsFor("cub review-comment list"), include, defaultReviewCommentColumns, reviewCommentAliases, reviewCommentCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		params.Select = &selectValue
	}
	opts := listPageOpts("ASC:CreatedAt")
	if opts.OrderBy != "" {
		params.OrderBy = &opts.OrderBy
	}
	return cubapi.ReadPages(opts, func(limit *int, token *string) (*http.Response, *[]goclientnew.ExtendedReviewComment, error) {
		params.Limit, params.Continue = limit, token
		res, err := cubClientNew.ListReviewCommentsWithResponse(ctx, spaceID, unitID, params)
		if cubapi.IsAPIError(err, res) {
			return nil, nil, cubapi.InterpretErrorGeneric(err, res)
		}
		return res.HTTPResponse, res.JSON200, nil
	})
}
