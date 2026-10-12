// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	"github.com/spf13/cobra"
)

var componentOpenArgs struct {
	variant string
}

var componentOpenCmd = &cobra.Command{
	Use:   "open [<name>]",
	Short: "Open components in the web UI",
	Args:  cobra.MaximumNArgs(1),
	Long: getCommandHelp(`Open the web UI's component view on a component, or on the component overview when no component is named.

The component view shows the component's variants as a deployment graph, and is where changes made
to one variant are promoted downstream to the others.

Examples:
`+"```"+`
  # Open the component overview
  cub component open

  # Open a specific component
  cub component open cubbychat

  # Open a component with one of its variants preselected
  cub component open cubbychat --variant prod

  # Print the URL instead of opening a browser
  cub component open cubbychat --print-url
`+"```"+`
`, ""),
	RunE: componentOpenCmdRun,
}

func init() {
	enableOpenFlags(componentOpenCmd)
	componentOpenCmd.Flags().StringVar(&componentOpenArgs.variant, "variant", "",
		"preselect the space with this \"Variant\" label in the component's deployment graph")
	componentCmd.AddCommand(componentOpenCmd)
}

func componentOpenCmdRun(_ *cobra.Command, args []string) error {
	// --variant names the component inline as <component>/<variant>, or alone beside the
	// positional, which then has to agree.
	componentName := ""
	if len(args) == 1 {
		componentName = args[0]
	}
	variant := ""
	if componentOpenArgs.variant != "" {
		var err error
		componentName, variant, err = qualifiedVariant(componentName, componentOpenArgs.variant)
		if err != nil {
			return err
		}
	}
	if componentName == "" {
		return openWebUI(cubapi.GetComponentListURL(webUIServerURL()))
	}

	component, err := apiGetComponentFromName(componentName)
	if err != nil {
		return err
	}

	spaceID := ""
	if variant != "" {
		spaceID, err = component.spaceIDForVariant(variant)
		if err != nil {
			return err
		}
	}
	return openWebUI(cubapi.GetComponentURL(webUIServerURL(), component.Name, spaceID))
}
