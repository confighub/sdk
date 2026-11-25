// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func init() {
	channelCmd.AddCommand(newUnarchiveCmd(unarchiveCommand{
		entity: "channel",
		long: `Unarchive channels archived with cub channel delete --archive.

An archived channel is named by a slug made from its ID, id followed by the UUID, or by its UUID.
Its slug is restored unless another channel in its space has taken it since, in which case it keeps
the one made from its ID, and can be renamed. Its backing unit is unarchived with it, and it can be
claimed again. It is refused while it names an entity that is still archived.

Examples:
` + "```" + `
  # Unarchive one channel
  cub channel unarchive --space my-space id5f0c1d2e-0a4b-4c3d-9e8f-7a6b5c4d3e2f

  # Unarchive the channels archived from a space whose slug was my-channel
  cub channel unarchive --space my-space --where "Labels.ArchivedSlug = 'my-channel'"
` + "```" + `
`,
		unarchiveOne: func(nameOrID string) (string, *goclientnew.UnarchiveResponse, error) {
			details, err := resolveChannel(nameOrID, selectedSpaceID, "ChannelID,SpaceID,Slug")
			if err != nil {
				return "", nil, err
			}
			res, err := cubClientNew.UnarchiveChannelWithResponse(ctx, details.Channel.SpaceID, details.Channel.ChannelID)
			if cubapi.IsAPIError(err, res) {
				return "", nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return details.Channel.ChannelID.String(), res.JSON200, nil
		},
		unarchiveSelected: func(where, filterID string) ([]goclientnew.UnarchiveResponse, error) {
			params := &goclientnew.BulkUnarchiveChannelsParams{Where: &where}
			if filterID != "" {
				params.Filter = &filterID
			}
			res, err := cubClientNew.BulkUnarchiveChannelsWithResponse(ctx, params)
			if cubapi.IsAPIError(err, res) {
				return nil, cubapi.InterpretErrorGeneric(err, res)
			}
			return bulkUnarchiveResponses(res.StatusCode(), res.JSON200, res.JSON207)
		},
	}))
}
