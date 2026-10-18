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
	Long: getCommandHelp(`Show client and server version, commit, and build date.

The client version is printed first. The server's is fetched from the server of the
current context; when it cannot be fetched, the reason is printed to stderr and the
command exits non-zero, as it does when this cub is too old for the server, so that
a script can tell a cub that works against its server from one that does not.
--client prints the client version alone and does not contact the server.

Examples:
  cub version
  cub version --client`, "Authentication not required."),
	Args: cobra.NoArgs,
	RunE: versionCmdRun,
}

var versionClientOnly bool

func init() {
	versionCmd.Flags().BoolVar(&versionClientOnly, "client", false, "Show the client version only, without contacting the server")
	rootCmd.AddCommand(versionCmd)
}

func versionCmdRun(cmd *cobra.Command, args []string) error {
	fmt.Printf("Client Version:\n")
	fmt.Printf("  Version:    %s\n", Version)
	fmt.Printf("  Commit:     %s\n", BuildTag)
	fmt.Printf("  Build Date: %s\n", BuildDate)
	if versionClientOnly {
		return nil
	}
	serverURL := contextManager.ActiveContext().Coordinate.ServerURL
	apiInfo, err := fetchApiInfo()
	if err != nil {
		return fmt.Errorf("could not fetch the server version from %s: %w. 'cub version --client' prints the client version alone", serverURL, err)
	}
	fmt.Printf("Server Version:\n")
	fmt.Printf("  URL:        %s\n", serverURL)
	fmt.Printf("  Version:    %s\n", apiInfo.Version)
	fmt.Printf("  Commit:     %s\n", apiInfo.Build)
	fmt.Printf("  Build Date: %s\n", apiInfo.BuiltAt)
	fmt.Printf("  Client ID:  %s\n", apiInfo.ClientID)
	// Reported after both versions are printed, so the user sees what the
	// complaint is about even when it exits non-zero.
	return checkVersionSkew(Version, apiInfo.Version)
}

func GetApiInfo() goclientnew.ApiInfo {
	apiInfo, err := fetchApiInfo()
	failOnError(err)
	return apiInfo
}

func fetchApiInfo() (goclientnew.ApiInfo, error) {
	payloadRes, err := cubClientNew.ApiInfoWithResponse(ctx)
	if cubapi.IsAPIError(err, payloadRes) {
		return goclientnew.ApiInfo{}, cubapi.InterpretErrorGeneric(err, payloadRes)
	}

	// empty 200 response from server shouldn't happen
	if payloadRes.JSON200 == nil {
		return goclientnew.ApiInfo{}, nil
	}
	return *payloadRes.JSON200, nil
}
