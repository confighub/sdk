// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var unitCmd = &cobra.Command{
	Use:               "unit",
	Short:             "Unit commands",
	Long:              getUnitCommandGroupHelp(),
	PersistentPreRunE: spacePreRunE,
}

// Unit-specific destroy gate support
var destroyGate []string

func getUnitCommandGroupHelp() string {
	baseHelp := `The unit subcommands are used to manage units.

Units are explained at https://docs.confighub.com/background/entities/unit/.`
	agentContext := `Units are the core configuration entities in ConfigHub, containing structured data like Kubernetes YAML, properties files, or HCL configurations.

Unit lifecycle workflow:
1. Create units from configuration files ('unit create')
2. Inspect and modify units using functions ('function do')
3. Validate configuration ('function do' with validation functions)
4. Approve units for deployment ('unit approve')
5. Publish a Release for the space ('release publish'), which Argo CD / Flux pull

Key commands for agents:
- 'unit list' - discover existing units with filtering
- 'unit get' - retrieve unit details and configuration data
- 'unit create' - create new units from local files
- 'unit update' - modify existing units

Units are scoped to spaces and can be linked to other units to model dependencies.`

	return getCommandHelp(baseHelp, agentContext)
}

func init() {
	addSpaceFlags(unitCmd)
	rootCmd.AddCommand(unitCmd)
	addExplainCmd(unitCmd, "Unit")
}

// buildWhereClauseFromUnits generates a WHERE clause from unit identifiers
func buildWhereClauseFromUnits(unitIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(unitIds, "UnitID", "Slug")
}

// setDestroyGates processes destroy gate flags and updates the provided destroy gate map
func setDestroyGatesField(destroyGateMap *map[string]bool) error {
	if destroyGate != nil && len(destroyGate) != 0 {
		if *destroyGateMap == nil {
			*destroyGateMap = map[string]bool{}
		}
		for _, destroyGateString := range destroyGate {
			keyValue := strings.Split(destroyGateString, "=")
			switch len(keyValue) {
			case 1:
				(*destroyGateMap)[keyValue[0]] = true
			case 2:
				value := keyValue[1]
				if value != "true" {
					return fmt.Errorf("invalid destroy-gate value; only 'true' is allowed: %s", destroyGateString)
				}
				(*destroyGateMap)[keyValue[0]] = true
			default:
				return fmt.Errorf("invalid destroy-gate; expected key or key=true: %s", destroyGateString)
			}
		}
	}
	return nil
}

func setDestroyGatesInPatch(patchMap map[string]interface{}) error {
	if len(destroyGate) != 0 {
		var destroyGateMap map[string]interface{}

		// Preserve existing destroy gates in the patch if any
		if existingDestroyGates, ok := patchMap["DestroyGates"]; ok {
			destroyGateMap = existingDestroyGates.(map[string]interface{})
		} else {
			destroyGateMap = make(map[string]interface{})
		}

		for _, destroyGateString := range destroyGate {
			keyValue := strings.Split(destroyGateString, "=")
			key := keyValue[0]
			switch len(keyValue) {
			case 1:
				destroyGateMap[key] = true
			case 2:
				value := keyValue[1]
				if value == "-" {
					destroyGateMap[key] = nil
				} else if value != "true" {
					return fmt.Errorf("invalid destroy-gate value; only 'true' is allowed: %s", destroyGateString)
				} else {
					destroyGateMap[key] = true
				}
			default:
				return fmt.Errorf("invalid destroy-gate; expected key or key=true: %s", destroyGateString)
			}
		}

		patchMap["DestroyGates"] = destroyGateMap
	}
	return nil
}

// destroyGatesToString converts a destroy gate map to a display string
func destroyGatesToString(destroyGates map[string]bool) string {
	if len(destroyGates) == 0 {
		return ""
	}

	keys := make([]string, 0, len(destroyGates))
	for key := range destroyGates {
		keys = append(keys, key)
	}
	return strings.Join(keys, ", ")
}

// enableDestroyGateFlag adds the --destroy-gate flag to the given command
func enableDestroyGateFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&destroyGate, "destroy-gate", []string{}, "destroy gate for the unit (can be repeated or comma-separated)")
}

// ValidateDestroyGateRemoval validates that destroy gate removal is only attempted in patch mode
func ValidateDestroyGateRemoval(destroyGates []string, isPatchMode bool) error {
	if !isPatchMode {
		for _, destroyGateString := range destroyGates {
			keyValue := strings.Split(destroyGateString, "=")
			if len(keyValue) == 2 && keyValue[1] == "-" {
				return fmt.Errorf("destroy gate removal (--destroy-gate %s) requires --patch flag", destroyGateString)
			}
		}
	}
	return nil
}
