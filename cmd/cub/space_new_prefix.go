// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	cubbyname "github.com/confighub/sdk/cubbyname"
	"github.com/spf13/cobra"
)

var spaceNewPrefixCmd = &cobra.Command{
	Use:   "new-prefix",
	Short: "Generate a random space name that is not a prefix of existing spaces",
	Long: getCommandHelp(`Generate a random space name using cubbyname.Random() until finding one that is not a prefix of any existing space.

This command is useful for creating unique space names that won't conflict with existing spaces.

Examples:
`+"```"+`
  # Generate a new unique space name
  cub space new-prefix

  # Generate a new unique space name in JSON format
  cub space new-prefix --json
`+"```"+`
`, ""),
	RunE: spaceNewPrefixCmdRun,
}

var spaceNewPrefixArgs struct {
	json bool
}

func init() {
	spaceNewPrefixCmd.Flags().BoolVar(&spaceNewPrefixArgs.json, "json", false, "Output in JSON format")
	spaceCmd.AddCommand(spaceNewPrefixCmd)
}

func spaceNewPrefixCmdRun(cmd *cobra.Command, args []string) error {
	// Get all existing spaces
	spaces, err := apiListSpaces("", "*")
	if err != nil {
		return fmt.Errorf("failed to list existing spaces: %w", err)
	}

	// Extract space slugs
	existingSlugs := make([]string, len(spaces))
	for i, space := range spaces {
		existingSlugs[i] = space.Slug
	}

	// Generate random names until we find one that's not a prefix
	for range 1000 {
		candidateName := cubbyname.Random()

		// Check if this name is a prefix of any existing space
		isPrefix := false
		for _, slug := range existingSlugs {
			if strings.HasPrefix(slug, candidateName) {
				isPrefix = true
				break
			}
		}

		if !isPrefix {
			if spaceNewPrefixArgs.json {
				output := map[string]string{"prefix": candidateName}
				jsonBytes, err := json.MarshalIndent(output, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON output: %w", err)
				}
				fmt.Println(string(jsonBytes))
			} else {
				fmt.Println(candidateName)
			}
			return nil
		}
	}

	return fmt.Errorf("failed to generate a unique space name after 1000 attempts")
}
