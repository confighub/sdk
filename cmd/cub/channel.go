// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var channelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Manage channels",
	Long: getCommandHelp(`Manage channels.

A channel is a mailbox between ConfigHub and one bot. A client claims it and polls it; callers
run the commands it declares on it; it subscribes to events and bookmarks how far it has read.
A channel owns no identity: a service account claims it by holding Claim on it. What a channel
declares and watches is set here, on the channel, not by the client that connects to it.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(channelCmd)
	rootCmd.AddCommand(channelCmd)
	addExplainCmd(channelCmd, "Channel")
}
