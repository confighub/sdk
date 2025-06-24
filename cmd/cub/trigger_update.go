// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var triggerUpdateCmd = &cobra.Command{
	Use:   "update <slug or id> <event> <config type> <function> [<arg1> ...]",
	Short: "Update a trigger",
	Long:  `Update a trigger.`,
	Args:  cobra.MinimumNArgs(4),
	RunE:  triggerUpdateCmdRun,
}

var disableTrigger bool
var enforceTrigger bool
var workerSlug string

func init() {
	addStandardUpdateFlags(triggerUpdateCmd)
	triggerUpdateCmd.Flags().BoolVar(&disableTrigger, "disable", false, "Disable trigger")
	triggerUpdateCmd.Flags().BoolVar(&enforceTrigger, "enforce", false, "Enforce trigger")
	triggerUpdateCmd.Flags().StringVar(&workerSlug, "worker", "", "worker to execute the trigger function")
	triggerCmd.AddCommand(triggerUpdateCmd)
}

func triggerUpdateCmdRun(cmd *cobra.Command, args []string) error {
	currentTrigger, err := apiGetTriggerFromSlug(args[0])
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)
	if flagPopulateModelFromStdin {
		// TODO: this could clobber a lot of fields
		if err := populateNewModelFromStdin(currentTrigger); err != nil {
			return err
		}
	} else if flagReplaceModelFromStdin {
		// TODO: this could clobber a lot of fields
		existingTrigger := currentTrigger
		currentTrigger = new(goclientnew.Trigger)
		// Before reading from stdin so it can be overridden by stdin
		currentTrigger.Version = existingTrigger.Version
		if err := populateNewModelFromStdin(currentTrigger); err != nil {
			return err
		}
		// After reading from stdin so it can't be clobbered by stdin
		currentTrigger.OrganizationID = existingTrigger.OrganizationID
		currentTrigger.SpaceID = existingTrigger.SpaceID
		currentTrigger.TriggerID = existingTrigger.TriggerID
	}
	err = setLabels(&currentTrigger.Labels)
	if err != nil {
		return err
	}

	// If this was set from stdin, it will be overridden
	currentTrigger.SpaceID = spaceID
	if disableTrigger {
		currentTrigger.Disabled = true
	}
	if enforceTrigger {
		currentTrigger.Enforced = true
	}
	if workerSlug != "" {
		worker, err := apiGetBridgeWorkerFromSlug(workerSlug)
		if err != nil {
			return err
		}
		currentTrigger.BridgeWorkerID = &worker.BridgeWorkerID
	}

	// TODO: update with overriden string type TriggerEvent
	// params.Trigger.Event = models.ModelsTriggerEvent(args[1])
	currentTrigger.Event = args[1]
	currentTrigger.ToolchainType = args[2]
	currentTrigger.FunctionName = args[3]
	invokeArgs := args[4:]
	newArgs := make([]goclientnew.FunctionArgument, 0, len(invokeArgs))
	// Note: This assumes all the string args will be cast to appropriate scalar data types
	for _, invokeArg := range invokeArgs {
		funcArgValue := &goclientnew.FunctionArgument_Value{}
		funcArgValue.FromFunctionArgumentValue0(invokeArg)
		newArgs = append(newArgs, goclientnew.FunctionArgument{Value: funcArgValue})
	}
	currentTrigger.Arguments = newArgs
	triggerRes, err := cubClientNew.UpdateTriggerWithResponse(ctx, spaceID, currentTrigger.TriggerID, *currentTrigger)
	if IsAPIError(err, triggerRes) {
		return InterpretErrorGeneric(err, triggerRes)
	}

	triggerDetails := triggerRes.JSON200
	displayUpdateResults(triggerDetails, "trigger", args[0], triggerDetails.TriggerID.String(), displayTriggerDetails)
	return nil
}
