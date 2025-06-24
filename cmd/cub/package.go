// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// The package commands are used to create packages and load packages into an org.
// A package is a serialized collection of spaces, units, links, etc.
//
// This command group is currently experimental.

package main

import (
	"os"

	"github.com/spf13/cobra"
)

var packageCmdGroup = &cobra.Command{
	Use:   "package <command>",
	Short: "package commands",
	Long:  `package commands`,
}

func init() {
	if os.Getenv("CONFIGHUB_EXPERIMENTAL") != "" {
		rootCmd.AddCommand(packageCmdGroup)
	}
}

type PackageManifest struct {
	Spaces  []SpaceEntry  `json:"spaces"`
	Units   []UnitEntry   `json:"units"`
	Targets []TargetEntry `json:"targets"`
	Workers []WorkerEntry `json:"workers"`
}

type SpaceEntry struct {
	Slug       string `json:"slug"`
	DetailsLoc string `json:"details_loc"`
}

type UnitEntry struct {
	Slug        string `json:"slug"`
	SpaceSlug   string `json:"space_slug"`
	DetailsLoc  string `json:"details_loc"`
	UnitDataLoc string `json:"unit_data_loc"`
	Target      string `json:"target"`
}

type TargetEntry struct {
	Slug       string `json:"slug"`
	SpaceSlug  string `json:"space_slug"`
	DetailsLoc string `json:"details_loc"`
	Worker     string `json:"worker"`
}

type WorkerEntry struct {
	Slug       string `json:"slug"`
	SpaceSlug  string `json:"space_slug"`
	DetailsLoc string `json:"details_loc"`
}
