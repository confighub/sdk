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

var unitLiveDataCmd = &cobra.Command{
	Use:   "livedata unit-slug-or-id",
	Short: "Show the LiveData of a unit",
	Long: getCommandHelp(`Display the LiveData YAML of a unit, which includes the inventory ConfigMap
used for tracking applied resources and the actual Kubernetes resources.

The inventory ConfigMap appears at the beginning of the LiveData and contains:

- Labels: cli-utils.sigs.k8s.io/inventory-id
- Annotations: config.k8s.io/function: inventory
- Data: Object references for pruning and lifecycle management`, ""),
	Args: cobra.ExactArgs(1),
	Run:  runUnitLiveData,
}

var (
	liveDataOutput        string
	liveDataDecoded       bool
	liveDataInventoryOnly bool
)

func init() {
	unitLiveDataCmd.Flags().StringVarP(&liveDataOutput, "output", "o", "", "Output LiveData to file")
	unitLiveDataCmd.Flags().BoolVarP(&liveDataDecoded, "decode", "d", true, "Decode base64 LiveData (default: true)")
	unitLiveDataCmd.Flags().BoolVarP(&liveDataInventoryOnly, "inventory-only", "i", false, "Show only the inventory ConfigMap")
	unitCmd.AddCommand(unitLiveDataCmd)
}

func runUnitLiveData(cmd *cobra.Command, args []string) {
	unitSlugOrID := args[0]

	// Get the unit with LiveData field
	unit, err := apiGetUnitFromSlugInSpace(unitSlugOrID, selectedSpaceID, "Slug,LiveData")
	if err != nil {
		failOnError(fmt.Errorf("failed to get unit: %w", err))
	}

	if unit.LiveData == "" {
		fmt.Fprintf(os.Stderr, "No LiveData found for unit: %s\n", unit.Slug)
		os.Exit(1)
	}

	// Decode LiveData if needed
	var liveDataYAML string
	if liveDataDecoded {
		decoded, err := base64.StdEncoding.DecodeString(unit.LiveData)
		if err != nil {
			failOnError(fmt.Errorf("failed to decode LiveData: %w", err))
		}
		liveDataYAML = string(decoded)
	} else {
		liveDataYAML = unit.LiveData
	}

	// If inventory-only flag is set, extract just the inventory ConfigMap
	if liveDataInventoryOnly {
		liveDataYAML = extractInventoryConfigMap(liveDataYAML)
		if liveDataYAML == "" {
			fmt.Fprintf(os.Stderr, "No inventory ConfigMap found in LiveData for unit: %s\n", unit.Slug)
			os.Exit(1)
		}
	}

	// Output to file or stdout
	if liveDataOutput != "" {
		err := os.WriteFile(liveDataOutput, []byte(liveDataYAML), 0644)
		if err != nil {
			failOnError(fmt.Errorf("failed to write LiveData to file: %w", err))
		}
		fmt.Printf("LiveData written to: %s\n", liveDataOutput)
	} else {
		fmt.Print(liveDataYAML)
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
