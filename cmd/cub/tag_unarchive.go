// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func init() {
	tagCmd.AddCommand(newUnarchiveCmd(unarchiveCommand{
		entity: "tag",
		long: `Unarchive tags archived with cub tag delete --archive.

An archived tag is named by a slug made from its ID, id followed by the UUID, or by its UUID. Its
slug is restored unless another tag in its space has taken it since, in which case it keeps the one
made from its ID, and can be renamed. A tag a change set, change order or release made is archived
with it, and comes back when it does.

Examples:
` + "```" + `
  # Unarchive one tag
  cub tag unarchive --space my-space id5f0c1d2e-0a4b-4c3d-9e8f-7a6b5c4d3e2f

  # Unarchive the tags archived from a space whose slug was my-tag
  cub tag unarchive --space my-space --where "Labels.ArchivedSlug = 'my-tag'"
` + "```" + `
`,
		unarchiveOne: func(nameOrID string) (string, *goclientnew.UnarchiveResponse, error) {
			details, err := resolveTag(nameOrID, selectedSpaceID, "TagID,SpaceID,Slug")
			if err != nil {
				return "", nil, err
			}
			res, err := cubClientNew.UnarchiveTagWithResponse(ctx, details.Tag.SpaceID, details.Tag.TagID)
			if cubapi.IsAPIError(err, res) {
				return "", nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return details.Tag.TagID.String(), res.JSON200, nil
		},
		unarchiveSelected: func(where, filterID string) ([]goclientnew.UnarchiveResponse, error) {
			params := &goclientnew.BulkUnarchiveTagsParams{Where: &where}
			if filterID != "" {
				params.Filter = &filterID
			}
			res, err := cubClientNew.BulkUnarchiveTagsWithResponse(ctx, params)
			if cubapi.IsAPIError(err, res) {
				return nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return bulkUnarchiveResponses(res.StatusCode(), res.JSON200, res.JSON207)
		},
	}))
}
