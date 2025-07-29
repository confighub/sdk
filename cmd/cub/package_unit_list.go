// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// The unit list command lists units in a package directory.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var packageUnitListCmd = &cobra.Command{
	Use:   "list <dir>",
	Short: "list units in a package directory",
	Long:  `list units in a package directory with optional label columns`,
	Args:  cobra.ExactArgs(1),
	RunE:  packageUnitListCmdRun,
}

func init() {
	packageUnitListCmd.Flags().StringSlice("label-columns", []string{}, "comma-separated list of label keys to display as columns")
	packageUnitCmd.AddCommand(packageUnitListCmd)
}

type UnitDetails struct {
	Slug   string            `json:"Slug"`
	Labels map[string]string `json:"Labels"`
}

func packageUnitListCmdRun(cmd *cobra.Command, args []string) error {
	dir := args[0]
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("package directory does not exist: %s", dir)
	}

	labelColumns, err := cmd.Flags().GetStringSlice("label-columns")
	if err != nil {
		return err
	}

	// Load manifest
	manifest, err := loadManifest(dir)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %v", err)
	}

	// Sort units by slug for consistent output
	sort.Slice(manifest.Units, func(i, j int) bool {
		return manifest.Units[i].Slug < manifest.Units[j].Slug
	})

	// Create tab writer for aligned output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	// Print header
	header := []string{"SLUG", "SPACE"}
	for _, label := range labelColumns {
		header = append(header, strings.ToUpper(label))
	}
	fmt.Fprintln(w, strings.Join(header, "\t"))

	// Process each unit
	for _, unit := range manifest.Units {
		// Load unit details to get labels
		unitDetails, err := loadUnitDetailsForList(dir, unit)
		if err != nil {
			// If we can't load details, still show the basic info
			row := []string{unit.Slug, unit.SpaceSlug}
			for range labelColumns {
				row = append(row, "-")
			}
			fmt.Fprintln(w, strings.Join(row, "\t"))
			continue
		}

		// Build row
		row := []string{unit.Slug, unit.SpaceSlug}
		for _, labelKey := range labelColumns {
			if value, exists := unitDetails.Labels[labelKey]; exists {
				row = append(row, value)
			} else {
				row = append(row, "-")
			}
		}
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	return nil
}

func loadUnitDetailsForList(dir string, unit UnitEntry) (*UnitDetails, error) {
	jsonBytes, err := os.ReadFile(dir + unit.DetailsLoc)
	if err != nil {
		return nil, err
	}

	unitDetails := &UnitDetails{}
	err = json.Unmarshal(jsonBytes, unitDetails)
	if err != nil {
		return nil, err
	}

	return unitDetails, nil
}
