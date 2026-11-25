// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func init() {
	changeorderCmd.AddCommand(newUnarchiveCmd(unarchiveCommand{
		entity: "change order",
		long: `Unarchive change orders archived with cub changeorder delete --archive.

An archived change order is named by a slug made from its ID, id followed by the UUID, or by its
UUID. Its slug is restored unless another change order in its space has taken it since, in which
case it keeps the one made from its ID, and can be renamed. The tags it made are unarchived with it,
and get back their names with its own. When its name, or one of its tags' names, has been taken
since, it and its tags keep the names made from its ID. One its archive aborted stays aborted.

Examples:
` + "```" + `
  # Unarchive one change order
  cub changeorder unarchive --space my-space id5f0c1d2e-0a4b-4c3d-9e8f-7a6b5c4d3e2f

  # Unarchive the change orders archived from a space whose slug was my-changeorder
  cub changeorder unarchive --space my-space --where "Labels.ArchivedSlug = 'my-changeorder'"
` + "```" + `
`,
		unarchiveOne: func(nameOrID string) (string, *goclientnew.UnarchiveResponse, error) {
			details, err := resolveChangeOrder(nameOrID, selectedSpaceID, "ChangeOrderID,SpaceID,Slug")
			if err != nil {
				return "", nil, err
			}
			res, err := cubClientNew.UnarchiveChangeOrderWithResponse(ctx, details.ChangeOrder.SpaceID, details.ChangeOrder.ChangeOrderID)
			if cubapi.IsAPIError(err, res) {
				return "", nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return details.ChangeOrder.ChangeOrderID.String(), res.JSON200, nil
		},
		unarchiveSelected: func(where, filterID string) ([]goclientnew.UnarchiveResponse, error) {
			params := &goclientnew.BulkUnarchiveChangeOrdersParams{Where: &where}
			if filterID != "" {
				params.Filter = &filterID
			}
			res, err := cubClientNew.BulkUnarchiveChangeOrdersWithResponse(ctx, params)
			if cubapi.IsAPIError(err, res) {
				return nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return bulkUnarchiveResponses(res.StatusCode(), res.JSON200, res.JSON207)
		},
	}))
}
