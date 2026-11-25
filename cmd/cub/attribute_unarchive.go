// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func init() {
	attributeCmd.AddCommand(newUnarchiveCmd(unarchiveCommand{
		entity: "attribute",
		long: `Unarchive attributes archived with cub attribute delete --archive.

An archived attribute is named by a slug made from its ID, id followed by the UUID, or by its UUID.
Its slug is restored unless another attribute in its space has taken it since, in which case it
keeps the one made from its ID, and can be renamed. Its backing unit is unarchived with it, and its
space uses it again. It is refused while it names an entity that is still archived.

Examples:
` + "```" + `
  # Unarchive one attribute
  cub attribute unarchive --space my-space id5f0c1d2e-0a4b-4c3d-9e8f-7a6b5c4d3e2f

  # Unarchive the attributes archived from a space whose slug was my-attribute
  cub attribute unarchive --space my-space --where "Labels.ArchivedSlug = 'my-attribute'"
` + "```" + `
`,
		unarchiveOne: func(nameOrID string) (string, *goclientnew.UnarchiveResponse, error) {
			details, err := resolveAttribute(nameOrID, selectedSpaceID, "AttributeID,SpaceID,Slug")
			if err != nil {
				return "", nil, err
			}
			res, err := cubClientNew.UnarchiveAttributeWithResponse(ctx, details.Attribute.SpaceID, details.Attribute.AttributeID)
			if cubapi.IsAPIError(err, res) {
				return "", nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return details.Attribute.AttributeID.String(), res.JSON200, nil
		},
		unarchiveSelected: func(where, filterID string) ([]goclientnew.UnarchiveResponse, error) {
			params := &goclientnew.BulkUnarchiveAttributesParams{Where: &where}
			if filterID != "" {
				params.Filter = &filterID
			}
			res, err := cubClientNew.BulkUnarchiveAttributesWithResponse(ctx, params)
			if cubapi.IsAPIError(err, res) {
				return nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return bulkUnarchiveResponses(res.StatusCode(), res.JSON200, res.JSON207)
		},
	}))
}
