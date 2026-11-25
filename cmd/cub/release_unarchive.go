// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

func init() {
	releaseCmd.AddCommand(newUnarchiveCmd(unarchiveCommand{
		entity: "release",
		long: `Unarchive releases archived with cub release delete --archive, identified by their IDs.

The release, its bundle and its tag come back, which, like publishing or deleting one, changes what
is served to the cluster behind the release Target and needs the same permissions.

Examples:
` + "```" + `
  # Unarchive one release
  cub release unarchive 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c

  # Unarchive the archived releases of a space
  cub release unarchive --space my-space --where "ReleaseNum > 3"
` + "```" + `
`,
		unarchiveOne: func(id string) (string, *goclientnew.UnarchiveResponse, error) {
			releaseID, err := uuid.Parse(id)
			if err != nil {
				return "", nil, fmt.Errorf("invalid release id %q: %w", id, err)
			}
			releaseSpaceID, err := apiGetReleaseSpaceID(releaseID)
			if err != nil {
				return "", nil, err
			}
			res, err := cubClientNew.UnarchiveReleaseWithResponse(ctx, releaseSpaceID, releaseID)
			if cubapi.IsAPIError(err, res) {
				return "", nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return releaseID.String(), res.JSON200, nil
		},
		unarchiveSelected: func(where, filterID string) ([]goclientnew.UnarchiveResponse, error) {
			params := &goclientnew.BulkUnarchiveReleasesParams{Where: &where}
			if filterID != "" {
				params.Filter = &filterID
			}
			res, err := cubClientNew.BulkUnarchiveReleasesWithResponse(ctx, params)
			if cubapi.IsAPIError(err, res) {
				return nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return bulkUnarchiveResponses(res.StatusCode(), res.JSON200, res.JSON207)
		},
	}))
}
