// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	BuildTag  = "unknown"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show client and server version, commit, and build date",
	Long:  getCommandHelp(`Show client and server version, commit, and build date.`, "Authentication not required."),
	RunE:  versionCmdRun,
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func versionCmdRun(cmd *cobra.Command, args []string) error {
	fmt.Printf("Client Version:\n")
	fmt.Printf("  Version:    %s\n", Version)
	fmt.Printf("  Commit:     %s\n", BuildTag)
	fmt.Printf("  Build Date: %s\n", BuildDate)
	apiInfo := GetApiInfo()
	fmt.Printf("Server Version:\n")
	fmt.Printf("  URL:        %s\n", contextManager.ActiveContext().Coordinate.ServerURL)
	fmt.Printf("  Version:    %s\n", apiInfo.Version)
	fmt.Printf("  Commit:     %s\n", apiInfo.Build)
	fmt.Printf("  Build Date: %s\n", apiInfo.BuiltAt)
	fmt.Printf("  Client ID:  %s\n", apiInfo.ClientID)
	// Reported after both versions are printed, so the user sees what the
	// complaint is about even when it exits non-zero.
	return checkVersionSkew(Version, apiInfo.Version)
}

func GetApiInfo() goclientnew.ApiInfo {
	payloadRes, err := cubClientNew.ApiInfoWithResponse(ctx)
	if cubapi.IsAPIError(err, payloadRes) {
		failOnError(cubapi.InterpretErrorGeneric(err, payloadRes))
	}

	// empty 200 response from server shouldn't happen
	if payloadRes.JSON200 == nil {
		return goclientnew.ApiInfo{}
	}
	return *payloadRes.JSON200
}
