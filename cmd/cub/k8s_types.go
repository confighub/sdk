// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var k8sTypesCmd = &cobra.Command{
	Use:         "types [<type>[,<type>...]]",
	Short:       "List the Kubernetes resource types stored in ConfigHub",
	Long:        getK8sTypesHelp(),
	Args:        cobra.MaximumNArgs(1),
	Annotations: map[string]string{"OrgLevel": ""},
	PreRunE:     k8sPreRunE,
	RunE:        k8sTypesCmdRun,
}

func getK8sTypesHelp() string {
	baseHelp := `List the Kubernetes resource types present in ConfigHub Units, with how many resources,
Units, and Spaces each spans.

This answers "what is actually in here?" — useful before a fleet-wide query, and for
discovering which custom resource types a cluster's configuration uses. It accepts the same
filters as "cub k8s get" and, optionally, the same type argument to narrow the survey.

Examples:
` + "```" + `
  # Every resource type in a space
  cub k8s types --space my-space

  # Every resource type across the organization
  cub k8s types --space "*"

  # Everything except CustomResourceDefinitions
  cub k8s types all --space "*"

  # Types delivered to one target
  cub k8s types --target prod-use2/prod-use2-oci

  # Just the type names, for scripting
  cub k8s types --space "*" -o name
` + "```" + `
`

	agentContext := `Run this first when you don't know what resource types an organization or target holds;
then query the interesting ones with "cub k8s get".

It reads only names and types (not resource bodies), so it is the cheapest way to survey a
large scope — though it still visits every selected Unit, so --space, --target, and --where
are what bound the cost.`

	return getCommandHelp(baseHelp, agentContext)
}

func init() {
	addK8sQueryFlags(k8sTypesCmd)
	k8sCmd.AddCommand(k8sTypesCmd)
}

// k8sResourceTypeSummary is one row of `cub k8s types`.
type k8sResourceTypeSummary struct {
	ResourceType string
	APIVersion   string
	Kind         string
	Resources    int
	Units        int
	Spaces       int

	unitIDs  map[uuid.UUID]bool
	spaceIDs map[uuid.UUID]bool
}

func k8sTypesCmdRun(_ *cobra.Command, args []string) error {
	if err := checkK8sOutputFormat(); err != nil {
		return err
	}
	var types *resourceTypeFilter
	if len(args) == 1 {
		var err error
		if types, err = parseResourceTypes(args[0]); err != nil {
			return err
		}
	}

	resources, err := listK8sResources(types, nil, false)
	if err != nil {
		return err
	}
	summaries := summarizeResourceTypes(resources)

	if renderPayload(summaries) {
		return nil
	}
	if effectiveOutput().Kind == OutputName {
		for _, summary := range summaries {
			tprintRaw(summary.ResourceType)
		}
		return nil
	}
	if quiet && !verbose {
		return nil
	}
	displayK8sResourceTypeList(summaries)
	return nil
}

func summarizeResourceTypes(resources []*k8sResource) []*k8sResourceTypeSummary {
	byType := map[string]*k8sResourceTypeSummary{}
	for _, resource := range resources {
		summary := byType[resource.ResourceType]
		if summary == nil {
			summary = &k8sResourceTypeSummary{
				ResourceType: resource.ResourceType,
				APIVersion:   resource.APIVersion,
				Kind:         resource.Kind,
				unitIDs:      map[uuid.UUID]bool{},
				spaceIDs:     map[uuid.UUID]bool{},
			}
			byType[resource.ResourceType] = summary
		}
		summary.Resources++
		summary.unitIDs[resource.UnitID] = true
		summary.spaceIDs[resource.SpaceID] = true
	}

	summaries := make([]*k8sResourceTypeSummary, 0, len(byType))
	for _, summary := range byType {
		summary.Units = len(summary.unitIDs)
		summary.Spaces = len(summary.spaceIDs)
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].ResourceType < summaries[j].ResourceType
	})
	return summaries
}

func displayK8sResourceTypeList(summaries []*k8sResourceTypeSummary) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Resource Type", "Kind", "Resources", "Units", "Spaces"})
	}
	for _, summary := range summaries {
		table.Append([]string{
			summary.ResourceType,
			summary.Kind,
			fmt.Sprintf("%d", summary.Resources),
			fmt.Sprintf("%d", summary.Units),
			fmt.Sprintf("%d", summary.Spaces),
		})
	}
	table.Render()
}
