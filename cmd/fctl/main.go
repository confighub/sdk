// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/confighub/sdk/function/client"
	"github.com/confighub/sdk/workerapi"
	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var transportConfig *client.TransportConfig
var toolchainString string
var toolchain workerapi.ToolchainType

// This CLI is for testing the reference function webhook receiver.
func main() {
	if os.Getenv("CONFIGHUB_FUNCTION_HOST") != "" {
		splitHost := strings.Split(os.Getenv("CONFIGHUB_FUNCTION_HOST"), "://")
		transportConfig = &client.TransportConfig{
			Host:      splitHost[1],
			BasePath:  "/function",
			Scheme:    splitHost[0],
			UserAgent: "fctl",
		}
	} else {
		// During early development this is the default. Later on, we'll get the default from openapi spec.
		transportConfig = &client.TransportConfig{
			Host:      "localhost:9080",
			BasePath:  "/function",
			Scheme:    "http",
			UserAgent: "fctl",
		}
	}

	rootCmd := &cobra.Command{
		Use:   "fctl",
		Short: "function server client tool",
		Long: `Command line tool for a ConfigHub function server
To change the default host, set the CONFIGHUB_FUNCTION_HOST environment variable.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			toolchain = workerapi.ToolchainType(toolchainString)
			tcPath := transportConfig.ToolchainToPath(toolchain)
			if tcPath == client.InvalidPath {
				failOnError(fmt.Errorf("unsupported ToolchainType %s", toolchain))
			}
		},
	}

	rootCmd.PersistentFlags().StringVar(&toolchainString, "toolchain", string(workerapi.ToolchainKubernetesYAML), "ToolchainType of config data; Kubernetes/YAML by default")

	// Add subcommands
	rootCmd.AddCommand(newDoCommand())
	rootCmd.AddCommand(newDoSeqCommand())
	rootCmd.AddCommand(newListCommand())
	rootCmd.AddCommand(newListPathsCommand())
	rootCmd.AddCommand(newOkCommand())
	rootCmd.AddCommand(newShutdownCommand())

	failOnError(rootCmd.Execute())
}

func failOnError(err error) {
	if err != nil {
		red := color.New(color.FgRed).Add(color.Bold)
		redf := red.SprintFunc()
		errstring := redf(err.Error())
		rederr := errors.New(errstring)
		log.Fatal(rederr)
	}
}

func detailView() *tablewriter.Table {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(true)
	table.SetBorder(false)
	table.SetTablePadding("    ")
	table.SetNoWhiteSpace(true)
	return table
}

func tableView() *tablewriter.Table {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetTablePadding("    ")
	table.SetNoWhiteSpace(true)
	return table
}
