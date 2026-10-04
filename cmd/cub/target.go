// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var targetCmd = &cobra.Command{
	Use:   "target",
	Short: "Target commands",
	Long: getCommandHelp(`The target subcommands are used to manage targets.

Targets are explained at https://docs.confighub.com/background/entities/target/.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(targetCmd)
	rootCmd.AddCommand(targetCmd)
	addExplainCmd(targetCmd, "Target")
}

// targetPatchEnhancer adds --fact, --where-trigger and --trigger-filter to the patch a bulk create
// or bulk patch of Targets sends. "-" clears WhereTrigger or TriggerFilterID.
func targetPatchEnhancer(whereTrigger, triggerFilter string) (PatchEnhancer, error) {
	var triggerFilterUUID *uuid.UUID
	if triggerFilter != "" && triggerFilter != "-" {
		triggerFilterID, err := parseFilterFlag(triggerFilter)
		if err != nil {
			return nil, err
		}
		parsed := uuid.MustParse(triggerFilterID)
		triggerFilterUUID = &parsed
	}
	return func(patchMap map[string]interface{}) {
		if len(fact) > 0 {
			factMap := make(map[string]interface{})
			if existingFacts, ok := patchMap["Facts"]; ok {
				if factMapInterface, ok := existingFacts.(map[string]interface{}); ok {
					for k, v := range factMapInterface {
						factMap[k] = v
					}
				}
			}
			_ = patchKeyValues(factMap, fact)
			patchMap["Facts"] = factMap
		}
		if whereTrigger == "-" {
			patchMap["WhereTrigger"] = ""
		} else if whereTrigger != "" {
			patchMap["WhereTrigger"] = whereTrigger
		}
		if triggerFilter == "-" {
			patchMap["TriggerFilterID"] = nil
		} else if triggerFilterUUID != nil {
			patchMap["TriggerFilterID"] = triggerFilterUUID.String()
		}
	}, nil
}
