// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/cockroachdb/errors"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var triggerUpdateCmd = &cobra.Command{
	Use:   "update [<slug or id>] [<event> <config type> <function> [<arg1> ...]]",
	Short: "Update a trigger or multiple triggers",
	Long: `Update a trigger or multiple triggers using bulk operations.

Single trigger update:
Function arguments can be provided as positional arguments or as named arguments using --argumentname=value syntax.
Once a named argument is used, all subsequent arguments must be named. Use "--" to separate command flags from function arguments when using named function arguments.

Example with named arguments:
  cub trigger update --space my-space my-trigger Mutation Kubernetes/YAML -- set-annotation --key=cloned --value=true

Bulk update with --patch:
Update multiple triggers at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
  # Disable all triggers for a specific function
  cub trigger update --patch --where "FunctionName = 'cel-validate'" --disable

  # Enable all disabled triggers
  cub trigger update --patch --where "Disabled = true" --enable

  # Update worker for all triggers of a certain type using JSON patch
  echo '{"BridgeWorkerID": "worker-uuid"}' | cub trigger update --patch --where "ToolchainType = 'Kubernetes/YAML'" --from-stdin

  # Mark triggers as enforced
  cub trigger update --patch --where "Event = 'Mutation'" --enforce

  # Update specific triggers by slug
  cub trigger update --patch --trigger my-trigger,another-trigger --disable`,
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        triggerUpdateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	disableTrigger     bool
	enableTrigger      bool
	enforceTrigger     bool
	unenforceTrigger   bool
	workerSlug         string
	triggerPatch       bool
	triggerIdentifiers []string
	invocationSlug     string
)

func init() {
	addStandardUpdateFlags(triggerUpdateCmd)
	triggerUpdateCmd.Flags().BoolVar(&disableTrigger, "disable", false, "Disable trigger")
	triggerUpdateCmd.Flags().BoolVar(&enableTrigger, "enable", false, "Enable trigger (use with --patch for bulk)")
	triggerUpdateCmd.Flags().BoolVar(&enforceTrigger, "enforce", false, "Enforce trigger")
	triggerUpdateCmd.Flags().BoolVar(&unenforceTrigger, "unenforce", false, "Unenforce trigger (use with --patch for bulk)")
	triggerUpdateCmd.Flags().StringVar(&workerSlug, "worker", "", "worker to execute the trigger function")
	triggerUpdateCmd.Flags().BoolVar(&triggerPatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(triggerUpdateCmd)
	enableFilterFlag(triggerUpdateCmd)
	triggerUpdateCmd.Flags().StringSliceVar(&triggerIdentifiers, "trigger", []string{}, "target specific triggers by slug or UUID for bulk patch (can be repeated or comma-separated)")
	triggerUpdateCmd.Flags().StringVar(&invocationSlug, "invocation", "", "invocation to execute (alternative to specifying function and arguments)")
	triggerCmd.AddCommand(triggerUpdateCmd)
}

func checkTriggerConflictingArgs(args []string) bool {
	// Check for bulk patch mode: no positional args
	isBulkPatchMode := len(args) == 0

	if isBulkPatchMode {
		if !triggerPatch {
			failOnError(errors.New("--patch is required in bulk mode"))
		}

		// Check for mutual exclusivity between --trigger and --where flags
		if len(triggerIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--trigger and --where flags are mutually exclusive"))
		}

	} else {
		// Single update mode validation
		if invocationSlug != "" {
			// When using invocation, we need 4 args: slug, event, config type (no function needed)
			if len(args) != 3 {
				failOnError(errors.New("single trigger update with --invocation requires: <slug> <event> <config type>"))
			}
		} else {
			// Traditional mode requires function and arguments
			if len(args) < 4 {
				failOnError(errors.New("single trigger update requires: <slug> <event> <config type> <function> [arguments...]"))
			}
		}

		if filter != "" || where != "" || len(triggerIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --trigger can only be specified with --patch and no positional arguments"))
		}
	}

	if disableTrigger && enableTrigger {
		failOnError(fmt.Errorf("--disable and --enable flags are mutually exclusive"))
	}

	if enforceTrigger && unenforceTrigger {
		failOnError(fmt.Errorf("--enforce and --unenforce flags are mutually exclusive"))
	}

	if triggerPatch && flagReplace {
		failOnError(fmt.Errorf("only one of --patch and --replace should be specified"))
	}

	if err := validateSpaceFlag(isBulkPatchMode); err != nil {
		failOnError(err)
	}

	if err := validateStdinFlags(); err != nil {
		failOnError(err)
	}

	return isBulkPatchMode
}

func runBulkTriggerUpdate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from trigger identifiers or use provided where clause
	var effectiveWhere string
	if len(triggerIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromTriggers(triggerIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Create patch data
	patchData := make(map[string]interface{})

	// Add enable/disable flags
	if disableTrigger {
		patchData["Disabled"] = true
	} else if enableTrigger {
		patchData["Disabled"] = false
	}

	// Add enforce/unenforce flags
	if enforceTrigger {
		patchData["Enforced"] = true
	} else if unenforceTrigger {
		patchData["Enforced"] = false
	}

	// Add worker if specified
	if workerSlug != "" {
		workerUUID, err := parseEntityIdentifierSingle[goclientnew.BridgeWorker](
			workerSlug,
			EntityTypeBridgeWorker,
			apiGetBridgeWorkerFromSlugInSpace,
			func(w *goclientnew.BridgeWorker) string { return w.BridgeWorkerID.String() },
		)
		if err != nil {
			return err
		}
		patchData["BridgeWorkerID"] = workerUUID.String()
	}

	// Add invocation if specified
	if invocationSlug != "" {
		invocationIDStr, err := parseInvocationSlug(invocationSlug)
		if err != nil {
			return err
		}
		patchData["InvocationID"] = invocationIDStr
		// Clear function-related fields when using invocation
		patchData["FunctionName"] = ""
		patchData["Arguments"] = nil
	}

	// Merge with stdin data if provided
	if flagPopulateModelFromStdin || flagFilename != "" {
		stdinBytes, err := getBytesFromFlags()
		if err != nil {
			return err
		}
		if len(stdinBytes) > 0 && string(stdinBytes) != "null" {
			var stdinData map[string]interface{}
			if err := json.Unmarshal(stdinBytes, &stdinData); err != nil {
				return fmt.Errorf("failed to parse stdin data: %w", err)
			}
			// Merge stdinData into patchData
			for k, v := range stdinData {
				patchData[k] = v
			}
		}
	}

	// Add labels if specified
	if len(label) > 0 {
		labelMap := make(map[string]string)
		// Preserve existing labels if any
		if existingLabels, ok := patchData["Labels"]; ok {
			if labelMapInterface, ok := existingLabels.(map[string]interface{}); ok {
				for k, v := range labelMapInterface {
					if strVal, ok := v.(string); ok {
						labelMap[k] = strVal
					}
				}
			}
		}
		err := setLabels(&labelMap)
		if err != nil {
			return err
		}
		patchData["Labels"] = labelMap
	}

	// Convert to JSON
	patchJSON, err := json.Marshal(patchData)
	if err != nil {
		return err
	}

	// Build bulk patch parameters
	include := "SpaceID"
	params := &goclientnew.BulkPatchTriggersParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Call the bulk patch API
	bulkRes, err := cubClientNew.BulkPatchTriggersWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkTriggerCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "update", effectiveWhere)
}

func triggerUpdateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkPatchMode := checkTriggerConflictingArgs(args)

	if isBulkPatchMode {
		return runBulkTriggerUpdate()
	}

	// Single trigger update logic
	if invocationSlug != "" {
		// When using invocation, we need 3 args: slug, event, config type
		if len(args) != 3 {
			return errors.New("single trigger update with --invocation requires: <slug or id> <event> <config type>")
		}
	} else {
		// Traditional mode requires function and arguments
		if len(args) < 4 {
			return errors.New("single trigger update requires: <slug or id> <event> <config type> <function> [arguments...]")
		}
	}

	currentTrigger, err := apiGetTriggerFromSlug(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)

	if triggerPatch {
		// Single trigger patch mode - we'll apply changes directly to the trigger object
		// Handle --from-stdin or --filename
		if flagPopulateModelFromStdin || flagFilename != "" {
			existingTrigger := currentTrigger.Trigger
			if err := populateModelFromFlags(currentTrigger.Trigger); err != nil {
				return err
			}
			// Ensure essential fields can't be clobbered
			currentTrigger.Trigger.OrganizationID = existingTrigger.OrganizationID
			currentTrigger.Trigger.SpaceID = existingTrigger.SpaceID
			currentTrigger.Trigger.TriggerID = existingTrigger.TriggerID
		}

		// Add flags to patch
		if disableTrigger {
			currentTrigger.Trigger.Disabled = true
		} else if enableTrigger {
			currentTrigger.Trigger.Disabled = false
		}
		if enforceTrigger {
			currentTrigger.Trigger.Enforced = true
		} else if unenforceTrigger {
			currentTrigger.Trigger.Enforced = false
		}
		if workerSlug != "" {
			workerUUID, err := parseEntityIdentifierSingle[goclientnew.BridgeWorker](
				workerSlug,
				EntityTypeBridgeWorker,
				apiGetBridgeWorkerFromSlugInSpace,
				func(w *goclientnew.BridgeWorker) string { return w.BridgeWorkerID.String() },
			)
			if err != nil {
				return err
			}
			workerID := goclientnew.UUID(workerUUID)
			currentTrigger.Trigger.BridgeWorkerID = &workerID
		}

		// Add labels if specified
		if len(label) > 0 {
			err := setLabels(&currentTrigger.Trigger.Labels)
			if err != nil {
				return err
			}
		}

		// Add function details from args
		currentTrigger.Trigger.Event = args[1]
		currentTrigger.Trigger.ToolchainType = args[2]
		
		if invocationSlug != "" {
			// Use invocation instead of function and arguments
			invocationID, err := parseInvocationSlug(invocationSlug)
			if err != nil {
				return err
			}
			currentTrigger.Trigger.InvocationID = &invocationID
			// Clear function-related fields when using invocation
			currentTrigger.Trigger.FunctionName = ""
			currentTrigger.Trigger.Arguments = nil
		} else {
			// Traditional function and arguments approach
			currentTrigger.Trigger.FunctionName = args[3]
			if len(args) > 4 {
				invokeArgs := args[4:]
				newArgs := parseFunctionArguments(invokeArgs)
				currentTrigger.Trigger.Arguments = newArgs
			}
		}

		// Convert trigger to patch data
		patchData, err := json.Marshal(currentTrigger.Trigger)
		if err != nil {
			return fmt.Errorf("failed to marshal patch data: %w", err)
		}

		triggerDetails, err := patchTrigger(spaceID, currentTrigger.Trigger.TriggerID, patchData)
		if err != nil {
			return err
		}

		displayUpdateResults(triggerDetails, "trigger", args[0], triggerDetails.TriggerID.String(), func(trigger *goclientnew.Trigger) {
			extendedTriggerWrapper := &goclientnew.ExtendedTrigger{
				Trigger: trigger,
			}
			displayTriggerDetails(extendedTriggerWrapper)
		})
		return nil
	}

	// Traditional update mode
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingTrigger := currentTrigger.Trigger
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			newTrigger := new(goclientnew.Trigger)
			newTrigger.Version = existingTrigger.Version
			currentTrigger.Trigger = newTrigger
		}

		if err := populateModelFromFlags(currentTrigger.Trigger); err != nil {
			return err
		}

		// Ensure essential fields can't be clobbered
		currentTrigger.Trigger.OrganizationID = existingTrigger.OrganizationID
		currentTrigger.Trigger.SpaceID = existingTrigger.SpaceID
		currentTrigger.Trigger.TriggerID = existingTrigger.TriggerID
	}
	err = setLabels(&currentTrigger.Trigger.Labels)
	if err != nil {
		return err
	}

	// If this was set from stdin, it will be overridden
	currentTrigger.Trigger.SpaceID = spaceID
	if disableTrigger {
		currentTrigger.Trigger.Disabled = true
	} else if enableTrigger {
		currentTrigger.Trigger.Disabled = false
	}
	if enforceTrigger {
		currentTrigger.Trigger.Enforced = true
	} else if unenforceTrigger {
		currentTrigger.Trigger.Enforced = false
	}
	if workerSlug != "" {
		workerUUID, err := parseEntityIdentifierSingle[goclientnew.BridgeWorker](
			workerSlug,
			EntityTypeBridgeWorker,
			apiGetBridgeWorkerFromSlugInSpace,
			func(w *goclientnew.BridgeWorker) string { return w.BridgeWorkerID.String() },
		)
		if err != nil {
			return err
		}
		workerID := goclientnew.UUID(workerUUID)
		currentTrigger.Trigger.BridgeWorkerID = &workerID
	}

	// TODO: update with overriden string type TriggerEvent
	// params.Trigger.Event = models.ModelsTriggerEvent(args[1])
	currentTrigger.Trigger.Event = args[1]
	currentTrigger.Trigger.ToolchainType = args[2]
	
	if invocationSlug != "" {
		// Use invocation instead of function and arguments
		invocationID, err := parseInvocationSlug(invocationSlug)
		if err != nil {
			return err
		}
		currentTrigger.Trigger.InvocationID = &invocationID
		// Clear function-related fields when using invocation
		currentTrigger.Trigger.FunctionName = ""
		currentTrigger.Trigger.Arguments = nil
	} else {
		// Traditional function and arguments approach
		currentTrigger.Trigger.FunctionName = args[3]
		invokeArgs := args[4:]
		newArgs := parseFunctionArguments(invokeArgs)
		currentTrigger.Trigger.Arguments = newArgs
	}
	triggerRes, err := cubClientNew.UpdateTriggerWithResponse(ctx, spaceID, currentTrigger.Trigger.TriggerID, *currentTrigger.Trigger)
	if IsAPIError(err, triggerRes) {
		return InterpretErrorGeneric(err, triggerRes)
	}

	triggerDetails := triggerRes.JSON200
	displayUpdateResults(triggerDetails, "trigger", args[0], triggerDetails.TriggerID.String(), func(trigger *goclientnew.Trigger) {
		extendedTriggerWrapper := &goclientnew.ExtendedTrigger{
			Trigger: trigger,
		}
		displayTriggerDetails(extendedTriggerWrapper)
	})
	return nil
}

func handleBulkTriggerCreateOrUpdateResponse(responses200 *[]goclientnew.TriggerCreateOrUpdateResponse, responses207 *[]goclientnew.TriggerCreateOrUpdateResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkGenericCreateOrUpdateResults(
		responses200, responses207, statusCode, "trigger", operationName, contextInfo,
		func(r *goclientnew.TriggerCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.TriggerCreateOrUpdateResponse) string {
			if r.Trigger != nil {
				return fmt.Sprintf("%s (ID: %s)", r.Trigger.Slug, r.Trigger.TriggerID)
			}
			return ""
		},
	)
}

func patchTrigger(spaceID uuid.UUID, triggerID uuid.UUID, patchData []byte) (*goclientnew.Trigger, error) {
	triggerRes, err := cubClientNew.PatchTriggerWithBodyWithResponse(
		ctx,
		spaceID,
		triggerID,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if IsAPIError(err, triggerRes) {
		return nil, InterpretErrorGeneric(err, triggerRes)
	}

	return triggerRes.JSON200, nil
}
