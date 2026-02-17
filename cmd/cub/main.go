// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cub",
	Short: "ConfigHub CLI",
	Long:  getSimpleHelp(),
}

// Global context flag
var globalContextFlag string

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
	var err error
	if globalContextFlag != "" {
		err = contextManager.OverrideCurrentContext(globalContextFlag)
		if err != nil {
			return err
		}
	}

	cubClientNew, err = InitializeClient(contextManager.ActiveContext())
	if err != nil {
		return err
	}

	return nil
}

func main() {
	var err error
	// CUB_CONFIG is optional. If not set, the default path will be used.

	contextManager, err = NewContextManagerWithPath(os.Getenv("CUB_CONFIG"))
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

	err = rootCmd.Execute()
	failOnError(err)
}
