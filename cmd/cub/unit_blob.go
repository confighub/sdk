// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"os"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// Shared flag state for the four `cub unit <blob>` subcommands:
// data, livedata, livestate, bridgestate. Each command resets these globals at
// parse time; at run time exactly one command is dispatched.
var (
	unitBlobOutputFile string
	unitBlobDecode     bool
)

// newUnitBlobCmd builds one of the unit-blob subcommands.
//
//	section:     user-facing name ("data", "livedata", "livestate", "bridgestate")
//	selectField: Unit struct field to select and read ("Data", "LiveData", ...)
//	short, long: command help strings
//	legacyFlags: optional back-compat flag setup (e.g., the deprecated -o/--output
//	             aliases on the three state commands, or the --filename alias on data)
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
	cmd.Flags().BoolVarP(&unitBlobDecode, "decode", "d", true,
		fmt.Sprintf("Decode base64 %s (default: true)", section))
	if legacyFlags != nil {
		legacyFlags(cmd)
	}
	unitCmd.AddCommand(cmd)
	return cmd
}

// unitBlobField returns the raw (still base64-encoded) value of a named blob
// field on a Unit. Returns "" if the field is unknown.
func unitBlobField(u *goclientnew.Unit, field string) string {
	switch field {
	case "Data":
		return u.Data
	case "LiveData":
		return u.LiveData
	case "LiveState":
		return u.LiveState
	case "BridgeState":
		return u.BridgeState
	}
	return ""
}

// runUnitBlob is the shared implementation for the four blob subcommands.
// Fetches the unit with just Slug and the selected blob field, optionally
// base64-decodes, and emits either to stdout or to the configured file.
func runUnitBlob(unitSlugOrID, section, selectField string) error {
	// TODO: Use a dedicated API endpoint when one is available, to avoid
	// pulling the entire unit when only the blob is wanted.
	unit, err := apiGetUnitFromSlugInSpace(unitSlugOrID, selectedSpaceID, "Slug,"+selectField)
	if err != nil {
		return fmt.Errorf("failed to get unit: %w", err)
	}

	raw := unitBlobField(unit, selectField)
	if raw == "" {
		return fmt.Errorf("no %s found for unit: %s", section, unit.Slug)
	}

	out := []byte(raw)
	if unitBlobDecode {
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return fmt.Errorf("failed to decode %s: %w", section, err)
		}
		out = decoded
	}

	if unitBlobOutputFile != "" {
		if err := os.WriteFile(unitBlobOutputFile, out, 0o644); err != nil {
			return fmt.Errorf("failed to write %s to file: %w", section, err)
		}
		if !quiet {
			tprint("%s written to %s", section, unitBlobOutputFile)
		}
		return nil
	}
	tprintRaw(string(out))
	return nil
}

// legacyOutputAlias adds the deprecated --output string flag as an alias for
// --output-file / -O. Used by the three "state" subcommands that had --output
// before the rationalization.
func legacyOutputAlias(cmd *cobra.Command) {
	cmd.Flags().StringVar(&unitBlobOutputFile, "output", "", "Deprecated: use --output-file / -O")
	_ = cmd.Flags().MarkDeprecated("output", "use --output-file / -O")
}

// legacyFilenameAlias adds --filename as an alias for --output-file on the
// `cub unit data` subcommand. Retained because early docs referenced it.
func legacyFilenameAlias(cmd *cobra.Command) {
	cmd.Flags().StringVar(&unitBlobOutputFile, "filename", "", "Deprecated: use --output-file / -O")
	_ = cmd.Flags().MarkDeprecated("filename", "use --output-file / -O")
}
