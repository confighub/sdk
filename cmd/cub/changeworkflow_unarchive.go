// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func init() {
	changeworkflowCmd.AddCommand(newUnarchiveCmd(unarchiveCommand{
		entity: "change workflow",
		long: `Unarchive change workflows archived with cub changeworkflow delete --archive.

An archived change workflow is named by a slug made from its ID, id followed by the UUID, or by its
UUID. Its slug is restored unless another change workflow in its space has taken it since, in which
case it keeps the one made from its ID, and can be renamed. Its backing unit is unarchived with it.
It is refused while it names an entity that is still archived.

Examples:
` + "```" + `
  # Unarchive one change workflow
  cub changeworkflow unarchive --space my-space id5f0c1d2e-0a4b-4c3d-9e8f-7a6b5c4d3e2f

  # Unarchive the change workflows archived from a space whose slug was my-changeworkflow
  cub changeworkflow unarchive --space my-space --where "Labels.ArchivedSlug = 'my-changeworkflow'"
` + "```" + `
`,
		unarchiveOne: func(nameOrID string) (string, *goclientnew.UnarchiveResponse, error) {
			details, err := resolveChangeWorkflow(nameOrID, selectedSpaceID, "ChangeWorkflowID,SpaceID,Slug")
			if err != nil {
				return "", nil, err
			}
			res, err := cubClientNew.UnarchiveChangeWorkflowWithResponse(ctx, details.ChangeWorkflow.SpaceID, details.ChangeWorkflow.ChangeWorkflowID)
			if cubapi.IsAPIError(err, res) {
				return "", nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return details.ChangeWorkflow.ChangeWorkflowID.String(), res.JSON200, nil
		},
		unarchiveSelected: func(where, filterID string) ([]goclientnew.UnarchiveResponse, error) {
			params := &goclientnew.BulkUnarchiveChangeWorkflowsParams{Where: &where}
			if filterID != "" {
				params.Filter = &filterID
			}
			res, err := cubClientNew.BulkUnarchiveChangeWorkflowsWithResponse(ctx, params)
			if cubapi.IsAPIError(err, res) {
				return nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return bulkUnarchiveResponses(res.StatusCode(), res.JSON200, res.JSON207)
		},
	}))
}
