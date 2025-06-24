// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"net/url"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var targetListCmd = &cobra.Command{
	Use:   "list",
	Short: "List targets",
	Long:  `List targets you have access to in a space`,
	Args:  cobra.ExactArgs(0),
	RunE:  targetListCmdRun,
}

func init() {
	addStandardListFlags(targetListCmd)
	targetCmd.AddCommand(targetListCmd)
}

func targetListCmdRun(cmd *cobra.Command, args []string) error {
	targets, err := apiListTargets(selectedSpaceID, where)
	if err != nil {
		return err
	}
	displayListResults(targets, getTargetSlug, displayTargetList)
	return nil
}

func getTargetSlug(exTarget *goclientnew.ExtendedTarget) string {
	return exTarget.Target.Slug
}

func displayTargetList(exTargets []*goclientnew.ExtendedTarget) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Display-Name", "Slug", "ID", "Worker-Slug", "Parameters"})
	}
	for _, exTarget := range exTargets {
		workerSlug := ""
		if exTarget.BridgeWorker != nil {
			workerSlug = exTarget.BridgeWorker.Slug
		}
		table.Append([]string{
			exTarget.Target.DisplayName,
			exTarget.Target.Slug,
			exTarget.Target.TargetID.String(),
			workerSlug,
			exTarget.Target.Parameters,
		})
	}
	table.Render()
}

func apiListTargets(spaceID string, whereFilter string) ([]*goclientnew.ExtendedTarget, error) {
	newParams := &goclientnew.ListTargetsParams{}
	if whereFilter != "" {
		whereFilter = url.QueryEscape(whereFilter)
		newParams.Where = &whereFilter
	}
	include := "BridgeWorkerID"
	newParams.Include = &include
	targetsRes, err := cubClientNew.ListTargetsWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if IsAPIError(err, targetsRes) {
		return nil, InterpretErrorGeneric(err, targetsRes)
	}

	targets := make([]*goclientnew.ExtendedTarget, 0, len(*targetsRes.JSON200))
	for _, target := range *targetsRes.JSON200 {
		targets = append(targets, &target)
	}
	return targets, nil
}
