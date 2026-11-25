// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func init() {
	invocationCmd.AddCommand(newUnarchiveCmd(unarchiveCommand{
		entity: "invocation",
		long: `Unarchive invocations archived with cub invocation delete --archive.

An archived invocation is named by a slug made from its ID, id followed by the UUID, or by its UUID.
Its slug is restored unless another invocation in its space has taken it since, in which case it
keeps the one made from its ID, and can be renamed. Its backing unit is unarchived with it. It is
refused while it names an entity that is still archived.

Examples:
` + "```" + `
  # Unarchive one invocation
  cub invocation unarchive --space my-space id5f0c1d2e-0a4b-4c3d-9e8f-7a6b5c4d3e2f

  # Unarchive the invocations archived from a space whose slug was my-invocation
  cub invocation unarchive --space my-space --where "Labels.ArchivedSlug = 'my-invocation'"
` + "```" + `
`,
		unarchiveOne: func(nameOrID string) (string, *goclientnew.UnarchiveResponse, error) {
			details, err := resolveInvocation(nameOrID, selectedSpaceID, "InvocationID,SpaceID,Slug")
			if err != nil {
				return "", nil, err
			}
			res, err := cubClientNew.UnarchiveInvocationWithResponse(ctx, details.Invocation.SpaceID, details.Invocation.InvocationID)
			if cubapi.IsAPIError(err, res) {
				return "", nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return details.Invocation.InvocationID.String(), res.JSON200, nil
		},
		unarchiveSelected: func(where, filterID string) ([]goclientnew.UnarchiveResponse, error) {
			params := &goclientnew.BulkUnarchiveInvocationsParams{Where: &where}
			if filterID != "" {
				params.Filter = &filterID
			}
			res, err := cubClientNew.BulkUnarchiveInvocationsWithResponse(ctx, params)
			if cubapi.IsAPIError(err, res) {
				return nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return bulkUnarchiveResponses(res.StatusCode(), res.JSON200, res.JSON207)
		},
	}))
}
