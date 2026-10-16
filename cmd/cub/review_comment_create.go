// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var reviewCommentCreateCmd = &cobra.Command{
	Use:   "create <unit>",
	Short: "Create a review comment",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Create a review comment on a revision of a unit.

The comment can be about one resource, named as <resource-type>:<resource-name>, and one path in
it. With --reply-to it answers another review comment on the same unit.

With --change-order the comment is part of the review of a change order. That is for a unit the
change order will change but whose revisions do not record the change yet: one it promotes a
change made elsewhere into, or one it invokes a function on. A reply takes the change order of the
comment it answers. A comment on a change set needs no --change-order, since the change set's
revisions are marked with it.

Examples:
`+"```"+`
  # Comment on revision 4 of a unit
  cub review-comment create my-space/my-deployment --revision-num 4 --text "Why 5 replicas?"

  # Comment on one path of one resource
  cub review-comment create my-space/my-deployment --revision-num 4 \
    --resource apps/v1/Deployment:default/web --path spec.replicas --text "Why 5 replicas?"

  # Comment on the revision a change order will change
  cub review-comment create prod/my-deployment --revision-num 7 \
    --change-order dev/raise-replicas --text "Is prod sized for this?"

  # Reply to a comment
  cub review-comment create my-space/my-deployment --revision-num 4 \
    --reply-to 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c --text "Load test results; see the change description."
`+"```"+`
`, ""),
	RunE: reviewCommentCreateCmdRun,
}

var reviewCommentCreateArgs struct {
	revisionNum int64
	text        string
	replyTo     string
	resource    string
	path        string
	changeOrder string
}

func init() {
	enableDryRunFlag(reviewCommentCreateCmd)
	enableFromStdinFlag(reviewCommentCreateCmd)
	enableFilenameFlag(reviewCommentCreateCmd)
	addStandardDisplayFlags(reviewCommentCreateCmd)
	enableOptionalSpace(reviewCommentCreateCmd)
	reviewCommentCreateCmd.Flags().Int64Var(&reviewCommentCreateArgs.revisionNum, "revision-num", 0, "number of the revision the comment is about (required)")
	reviewCommentCreateCmd.Flags().StringVar(&reviewCommentCreateArgs.text, "text", "", "text of the comment (required)")
	reviewCommentCreateCmd.Flags().StringVar(&reviewCommentCreateArgs.replyTo, "reply-to", "", "ID of the review comment, on the same unit, this one replies to")
	reviewCommentCreateCmd.Flags().StringVar(&reviewCommentCreateArgs.resource, "resource", "", "resource the comment is about, as <resource-type>:<resource-name>, e.g. apps/v1/Deployment:default/web")
	reviewCommentCreateCmd.Flags().StringVar(&reviewCommentCreateArgs.path, "path", "", "path, within the resource, the comment is about")
	reviewCommentCreateCmd.Flags().StringVar(&reviewCommentCreateArgs.changeOrder, "change-order", "", "change order the comment is part of the review of, as <space>/<slug> or an ID; a bare slug resolves in the selected space, and a change order resides where its change was made, which is often not the unit's space")
	reviewCommentCmd.AddCommand(reviewCommentCreateCmd)
}

// parseReviewCommentResource parses --resource: <resource-type>:<resource-name>.
func parseReviewCommentResource(value string) (*goclientnew.ResourceInfoType2, error) {
	resourceType, resourceName, ok := strings.Cut(value, ":")
	if !ok || resourceType == "" || resourceName == "" {
		return nil, fmt.Errorf("--resource %q: expected <resource-type>:<resource-name>", value)
	}
	return &goclientnew.ResourceInfoType2{ResourceType: resourceType, ResourceName: resourceName}, nil
}

func reviewCommentCreateCmdRun(cmd *cobra.Command, args []string) error {
	if err := validateStdinFlags(); err != nil {
		return err
	}
	resolved, err := resolveUnit(args[0], selectedSpaceID, "*")
	if err != nil {
		return err
	}
	unit := resolved.Unit

	newBody := goclientnew.ReviewComment{}
	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(&newBody); err != nil {
			return err
		}
	}
	if reviewCommentCreateArgs.revisionNum != 0 {
		newBody.RevisionNum = reviewCommentCreateArgs.revisionNum
	}
	if reviewCommentCreateArgs.text != "" {
		newBody.Text = reviewCommentCreateArgs.text
	}
	if reviewCommentCreateArgs.replyTo != "" {
		replyToID, err := uuid.Parse(reviewCommentCreateArgs.replyTo)
		if err != nil {
			return fmt.Errorf("--reply-to %q is not a review comment ID: %w", reviewCommentCreateArgs.replyTo, err)
		}
		newBody.ReplyToID = &replyToID
	}
	if reviewCommentCreateArgs.resource != "" {
		resource, err := parseReviewCommentResource(reviewCommentCreateArgs.resource)
		if err != nil {
			return err
		}
		newBody.Resource = resource
	}
	if reviewCommentCreateArgs.path != "" {
		newBody.Path = reviewCommentCreateArgs.path
	}
	if reviewCommentCreateArgs.changeOrder != "" {
		changeOrderID, err := resolveChangeOrderID(reviewCommentCreateArgs.changeOrder)
		if err != nil {
			return err
		}
		newBody.ChangeOrderID = &changeOrderID
	}
	if newBody.RevisionNum == 0 {
		return fmt.Errorf("--revision-num is required")
	}
	if newBody.Text == "" {
		return fmt.Errorf("--text is required")
	}

	params := &goclientnew.CreateReviewCommentParams{DryRun: dryRunParam()}
	res, err := cubClientNew.CreateReviewCommentWithResponse(ctx, unit.SpaceID, unit.UnitID, params, newBody)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	reviewComment := res.JSON200
	displayCreateResults(reviewComment, "review comment", reviewComment.ReviewCommentID.String(), reviewComment.ReviewCommentID.String(), displayReviewCommentDetails)
	return nil
}
