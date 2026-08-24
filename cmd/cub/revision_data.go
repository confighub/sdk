// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

var revisionDataCmd = &cobra.Command{
	Use:   "data <unit> <revision-num>",
	Short: "Show the config data of a revision",
	Long: getCommandHelp(`Display the configuration data of a specific unit revision, as text.

Replaces 'cub revision get --data-only'.`, ""),
	Args: cobra.ExactArgs(2),
	RunE: runRevisionData,
}

var (
	revisionDataFilename string
	revisionDataDecoded  bool
)

func init() {
	revisionDataCmd.Flags().StringVar(&revisionDataFilename, "filename", "", "Write config data to file instead of stdout")
	// Configuration data is text on the wire, so there is nothing left to decode. The flag
	// stays as a no-op so a script that passes it keeps working.
	revisionDataCmd.Flags().BoolVar(&revisionDataDecoded, "decode", true, "Deprecated: Data is no longer base64-encoded")
	_ = revisionDataCmd.Flags().MarkDeprecated("decode", "Data is no longer base64-encoded")
	revisionCmd.AddCommand(revisionDataCmd)
}

func runRevisionData(cmd *cobra.Command, args []string) error {
	unit, err := apiGetUnitFromSlug(args[0], "*")
	if err != nil {
		return err
	}
	num, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid revision number %q: %w", args[1], err)
	}
	rev, err := apiGetRevisionFromNumber(num, unit.UnitID.String(), "RevisionID,DataHash")
	if err != nil {
		return err
	}
	data, err := fetchRevisionData(unit.SpaceID, unit.UnitID, rev.RevisionID)
	if err != nil {
		return err
	}
	if data == "" {
		return fmt.Errorf("no config data for revision %d of unit %s", num, unit.Slug)
	}

	out := []byte(data)
	if revisionDataFilename != "" {
		if err := os.WriteFile(revisionDataFilename, out, 0o644); err != nil {
			return fmt.Errorf("failed to write config data to file: %w", err)
		}
		if !quiet {
			tprint("Config data written to %s", revisionDataFilename)
		}
		return nil
	}
	tprintBytes(out)
	return nil
}
