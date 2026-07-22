// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cub",
	Short: "ConfigHub CLI",
	Long:  getSimpleHelp(),
}

// Global context flag
var globalContextFlag string

// activeContextOverrideSource describes how the active context was overridden for
// this invocation (e.g. by the --context flag or $CUB_CONTEXT), or is empty when
// the persisted current context is in effect. Set by applyContextOverride and
// read by the context display commands so the override is never invisible.
var activeContextOverrideSource string

// resolveContextOverride determines the context override for this invocation and a
// human description of its source. Precedence, highest first: the --context flag,
// then the $CUB_CONTEXT environment variable. An empty name means no override (the
// persisted current context is used).
func resolveContextOverride() (name, source string) {
	if globalContextFlag != "" {
		return globalContextFlag, "--context flag"
	}
	if env := os.Getenv("CUB_CONTEXT"); env != "" {
		return env, "CUB_CONTEXT environment variable"
	}
	return "", ""
}

// applyContextOverride applies the resolved context override (if any) to the
// context manager, recording its source for display. A named-but-missing context
// is a hard error rather than a silent fallback, so commands never run against an
// unexpected context.
func applyContextOverride() error {
	name, source := resolveContextOverride()
	if name == "" {
		return nil
	}
	if err := contextManager.OverrideCurrentContext(name); err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	activeContextOverrideSource = source
	return nil
}

func globalPreRun(cmd *cobra.Command, args []string) error {
	if debug {
		err := os.Setenv("CONFIGHUB_DEBUG", "1")
		if err != nil {
			return err
		}
	} else if os.Getenv("CONFIGHUB_DEBUG") == "1" {
		// Required for the new goclientnew.Client Debug mode
		fmt.Printf("cub Debug mode enabled. version: %s, buildDate: %s\n", BuildTag, BuildDate)
		debug = true
	}
	if err := applyContextOverride(); err != nil {
		return err
	}

	var err error
	cubClientNew, err = InitializeClient(contextManager.ActiveContext())
	if err != nil {
		return err
	}

	return nil
}

func main() {
	var err error
	// CUB_CONFIG is optional. If not set, the default path will be used.

	configPath := os.Getenv("CUB_CONFIG")
	if configPath != "" {
		if info, err := os.Stat(configPath); err == nil && info.IsDir() {
			configPath = filepath.Join(configPath, "config.yaml")
		}
	}
	contextManager, err = NewContextManagerWithPath(configPath)
	if err != nil {
		failOnError(err)
	}
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Debug output")
	rootCmd.PersistentFlags().StringVar(&globalContextFlag, "context", "", "The context to use for this command")

	// Add --help-overview flag
	var helpOverview bool
	rootCmd.Flags().BoolVar(&helpOverview, "help-overview", false, "Show detailed overview instead of standard help")

	// Store original help function before overriding
	originalHelpFunc := rootCmd.HelpFunc()

	// Override the help function to handle --help-overview
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if helpOverview {
			fmt.Print(getFormattedOverview())
			return
		}
		// Use the original help function
		originalHelpFunc(cmd, args)
	})

	// This turns off printing Usage after an error
	rootCmd.SilenceUsage = true
	// We don't want root command to print errors. We'll do it ourselves.
	rootCmd.SilenceErrors = true

	rootCmd.PersistentPreRunE = globalPreRun

	// Include plugin command names in shell tab-completion (canonical names only).
	rootCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var completions []string
		for _, p := range discoverPlugins() {
			for _, c := range p.Commands {
				if c.Entrypoint == "" || c.Name == "" {
					continue
				}
				if strings.HasPrefix(c.Name, toComplete) {
					completions = append(completions, c.Name)
				}
			}
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}

	cmd, err := rootCmd.ExecuteC()
	if err != nil {
		// If the error is "unknown command" at the root level, try plugin resolution.
		if cmd == rootCmd && isUnknownCommandError(err) {
			// Apply context override manually since PersistentPreRunE may not have run.
			if overrideErr := applyContextOverride(); overrideErr != nil {
				failOnError(overrideErr)
			}
			pluginArgs := extractPluginArgs(os.Args[1:])
			found, pluginErr := handlePluginCommand(pluginArgs)
			if found {
				// The command resolved to a plugin but failed to exec — show why.
				failOnError(pluginErr)
			}
			// No such plugin command — show the original Cobra error.
			failOnError(err)
		}
		failOnError(err)
	}
}

// isUnknownCommandError checks whether a Cobra error is an "unknown command" error.
func isUnknownCommandError(err error) bool {
	return strings.Contains(err.Error(), "unknown command")
}

// extractPluginArgs returns the arguments intended for plugin resolution by
// stripping known persistent flags (--context, --debug) from os.Args.
func extractPluginArgs(args []string) []string {
	var result []string
	skip := false
	for _, arg := range args {
		if skip {
			skip = false
			continue
		}
		if arg == "--debug" {
			continue
		}
		if arg == "--context" {
			skip = true // skip next arg (the value)
			continue
		}
		if strings.HasPrefix(arg, "--context=") {
			continue
		}
		result = append(result, arg)
	}
	return result
}
