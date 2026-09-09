// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strconv"
	"time"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var triggerGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a trigger",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a trigger in a space including its ID, slug, display name, event type, toolchain type, function name, and arguments.

Examples:
`+"```"+`
  # Get details about a trigger that validates replicas
  cub trigger get --space my-space --json validate-replicas

  # Get details about a trigger that enforces low resource usage
  cub trigger get --space my-space --json enforce-low-cost
`+"```"+`
`, ""),
	RunE: triggerGetCmdRun,
}

func init() {
	addStandardGetFlags(triggerGetCmd)
	enableOptionalSpace(triggerGetCmd)
	triggerCmd.AddCommand(triggerGetCmd)
}

func triggerGetCmdRun(cmd *cobra.Command, args []string) error {
	triggerDetails, err := resolveTrigger(args[0], selectedSpaceID, selectFields)
	if err != nil {
		return err
	}

	displayGetResults(triggerDetails, displayExtendedTriggerDetails)
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

	// Fallback: the raw JSON, since the union type holds its payload in an
	// unexported field that %v would render as Go bytes.
	if raw, err := value.MarshalJSON(); err == nil {
		return string(raw)
	}
	return "<unknown>"
}

func displayExtendedTriggerDetails(extendedTrigger *goclientnew.ExtendedTrigger) {
	trigger := extendedTrigger.Trigger
	view := tableView()
	view.Append([]string{"ID", trigger.TriggerID.String()})
	view.Append([]string{"Name", trigger.Slug})

	// Show Space slug instead of Space ID when available
	if extendedTrigger.Space != nil {
		view.Append([]string{"Space", extendedTrigger.Space.Slug})
	} else {
		view.Append([]string{"Space ID", trigger.SpaceID.String()})
	}
	view.Append([]string{"Created At", trigger.CreatedAt.String()})
	view.Append([]string{"Updated At", trigger.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(trigger.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(trigger.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(trigger.Annotations)})
	view.Append([]string{"Organization ID", trigger.OrganizationID.String()})

	// Show BridgeWorker slug instead of BridgeWorkerID when available
	if extendedTrigger.BridgeWorker != nil {
		view.Append([]string{"Worker", extendedTrigger.BridgeWorker.Slug})
	} else if trigger.BridgeWorkerID != nil && *trigger.BridgeWorkerID != uuid.Nil {
		view.Append([]string{"Worker ID", trigger.BridgeWorkerID.String()})
	}

	view.Append([]string{"Event", trigger.Event})
	view.Append([]string{"Validating", strconv.FormatBool(trigger.Validating)})
	view.Append([]string{"Disabled", strconv.FormatBool(trigger.Disabled)})
	view.Append([]string{"Warn", strconv.FormatBool(trigger.Warn)})
	// Protect, and the two statements about guards beside it, are shown the way cub link get
	// shows them: what a Trigger claims and states about what it writes belongs next to what
	// it does, not somewhere a reader has to go looking for it.
	view.Append([]string{"Protect", strconv.FormatBool(trigger.Protect)})
	if clearance := formatClearance(trigger.Clearance); clearance != "" {
		view.Append([]string{"Clearance", clearance})
	}
	if guards := formatGuardStamp(trigger.Guards); guards != "" {
		view.Append([]string{"Guards", guards})
	}
	view.Append([]string{"Toolchain Type", (trigger.ToolchainType)})
	if trigger.Description != "" {
		view.Append([]string{"Description", trigger.Description})
	}

	// Show Invocation slug instead of InvocationID when available
	if extendedTrigger.Invocation != nil {
		view.Append([]string{"Invocation", extendedTrigger.Invocation.Slug})
	} else if trigger.InvocationID != nil && *trigger.InvocationID != uuid.Nil {
		view.Append([]string{"Invocation ID", trigger.InvocationID.String()})
	}
	if trigger.FunctionName != "" {
		view.Append([]string{"Function Name", (trigger.FunctionName)})
		for i := range trigger.Arguments {
			argLabel := fmt.Sprintf("Argument %d", i)
			if trigger.Arguments[i].ParameterName != nil {
				argLabel = fmt.Sprintf("Argument %d (%s)", i, *trigger.Arguments[i].ParameterName)
			}
			view.Append([]string{argLabel, formatFunctionArgumentValue(trigger.Arguments[i].Value)})
		}
	}
	if trigger.WhereUnit != "" {
		view.Append([]string{"Where Unit", trigger.WhereUnit})
	}
	if extendedTrigger.UnitFilter != nil {
		view.Append([]string{"Unit Filter", extendedTrigger.UnitFilter.Slug})
	} else if trigger.UnitFilterID != nil && *trigger.UnitFilterID != uuid.Nil {
		view.Append([]string{"Unit Filter ID", trigger.UnitFilterID.String()})
	}
	if trigger.WhereResource != "" {
		view.Append([]string{"Where Resource", trigger.WhereResource})
	}
	if trigger.OtherDataSource != "" {
		view.Append([]string{"Other Data Source", trigger.OtherDataSource})
	}
	if trigger.FailOpenAfter != 0 {
		view.Append([]string{"Fail Open After", time.Duration(trigger.FailOpenAfter).String()})
	}
	view.Render()
}

func displayTriggerDetails(trigger *goclientnew.Trigger) {
	view := tableView()
	view.Append([]string{"ID", trigger.TriggerID.String()})
	view.Append([]string{"Name", trigger.Slug})
	view.Append([]string{"Space ID", trigger.SpaceID.String()})
	view.Append([]string{"Created At", trigger.CreatedAt.String()})
	view.Append([]string{"Updated At", trigger.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(trigger.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(trigger.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(trigger.Annotations)})
	view.Append([]string{"Organization ID", trigger.OrganizationID.String()})

	if trigger.BridgeWorkerID != nil && *trigger.BridgeWorkerID != uuid.Nil {
		view.Append([]string{"Worker ID", trigger.BridgeWorkerID.String()})
	}

	view.Append([]string{"Event", trigger.Event})
	view.Append([]string{"Validating", strconv.FormatBool(trigger.Validating)})
	view.Append([]string{"Disabled", strconv.FormatBool(trigger.Disabled)})
	view.Append([]string{"Warn", strconv.FormatBool(trigger.Warn)})
	// Protect, and the two statements about guards beside it, are shown the way cub link get
	// shows them: what a Trigger claims and states about what it writes belongs next to what
	// it does, not somewhere a reader has to go looking for it.
	view.Append([]string{"Protect", strconv.FormatBool(trigger.Protect)})
	if clearance := formatClearance(trigger.Clearance); clearance != "" {
		view.Append([]string{"Clearance", clearance})
	}
	if guards := formatGuardStamp(trigger.Guards); guards != "" {
		view.Append([]string{"Guards", guards})
	}
	view.Append([]string{"Toolchain Type", (trigger.ToolchainType)})
	if trigger.Description != "" {
		view.Append([]string{"Description", trigger.Description})
	}

	if trigger.InvocationID != nil && *trigger.InvocationID != uuid.Nil {
		view.Append([]string{"Invocation ID", trigger.InvocationID.String()})
	}
	if trigger.FunctionName != "" {
		view.Append([]string{"Function Name", (trigger.FunctionName)})
		for i := range trigger.Arguments {
			argLabel := fmt.Sprintf("Argument %d", i)
			if trigger.Arguments[i].ParameterName != nil {
				argLabel = fmt.Sprintf("Argument %d (%s)", i, *trigger.Arguments[i].ParameterName)
			}
			view.Append([]string{argLabel, formatFunctionArgumentValue(trigger.Arguments[i].Value)})
		}
	}
	if trigger.WhereUnit != "" {
		view.Append([]string{"Where Unit", trigger.WhereUnit})
	}
	if trigger.UnitFilterID != nil && *trigger.UnitFilterID != uuid.Nil {
		view.Append([]string{"Unit Filter ID", trigger.UnitFilterID.String()})
	}
	if trigger.WhereResource != "" {
		view.Append([]string{"Where Resource", trigger.WhereResource})
	}
	if trigger.OtherDataSource != "" {
		view.Append([]string{"Other Data Source", trigger.OtherDataSource})
	}
	if trigger.FailOpenAfter != 0 {
		view.Append([]string{"Fail Open After", time.Duration(trigger.FailOpenAfter).String()})
	}
	view.Render()
}
