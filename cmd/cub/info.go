// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show some information",
	Long:  `Show some information`,
	RunE:  infoCmdRun,
}

func init() {
	rootCmd.AddCommand(infoCmd)
}

func infoCmdRun(cmd *cobra.Command, args []string) error {
	apiInfo := GetApiInfo()
	detail := detailView()
	detail.Append([]string{"Server URL:", cubContext.ConfigHubURL})
	detail.Append([]string{"Client ID:", apiInfo.ClientID})
	detail.Append([]string{"Build:", apiInfo.Build})
	detail.Append([]string{"BuiltAt:", apiInfo.BuiltAt})
	detail.Append([]string{"Revision:", apiInfo.Revision})
	detail.Render()
	return nil
}

func GetApiInfo() goclientnew.ApiInfo {
	payloadRes, err := cubClientNew.ApiInfoWithResponse(ctx)
	if IsAPIError(err, payloadRes) {
		failOnError(InterpretErrorGeneric(err, payloadRes))
	}

	// empty 200 response from server shouldn't happen
	if payloadRes.JSON200 == nil {
		return goclientnew.ApiInfo{}
	}
	return *payloadRes.JSON200
}
