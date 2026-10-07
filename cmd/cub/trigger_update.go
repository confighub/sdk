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
	Use:   "update [<slug or id> [<event> <config type> <function> [<arg1> ...]]]",
	Short: "Update a trigger or multiple triggers",
	Long: getCommandHelp(`Update a trigger or multiple triggers using bulk operations.

Single trigger update:

A whole update names the trigger's event, config type and function, as create does. With --patch, the slug alone
patches only the fields the flags set:
`+"```"+`
  cub trigger update --space my-space --patch my-trigger --label owner=platform --warn
`+"```"+`

Function arguments can be provided as positional arguments or as named arguments using --argumentname=value syntax.
Once a named argument is used, all subsequent arguments must be named. Use "--" to separate command flags from function arguments when using named function arguments.

Example with named arguments:
`+"```"+`
  cub trigger update --space my-space my-trigger Mutation Kubernetes/YAML -- set-annotation --annotation-key=cloned --annotation-value=true
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
	addFieldEditFlags(triggerUpdateCmd, "Trigger")
	enableUpdatePermissionFlag(triggerUpdateCmd)
	triggerUpdateCmd.Flags().BoolVar(&disableTrigger, "disable", false, "Disable trigger")
	triggerUpdateCmd.Flags().BoolVar(&enableTrigger, "enable", false, "Enable trigger (use with --patch for bulk)")
	triggerUpdateCmd.Flags().BoolVar(&warnTrigger, "warn", false, "Set trigger to produce ValidationWarnings instead of ValidationErrors")
	triggerUpdateCmd.Flags().BoolVar(&unwarnTrigger, "unwarn", false, "Set trigger to produce ValidationErrors (default, use with --patch for bulk)")
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
	triggerUpdateCmd.Flags().StringVar(&triggerWhereUnit, "where-unit-field", "", "filter expression to restrict which Units this trigger applies to (its WhereUnit). It takes what --where does on unit list, attributes of what a unit refers to included, as in \"Space.Labels.Environment = 'prod'\"")
	triggerUpdateCmd.Flags().StringVar(&triggerUnitFilter, "unit-filter", "", "filter entity (slug or UUID) to restrict which Units this trigger applies to")
	triggerUpdateCmd.Flags().StringVar(&triggerWhereResource, "where-resource", "", "metadata path expression to restrict which resources the trigger operates on")
	triggerUpdateCmd.Flags().StringVar(&triggerFailOpenAfter, "fail-open-after", "", "duration after which disconnected worker triggers fail open (e.g., 6h, 30m)")
	triggerUpdateCmd.Flags().StringVar(&triggerOtherDataSource, "other-data-source", "", "source of additional data to pass to the function (e.g., LastReleasedRevisionNum); defaults to the sources the function expects")
	addBackingUnitFlags(triggerUpdateCmd, "Trigger", false, false)
	addFromBackingUnitsFlags(triggerUpdateCmd, "Trigger", false)
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
		if err := checkSingleTriggerUpdateArgs(args); err != nil {
			failOnError(err)
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

	enhancer, err := triggerPatchEnhancer()
	if err != nil {
		return err
	}

	// Build patch data using consolidated function
	patchJSON, err := BuildPatchDataWithPermissions(enhancer, permissionFlag)
	if err != nil {
		return err
	}

	// Build bulk patch parameters
	include := "SpaceID"
	params := &goclientnew.BulkPatchTriggersParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	params.IncludeHidden = includeHiddenParam()
	params.WithBackingUnits = withBackingUnitsParam()
	params.FromBackingUnits = fromBackingUnitsParam()
	if filterID != "" {
		params.Filter = &filterID
	}

	params.DryRun = dryRunParam()
	// Call the bulk patch API
	bulkRes, err := cubClientNew.BulkPatchTriggersWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkTriggerCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "update", effectiveWhere)
}

func triggerUpdateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkPatchMode := checkTriggerConflictingArgs(args)

	if isBulkPatchMode {
		return runBulkTriggerUpdate()
	}

	currentTrigger, err := resolveTrigger(args[0], selectedSpaceID, "*") // get all fields for RMW
	if err != nil {
		return err
	}

	spaceID := currentTrigger.Trigger.SpaceID

	if triggerPatch {
		flagFields, err := triggerPatchEnhancer()
		if err != nil {
			return err
		}
		triggerEnhancer := func(patchData map[string]interface{}) {
			flagFields(patchData)
			if len(args) == 1 {
				return
			}
			patchData["Event"] = args[1]
			patchData["ToolchainType"] = args[2]
			if invocationSlug == "" {
				patchData["FunctionName"] = args[3]
				if len(args) > 4 {
					patchData["Arguments"] = parseFunctionArguments(args[4:])
				}
			}
		}

		patchData, err := BuildPatchDataWithPermissions(triggerEnhancer, permissionFlag)
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
	setDisplayNameAndHiddenReason(&currentTrigger.Trigger.DisplayName, &currentTrigger.Trigger.HiddenReason)
	if err := setDeleteGates(&currentTrigger.Trigger.DeleteGates); err != nil {
		return err
	}
	err = setAnnotations(&currentTrigger.Trigger.Annotations)
	if err != nil {
		return err
	}
	err = setLabels(&currentTrigger.Trigger.Labels)
	if err != nil {
		return err
	}
	if err := setPermissions(&currentTrigger.Trigger.Permissions); err != nil {
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
		workerUUID, err := resolveWorkerID(workerSlug)
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
		invocationID, err := resolveInvocationID(invocationSlug)
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
		filterUUID, err := resolveFilterID(triggerUnitFilter)
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
	if err := applyFieldEdits("Trigger", currentTrigger.Trigger); err != nil {
		return err
	}
	triggerRes, err := cubClientNew.UpdateTriggerWithResponse(ctx, spaceID, currentTrigger.Trigger.TriggerID, &goclientnew.UpdateTriggerParams{DryRun: dryRunParam()}, *currentTrigger.Trigger)
	if cubapi.IsAPIError(err, triggerRes) {
		return cubapi.InterpretErrorGeneric(err, triggerRes)
	}

	triggerDetails := triggerRes.JSON200
	displayUpdateResults(triggerDetails, "trigger", args[0], triggerDetails.TriggerID.String(), displayTriggerDetails)
	return nil
}

// checkSingleTriggerUpdateArgs checks the positional arguments of an update of one trigger. A whole
// update names the event, config type and function (or, with --invocation, the event and config
// type), since it replaces them; a patch may name the trigger alone and leave them as they are.
func checkSingleTriggerUpdateArgs(args []string) error {
	if triggerPatch && len(args) == 1 {
		return nil
	}
	if invocationSlug != "" {
		if len(args) != 3 {
			return errors.New("single trigger update with --invocation requires: <slug> <event> <config type>, or with --patch <slug> alone")
		}
		return nil
	}
	if len(args) < 4 {
		return errors.New("single trigger update requires: <slug> <event> <config type> <function> [arguments...], or with --patch <slug> alone")
	}
	return nil
}

// triggerPatchEnhancer returns the enhancer that adds the fields the trigger flags set to a patch,
// for a patch of one trigger and a bulk patch alike. It resolves and parses the flags first, so
// that a flag naming nothing, or a malformed one, is refused rather than left out of the patch.
func triggerPatchEnhancer() (PatchEnhancer, error) {
	var workerID *uuid.UUID
	if workerSlug != "" {
		id, err := resolveWorkerID(workerSlug)
		if err != nil {
			return nil, err
		}
		workerID = &id
	}
	var invocationID *uuid.UUID
	if invocationSlug != "" {
		id, err := resolveInvocationID(invocationSlug)
		if err != nil {
			return nil, err
		}
		invocationID = &id
	}
	var unitFilterID *uuid.UUID
	if triggerUnitFilter != "" {
		id, err := resolveFilterID(triggerUnitFilter)
		if err != nil {
			return nil, err
		}
		unitFilterID = &id
	}
	var failOpenAfter *time.Duration
	if triggerFailOpenAfter != "" {
		duration, err := time.ParseDuration(triggerFailOpenAfter)
		if err != nil {
			return nil, fmt.Errorf("invalid --fail-open-after duration: %w", err)
		}
		failOpenAfter = &duration
	}
	parsedClearance, parsedGuards, err := parseTriggerPolicyFlags()
	if err != nil {
		return nil, err
	}

	return func(patchMap map[string]interface{}) {
		if disableTrigger {
			patchMap["Disabled"] = true
		} else if enableTrigger {
			patchMap["Disabled"] = false
		}
		if warnTrigger {
			patchMap["Warn"] = true
		} else if unwarnTrigger {
			patchMap["Warn"] = false
		}
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
		if workerID != nil {
			patchMap["BridgeWorkerID"] = workerID.String()
		}
		if invocationID != nil {
			patchMap["InvocationID"] = invocationID.String()
			// An invocation takes the place of the function and its arguments.
			patchMap["FunctionName"] = ""
			patchMap["Arguments"] = nil
		}
		if triggerDescription != "" {
			patchMap["Description"] = triggerDescription
		}
		if triggerWhereUnit != "" {
			patchMap["WhereUnit"] = triggerWhereUnit
		}
		if unitFilterID != nil {
			patchMap["UnitFilterID"] = unitFilterID.String()
		}
		if triggerWhereResource != "" {
			patchMap["WhereResource"] = triggerWhereResource
		}
		if triggerOtherDataSource != "" {
			patchMap["OtherDataSource"] = triggerOtherDataSource
		}
		if failOpenAfter != nil {
			patchMap["FailOpenAfter"] = int(*failOpenAfter)
		}
	}, nil
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
		&goclientnew.PatchTriggerParams{DryRun: dryRunParam()},
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
