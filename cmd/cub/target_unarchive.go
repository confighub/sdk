// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func init() {
	targetCmd.AddCommand(newUnarchiveCmd(unarchiveCommand{
		entity: "target",
		long: `Unarchive targets archived with cub target delete --archive.

An archived target is named by a slug made from its ID, id followed by the UUID, or by its UUID. Its
slug is restored unless another target in its space has taken it since, in which case it keeps the
one made from its ID, and can be renamed. Its backing unit is unarchived with it. A space whose
release target was detached by the archive does not release to it again until it is set once more.
It is refused while it names an entity that is still archived.

Examples:
` + "```" + `
  # Unarchive one target
  cub target unarchive --space my-space id5f0c1d2e-0a4b-4c3d-9e8f-7a6b5c4d3e2f

  # Unarchive the targets archived from a space whose slug was my-target
  cub target unarchive --space my-space --where "Labels.ArchivedSlug = 'my-target'"
` + "```" + `
`,
		unarchiveOne: func(nameOrID string) (string, *goclientnew.UnarchiveResponse, error) {
			details, err := resolveTarget(nameOrID, selectedSpaceID, "TargetID,SpaceID,Slug")
			if err != nil {
				return "", nil, err
			}
			res, err := cubClientNew.UnarchiveTargetWithResponse(ctx, details.Target.SpaceID, details.Target.TargetID)
			if cubapi.IsAPIError(err, res) {
				return "", nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return details.Target.TargetID.String(), res.JSON200, nil
		},
		unarchiveSelected: func(where, filterID string) ([]goclientnew.UnarchiveResponse, error) {
			params := &goclientnew.BulkUnarchiveTargetsParams{Where: &where}
			if filterID != "" {
				params.Filter = &filterID
			}
			res, err := cubClientNew.BulkUnarchiveTargetsWithResponse(ctx, params)
			if cubapi.IsAPIError(err, res) {
				return nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return bulkUnarchiveResponses(res.StatusCode(), res.JSON200, res.JSON207)
		},
	}))
}
