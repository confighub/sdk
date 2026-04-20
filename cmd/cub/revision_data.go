// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

var revisionDataCmd = &cobra.Command{
	Use:   "data <unit> <revision-num>",
	Short: "Show the decoded config data of a revision",
	Long: getCommandHelp(`Display the decoded configuration Data of a specific unit revision.

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
	revisionDataCmd.Flags().BoolVar(&revisionDataDecoded, "decode", true, "Decode base64 Data (default: true)")
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
	rev, err := apiGetRevisionFromNumber(num, unit.UnitID.String(), "Data")
	if err != nil {
		return err
	}
	if rev.Data == "" {
		return fmt.Errorf("no config data for revision %d of unit %s", num, unit.Slug)
	}

	out := []byte(rev.Data)
	if revisionDataDecoded {
		decoded, derr := base64.StdEncoding.DecodeString(rev.Data)
		if derr != nil {
			return fmt.Errorf("failed to decode config data: %w", derr)
		}
		out = decoded
	}

	if revisionDataFilename != "" {
		if err := os.WriteFile(revisionDataFilename, out, 0o644); err != nil {
			return fmt.Errorf("failed to write config data to file: %w", err)
		}
		if !quiet {
			tprint("Config data written to %s", revisionDataFilename)
		}
		return nil
	}
	tprintRaw(string(out))
	return nil
}
