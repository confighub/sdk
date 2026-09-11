// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var changeworkflowListCmd = &cobra.Command{
	Use:   "list",
	Short: "List change workflows",
	Long: getCommandHelp(`List change workflows you have access to in a space or across all spaces.

Examples:
`+"```"+`
  # List all change workflows in a space with headers
  cub changeworkflow list --space workflows

  # List change workflows across all spaces (requires --space "*")
  cub changeworkflow list --space "*"

  # List the workflows whose last stage is gated on live status
  cub changeworkflow list --space "*" --where "Final.Prerequisites ? 'Healthy'"

  # List the workflows that have a stage named prod
  cub changeworkflow list --space "*" --where "Stages.*.Name = 'prod'"

  # List change workflows without headers for scripting
  cub changeworkflow list --space workflows --no-headers

  # List change workflows in JSON format
  cub changeworkflow list --space workflows -o json
`+"```"+`
`, ""),
	Args:        cobra.ExactArgs(0),
	RunE:        changeworkflowListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultChangeWorkflowColumns = []string{"ChangeWorkflow.Slug", "Space.Slug", "ChangeWorkflow.Stages", "ChangeWorkflow.Final"}

// changeWorkflowListInclude is the Include parameter for change workflow list queries.
const changeWorkflowListInclude = "SpaceID"

// changeWorkflowBaseSelectFields are the fields always returned by change workflow list queries.
var changeWorkflowBaseSelectFields = []string{"Slug", "ChangeWorkflowID", "SpaceID", "OrganizationID"}

// ChangeWorkflow-specific aliases
var changeWorkflowAliases = map[string]string{
	"Name": "ChangeWorkflow.Slug",
	"ID":   "ChangeWorkflow.ChangeWorkflowID",
}

// ChangeWorkflow custom column dependencies
var changeWorkflowCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(changeworkflowListCmd)
	changeworkflowCmd.AddCommand(changeworkflowListCmd)
}

func changeworkflowListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	extendedChangeWorkflows, err := apiListChangeWorkflows(selectedSpaceID, where, selectFields, filterID)
	if err != nil {
		return err
	}

	displayListResults(extendedChangeWorkflows, getChangeWorkflowSlug, displayChangeWorkflowList)
	return nil
}

func getChangeWorkflowSlug(changeWorkflow *goclientnew.ExtendedChangeWorkflow) string {
	space := ""
	if changeWorkflow.Space != nil {
		space = changeWorkflow.Space.Slug
	}
	return prefixedSlug(space, changeWorkflow.ChangeWorkflow.Slug)
}

// formatChangeWorkflowStagesForDisplay names the stages in the order a change is promoted through
// them, which is what a listing is asked for: the shape of the rollout, not its selectors.
func formatChangeWorkflowStagesForDisplay(stages []goclientnew.ChangeWorkflowStage) string {
	if len(stages) == 0 {
		return ""
	}

	names := make([]string, len(stages))
	for i, stage := range stages {
		names[i] = stage.Name
	}

	result := strings.Join(names, " -> ")
	if len(result) > 40 {
		return result[:37] + "..."
	}
	return result
}

func displayChangeWorkflowList(changeWorkflows []*goclientnew.ExtendedChangeWorkflow) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "Stages", "Final-Gates"})
	}
	for _, cw := range changeWorkflows {
		changeWorkflow := cw.ChangeWorkflow
		spaceSlug := changeWorkflow.SpaceID.String()
		if cw.Space != nil {
			spaceSlug = cw.Space.Slug
		} else if selectedSpaceID != "*" {
			spaceSlug = selectedSpaceSlug
		}

		table.Append([]string{
			changeWorkflow.Slug,
			spaceSlug,
			formatChangeWorkflowStagesForDisplay(changeWorkflow.Stages),
			strings.Join(changeWorkflowFinalPrerequisites(changeWorkflow.Final), ", "),
		})
	}
	table.Render()
}

// apiListChangeWorkflows lists change workflows via the org-level endpoint, scoped to a single
// space by a SpaceID clause unless spaceID is "*" (list across all spaces).
func apiListChangeWorkflows(spaceID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedChangeWorkflow, error) {
	where := cubapi.NewWhere(whereFilter)
	if spaceID != "*" {
		where = where.SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	}
	return apiListAllChangeWorkflows(where, selectParam, filterParam)
}

func apiListAllChangeWorkflows(where cubapi.Where, selectParam string, filterParam string) ([]*goclientnew.ExtendedChangeWorkflow, error) {
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("ChangeWorkflow", nil, changeWorkflowListInclude, defaultChangeWorkflowColumns,
			changeWorkflowAliases, changeWorkflowCustomColumnDependencies, changeWorkflowBaseSelectFields)
	})
	return cubapi.ListChangeWorkflows(ctx, cubClient, where, cubapi.ListOpts{
		Select:   cubapi.SelectFields(selectValue),
		Include:  changeWorkflowListInclude,
		Filter:   filterParam,
		Contains: contains,
	})
}
