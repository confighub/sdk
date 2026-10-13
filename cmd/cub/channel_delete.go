// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var channelDeleteCmd = &cobra.Command{
	Use:   "delete [<name or id>]",
	Short: "Delete a channel or multiple channels",
	Args:  cobra.MaximumNArgs(1),
	Long: getCommandHelp(`Delete a Channel. With no name, delete every channel --where selects.

Examples:
`+"```"+`
  # Delete a channel
  cub channel delete previews/preview-bot

  # Delete every channel nothing has claimed in a space
  cub channel delete --space previews --where "Condition = 'Unclaimed'"
`+"```"+`
`, ""),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        channelDeleteCmdRun,
}

func init() {
	addStandardDeleteFlags(channelDeleteCmd)
	enableOptionalSpace(channelDeleteCmd)
	enableWhereFlag(channelDeleteCmd)
	channelCmd.AddCommand(channelDeleteCmd)
}

func channelDeleteCmdRun(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		if where == "" {
			return errors.New("--where is required to delete multiple channels")
		}
		params := &goclientnew.BulkDeleteChannelsParams{Where: &where}
		params.IncludeHidden = includeHiddenParam()
		bulkRes, err := cubClientNew.BulkDeleteChannelsWithResponse(ctx, params)
		if cubapi.IsAPIError(err, bulkRes) {
			return cubapi.InterpretErrorGeneric(err, bulkRes)
		}
		return displayBulkDeleteResults(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "channel", deleteOperationName(), where)
	}
	if where != "" {
		return errors.New("--where selects the channels to delete, so it takes no channel name")
	}
	channel, err := resolveChannel(args[0], selectedSpaceID, "ChannelID,SpaceID,Slug")
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteChannelWithResponse(ctx, channel.Channel.SpaceID, channel.Channel.ChannelID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteResults("channel", args[0], channel.Channel.ChannelID.String(), deleteRes)
	return nil
}
