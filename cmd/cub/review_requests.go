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

// The review requests --request-review and --withdraw-review-request name: [<stage>:]<user> for a
// change order, <user> for a change set.
var (
	requestReviewFlags         []string
	withdrawReviewRequestFlags []string
)

const requestReviewFlagHelp = "ask a user (username or ID) to review the change, as [<stage>:]<user>: where it stands in that stage's spaces, or with no stage in the change order's own space. Asking again re-requests the review (can be repeated or comma-separated)"

func enableRequestReviewFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&requestReviewFlags, "request-review", nil, requestReviewFlagHelp)
}

func enableWithdrawReviewRequestFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&withdrawReviewRequestFlags, "withdraw-review-request", nil,
		"withdraw a review request, named as [<stage>:]<user> (can be repeated or comma-separated)")
}

func enableChangeSetRequestReviewFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&requestReviewFlags, "request-review", nil,
		"ask a user (username or ID) to review the change set. Asking again re-requests the review (can be repeated or comma-separated)")
}

func enableChangeSetWithdrawReviewRequestFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&withdrawReviewRequestFlags, "withdraw-review-request", nil,
		"withdraw the review requested of a user (username or ID) (can be repeated or comma-separated)")
}

func reviewRequestFlagsGiven() bool {
	return len(requestReviewFlags) > 0 || len(withdrawReviewRequestFlags) > 0
}

// reviewRequestKey names one review request: who is asked, and for a change order, in which stage.
type reviewRequestKey struct {
	stage  string
	userID uuid.UUID
}

// parseReviewRequestFlag splits [<stage>:]<user> and resolves the user.
func parseReviewRequestFlag(value string) (reviewRequestKey, error) {
	userRef := value
	stage := ""
	if i := strings.Index(value, ":"); i >= 0 {
		stage, userRef = value[:i], value[i+1:]
	}
	if userRef == "" {
		return reviewRequestKey{}, errors.Errorf("review request %q names no user; use [<stage>:]<user>", value)
	}
	userID, err := resolveReviewer(userRef)
	return reviewRequestKey{stage: stage, userID: userID}, err
}

// parseChangeSetReviewRequestFlag resolves the user a change set's review request names.
func parseChangeSetReviewRequestFlag(value string) (reviewRequestKey, error) {
	userID, err := resolveReviewer(value)
	return reviewRequestKey{userID: userID}, err
}

func resolveReviewer(userRef string) (uuid.UUID, error) {
	user, err := resolveUserCore(userRef)
	if err != nil {
		return uuid.Nil, errors.Wrapf(err, "failed to resolve user %q", userRef)
	}
	return user.UserID, nil
}

// applyReviewRequestFlags returns the change order's review requests with the flags applied.
func applyReviewRequestFlags(current []goclientnew.ChangeOrderReviewRequest) ([]goclientnew.ChangeOrderReviewRequest, error) {
	return applyReviewRequestEdits(current, parseReviewRequestFlag,
		func(request *goclientnew.ChangeOrderReviewRequest) (reviewRequestKey, *time.Time) {
			return reviewRequestKey{request.Stage, request.UserID}, &request.RequestedAt
		},
		func(key reviewRequestKey) goclientnew.ChangeOrderReviewRequest {
			return goclientnew.ChangeOrderReviewRequest{Stage: key.stage, UserID: key.userID}
		},
		func(key reviewRequestKey) string { return " " + reviewRequestPlace(key.stage) })
}

// applyChangeSetReviewRequestFlags returns the change set's review requests with the flags applied.
func applyChangeSetReviewRequestFlags(current []goclientnew.ChangeSetReviewRequest) ([]goclientnew.ChangeSetReviewRequest, error) {
	return applyReviewRequestEdits(current, parseChangeSetReviewRequestFlag,
		func(request *goclientnew.ChangeSetReviewRequest) (reviewRequestKey, *time.Time) {
			return reviewRequestKey{userID: request.UserID}, &request.RequestedAt
		},
		func(key reviewRequestKey) goclientnew.ChangeSetReviewRequest {
			return goclientnew.ChangeSetReviewRequest{UserID: key.userID}
		},
		func(reviewRequestKey) string { return "" })
}

// applyReviewRequestEdits returns the review requests with --withdraw-review-request and
// --request-review applied. The list is written back whole, since a merge patch replaces an array.
// A new request is sent without a time, which the server stamps. Asking again for a review already
// requested sends a time later than the one recorded, which the server takes as a re-request and
// stamps again. place says where a withdrawn request was looked for, after the user, with its
// leading space.
func applyReviewRequestEdits[T any](current []T, parse func(string) (reviewRequestKey, error),
	fields func(*T) (reviewRequestKey, *time.Time), newRequest func(reviewRequestKey) T,
	place func(reviewRequestKey) string) ([]T, error) {
	requests := append([]T{}, current...)
	find := func(key reviewRequestKey) int {
		for i := range requests {
			if k, _ := fields(&requests[i]); k == key {
				return i
			}
		}
		return -1
	}
	for _, value := range withdrawReviewRequestFlags {
		key, err := parse(value)
		if err != nil {
			return nil, err
		}
		i := find(key)
		if i < 0 {
			return nil, errors.Errorf("no review was requested of %s%s", value, place(key))
		}
		requests = append(requests[:i], requests[i+1:]...)
	}
	for _, value := range requestReviewFlags {
		key, err := parse(value)
		if err != nil {
			return nil, err
		}
		if i := find(key); i >= 0 {
			_, requestedAt := fields(&requests[i])
			later := time.Now().UTC()
			if !later.After(*requestedAt) {
				later = requestedAt.Add(time.Second)
			}
			*requestedAt = later
			continue
		}
		requests = append(requests, newRequest(key))
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

// changeSetReviewRequestsSummary describes the review requests for `cub changeset get`, one per line.
func changeSetReviewRequestsSummary(requests []goclientnew.ChangeSetReviewRequest) string {
	lines := make([]string, 0, len(requests))
	for _, request := range requests {
		lines = append(lines, fmt.Sprintf("%s, requested %s", request.UserID, request.RequestedAt.Format(time.RFC3339)))
	}
	return strings.Join(lines, "\n")
}
