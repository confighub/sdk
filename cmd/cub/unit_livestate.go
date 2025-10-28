// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var unitLiveStateCmd = &cobra.Command{
	Use:   "livestate [unit-slug-or-id]",
	Short: "Show the LiveState of a unit",
	Long: getCommandHelp(`Display the LiveState YAML of a unit, which includes the inventory ConfigMap
used for tracking applied resources and the actual Kubernetes resources.

The inventory ConfigMap appears at the beginning of the LiveState and contains:

- Labels: cli-utils.sigs.k8s.io/inventory-id
- Annotations: config.k8s.io/function: inventory
- Data: Object references for pruning and lifecycle management`, ""),
	Args: cobra.ExactArgs(1),
	Run:  runUnitLiveState,
}

var (
	liveStateOutput        string
	liveStateDecoded       bool
	liveStateInventoryOnly bool
)

func init() {
	unitLiveStateCmd.Flags().StringVarP(&liveStateOutput, "output", "o", "", "Output LiveState to file")
	unitLiveStateCmd.Flags().BoolVarP(&liveStateDecoded, "decode", "d", true, "Decode base64 LiveState (default: true)")
	unitLiveStateCmd.Flags().BoolVarP(&liveStateInventoryOnly, "inventory-only", "i", false, "Show only the inventory ConfigMap")
	unitCmd.AddCommand(unitLiveStateCmd)
}

func runUnitLiveState(cmd *cobra.Command, args []string) {
	unitSlugOrID := args[0]

	// Get the unit with LiveState field
	unit, err := apiGetUnitFromSlugInSpace(unitSlugOrID, selectedSpaceID, "Slug,LiveState")
	if err != nil {
		failOnError(fmt.Errorf("failed to get unit: %w", err))
	}

	if unit.LiveState == "" {
		fmt.Fprintf(os.Stderr, "No LiveState found for unit: %s\n", unit.Slug)
		os.Exit(1)
	}

	// Decode LiveState if needed
	var liveStateYAML string
	if liveStateDecoded {
		decoded, err := base64.StdEncoding.DecodeString(unit.LiveState)
		if err != nil {
			failOnError(fmt.Errorf("failed to decode LiveState: %w", err))
		}
		liveStateYAML = string(decoded)
	} else {
		liveStateYAML = unit.LiveState
	}

	// If inventory-only flag is set, extract just the inventory ConfigMap
	if liveStateInventoryOnly {
		liveStateYAML = extractInventoryConfigMap(liveStateYAML)
		if liveStateYAML == "" {
			fmt.Fprintf(os.Stderr, "No inventory ConfigMap found in LiveState for unit: %s\n", unit.Slug)
			os.Exit(1)
		}
	}

	// Output to file or stdout
	if liveStateOutput != "" {
		err := os.WriteFile(liveStateOutput, []byte(liveStateYAML), 0644)
		if err != nil {
			failOnError(fmt.Errorf("failed to write LiveState to file: %w", err))
		}
		fmt.Printf("LiveState written to: %s\n", liveStateOutput)
	} else {
		fmt.Print(liveStateYAML)
	}
}

// extractInventoryConfigMap extracts the first ConfigMap that looks like an inventory
func extractInventoryConfigMap(yamlContent string) string {
	// Split by YAML document separator
	documents := strings.Split(yamlContent, "\n---\n")

	for _, doc := range documents {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		// Check if this document is a ConfigMap with inventory markers
		if strings.Contains(doc, "kind: ConfigMap") &&
			(strings.Contains(doc, "cli-utils.sigs.k8s.io/inventory-id") ||
				strings.Contains(doc, "config.k8s.io/function: inventory") ||
				strings.Contains(doc, "name: inventory")) {
			return doc
		}
	}

	return ""
}
