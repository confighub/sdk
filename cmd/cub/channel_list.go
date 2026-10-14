// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var channelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List channels",
	Long: getCommandHelp(`List channels across the organization, or in one space with --space.

Examples:
`+"```"+`
  # Every channel, with whether a bot holds it
  cub channel list

  # The channels declaring a command
  cub channel list --where "Commands ? 'preview.deploy'"
`+"```"+`
`, ""),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        channelListCmdRun,
}

var defaultChannelColumns = []string{"Channel.Slug", "Space.Slug", "Channel.Condition", "Channel.LastSeenAt", "Channel.EventPosition", "Channel.Commands"}

const channelListInclude = "SpaceID"

var channelBaseSelectFields = []string{"Slug", "ChannelID", "SpaceID", "OrganizationID"}

var channelAliases = map[string]string{
	"Name": "Channel.Slug",
	"ID":   "Channel.ChannelID",
}

var channelCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(channelListCmd)
	enableListPagingFlags(channelListCmd)
	channelCmd.AddCommand(channelListCmd)
}

func channelListCmdRun(_ *cobra.Command, _ []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}
	channels, err := apiListChannels(selectedSpaceID, where, selectFields, filterID)
	if err != nil {
		return err
	}
	displayListResults(channels, getExtendedChannelSlug, displayExtendedChannelList)
	return nil
}

func apiListChannels(spaceID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedChannel, error) {
	whereClause := cubapi.NewWhere(whereFilter)
	if spaceID != "*" && spaceID != "" {
		whereClause = whereClause.SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	}
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("Channel", listColumnsFor("cub channel list"), channelListInclude, defaultChannelColumns, channelAliases, channelCustomColumnDependencies, channelBaseSelectFields)
	})
	return cubapi.ListChannels(ctx, cubClient, whereClause, cubapi.ListOpts{
		Limit:         listLimit,
		OrderBy:       listOrderBy,
		Select:        cubapi.SelectFields(selectValue),
		Include:       channelListInclude,
		Filter:        filterParam,
		Contains:      contains,
		IncludeHidden: includeHidden,
	})
}

func getExtendedChannelSlug(channel *goclientnew.ExtendedChannel) string {
	space := ""
	if channel.Space != nil {
		space = channel.Space.Slug
	}
	return prefixedSlug(space, channel.Channel.Slug)
}

func displayExtendedChannelList(channels []*goclientnew.ExtendedChannel) {
	if displayRequestedColumns(channels, channelAliases, nil) {
		return
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "Condition", "Last-Seen", "Position", "Commands"})
	}
	for _, channel := range channels {
		spaceSlug := ""
		if channel.Space != nil {
			spaceSlug = channel.Space.Slug
		}
		lastSeen := ""
		if !channel.Channel.LastSeenAt.IsZero() {
			lastSeen = channel.Channel.LastSeenAt.Format("2006-01-02 15:04:05")
		}
		table.Append([]string{
			channel.Channel.Slug,
			spaceSlug,
			string(channel.Channel.Condition),
			lastSeen,
			fmt.Sprintf("%d", channel.Channel.EventPosition),
			commandNamesToString(channel.Channel.Commands),
		})
	}
	table.Render()
}
