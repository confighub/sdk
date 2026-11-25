// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func init() {
	changesetCmd.AddCommand(newUnarchiveCmd(unarchiveCommand{
		entity: "change set",
		long: `Unarchive change sets archived with cub changeset delete --archive.

An archived change set is named by a slug made from its ID, id followed by the UUID, or by its UUID.
Its slug is restored unless another change set in its space has taken it since, in which case it
keeps the one made from its ID, and can be renamed. Its start and end tags are unarchived with it,
and get back their names with its own. When its name, or one of its tags' names, has been taken
since, it and its tags keep the names made from its ID. It is refused while it names an entity that
is still archived.

Examples:
` + "```" + `
  # Unarchive one change set
  cub changeset unarchive --space my-space id5f0c1d2e-0a4b-4c3d-9e8f-7a6b5c4d3e2f

  # Unarchive the change sets archived from a space whose slug was my-changeset
  cub changeset unarchive --space my-space --where "Labels.ArchivedSlug = 'my-changeset'"
` + "```" + `
`,
		unarchiveOne: func(nameOrID string) (string, *goclientnew.UnarchiveResponse, error) {
			details, err := resolveChangeSet(nameOrID, selectedSpaceID, "ChangeSetID,SpaceID,Slug")
			if err != nil {
				return "", nil, err
			}
			res, err := cubClientNew.UnarchiveChangeSetWithResponse(ctx, details.ChangeSet.SpaceID, details.ChangeSet.ChangeSetID)
			if cubapi.IsAPIError(err, res) {
				return "", nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return details.ChangeSet.ChangeSetID.String(), res.JSON200, nil
		},
		unarchiveSelected: func(where, filterID string) ([]goclientnew.UnarchiveResponse, error) {
			params := &goclientnew.BulkUnarchiveChangeSetsParams{Where: &where}
			if filterID != "" {
				params.Filter = &filterID
			}
			res, err := cubClientNew.BulkUnarchiveChangeSetsWithResponse(ctx, params)
			if cubapi.IsAPIError(err, res) {
				return nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return bulkUnarchiveResponses(res.StatusCode(), res.JSON200, res.JSON207)
		},
	}))
}
