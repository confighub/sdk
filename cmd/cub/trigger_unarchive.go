// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func init() {
	triggerCmd.AddCommand(newUnarchiveCmd(unarchiveCommand{
		entity: "trigger",
		long: `Unarchive triggers archived with cub trigger delete --archive.

An archived trigger is named by a slug made from its ID, id followed by the UUID, or by its UUID.
Its slug is restored unless another trigger in its space has taken it since, in which case it keeps
the one made from its ID, and can be renamed. Its backing unit is unarchived with it, and its space
applies it again.

Examples:
` + "```" + `
  # Unarchive one trigger
  cub trigger unarchive --space my-space id5f0c1d2e-0a4b-4c3d-9e8f-7a6b5c4d3e2f

  # Unarchive the triggers archived from a space whose slug was my-trigger
  cub trigger unarchive --space my-space --where "Labels.ArchivedSlug = 'my-trigger'"
` + "```" + `
`,
		unarchiveOne: func(nameOrID string) (string, *goclientnew.UnarchiveResponse, error) {
			details, err := resolveTrigger(nameOrID, selectedSpaceID, "TriggerID,SpaceID,Slug")
			if err != nil {
				return "", nil, err
			}
			res, err := cubClientNew.UnarchiveTriggerWithResponse(ctx, details.Trigger.SpaceID, details.Trigger.TriggerID)
			if cubapi.IsAPIError(err, res) {
				return "", nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return details.Trigger.TriggerID.String(), res.JSON200, nil
		},
		unarchiveSelected: func(where, filterID string) ([]goclientnew.UnarchiveResponse, error) {
			params := &goclientnew.BulkUnarchiveTriggersParams{Where: &where}
			if filterID != "" {
				params.Filter = &filterID
			}
			res, err := cubClientNew.BulkUnarchiveTriggersWithResponse(ctx, params)
			if cubapi.IsAPIError(err, res) {
				return nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return bulkUnarchiveResponses(res.StatusCode(), res.JSON200, res.JSON207)
		},
	}))
}
