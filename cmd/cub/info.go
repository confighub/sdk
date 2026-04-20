// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:        "info",
	Short:      "Show server details",
	Long:       getCommandHelp(`Show server URL, version, and build information.`, "Authentication not required."),
	Deprecated: "Use 'cub version' instead.",
	RunE:       infoCmdRun,
}

func init() {
	rootCmd.AddCommand(infoCmd)
}

func infoCmdRun(cmd *cobra.Command, args []string) error {
	apiInfo := GetApiInfo()
	detail := detailView()
	detail.Append([]string{"Server URL:", contextManager.ActiveContext().Coordinate.ServerURL})
	detail.Append([]string{"Version:", apiInfo.Version})
	detail.Append([]string{"Client ID:", apiInfo.ClientID})
	detail.Append([]string{"Build:", apiInfo.Build})
	detail.Append([]string{"BuiltAt:", apiInfo.BuiltAt})
	detail.Render()
	return nil
}
