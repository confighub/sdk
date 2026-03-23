// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var unitBridgeStateCmd = &cobra.Command{
	Use:   "bridgestate unit-slug-or-id",
	Short: "Show the BridgeState of a unit",
	Long:  getCommandHelp(`Display the BridgeState of a unit.`, ""),
	Args:  cobra.ExactArgs(1),
	Run:   runUnitBridgeState,
}

var (
	bridgeStateOutput  string
	bridgeStateDecoded bool
)

func init() {
	unitBridgeStateCmd.Flags().StringVarP(&bridgeStateOutput, "output", "o", "", "Output BridgeState to file")
	unitBridgeStateCmd.Flags().BoolVarP(&bridgeStateDecoded, "decode", "d", true, "Decode base64 BridgeState (default: true)")
	unitCmd.AddCommand(unitBridgeStateCmd)
}

func runUnitBridgeState(cmd *cobra.Command, args []string) {
	unitSlugOrID := args[0]

	// Get the unit with BridgeState field
	unit, err := apiGetUnitFromSlugInSpace(unitSlugOrID, selectedSpaceID, "Slug,BridgeState")
	if err != nil {
		failOnError(fmt.Errorf("failed to get unit: %w", err))
	}

	if unit.BridgeState == "" {
		fmt.Fprintf(os.Stderr, "No BridgeState found for unit: %s\n", unit.Slug)
		os.Exit(1)
	}

	// Decode BridgeState if needed
	var bridgeStateString string
	if bridgeStateDecoded {
		decoded, err := base64.StdEncoding.DecodeString(unit.BridgeState)
		if err != nil {
			failOnError(fmt.Errorf("failed to decode BridgeState: %w", err))
		}
		bridgeStateString = string(decoded)
	} else {
		bridgeStateString = unit.BridgeState
	}

	// Output to file or stdout
	if bridgeStateOutput != "" {
		err := os.WriteFile(bridgeStateOutput, []byte(bridgeStateString), 0644)
		if err != nil {
			failOnError(fmt.Errorf("failed to write BridgeState to file: %w", err))
		}
		fmt.Printf("BridgeState written to: %s\n", bridgeStateOutput)
	} else {
		fmt.Print(bridgeStateString)
	}
}
