// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var triggerUpdateCmd = &cobra.Command{
	Use:   "update [<slug or id>] [<event> <config type> <function> [<arg1> ...]]",
	Short: "Update a trigger or multiple triggers",
	Long: getCommandHelp(`Update a trigger or multiple triggers using bulk operations.

Single trigger update:

Function arguments can be provided as positional arguments or as named arguments using --argumentname=value syntax.
Once a named argument is used, all subsequent arguments must be named. Use "--" to separate command flags from function arguments when using named function arguments.

Example with named arguments:
`+"```"+`
  cub trigger update --space my-space my-trigger Mutation Kubernetes/YAML -- set-annotation --key=cloned --value=true
`+"```"+`

Bulk update with --patch:

Update multiple triggers at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
`+"```"+`
  # Disable all triggers for a specific function
  cub trigger update --patch --where "FunctionName = 'vet-cel'" --disable

  # Enable all disabled triggers
  cub trigger update --patch --where "Disabled = true" --enable

  # Update worker for all triggers of a certain type using JSON patch
  echo '{"BridgeWorkerID": "worker-uuid"}' | cub trigger update --patch --where "ToolchainType = 'Kubernetes/YAML'" --from-stdin

  # Mark triggers as warn mode
  cub trigger update --patch --where "Event = 'Mutation'" --warn

  # Update specific triggers by slug
  cub trigger update --patch --trigger my-trigger,another-trigger --disable
`+"```"+`
`, ""),
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        triggerUpdateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	disableTrigger         bool
	enableTrigger          bool
	warnTrigger            bool
	unwarnTrigger          bool
	protectTrigger         bool
	unprotectTrigger       bool
	workerSlug             string
	triggerPatch           bool
	triggerIdentifiers     []string
	invocationSlug         string
	triggerDescription     string
	triggerWhereUnit       string
	triggerUnitFilter      string
	triggerWhereResource   string
	triggerFailOpenAfter   string
	triggerOtherDataSource string
)

func init() {
	addStandardUpdateFlags(triggerUpdateCmd)
	triggerUpdateCmd.Flags().BoolVar(&disableTrigger, "disable", false, "Disable trigger")
	triggerUpdateCmd.Flags().BoolVar(&enableTrigger, "enable", false, "Enable trigger (use with --patch for bulk)")
	triggerUpdateCmd.Flags().BoolVar(&warnTrigger, "warn", false, "Set trigger to produce ApplyWarnings instead of ApplyGates")
	triggerUpdateCmd.Flags().BoolVar(&unwarnTrigger, "unwarn", false, "Set trigger to produce ApplyGates (default, use with --patch for bulk)")
	addTriggerClearanceFlag(triggerUpdateCmd)
	addTriggerGuardFlag(triggerUpdateCmd)
	triggerUpdateCmd.Flags().BoolVar(&protectTrigger, "protect", false, "record the paths this trigger's function writes as protected local overrides, so a later merge from upstream does not overwrite them; for a trigger that decides a value the unit then owns, such as a PostClone trigger customizing a variant")
	triggerUpdateCmd.Flags().BoolVar(&unprotectTrigger, "unprotect", false, "return this trigger to the default: it claims nothing it writes")
	triggerUpdateCmd.Flags().StringVar(&workerSlug, "worker", "", "worker to execute the trigger function")
	triggerUpdateCmd.Flags().BoolVar(&triggerPatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(triggerUpdateCmd)
	enableFilterFlag(triggerUpdateCmd)
	triggerUpdateCmd.Flags().StringSliceVar(&triggerIdentifiers, "trigger", []string{}, "target specific triggers by slug or UUID for bulk patch (can be repeated or comma-separated)")
	triggerUpdateCmd.Flags().StringVar(&invocationSlug, "invocation", "", "invocation to execute (alternative to specifying function and arguments)")
	triggerUpdateCmd.Flags().StringVar(&triggerDescription, "description", "", "description explaining the trigger's purpose and how to fix failures")
	triggerUpdateCmd.Flags().StringVar(&triggerWhereUnit, "where-unit", "", "filter expression to restrict which Units this trigger applies to")
	triggerUpdateCmd.Flags().StringVar(&triggerUnitFilter, "unit-filter", "", "filter entity (slug or UUID) to restrict which Units this trigger applies to")
	triggerUpdateCmd.Flags().StringVar(&triggerWhereResource, "where-resource", "", "metadata path expression to restrict which resources the trigger operates on")
	triggerUpdateCmd.Flags().StringVar(&triggerFailOpenAfter, "fail-open-after", "", "duration after which disconnected worker triggers fail open (e.g., 6h, 30m)")
	triggerUpdateCmd.Flags().StringVar(&triggerOtherDataSource, "other-data-source", "", "source of additional data to pass to the function (e.g., LastReleasedRevisionNum)")
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

	if protectTrigger && unprotectTrigger {
		failOnError(fmt.Errorf("--protect and --unprotect flags are mutually exclusive"))
	}
	if warnTrigger && unwarnTrigger {
		failOnError(fmt.Errorf("--warn and --unwarn flags are mutually exclusive"))
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

	// Validate label removal only works with patch
	if err := ValidateLabelRemoval(label, triggerPatch); err != nil {
		failOnError(err)
	}
	// Validate delete gate removal only works with patch
	if err := ValidateDeleteGateRemoval(deleteGate, triggerPatch); err != nil {
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

	// Validate and resolve entity references early
	var workerUUID *uuid.UUID
	if workerSlug != "" {
		workerID, err := parseEntityIdentifierSingle[goclientnew.BridgeWorker](
			workerSlug,
			EntityTypeBridgeWorker,
			apiGetBridgeWorkerFromSlugInSpace,
			func(w *goclientnew.BridgeWorker) string { return w.BridgeWorkerID.String() },
		)
		if err != nil {
			return err
		}
		workerUUID = &workerID
	}

	var invocationIDStr string
	if invocationSlug != "" {
		invocationID, err := parseInvocationSlug(invocationSlug)
		if err != nil {
			return err
		}
		invocationIDStr = invocationID.String()
	}

	parsedClearance, parsedGuards, err := parseTriggerPolicyFlags()
	if err != nil {
		return err
	}

	// Create enhancer function for trigger-specific fields
	enhancer := func(patchMap map[string]interface{}) {
		// Add enable/disable flags
		if disableTrigger {
			patchMap["Disabled"] = true
		} else if enableTrigger {
			patchMap["Disabled"] = false
		}

		// Add warn/unwarn flags
		if warnTrigger {
			patchMap["Warn"] = true
		} else if unwarnTrigger {
			patchMap["Warn"] = false
		}

		// Add protect/unprotect flags
		if parsedClearance != nil {
			patchMap["Clearance"] = parsedClearance
		}
		if parsedGuards != nil {
			patchMap["Guards"] = parsedGuards
		}
		if protectTrigger {
			patchMap["Protect"] = true
		} else if unprotectTrigger {
			patchMap["Protect"] = false
		}

		// Add worker if specified
		if workerUUID != nil {
			patchMap["BridgeWorkerID"] = workerUUID.String()
		}

		// Add invocation if specified
		if invocationIDStr != "" {
			patchMap["InvocationID"] = invocationIDStr
			// Clear function-related fields when using invocation
			patchMap["FunctionName"] = ""
			patchMap["Arguments"] = nil
		}

		// Add new trigger fields if specified
		if triggerDescription != "" {
			patchMap["Description"] = triggerDescription
		}
		if triggerWhereUnit != "" {
			patchMap["WhereUnit"] = triggerWhereUnit
		}
		if triggerWhereResource != "" {
			patchMap["WhereResource"] = triggerWhereResource
		}
		if triggerOtherDataSource != "" {
			patchMap["OtherDataSource"] = triggerOtherDataSource
		}
		if triggerFailOpenAfter != "" {
			duration, err := time.ParseDuration(triggerFailOpenAfter)
			if err == nil {
				patchMap["FailOpenAfter"] = int(duration)
			}
		}
	}

	// Build patch data using consolidated function
	patchJSON, err := BuildPatchData(enhancer)
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
		// Single trigger patch mode
		// Handle error-prone operations before enhancer
		var workerID *goclientnew.UUID
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
			workerUUIDConverted := goclientnew.UUID(workerUUID)
			workerID = &workerUUIDConverted
		}

		var invocationID *uuid.UUID
		if invocationSlug != "" {
			id, err := parseInvocationSlug(invocationSlug)
			if err != nil {
				return err
			}
			invocationID = &id
		}

		// Parse function arguments if needed
		var newArgs []goclientnew.FunctionArgument
		if invocationSlug == "" && len(args) > 4 {
			invokeArgs := args[4:]
			newArgs = parseFunctionArguments(invokeArgs)
		}

		parsedClearance, parsedGuards, perr := parseTriggerPolicyFlags()
		if perr != nil {
			return perr
		}

		// Build patch data using BuildPatchData with trigger enhancer
		triggerEnhancer := func(patchData map[string]interface{}) {
			// Add trigger-specific fields
			if warnTrigger {
				patchData["Warn"] = true
			} else if unwarnTrigger {
				patchData["Warn"] = false
			}

			if parsedClearance != nil {
				patchData["Clearance"] = parsedClearance
			}
			if parsedGuards != nil {
				patchData["Guards"] = parsedGuards
			}
			if protectTrigger {
				patchData["Protect"] = true
			} else if unprotectTrigger {
				patchData["Protect"] = false
			}

			if disableTrigger {
				patchData["Disabled"] = true
			} else if enableTrigger {
				patchData["Disabled"] = false
			}

			if workerID != nil {
				patchData["BridgeWorkerID"] = *workerID
			}

			// Add function details from args
			patchData["Event"] = args[1]
			patchData["ToolchainType"] = args[2]

			if invocationID != nil {
				// Use invocation instead of function and arguments
				patchData["InvocationID"] = *invocationID
				// Clear function-related fields when using invocation
				patchData["FunctionName"] = ""
				patchData["Arguments"] = nil
			} else {
				// Traditional function and arguments approach
				patchData["FunctionName"] = args[3]
				if newArgs != nil {
					patchData["Arguments"] = newArgs
				}
			}

			// Add new trigger fields if specified
			if triggerDescription != "" {
				patchData["Description"] = triggerDescription
			}
			if triggerWhereUnit != "" {
				patchData["WhereUnit"] = triggerWhereUnit
			}
			if triggerWhereResource != "" {
				patchData["WhereResource"] = triggerWhereResource
			}
			if triggerOtherDataSource != "" {
				patchData["OtherDataSource"] = triggerOtherDataSource
			}
			if triggerFailOpenAfter != "" {
				duration, err := time.ParseDuration(triggerFailOpenAfter)
				if err == nil {
					patchData["FailOpenAfter"] = int(duration)
				}
			}
		}

		patchData, err := BuildPatchData(triggerEnhancer)
		if err != nil {
			return fmt.Errorf("failed to build patch data: %w", err)
		}

		triggerDetails, err := patchTrigger(spaceID, currentTrigger.Trigger.TriggerID, patchData)
		if err != nil {
			return err
		}

		displayUpdateResults(triggerDetails, "trigger", args[0], triggerDetails.TriggerID.String(), displayTriggerDetails)
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
	err = setAnnotations(&currentTrigger.Trigger.Annotations)
	if err != nil {
		return err
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
	if warnTrigger {
		currentTrigger.Trigger.Warn = true
	} else if unwarnTrigger {
		currentTrigger.Trigger.Warn = false
	}
	if protectTrigger {
		currentTrigger.Trigger.Protect = true
	} else if unprotectTrigger {
		currentTrigger.Trigger.Protect = false
	}
	// The full-update path assigns these as well as the two patch paths, since all three
	// accept the flags and a field the command takes has to reach the entity on every route
	// that takes it.
	fullClearance, fullGuards, policyErr := parseTriggerPolicyFlags()
	if policyErr != nil {
		return policyErr
	}
	if fullClearance != nil {
		currentTrigger.Trigger.Clearance = &fullClearance
	}
	if fullGuards != nil {
		currentTrigger.Trigger.Guards = &fullGuards
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
	if triggerDescription != "" {
		currentTrigger.Trigger.Description = triggerDescription
	}
	if triggerWhereUnit != "" {
		currentTrigger.Trigger.WhereUnit = triggerWhereUnit
	}
	if triggerUnitFilter != "" {
		filterUUID, err := parseEntityIdentifierSingle[goclientnew.Filter](
			triggerUnitFilter,
			EntityTypeFilter,
			apiGetFilterFromSlugInSpace,
			func(f *goclientnew.Filter) string { return f.FilterID.String() },
		)
		if err != nil {
			return err
		}
		filterID := goclientnew.UUID(filterUUID)
		currentTrigger.Trigger.UnitFilterID = &filterID
	}
	if triggerWhereResource != "" {
		currentTrigger.Trigger.WhereResource = triggerWhereResource
	}
	if triggerOtherDataSource != "" {
		currentTrigger.Trigger.OtherDataSource = triggerOtherDataSource
	}
	if triggerFailOpenAfter != "" {
		duration, err := time.ParseDuration(triggerFailOpenAfter)
		if err != nil {
			return fmt.Errorf("invalid --fail-open-after duration: %w", err)
		}
		currentTrigger.Trigger.FailOpenAfter = int(duration)
	}
	triggerRes, err := cubClientNew.UpdateTriggerWithResponse(ctx, spaceID, currentTrigger.Trigger.TriggerID, *currentTrigger.Trigger)
	if cubapi.IsAPIError(err, triggerRes) {
		return cubapi.InterpretErrorGeneric(err, triggerRes)
	}

	triggerDetails := triggerRes.JSON200
	displayUpdateResults(triggerDetails, "trigger", args[0], triggerDetails.TriggerID.String(), displayTriggerDetails)
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
	if cubapi.IsAPIError(err, triggerRes) {
		return nil, cubapi.InterpretErrorGeneric(err, triggerRes)
	}

	return triggerRes.JSON200, nil
}

// parseTriggerPolicyFlags parses --clearance and --guard once, up front, so that a malformed
// flag is refused before anything is written. The patch enhancers cannot report an error -- they
// take only the map they fill -- and dropping a field a malformed flag produced would report
// success on an update that did not make the change asked for.
//
// A nil result means the flag was not given, which is distinct from an empty one: an empty
// clearance clears nothing and is how a clearance is removed.
func parseTriggerPolicyFlags() (goclientnew.Clearance, goclientnew.GuardStamp, error) {
	var clearance goclientnew.Clearance
	var guards goclientnew.GuardStamp
	if len(triggerClearance) > 0 {
		parsed, err := parseClearanceSpecs(triggerClearance)
		if err != nil {
			return nil, nil, err
		}
		clearance = parsed
	}
	if len(triggerGuards) > 0 {
		parsed, err := parseGuardStampSpecs(triggerGuards)
		if err != nil {
			return nil, nil, err
		}
		if parsed == nil {
			parsed = goclientnew.GuardStamp{}
		}
		guards = parsed
	}
	return clearance, guards, nil
}
