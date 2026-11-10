// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strconv"

	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var triggerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List triggers",
	Long: getCommandHelp(`List triggers you have access to in a space or across all spaces. The output includes slugs, space slugs, the pack each belongs to (its `+constants.PackLabel+` label), whether each validates, is disabled, or only warns, the priority of a validating trigger's policy (its `+constants.TriggerPriorityAnnotation+` annotation), and the invocation it runs, if any. What a trigger runs is shown with --columns, as in --columns Name,Event,ToolchainType,Channel.Slug,FunctionName,Arguments, and by trigger get.

Examples:
`+"```"+`
  # List all triggers in a space with headers
  cub trigger list --space my-space

  # List triggers across all spaces
  cub trigger list --where "Event = 'Mutation'"

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
  cub trigger list --space my-space --where "FunctionName = 'vet-cel'"

  # Find triggers still using a deprecated function
  cub trigger list --where "FunctionName = 'cel-validate'"

  # List disabled triggers across all spaces
  cub trigger list --where "Disabled = true"

  # List triggers that set a particular attribute, by the argument bound to a named parameter
  cub trigger list --where "Arguments.?ParameterName=attribute-name.Value = 'owner'"

  # List triggers whose function takes a given value as any argument
  cub trigger list --where "Arguments.*.Value LIKE 'confighub.com/%'"

  # List triggers cleared to overwrite a guarded path
  cub trigger list --where "Clearance.*.Key = 'owner'"

  # List triggers that stamp a guard on what they write
  cub trigger list --where "Guards.owner = 'platform-trigger'"

  # List the triggers of two packs in a policy space
  cub trigger list --space confighub-system --pack basic --pack rbac
`+"```"+`
`, ""),
	Args:        cobra.ExactArgs(0),
	RunE:        triggerListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
// What a trigger runs (event, config type, channel, function and arguments) is left to
// --columns and trigger get; the list shows what each is for and how far it is enforced.
var defaultTriggerColumns = []string{"Trigger.Slug", "Space.Slug", "Trigger.Labels", "Trigger.Validating", "Trigger.Disabled", "Trigger.Warn", "Trigger.Annotations", "Invocation.Slug"}

// triggerListInclude is the Include parameter for trigger list queries (the
// related entities expanded into each ExtendedTrigger).
const triggerListInclude = "SpaceID,ChannelID,InvocationID"

// triggerBaseSelectFields are the fields always returned by trigger list queries,
// regardless of the requested columns.
var triggerBaseSelectFields = []string{"Slug", "TriggerID", "SpaceID", "OrganizationID"}

// Trigger-specific aliases
var triggerAliases = map[string]string{
	"Name": "Trigger.Slug",
	"ID":   "Trigger.TriggerID",
}

// Trigger custom column dependencies: Pack comes from Labels.Pack, and Priority from
// Annotations.Priority and Validating.
var triggerCustomColumnDependencies = map[string][]string{"Pack": {"Labels"}, "Priority": {"Annotations", "Validating"}}

// triggerCustomColumns are the columns the default table computes rather than
// reads.
var triggerCustomColumns = map[string]func(any) string{
	"Pack": func(obj any) string {
		if t, ok := obj.(*goclientnew.ExtendedTrigger); ok && t.Trigger != nil {
			return t.Trigger.Labels[constants.PackLabel]
		}
		return ""
	},
	"Priority": func(obj any) string {
		if t, ok := obj.(*goclientnew.ExtendedTrigger); ok && t.Trigger != nil {
			return triggerPriorityColumn(t.Trigger)
		}
		return ""
	},
}

// triggerPriorityColumn is a validating trigger's priority, Unspecified when it has none, and
// nothing for a trigger that does not validate, which has no policy to enforce.
func triggerPriorityColumn(trigger *goclientnew.Trigger) string {
	if !trigger.Validating {
		return ""
	}
	if p, err := cubapi.ParseTriggerPriority(trigger.Annotations[constants.TriggerPriorityAnnotation]); err == nil && p != "" {
		return p
	}
	return cubapi.TriggerPriorityUnspecified
}

func init() {
	addStandardListFlags(triggerListCmd)
	enableWherePermissionFlag(triggerListCmd)
	enableListPagingFlags(triggerListCmd)
	enableViewFlag(triggerListCmd)
	addTriggerPackFlag(triggerListCmd)
	triggerCmd.AddCommand(triggerListCmd)
}

func triggerListCmdRun(cmd *cobra.Command, args []string) error {
	if err := applyTriggerPackFlag(); err != nil {
		return err
	}
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
	if displayRequestedColumns(triggers, triggerAliases, triggerCustomColumns) {
		return
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "Pack", "Validating", "Disabled", "Warn", "Priority", "Invocation"})
	}
	for _, t := range triggers {
		trigger := t.Trigger
		spaceSlug := t.Trigger.SpaceID.String()
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
			trigger.Labels[constants.PackLabel],
			strconv.FormatBool(trigger.Validating),
			strconv.FormatBool(trigger.Disabled),
			strconv.FormatBool(trigger.Warn),
			triggerPriorityColumn(trigger),
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
		return buildSelectList("Trigger", listColumnsFor("cub trigger list"), triggerListInclude, defaultTriggerColumns, triggerAliases, triggerCustomColumnDependencies, triggerBaseSelectFields)
	})
	return cubapi.ListTriggers(ctx, cubClient, where, cubapi.ListOpts{
		Limit:           listLimit,
		OrderBy:         listOrderBy,
		Select:          cubapi.SelectFields(selectValue),
		Include:         triggerListInclude,
		Filter:          filterParam,
		Contains:        contains,
		WherePermission: wherePermission,
		IncludeHidden:   includeHidden,
		View:            listViewFor("cub trigger list"),
	})
}
