// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/confighub/sdk/workerapi"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitCreateCmd = &cobra.Command{
	Use:   "create <slug> [config-file]",
	Short: "Create a unit",
	Long: `Create a new unit in a space. A unit represents a collection of related resources that can be managed together.

Units can be created in several ways:
  1. From a configuration file
  2. By cloning an existing upstream unit (using --upstream-unit)
  3. From stdin (using '-' as the config-file)

Examples:
  # Create a unit from a YAML file with triggers
  cub unit create --space my-space --json --from-stdin myns --wait test-ns.yaml

  # Create a unit for a deployment
  cub unit create --space my-space --json --from-stdin mydeployment deployment.yaml

  # Create a unit for a complex application
  cub unit create --space my-space --json --from-stdin headlamp kubernetes-headlamp.yaml

  # Clone an existing unit
  cub unit create --space my-space --json --from-stdin myclone --upstream-unit sample-deployment`,
	Args: cobra.RangeArgs(1, 2),
	RunE: unitCreateCmdRun,
}

var unitCreateArgs struct {
	upstreamUnitSlug  string
	upstreamSpaceSlug string
	importUnitSlug    string
	toolchainType     string
	targetSlug        string
}

func init() {
	addStandardCreateFlags(unitCreateCmd)
	enableWaitFlag(unitCreateCmd)
	unitCreateCmd.Flags().StringVar(&unitCreateArgs.targetSlug, "target", "", "target for the unit")
	unitCreateCmd.Flags().StringVar(&unitCreateArgs.upstreamUnitSlug, "upstream-unit", "", "upstream unit slug to clone")
	unitCreateCmd.Flags().StringVar(&unitCreateArgs.upstreamSpaceSlug, "upstream-space", "", "space slug of upstream unit to clone")
	unitCreateCmd.Flags().StringVar(&unitCreateArgs.importUnitSlug, "import", "", "source unit slug")
	// default to ToolchainKubernetesYAML
	unitCreateCmd.Flags().StringVarP(&unitCreateArgs.toolchainType, "toolchain", "t", string(workerapi.ToolchainKubernetesYAML), "toolchain type")
	unitCmd.AddCommand(unitCreateCmd)
}

func unitCreateCmdRun(cmd *cobra.Command, args []string) error {
	spaceID := uuid.MustParse(selectedSpaceID)
	newUnit := &goclientnew.Unit{}
	newParams := &goclientnew.CreateUnitParams{}
	if flagPopulateModelFromStdin {
		if err := populateNewModelFromStdin(newUnit); err != nil {
			return err
		}
	}
	err := setLabels(&newUnit.Labels)
	if err != nil {
		return err
	}
	var upstreamSpaceID, upstreamUnitID uuid.UUID
	if unitCreateArgs.upstreamSpaceSlug != "" {
		upstreamSpace, err := apiGetSpaceFromSlug(unitCreateArgs.upstreamSpaceSlug)
		if err != nil {
			return err
		}
		upstreamSpaceID = upstreamSpace.SpaceID
	}
	if unitCreateArgs.upstreamUnitSlug != "" {
		if unitCreateArgs.upstreamSpaceSlug == "" {
			upstreamSpaceID = spaceID
		}
		upstreamUnit, err := apiGetUnitFromSlugInSpace(unitCreateArgs.upstreamUnitSlug, upstreamSpaceID.String())
		if err != nil {
			return err
		}
		upstreamUnitID = upstreamUnit.UnitID
	}
	if unitCreateArgs.targetSlug != "" {
		target, err := apiGetTargetFromSlug(unitCreateArgs.targetSlug, selectedSpaceID)
		if err != nil {
			return err
		}
		newUnit.TargetID = &target.Target.TargetID
	}

	// If these were set from stdin, they will be overridden
	newUnit.SpaceID = spaceID
	newUnit.Slug = makeSlug(args[0])
	if newUnit.DisplayName == "" {
		newUnit.DisplayName = args[0]
	}
	newUnit.ToolchainType = unitCreateArgs.toolchainType

	if unitCreateArgs.upstreamUnitSlug != "" {
		newParams.UpstreamSpaceId = &upstreamSpaceID
		newParams.UpstreamUnitId = &upstreamUnitID
	}

	// Read test payload
	if len(args) > 1 {
		if unitCreateArgs.upstreamUnitSlug != "" {
			return errors.New("shouldn't specify both an upstream to clone and config data")
		}
		var content strfmt.Base64
		if args[1] == "-" {
			if flagPopulateModelFromStdin {
				return errors.New("can't read both entity attributes and config data from stdin")
			}
			content, err = readStdin()
			if err != nil {
				return err
			}
		} else {
			content = readFile(args[1])
		}
		newUnit.Data = content.String()
	}

	unitRes, err := cubClientNew.CreateUnitWithResponse(ctx, spaceID, newParams, *newUnit)
	if IsAPIError(err, unitRes) {
		return InterpretErrorGeneric(err, unitRes)
	}

	unitDetails := unitRes.JSON200
	if wait {
		err = awaitTriggersRemoval(unitDetails)
		if err != nil {
			return err
		}
	}
	displayCreateResults(unitDetails, "unit", args[0], unitDetails.UnitID.String(), displayUnitDetails)
	return nil
}
