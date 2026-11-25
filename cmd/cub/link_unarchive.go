// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func init() {
	linkCmd.AddCommand(newUnarchiveCmd(unarchiveCommand{
		entity: "link",
		long: `Unarchive links archived with cub link delete --archive.

An archived link is named by a slug made from its ID, id followed by the UUID, or by its UUID. Its
slug is restored unless another link in its space has taken it since, in which case it keeps the one
made from its ID, and can be renamed. Its backing unit is unarchived with it, and its unit takes
values through it again. It is refused while either of its units is archived, and for a link
archived with its unit, which comes back when the unit is unarchived.

Examples:
` + "```" + `
  # Unarchive one link
  cub link unarchive --space my-space id5f0c1d2e-0a4b-4c3d-9e8f-7a6b5c4d3e2f

  # Unarchive the links archived from a space whose slug was my-link
  cub link unarchive --space my-space --where "Labels.ArchivedSlug = 'my-link'"
` + "```" + `
`,
		unarchiveOne: func(nameOrID string) (string, *goclientnew.UnarchiveResponse, error) {
			details, err := resolveLink(nameOrID, selectedSpaceID, "LinkID,SpaceID,Slug")
			if err != nil {
				return "", nil, err
			}
			res, err := cubClientNew.UnarchiveLinkWithResponse(ctx, details.Link.SpaceID, details.Link.LinkID)
			if cubapi.IsAPIError(err, res) {
				return "", nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return details.Link.LinkID.String(), res.JSON200, nil
		},
		unarchiveSelected: func(where, filterID string) ([]goclientnew.UnarchiveResponse, error) {
			params := &goclientnew.BulkUnarchiveLinksParams{Where: &where}
			if filterID != "" {
				params.Filter = &filterID
			}
			res, err := cubClientNew.BulkUnarchiveLinksWithResponse(ctx, params)
			if cubapi.IsAPIError(err, res) {
				return nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return bulkUnarchiveResponses(res.StatusCode(), res.JSON200, res.JSON207)
		},
	}))
}
