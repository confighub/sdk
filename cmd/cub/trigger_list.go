// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strconv"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var triggerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List triggers",
	Long: getCommandHelp(`List triggers you have access to in a space or across all spaces. The output includes slugs, worker slugs, events, validation status, disabled status, enforcement status, toolchain types, function names, and the number of arguments.

Examples:
`+"```"+`
  # List all triggers in a space with headers
  cub trigger list --space my-space

  # List triggers across all spaces (requires --space "*")
  cub trigger list --space "*" --where "Event = 'Mutation'"

  # List triggers without headers for scripting
  cub trigger list --space my-space --no-headers

  # List triggers in JSON format
  cub trigger list --space my-space -o json

  # List only trigger names
  cub trigger list --space my-space --no-headers -o name

  # List triggers with a specific event type
  cub trigger list --space my-space --where "Event = 'Mutation'"

  # List triggers for a specific toolchain
  cub trigger list --space my-space --where "ToolchainType = 'Kubernetes/YAML'"

  # List triggers using a specific function
  cub trigger list --space my-space --where "FunctionName = 'cel-validate'"

  # List disabled triggers across all spaces
  cub trigger list --space "*" --where "Disabled = true"
`+"```"+`
`, ""),
	Args:        cobra.ExactArgs(0),
	RunE:        triggerListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultTriggerColumns = []string{"Trigger.Slug", "Space.Slug", "BridgeWorker.Slug", "Trigger.Event", "Trigger.Validating", "Trigger.Disabled", "Trigger.Warn", "Trigger.ToolchainType", "Trigger.FunctionName", "Trigger.Arguments", "Invocation.Slug"}

// triggerListInclude is the Include parameter for trigger list queries (the
// related entities expanded into each ExtendedTrigger).
const triggerListInclude = "SpaceID,BridgeWorkerID,InvocationID"

// triggerBaseSelectFields are the fields always returned by trigger list queries,
// regardless of the requested columns.
var triggerBaseSelectFields = []string{"Slug", "TriggerID", "SpaceID", "OrganizationID"}

// Trigger-specific aliases
var triggerAliases = map[string]string{
	"Name": "Trigger.Slug",
	"ID":   "Trigger.TriggerID",
}

// Trigger custom column dependencies
var triggerCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(triggerListCmd)
	triggerCmd.AddCommand(triggerListCmd)
}

func triggerListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	extendedTriggers, err := apiListTriggers(selectedSpaceID, where, selectFields, filterID)
	if err != nil {
		return err
	}

	displayListResults(extendedTriggers, getTriggerSlug, displayTriggerList)
	return nil
}

func getTriggerSlug(trigger *goclientnew.ExtendedTrigger) string {
	space := ""
	if trigger.Space != nil {
		space = trigger.Space.Slug
	}
	return prefixedSlug(space, trigger.Trigger.Slug)
}

func displayTriggerList(triggers []*goclientnew.ExtendedTrigger) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "Worker", "Event", "Validating", "Disabled", "Warn", "Toolchain-Type", "Function-Name", "Num-Args", "Invocation"})
	}
	for _, t := range triggers {
		trigger := t.Trigger
		workerSlug := ""
		if t.BridgeWorker != nil {
			workerSlug = t.BridgeWorker.Slug
		}
		spaceSlug := t.Trigger.TriggerID.String()
		if t.Space != nil {
			spaceSlug = t.Space.Slug
		} else if selectedSpaceID != "*" {
			spaceSlug = selectedSpaceSlug
		}
		invocationSlug := ""
		if t.Invocation != nil {
			invocationSlug = t.Invocation.Slug
		}
		table.Append([]string{
			trigger.Slug,
			spaceSlug,
			workerSlug,
			trigger.Event,
			strconv.FormatBool(trigger.Validating),
			strconv.FormatBool(trigger.Disabled),
			strconv.FormatBool(trigger.Warn),
			trigger.ToolchainType,
			trigger.FunctionName,
			fmt.Sprintf("%d", len(trigger.Arguments)),
			invocationSlug,
		})
	}
	table.Render()
}

// apiListTriggers lists triggers via the org-level endpoint, scoped to a single
// space by a SpaceID clause unless spaceID is "*" (list across all spaces).
func apiListTriggers(spaceID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedTrigger, error) {
	where := cubapi.NewWhere(whereFilter)
	if spaceID != "*" {
		where = where.SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	}
	return apiListAllTriggers(where, selectParam, filterParam)
}

func apiListAllTriggers(where cubapi.Where, selectParam string, filterParam string) ([]*goclientnew.ExtendedTrigger, error) {
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("Trigger", nil, triggerListInclude, defaultTriggerColumns, triggerAliases, triggerCustomColumnDependencies, triggerBaseSelectFields)
	})
	return cubapi.ListTriggers(ctx, cubClient, where, cubapi.ListOpts{
		Select:   cubapi.SelectFields(selectValue),
		Include:  triggerListInclude,
		Filter:   filterParam,
		Contains: contains,
	})
}
