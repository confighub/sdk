// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var unitLiveStateCmd = &cobra.Command{
	Use:   "livestate unit-slug-or-id",
	Short: "Show the LiveState of a unit",
	Long:  getCommandHelp(`Display the LiveState of a unit.`, ""),
	Args:  cobra.ExactArgs(1),
	Run:   runUnitLiveState,
}

var (
	liveStateOutput  string
	liveStateDecoded bool
)

func init() {
	unitLiveStateCmd.Flags().StringVarP(&liveStateOutput, "output", "o", "", "Output LiveState to file")
	unitLiveStateCmd.Flags().BoolVarP(&liveStateDecoded, "decode", "d", true, "Decode base64 LiveState (default: true)")
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
	var liveStateString string
	if liveStateDecoded {
		decoded, err := base64.StdEncoding.DecodeString(unit.LiveState)
		if err != nil {
			failOnError(fmt.Errorf("failed to decode LiveState: %w", err))
		}
		liveStateString = string(decoded)
	} else {
		liveStateString = unit.LiveState
	}

	// Output to file or stdout
	if liveStateOutput != "" {
		err := os.WriteFile(liveStateOutput, []byte(liveStateString), 0644)
		if err != nil {
			failOnError(fmt.Errorf("failed to write LiveState to file: %w", err))
		}
		fmt.Printf("LiveState written to: %s\n", liveStateOutput)
	} else {
		fmt.Print(liveStateString)
	}
}
