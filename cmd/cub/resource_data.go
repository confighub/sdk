// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var resourceDataCmd = &cobra.Command{
	Use:         "data <kind>/[<namespace>/]<name>",
	Short:       "Print one resource's configuration",
	Long:        getResourceDataHelp(),
	Args:        cobra.ExactArgs(1),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        resourceDataCmdRun,
}

func getResourceDataHelp() string {
	baseHelp := `Print the configuration of one resource, as it is stored: for a Kubernetes resource,
its YAML.

The resource is named the way kubectl names one, as <kind>/<name>, or
<kind>/<namespace>/<name> when the name is held in several namespaces. The kind is matched
in any case and takes kubectl's .group suffix (Ingress.networking.k8s.io) when the kind
alone is ambiguous. It has to match exactly one resource among the units --space, --variant
and --where select; none, or several, is an error that lists what matched.

Examples:
` + "```" + `
  # The backend Deployment of the web component's dev variant
  cub resource data --variant web/dev Deployment/backend

  # Qualified by namespace, in a space
  cub resource data --space web-dev deployment/shop/backend

  # A cluster-scoped resource, found across the organization
  cub resource data Namespace/shop
` + "```" + `
`
	agentContext := `This reads one resource without naming the unit that holds it. To read a whole unit, use
'cub unit data'; to list resources with their units and types, 'cub resource list'.`
	return getCommandHelp(baseHelp, agentContext)
}

func init() {
	enableWhereFlag(resourceDataCmd)
	resourceCmd.AddCommand(resourceDataCmd)
}

func resourceDataCmdRun(_ *cobra.Command, args []string) error {
	ref, err := parseResourceRef(args[0])
	if err != nil {
		return err
	}
	resolved, err := resolveResourceRef(ref, where)
	if err != nil {
		return err
	}
	// The resource is read through the function engine, as get-yq over the whole document,
	// narrowed to this resource within its unit: the request reads the narrowing from the
	// same variables the function verbs set from their flags.
	resourceType = resolved.ResourceType
	whereResource = resolved.whereResource()
	functionToolchainType = resolved.ToolchainType
	body, err := initializeFunctionInvocationsRequest([]string{"get-yq", "."})
	if err != nil {
		return err
	}
	resp, err := invokeFunctionsOnUnits(&invokeArgs{
		Where:        fmt.Sprintf("UnitID = '%s'", resolved.UnitID.String()),
		ResourceType: resourceType,
		Body:         body,
	})
	if err != nil {
		return err
	}
	if resp == nil || len(*resp) == 0 {
		return fmt.Errorf("no configuration came back for %s", resolved)
	}
	for _, r := range *resp {
		if !r.Success {
			reason := "the function reported no reason"
			if r.Error != nil && r.Error.Message != "" {
				reason = r.Error.Message
			}
			return fmt.Errorf("reading %s failed: %s", resolved, reason)
		}
	}
	renderFunctionOutputs(resp, OutputSpec{Kind: OutputDefault}, false)
	return nil
}
