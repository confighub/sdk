// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var releaseWithdrawCmd = &cobra.Command{
	Use:   "withdraw <release-id>",
	Short: "Withdraw a release",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Withdraw a release, identified by its globally-unique release ID.

Withdrawal takes the release out of service: it is no longer published for download,
but the release itself is retained. Use `+"`cub release delete`"+` to remove it.

The release is located by its ID regardless of which Space it lives in, so --space
is not required (a Release lives in its bundled Units' Space, which need not be the
caller's default).

If any Unit in the release's bundle has an outstanding DestroyGate, the withdrawal
is blocked until the gate clears.

Examples:
`+"```"+`
  cub release withdraw 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c
`+"```"+`
`, ""),
	RunE: releaseWithdrawCmdRun,
}

func init() {
	addStandardDisplayFlags(releaseWithdrawCmd)
	releaseCmd.AddCommand(releaseWithdrawCmd)
}

func releaseWithdrawCmdRun(cmd *cobra.Command, args []string) error {
	releaseID, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid release id %q: %w", args[0], err)
	}

	// A Release's ID is org-unique, but the withdraw endpoint is space-scoped — and a
	// Release lives in its bundled Units' Space, which need not be the caller's
	// --space. Resolve the Release's actual Space org-wide so withdrawal targets the
	// right row instead of silently no-op'ing against the wrong Space.
	releaseSpaceID, err := apiGetReleaseSpaceID(releaseID)
	if err != nil {
		return err
	}

	withdrawRes, err := cubClientNew.WithdrawReleaseWithResponse(ctx,
		releaseSpaceID,
		releaseID,
	)
	if cubapi.IsAPIError(err, withdrawRes) {
		return cubapi.InterpretErrorGeneric(err, withdrawRes)
	}

	release := withdrawRes.JSON200
	displayUpdateResults(release, "release", args[0], release.ReleaseID.String(), displayReleaseDetails)
	return nil
}

// apiGetReleaseSpaceID resolves the Space a Release lives in from its org-unique
// ID via an organization-wide search. The Release lifecycle endpoints are
// space-scoped, but the ID is globally unique, so callers should not have to know
// the Space up front. Returns a "not found" error when no such Release exists.
func apiGetReleaseSpaceID(releaseID uuid.UUID) (uuid.UUID, error) {
	releases, err := apiSearchListReleases(fmt.Sprintf("ReleaseID = '%s'", releaseID), "", "")
	if err != nil {
		return uuid.Nil, err
	}
	for _, er := range releases {
		if er.Release != nil && er.Release.ReleaseID == releaseID {
			return er.Release.SpaceID, nil
		}
	}
	return uuid.Nil, fmt.Errorf("release %s not found", releaseID)
}
