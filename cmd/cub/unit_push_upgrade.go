// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var unitPushUpgradeCmd = &cobra.Command{
	Use:   "push-upgrade <name>",
	Short: "Upgrade downstreams from unit",
	Long:  getUnitPushUpgradeHelp(),
	Args:  cobra.ExactArgs(1),
	RunE:  unitBulkUpgradeCmdRun,
}

func getUnitPushUpgradeHelp() string {
	baseHelp := `Upgrade all downstream units that depend on the specified upstream unit. This command finds all units that have the specified unit as their upstream source and upgrades them to match the latest version if they are behind.

The push-upgrade operation only affects downstream units where:
- Unit.UpstreamUnitID matches the specified unit
- Unit.UpstreamRevisionNum is less than the upstream unit's HeadRevisionNum

This is useful for propagating changes from a template or base configuration to all dependent units across your infrastructure.

Examples:
  # Upgrade all downstream units from a template unit
  cub unit push-upgrade --space my-space base-template

  # Upgrade downstream units with verbose output showing details
  cub unit push-upgrade --space my-space base-template --verbose

  # Upgrade and get JSON response for programmatic use
  cub unit push-upgrade --space my-space base-template --json

  # Upgrade with specific field selection using jq
  cub unit push-upgrade --space my-space base-template --jq '.[] | select(.Unit) | .Unit | {Slug, UnitID, HeadRevisionNum}'`

	agentContext := `Essential for maintaining consistency across dependent configurations.

Agent push-upgrade workflow:
1. Identify the upstream/template unit by slug
2. Execute bulk push-upgrade operation on all downstream units
3. Review results for any partial failures or issues
4. Handle any units that failed to upgrade

Common use cases:

Upgrade from template unit:
  cub unit push-upgrade --space SPACE template-name --verbose

Get structured push-upgrade results:
  cub unit push-upgrade --space SPACE template-name --json

Check specific push-upgrade outcomes:
  cub unit push-upgrade --space SPACE template-name --jq '.[] | select(.Error) | {Unit: .Unit.Slug, Error: .Error.Message}'

Key flags for agents:
- --verbose: Show detailed information about upgraded units
- --json: Get structured response with full push-upgrade details
- --jq: Extract specific information from push-upgrade results
- --quiet: Suppress default output for programmatic use

Post-upgrade workflow:
1. Review push-upgrade results for any failures
2. Check individual units that failed to upgrade for specific issues
3. Use 'unit get' to verify upgrade succeeded for critical units
4. Monitor apply gates and triggers for upgraded units`

	return getCommandHelp(baseHelp, agentContext)
}

func init() {
	addStandardUpdateFlags(unitPushUpgradeCmd)
	enableWaitFlag(unitPushUpgradeCmd)
	unitCmd.AddCommand(unitPushUpgradeCmd)
}

func unitBulkUpgradeCmdRun(cmd *cobra.Command, args []string) error {
	currentUnit, err := apiGetUnitFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}

	// Build WHERE clause for downstream units that need upgrading
	whereClause := fmt.Sprintf("Unit.UpstreamUnitID = '%s' AND Unit.UpstreamRevisionNum < UpstreamUnit.HeadRevisionNum", currentUnit.UnitID.String())

	// Build bulk patch parameters
	upgrade := true
	params := &goclientnew.BulkPatchUnitsParams{
		Where:   &whereClause,
		Upgrade: &upgrade, // Set upgrade to true
	}

	// Set include parameter to expand UpstreamUnitID
	include := "UpstreamUnitID"
	params.Include = &include

	// Use "null" as the patch body since we're only upgrading
	patchData := []byte("null")

	// Call the bulk patch API
	bulkRes, err := cubClientNew.BulkPatchUnitsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if IsAPIError(err, bulkRes) {
		return InterpretErrorGeneric(err, bulkRes)
	}

	// Handle response based on status code
	var responses *[]goclientnew.UnitCreateOrUpdateResponse
	var statusCode int

	if bulkRes.JSON200 != nil {
		responses = bulkRes.JSON200
		statusCode = 200
	} else if bulkRes.JSON207 != nil {
		responses = bulkRes.JSON207
		statusCode = 207
	} else {
		return fmt.Errorf("unexpected response from bulk patch API")
	}

	return handleBulkCreateOrUpdateResponse(responses, statusCode, "upgrade", fmt.Sprintf("%s (%s)", args[0], currentUnit.UnitID.String()))
}
