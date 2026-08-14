// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var releaseDeleteCmd = &cobra.Command{
	Use:         "delete <release-id>",
	Short:       "Delete a release",
	Args:        cobra.ExactArgs(1),
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Delete a release, identified by its globally-unique release ID.

Deletion removes the release and its stored bundle. To take a release out of
service while retaining it, use `+"`cub release withdraw`"+` instead.

The release is located by its ID regardless of which Space it lives in, so --space
is not required (a Release lives in its bundled Units' Space, which need not be the
caller's default).

Examples:
`+"```"+`
  cub release delete 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c
`+"```"+`
`, ""),
	RunE: releaseDeleteCmdRun,
}

func init() {
	addStandardDeleteFlags(releaseDeleteCmd)
	releaseCmd.AddCommand(releaseDeleteCmd)
}

func releaseDeleteCmdRun(cmd *cobra.Command, args []string) error {
	releaseID, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid release id %q: %w", args[0], err)
	}

	// A Release's ID is org-unique, but the delete endpoint is space-scoped — and a
	// Release lives in its bundled Units' Space, which need not be the caller's
	// --space. Resolve the Release's actual Space org-wide so deletion targets the
	// right row instead of silently no-op'ing against the wrong Space.
	releaseSpaceID, err := apiGetReleaseSpaceID(releaseID)
	if err != nil {
		return err
	}

	deleteRes, err := cubClientNew.DeleteReleaseWithResponse(ctx,
		releaseSpaceID,
		releaseID,
	)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("release", args[0], releaseID.String(), deleteRes)
	return nil
}
