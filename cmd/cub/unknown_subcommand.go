// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// unknownSubcommandError reports the word that named no subcommand of a command
// that only groups subcommands, such as `cub unit frobnicate`.
//
// Cobra refuses an unknown subcommand only at the root. Below it, a command with
// no Run of its own falls back to its help, whatever words follow it, and exits
// 0, so a misspelled verb in a script looks like success. Giving every such
// command a RunE would make cobra run the persistent pre-runs for it (which log
// in and settle a space) just to print help, and would add a "[flags]" usage line
// to help and to the generated reference. Cobra's fallback goes through the help
// function after the command's flags are parsed, so the help function asks this
// first: a parsed command line whose remaining words are not empty, with no
// --help, named a subcommand that does not exist. `cub help unit` and
// `cub unit --help` print help as before, and so does `cub unit` alone.
func unknownSubcommandError(cmd *cobra.Command) error {
	if !cmd.HasParent() || cmd.Runnable() || !cmd.HasSubCommands() || !cmd.Flags().Parsed() {
		return nil
	}
	if help, err := cmd.Flags().GetBool("help"); err == nil && help {
		return nil
	}
	args := cmd.Flags().Args()
	if len(args) == 0 {
		return nil
	}
	msg := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
	// Cobra defaults the distance only on the path it takes at the root.
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = 2
	}
	if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
		msg += "\n\nDid you mean this?\n\t" + strings.Join(suggestions, "\n\t") + "\n"
	}
	return fmt.Errorf("%s\nRun '%s --help' for usage.", msg, cmd.CommandPath())
}
