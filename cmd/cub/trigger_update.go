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
	Long: `Update a trigger.

Function arguments can be provided as positional arguments or as named arguments using --argumentname=value syntax.
Once a named argument is used, all subsequent arguments must be named. Use "--" to separate command flags from function arguments when using named function arguments.

Example with named arguments:
  cub trigger update --space my-space my-trigger Mutation Kubernetes/YAML -- set-annotation --key=cloned --value=true`,
	Args: cobra.MinimumNArgs(4),
	RunE: triggerUpdateCmdRun,
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
	if err := validateStdinFlags(); err != nil {
		return err
	}
	
	currentTrigger, err := apiGetTriggerFromSlug(args[0])
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingTrigger := currentTrigger
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			currentTrigger = new(goclientnew.Trigger)
			currentTrigger.Version = existingTrigger.Version
		}
		
		if err := populateModelFromFlags(currentTrigger); err != nil {
			return err
		}
		
		// Ensure essential fields can't be clobbered
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
	newArgs := parseFunctionArguments(invokeArgs)
	currentTrigger.Arguments = newArgs
	triggerRes, err := cubClientNew.UpdateTriggerWithResponse(ctx, spaceID, currentTrigger.TriggerID, *currentTrigger)
	if IsAPIError(err, triggerRes) {
		return InterpretErrorGeneric(err, triggerRes)
	}

	triggerDetails := triggerRes.JSON200
	displayUpdateResults(triggerDetails, "trigger", args[0], triggerDetails.TriggerID.String(), displayTriggerDetails)
	return nil
}
