// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// Shared flag state for the `cub unit <blob>` subcommands. Each command resets these
// globals at parse time; at run time exactly one command is dispatched.
var (
	unitBlobOutputFile string
	unitBlobDecode     bool
)

// newUnitBlobCmd builds one of the unit-blob subcommands.
//
//	section:     user-facing name ("data")
//	selectField: Unit struct field to select and read ("Data")
//	short, long: command help strings
//	legacyFlags: optional back-compat flag setup (e.g., the --filename alias on data)
func newUnitBlobCmd(section, selectField, short, long string, legacyFlags func(*cobra.Command)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   section + " <unit>",
		Short: short,
		Long:  getCommandHelp(long, ""),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUnitBlob(args[0], section, selectField)
		},
	}
	cmd.Flags().StringVarP(&unitBlobOutputFile, "output-file", "O", "",
		fmt.Sprintf("Write %s to file instead of stdout", section))
	// Configuration data is text on the wire, so there is nothing left to decode. The flag
	// stays as a no-op so a script that passes it keeps working.
	cmd.Flags().BoolVarP(&unitBlobDecode, "decode", "d", true,
		fmt.Sprintf("Deprecated: %s is no longer base64-encoded", section))
	_ = cmd.Flags().MarkDeprecated("decode", section+" is no longer base64-encoded")
	if legacyFlags != nil {
		legacyFlags(cmd)
	}
	unitCmd.AddCommand(cmd)
	return cmd
}

// unitBlobField returns the value of a named blob field on a Unit. Returns "" if the field
// is unknown. The configuration is not part of the Unit, so it is read from its endpoint.
func unitBlobField(u *goclientnew.Unit, field string) (string, error) {
	if field == "Data" {
		return fetchUnitData(u.SpaceID, u.UnitID)
	}
	return "", nil
}

// runUnitBlob is the shared implementation for the blob subcommands. It resolves the Unit
// by slug and reads the blob from its own endpoint, so the metadata request carries only
// what it needs to identify the Unit -- the blob is not a field to select any more.
func runUnitBlob(unitSlugOrID, section, selectField string) error {
	unit, err := apiGetUnitFromSlugInSpace(unitSlugOrID, selectedSpaceID, "UnitID,SpaceID,Slug")
	if err != nil {
		return fmt.Errorf("failed to get unit: %w", err)
	}

	raw, err := unitBlobField(unit, selectField)
	if err != nil {
		return err
	}
	if raw == "" {
		return fmt.Errorf("no %s found for unit: %s", section, unit.Slug)
	}

	out := []byte(raw)
	if unitBlobOutputFile != "" {
		if err := os.WriteFile(unitBlobOutputFile, out, 0o644); err != nil {
			return fmt.Errorf("failed to write %s to file: %w", section, err)
		}
		if !quiet {
			tprint("%s written to %s", section, unitBlobOutputFile)
		}
		return nil
	}
	tprintBytes(out)
	return nil
}

// legacyFilenameAlias adds --filename as an alias for --output-file on the
// `cub unit data` subcommand. Retained because early docs referenced it.
func legacyFilenameAlias(cmd *cobra.Command) {
	cmd.Flags().StringVar(&unitBlobOutputFile, "filename", "", "Deprecated: use --output-file / -O")
	_ = cmd.Flags().MarkDeprecated("filename", "use --output-file / -O")
}
