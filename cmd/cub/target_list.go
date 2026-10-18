// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/k8sutil/collect"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var targetListCmd = &cobra.Command{
	Use:   "list",
	Short: "List targets",
	Long: getCommandHelp(`List targets you have access to in a space or across all spaces. Omit --space to list targets across all spaces.

The table shows the well-known Target labels Owner, Environment and Region, which
--owner, --environment and --region also filter by, and the number of Triggers the
Target applies, the ones its WhereTrigger and TriggerFilterID select. It also shows the
Kubernetes version of the cluster a Target represents, the Cluster.KubernetesVersion fact
'cub k8s collect' stores. -o wide adds the Target's URL-TargetUI annotation, its link to an
external UI such as Argo CD's.`, `
  # List the Targets in a space
  cub target list --space infra

  # List the production Targets in one region, across all spaces
  cub target list --environment Prod --region us-east1`),
	Args:        cobra.ExactArgs(0),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        targetListCmdRun,
}

// Default columns to display when no custom columns are specified. It names what both the
// default and the -o wide table read, so that the select list covers either one.
var defaultTargetColumns = []string{"Target.Slug", "Target.SpaceSlug", "Target.Labels", "Target.Facts", "Target.TriggerIDs", "Target.Annotations"}

// targetListInclude is the Include parameter for target list queries (the related
// entities expanded into each ExtendedTarget).
const targetListInclude = "SpaceID,TriggerFilterID,TriggerIDs"

// targetBaseSelectFields are the fields always returned by target list queries,
// regardless of the requested columns.
var targetBaseSelectFields = []string{"Slug", "TargetID", "SpaceID", "OrganizationID"}

// Target-specific aliases
var targetAliases = map[string]string{
	"Name": "Target.Slug",
	"ID":   "Target.TargetID",
}

// Target custom column dependencies (e.g. Environment comes from Labels.Environment)
var targetCustomColumnDependencies = func() map[string][]string {
	deps := make(map[string][]string, len(standardTargetLabels)+1)
	for _, label := range standardTargetLabels {
		deps[label] = []string{"Labels"}
	}
	deps["AppliedTriggerCount"] = []string{"TriggerIDs"}
	return deps
}()

// targetCustomColumns are the columns the default table computes rather than reads: a
// standard label by its bare name, and the number of Triggers the Target applies.
var targetCustomColumns = func() map[string]func(any) string {
	cols := make(map[string]func(any) string, len(standardTargetLabels)+1)
	for _, label := range standardTargetLabels {
		cols[label] = func(obj any) string {
			if exTarget, ok := obj.(*goclientnew.ExtendedTarget); ok && exTarget.Target != nil {
				return exTarget.Target.Labels[label]
			}
			return ""
		}
	}
	cols["AppliedTriggerCount"] = func(obj any) string {
		if exTarget, ok := obj.(*goclientnew.ExtendedTarget); ok && exTarget.Target != nil {
			return fmt.Sprintf("%d", len(exTarget.Target.TriggerIDs))
		}
		return ""
	}
	return cols
}()

var targetListArgs struct {
	targetLabels standardLabelFlagValues
}

func init() {
	addStandardListFlags(targetListCmd)
	targetListArgs.targetLabels = addStandardLabelFilterFlags(targetListCmd, "Target", standardTargetLabels)
	enableListPagingFlags(targetListCmd)
	enableViewFlag(targetListCmd)
	targetCmd.AddCommand(targetListCmd)
}

func targetListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	effectiveWhere := targetListArgs.targetLabels.addToWhereClause(where)
	targets, err := apiListTargets(selectedSpaceID, effectiveWhere, selectFields, filterID)
	if err != nil {
		return err
	}
	displayListResults(targets, getTargetSlug, displayTargetList)
	return nil
}

func getTargetSlug(exTarget *goclientnew.ExtendedTarget) string {
	return prefixedSlug(exTarget.Target.SpaceSlug, exTarget.Target.Slug)
}

func displayTargetList(exTargets []*goclientnew.ExtendedTarget) {
	if displayRequestedColumns(exTargets, targetAliases, targetCustomColumns) {
		return
	}
	wide := effectiveOutput().Kind == OutputWide
	table := tableView()
	if !noheader {
		header := append([]string{"Name", "Space"}, standardTargetLabels...)
		header = append(header, "KubernetesVersion", "#AppliedTriggers")
		if wide {
			header = append(header, clusterAnnotationTargetUI)
		}
		table.SetHeader(header)
	}
	for _, exTarget := range exTargets {
		row := []string{exTarget.Target.Slug, exTarget.Target.SpaceSlug}
		for _, label := range standardTargetLabels {
			row = append(row, exTarget.Target.Labels[label])
		}
		row = append(row, exTarget.Target.Facts[collect.FactPrefix+"KubernetesVersion"], fmt.Sprintf("%d", len(exTarget.Target.TriggerIDs)))
		if wide {
			row = append(row, exTarget.Target.Annotations[clusterAnnotationTargetUI])
		}
		table.Append(row)
	}
	table.Render()
}

// apiListTargets lists targets via the org-level endpoint, scoped to a single
// space by a SpaceID clause unless spaceID is "*" (list across all spaces).
func apiListTargets(spaceID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedTarget, error) {
	where := cubapi.NewWhere(whereFilter)
	if spaceID != "*" {
		where = where.SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	}
	return apiListAllTargets(where, selectParam, filterParam)
}

func apiListAllTargets(where cubapi.Where, selectParam string, filterParam string) ([]*goclientnew.ExtendedTarget, error) {
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("Target", listColumnsFor("cub target list"), targetListInclude, defaultTargetColumns, targetAliases, targetCustomColumnDependencies, targetBaseSelectFields)
	})
	return cubapi.ListTargets(ctx, cubClient, where, cubapi.ListOpts{
		Limit:         listLimit,
		OrderBy:       listOrderBy,
		Select:        cubapi.SelectFields(selectValue),
		Include:       targetListInclude,
		Filter:        filterParam,
		Contains:      contains,
		IncludeHidden: includeHidden,
		View:          listViewFor("cub target list"),
	})
}
