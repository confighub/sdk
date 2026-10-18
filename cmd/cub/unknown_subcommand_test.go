// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// executeForHelp runs the command tree on args the way main does and returns
// what unknownSubcommandError said when cobra fell back to help, and whether it
// did. Only command lines that end in help are passed, so no command runs.
func executeForHelp(t *testing.T, args ...string) (helped bool, err error) {
	t.Helper()
	savedHelp := rootCmd.HelpFunc()
	t.Cleanup(func() {
		// Flag values outlive an execution, so a --help would carry over.
		var resetHelp func(c *cobra.Command)
		resetHelp = func(c *cobra.Command) {
			if f := c.Flags().Lookup("help"); f != nil {
				_ = f.Value.Set("false")
				f.Changed = false
			}
			for _, sub := range c.Commands() {
				resetHelp(sub)
			}
		}
		resetHelp(rootCmd)
		rootCmd.SetHelpFunc(savedHelp)
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		helped = true
		err = unknownSubcommandError(cmd)
	})
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs(args)
	if _, execErr := rootCmd.ExecuteC(); execErr != nil {
		t.Fatalf("cub %s: %v", strings.Join(args, " "), execErr)
	}
	return helped, err
}

// Every command that only groups subcommands refuses a word that names none of
// them, as the root does, rather than printing help and exiting 0.
func TestUnknownSubcommandOfEveryParentIsRefused(t *testing.T) {
	var parents []*cobra.Command
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.HasSubCommands() && !sub.Runnable() {
				parents = append(parents, sub)
			}
			walk(sub)
		}
	}
	walk(rootCmd)
	if len(parents) == 0 {
		t.Fatal("found no commands that only group subcommands")
	}
	t.Logf("%d commands only group subcommands", len(parents))
	for _, parent := range parents {
		path := strings.Fields(parent.CommandPath())[1:]
		helped, err := executeForHelp(t, append(path, "frobnicate")...)
		if !helped {
			t.Errorf("%s frobnicate: cobra did not fall back to help", parent.CommandPath())
			continue
		}
		want := `unknown command "frobnicate" for "` + parent.CommandPath() + `"`
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s frobnicate: error %v, want one containing %s", parent.CommandPath(), err, want)
		}
	}
}

func TestUnknownSubcommandHelpStillWorks(t *testing.T) {
	for _, args := range [][]string{
		{"unit"},
		{"unit", "--help"},
		{"unit", "frobnicate", "--help"},
		{"help", "unit"},
		{"--help"},
	} {
		helped, err := executeForHelp(t, args...)
		if !helped {
			t.Errorf("cub %s: printed no help", strings.Join(args, " "))
		}
		if err != nil {
			t.Errorf("cub %s: %v", strings.Join(args, " "), err)
		}
	}
}

func TestUnknownSubcommandSuggests(t *testing.T) {
	_, err := executeForHelp(t, "unit", "lst")
	if err == nil || !strings.Contains(err.Error(), "Did you mean this?\n\tget\n\tlist") {
		t.Errorf("cub unit lst: %v", err)
	}
}
