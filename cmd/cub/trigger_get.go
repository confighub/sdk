// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strconv"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var triggerGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a trigger",
	Args:  cobra.ExactArgs(1),
	Long: `Get detailed information about a trigger in a space including its ID, slug, display name, event type, toolchain type, function name, and arguments.

Examples:
  # Get details about a trigger that validates replicas
  cub trigger get --space my-space --json validate-replicas

  # Get details about a trigger that enforces low resource usage
  cub trigger get --space my-space --json enforce-low-cost

`,
	RunE: triggerGetCmdRun,
}

func init() {
	addStandardGetFlags(triggerGetCmd)
	triggerCmd.AddCommand(triggerGetCmd)
}

func triggerGetCmdRun(cmd *cobra.Command, args []string) error {
	triggerDetails, err := apiGetTriggerFromSlug(args[0])
	if err != nil {
		return err
	}

	// the previous call got the list resource. We want the "detail" resource just in case they're different
	triggerDetails, err = apiGetTrigger(triggerDetails.TriggerID.String())
	if err != nil {
		return err
	}
	displayGetResults(triggerDetails, displayTriggerDetails)
	return nil
}

func formatFunctionArgumentValue(value *goclientnew.FunctionArgument_Value) string {
	if value == nil {
		return "<nil>"
	}

	// Try string first (most common)
	if strVal, err := value.AsFunctionArgumentValue0(); err == nil {
		return fmt.Sprintf("%q", strVal)
	}

	// Try int64
	if intVal, err := value.AsFunctionArgumentValue1(); err == nil {
		return strconv.FormatInt(intVal, 10)
	}

	// Try bool
	if boolVal, err := value.AsFunctionArgumentValue2(); err == nil {
		return strconv.FormatBool(boolVal)
	}

	// Fallback: return the raw JSON
	return fmt.Sprintf("%v", value)
}

func displayTriggerDetails(triggerDetails *goclientnew.Trigger) {
	view := tableView()
	view.Append([]string{"ID", triggerDetails.TriggerID.String()})
	view.Append([]string{"Name", triggerDetails.Slug})
	view.Append([]string{"Space ID", triggerDetails.SpaceID.String()})
	view.Append([]string{"Created At", triggerDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", triggerDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(triggerDetails.Labels)})
	view.Append([]string{"Annotations", annotationsToString(triggerDetails.Annotations)})
	view.Append([]string{"Organization ID", triggerDetails.OrganizationID.String()})
	if triggerDetails.BridgeWorkerID != nil && *triggerDetails.BridgeWorkerID != uuid.Nil {
		view.Append([]string{"Worker ID", triggerDetails.BridgeWorkerID.String()})
	}
	view.Append([]string{"Event", triggerDetails.Event})
	view.Append([]string{"Validating", strconv.FormatBool(triggerDetails.Validating)})
	view.Append([]string{"Disabled", strconv.FormatBool(triggerDetails.Disabled)})
	view.Append([]string{"Enforced", strconv.FormatBool(triggerDetails.Enforced)})
	view.Append([]string{"Toolchain Type", (triggerDetails.ToolchainType)})
	view.Append([]string{"Function Name", (triggerDetails.FunctionName)})
	for i := range triggerDetails.Arguments {
		argLabel := fmt.Sprintf("Argument %d", i)
		if triggerDetails.Arguments[i].ParameterName != nil {
			argLabel = fmt.Sprintf("Argument %d (%s)", i, *triggerDetails.Arguments[i].ParameterName)
		}
		view.Append([]string{argLabel, formatFunctionArgumentValue(triggerDetails.Arguments[i].Value)})
	}
	view.Render()
}

func apiGetTrigger(triggerID string) (*goclientnew.Trigger, error) {
	newParams := &goclientnew.GetTriggerParams{}
	include := "BridgeWorkerID"
	newParams.Include = &include
	triggerRes, err := cubClientNew.GetTriggerWithResponse(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(triggerID), newParams)
	if IsAPIError(err, triggerRes) {
		return nil, InterpretErrorGeneric(err, triggerRes)
	}
	return triggerRes.JSON200.Trigger, nil
}

func apiGetTriggerFromSlug(slug string) (*goclientnew.Trigger, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetTrigger(id.String())
	}
	triggers, err := apiListTriggers(selectedSpaceID, "Slug = '"+slug+"'")
	if err != nil {
		return nil, err
	}
	// find trigger by slug
	for _, trigger := range triggers {
		if trigger.Trigger != nil && trigger.Trigger.Slug == slug {
			return trigger.Trigger, nil
		}
	}
	return nil, fmt.Errorf("trigger %s not found in space %s", slug, selectedSpaceSlug)
}
