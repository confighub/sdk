// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var channelGetCmd = &cobra.Command{
	Use:   "get <name or id>",
	Short: "Get details about a channel",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get a Channel: its lease, what it declares, what it watches, and how far it has read.

Examples:
`+"```"+`
  # Get a channel
  cub channel get previews/preview-bot

  # Just what it declares
  cub channel get previews/preview-bot -o jq=.Channel.Commands
`+"```"+`
`, ""),
	RunE: channelGetCmdRun,
}

func init() {
	addStandardGetFlags(channelGetCmd)
	enableOptionalSpace(channelGetCmd)
	channelCmd.AddCommand(channelGetCmd)
}

func channelGetCmdRun(_ *cobra.Command, args []string) error {
	extendedChannel, err := resolveChannel(args[0], selectedSpaceID, selectFields)
	if err != nil {
		return err
	}
	displayGetResults(extendedChannel, displayExtendedChannelDetails)
	return nil
}

func displayExtendedChannelDetails(extendedChannel *goclientnew.ExtendedChannel) {
	channel := extendedChannel.Channel
	view := tableView()
	view.Append([]string{"ID", channel.ChannelID.String()})
	view.Append([]string{"Name", channel.Slug})
	view.Append([]string{"Display Name", channel.DisplayName})
	if extendedChannel.Space != nil {
		view.Append([]string{"Space", extendedChannel.Space.Slug})
	} else {
		view.Append([]string{"Space ID", channel.SpaceID.String()})
	}
	view.Append([]string{"Condition", string(channel.Condition)})
	if !channel.LastSeenAt.IsZero() {
		view.Append([]string{"Last Seen At", channel.LastSeenAt.String()})
	}
	if channel.IPAddress != "" {
		view.Append([]string{"IP Address", channel.IPAddress})
	}
	view.Append([]string{"Event Position", fmt.Sprintf("%d", channel.EventPosition)})
	view.Append([]string{"Commands", commandNamesToString(channel.Commands)})
	view.Append([]string{"Subscriptions", subscriptionsToString(channel.Subscriptions)})
	view.Append([]string{"Created At", channel.CreatedAt.String()})
	view.Append([]string{"Updated At", channel.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(channel.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(channel.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(channel.Annotations)})
	view.Append([]string{"Permissions", permissionsToString(channel.Permissions)})
	appendBackingUnitRow(view, channel.BackingUnitID)
	view.Render()
}

func displayChannelDetails(channel *goclientnew.Channel) {
	displayExtendedChannelDetails(&goclientnew.ExtendedChannel{Channel: channel})
}

func commandNamesToString(commands map[string]goclientnew.CommandSignature) string {
	if len(commands) == 0 {
		return ""
	}
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func subscriptionsToString(subscriptions []goclientnew.EventSubscription) string {
	if len(subscriptions) == 0 {
		return ""
	}
	parts := make([]string, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		encoded, err := json.Marshal(subscription)
		if err != nil {
			continue
		}
		parts = append(parts, string(encoded))
	}
	return strings.Join(parts, " ")
}
