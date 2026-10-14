// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// The review requests --request-review and --withdraw-review-request name, as [<stage>:]<user>.
var (
	changeorderRequestReviews         []string
	changeorderWithdrawReviewRequests []string
)

const requestReviewFlagHelp = "ask a user (username or ID) to review the change, as [<stage>:]<user>: where it stands in that stage's spaces, or with no stage in the change order's own space. Asking again re-requests the review (can be repeated or comma-separated)"

func enableRequestReviewFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&changeorderRequestReviews, "request-review", nil, requestReviewFlagHelp)
}

func enableWithdrawReviewRequestFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&changeorderWithdrawReviewRequests, "withdraw-review-request", nil,
		"withdraw a review request, named as [<stage>:]<user> (can be repeated or comma-separated)")
}

func reviewRequestFlagsGiven() bool {
	return len(changeorderRequestReviews) > 0 || len(changeorderWithdrawReviewRequests) > 0
}

// parseReviewRequestFlag splits [<stage>:]<user> and resolves the user.
func parseReviewRequestFlag(value string) (stage string, userID uuid.UUID, err error) {
	userRef := value
	if i := strings.Index(value, ":"); i >= 0 {
		stage, userRef = value[:i], value[i+1:]
	}
	if userRef == "" {
		return "", uuid.Nil, errors.Errorf("review request %q names no user; use [<stage>:]<user>", value)
	}
	user, err := resolveUserCore(userRef)
	if err != nil {
		return "", uuid.Nil, errors.Wrapf(err, "failed to resolve user %q", userRef)
	}
	return stage, user.UserID, nil
}

// applyReviewRequestFlags returns the change order's review requests with the flags applied. The
// list is written back whole, since a merge patch replaces an array. A new request is sent without
// a time, which the server stamps. Asking again for a review already requested sends a time later
// than the one recorded, which the server takes as a re-request and stamps again.
func applyReviewRequestFlags(current []goclientnew.ChangeOrderReviewRequest) ([]goclientnew.ChangeOrderReviewRequest, error) {
	requests := append([]goclientnew.ChangeOrderReviewRequest{}, current...)
	find := func(stage string, userID uuid.UUID) int {
		for i := range requests {
			if requests[i].Stage == stage && requests[i].UserID == userID {
				return i
			}
		}
		return -1
	}
	for _, value := range changeorderWithdrawReviewRequests {
		stage, userID, err := parseReviewRequestFlag(value)
		if err != nil {
			return nil, err
		}
		i := find(stage, userID)
		if i < 0 {
			return nil, errors.Errorf("no review was requested of %s %s", value, reviewRequestPlace(stage))
		}
		requests = append(requests[:i], requests[i+1:]...)
	}
	for _, value := range changeorderRequestReviews {
		stage, userID, err := parseReviewRequestFlag(value)
		if err != nil {
			return nil, err
		}
		if i := find(stage, userID); i >= 0 {
			later := time.Now().UTC()
			if !later.After(requests[i].RequestedAt) {
				later = requests[i].RequestedAt.Add(time.Second)
			}
			requests[i].RequestedAt = later
			continue
		}
		requests = append(requests, goclientnew.ChangeOrderReviewRequest{Stage: stage, UserID: userID})
	}
	return requests, nil
}

func reviewRequestPlace(stage string) string {
	if stage == "" {
		return "in the change order's own space"
	}
	return fmt.Sprintf("in stage %s", stage)
}

// reviewRequestsSummary describes the review requests for `cub changeorder get`, one per line.
func reviewRequestsSummary(requests []goclientnew.ChangeOrderReviewRequest) string {
	lines := make([]string, 0, len(requests))
	for _, request := range requests {
		place := "own space"
		if request.Stage != "" {
			place = "stage " + request.Stage
		}
		lines = append(lines, fmt.Sprintf("%s: %s, requested %s", place, request.UserID, request.RequestedAt.Format(time.RFC3339)))
	}
	return strings.Join(lines, "\n")
}
