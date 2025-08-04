// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitPushUpgradeCmd = &cobra.Command{
	Use:   "upgrade downstreams from <name>",
	Short: "Upgrade downstreams from unit",
	Long:  `upgrade downstreams from the unit`,
	Args:  cobra.ExactArgs(1),
	RunE:  unitBulkUpgradeCmdRun,
}

func init() {
	addStandardUpdateFlags(unitPushUpgradeCmd)
	unitCmd.AddCommand(unitPushUpgradeCmd)
}

func unitBulkUpgradeCmdRun(cmd *cobra.Command, args []string) error {
	currentUnit, err := apiGetUnitFromSlug(args[0])
	if err != nil {
		return err
	}
	upgradeRes, err := cubClientNew.UpgradeDownstreamUnitsWithResponse(ctx, uuid.MustParse(selectedSpaceID), currentUnit.UnitID)
	if IsAPIError(err, upgradeRes) {
		return InterpretErrorGeneric(err, upgradeRes)
	}

	unitDetails := upgradeRes.JSON200
	displayUpgradeResults(unitDetails, "unit", args[0], currentUnit.UnitID.String(), displayBulkUpgradeDetails)
	return nil
}

func displayUpgradeResults[Result any](result *Result, entityName, slug, id string, display func(result *Result)) {
	if !quiet {
		tprint("Successfully upgraded %ss related to %s (%s)", entityName, slug, id)
	}
	if verbose {
		display(result)
	}
	if jsonOutput {
		displayJSON(result)
	}
	if jq != "" {
		displayJQ(result)
	}
}

func displayBulkUpgradeDetails(unitDetails *goclientnew.UpgradeUnitResponse) {
	tableSuccess := tableView()
	if !noheader {
		tableSuccess.SetHeader([]string{"Name", "ID", "Data-Bytes",
			"Head-Revision", "Apply-Gates",
			"Last-Change"})
	}

	for _, upgraded := range unitDetails.UpgradedUnits {
		applyGates := "None"
		if len(upgraded.ApplyGates) != 0 {
			if len(upgraded.ApplyGates) > 1 {
				applyGates = "Multiple"
			} else {
				for key := range upgraded.ApplyGates {
					applyGates = key
				}
			}
		}
		tableSuccess.Append([]string{
			upgraded.Slug,
			upgraded.UnitID.String(),
			fmt.Sprintf("%d", len(upgraded.Data)),
			fmt.Sprintf("%d", upgraded.HeadRevisionNum),
			applyGates,
			upgraded.LastChangeDescription,
		})
	}

	tableFails := tableView()
	if !noheader {
		tableFails.SetHeader([]string{"ID", "Error"})
	}
	for k, v := range unitDetails.FailedUnits {
		tableSuccess.Append([]string{k, v})
	}
	tableSuccess.Render()
	tableFails.Render()
}
