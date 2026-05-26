// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var targetUpdateArgs struct {
	whereTrigger    string
	triggerFilter   string
	liveStateType   string
	permissions     []string
	refreshTriggers bool
}

var targetUpdateCmd = &cobra.Command{
	Use:   "update [<slug or id>]",
	Short: "Update a target or multiple targets",
	Long: getCommandHelp(`Update a target or multiple targets using bulk operations.

Single target update:
`+"```"+`
  cub target update --space my-space my-target
`+"```"+`

Bulk update with --patch:

Update multiple targets at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
`+"```"+`
  # Update all targets with a specific toolchain type using JSON patch
  echo '{"Parameters": "{}"}' | cub target update --patch --where "ToolchainType = 'Kubernetes/YAML'" --from-stdin

  # Update specific targets by slug
  cub target update --patch --target my-target,another-target --from-stdin < patch.json
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(1), // Allow 0 args for bulk mode
	RunE:        targetUpdateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	targetPatch       bool
	targetIdentifiers []string
)

func init() {
	addStandardUpdateFlags(targetUpdateCmd)
	targetUpdateCmd.Flags().BoolVar(&targetPatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(targetUpdateCmd)
	enableFilterFlag(targetUpdateCmd)
	targetUpdateCmd.Flags().StringSliceVar(&targetIdentifiers, "target", []string{}, "target specific targets by slug or UUID for bulk patch (can be repeated or comma-separated)")
	targetUpdateCmd.Flags().StringSliceVar(&targetUpdateArgs.permissions, "permission", []string{}, "permission in format Action:UserIDOrUsername to add, or -Action:UserIDOrUsername to remove (e.g., Manage:user@example.com, -View:user@example.com, can be repeated)")
	targetUpdateCmd.Flags().StringVar(&targetUpdateArgs.whereTrigger, "where-trigger", "", "filter expression to identify Triggers that should be invoked on Units associated with this Target (use '-' to clear)")
	targetUpdateCmd.Flags().StringVar(&targetUpdateArgs.triggerFilter, "trigger-filter", "", "Filter slug or UUID to identify Triggers that should be invoked on Units associated with this Target (use '-' to clear)")
	enableOptionFlag(targetUpdateCmd)
	enableFactFlag(targetUpdateCmd)
	targetUpdateCmd.Flags().StringVar(&targetUpdateArgs.liveStateType, "livestate-type", "", "The toolchain type for live state of the target's provider type (use '-' to clear).\n\t(e.g., Kubernetes/YAML, ConfigHub/YAML)")
	targetUpdateCmd.Flags().BoolVar(&targetUpdateArgs.refreshTriggers, "refresh-triggers", false, "re-list the Triggers matching WhereTrigger and/or TriggerFilterID even if these fields have not changed")
	targetCmd.AddCommand(targetUpdateCmd)
}

// func checkTargetUpdateConflictingArgs(args []string) bool {
// }

func targetUpdateCmdRun(cmd *cobra.Command, args []string) error {
	// TODO: Refactor to checkTargetUpdateConflictingArgs

	if err := validateStdinFlags(); err != nil {
		return err
	}

	// Validate label removal only works with patch
	if err := ValidateLabelRemoval(label, targetPatch); err != nil {
		return err
	}
	// Validate delete gate removal only works with patch
	if err := ValidateDeleteGateRemoval(deleteGate, targetPatch); err != nil {
		return err
	}
	// Validate fact removal only works with patch
	if err := ValidateFactRemoval(fact, targetPatch); err != nil {
		return err
	}

	// Check for bulk patch mode (no positional args with --patch)
	isBulkPatchMode := len(args) == 0

	if isBulkPatchMode {
		if !targetPatch {
			failOnError(errors.New("--patch is required in bulk mode"))
		}
		// Check for mutual exclusivity between --target and --where flags
		if len(targetIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--target and --where flags are mutually exclusive"))
		}

		return targetBulkPatchCmdRun(cmd, args)
	}

	// Check for individual patch mode (single target with --patch)
	if targetPatch && len(args) == 1 {
		return targetIndividualPatchCmdRun(cmd, args)
	}

	// Regular update mode validation
	if len(args) != 1 {
		return errors.New("single target update requires: <slug or id>")
	}

	// Check that bulk-only flags are not used in single mode
	if filter != "" || where != "" || len(targetIdentifiers) > 0 {
		return fmt.Errorf("--filter, --where, or --target can only be specified with --patch")
	}

	currentTarget, err := apiGetTargetFromSlug(args[0], selectedSpaceID, "*") // get all fields for RMW
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingTarget := currentTarget.Target
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			currentTarget.Target = new(goclientnew.Target)
			currentTarget.Target.Version = existingTarget.Version
		}

		if err := populateModelFromFlags(currentTarget.Target); err != nil {
			return err
		}

		// Ensure essential fields can't be clobbered
		currentTarget.Target.OrganizationID = existingTarget.OrganizationID
		currentTarget.Target.SpaceID = existingTarget.SpaceID
		currentTarget.Target.TargetID = existingTarget.TargetID
	}

	// Set LiveStateType if provided
	if targetUpdateArgs.liveStateType == "-" {
		currentTarget.Target.LiveStateType = ""
	} else if targetUpdateArgs.liveStateType != "" {
		currentTarget.Target.LiveStateType = targetUpdateArgs.liveStateType
	}

	err = validateToolchainAndProvider(currentTarget.Target.ToolchainType, currentTarget.Target.ProviderType, currentTarget.Target.LiveStateType)
	if err != nil {
		return err
	}

	err = setAnnotations(&currentTarget.Target.Annotations)
	if err != nil {
		return err
	}
	err = setLabels(&currentTarget.Target.Labels)
	if err != nil {
		return err
	}
	err = setFacts(&currentTarget.Target.Facts)
	if err != nil {
		return err
	}
	err = setDeleteGates(&currentTarget.Target.DeleteGates)
	if err != nil {
		return err
	}
	err = setOptions(&currentTarget.Target.Options)
	if err != nil {
		return err
	}

	// Parse and set permissions
	err = parsePermissions(targetUpdateArgs.permissions, currentTarget.Target.Permissions)
	if err != nil {
		return err
	}

	// Set WhereTrigger if provided
	if targetUpdateArgs.whereTrigger == "-" {
		currentTarget.Target.WhereTrigger = ""
	} else if targetUpdateArgs.whereTrigger != "" {
		currentTarget.Target.WhereTrigger = targetUpdateArgs.whereTrigger
	}

	// Set TriggerFilterID if provided
	if targetUpdateArgs.triggerFilter == "-" {
		currentTarget.Target.TriggerFilterID = nil
	} else if targetUpdateArgs.triggerFilter != "" {
		triggerFilterID, err := parseFilterFlag(targetUpdateArgs.triggerFilter)
		if err != nil {
			return err
		}
		triggerFilterUUID := uuid.MustParse(triggerFilterID)
		currentTarget.Target.TriggerFilterID = &triggerFilterUUID
	}

	// If this was set from stdin, it will be overridden
	currentTarget.Target.SpaceID = spaceID

	updateParams := &goclientnew.UpdateTargetParams{}
	if targetUpdateArgs.refreshTriggers {
		updateParams.RefreshTriggers = &targetUpdateArgs.refreshTriggers
	}
	targetRes, err := cubClientNew.UpdateTargetWithResponse(ctx, spaceID, currentTarget.Target.TargetID, updateParams, *currentTarget.Target)
	if cubapi.IsAPIError(err, targetRes) {
		return cubapi.InterpretErrorGeneric(err, targetRes)
	}

	targetDetails := targetRes.JSON200
	extendedDetails := &goclientnew.ExtendedTarget{Target: targetDetails}
	displayUpdateResults(extendedDetails, "target", args[0], targetDetails.TargetID.String(), displayTargetDetails)
	return nil
}

func targetIndividualPatchCmdRun(cmd *cobra.Command, args []string) error {
	currentTarget, err := apiGetTargetFromSlug(args[0], selectedSpaceID, "*")
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)

	// Parse TriggerFilterID if provided
	var triggerFilterUUID *uuid.UUID
	if targetUpdateArgs.triggerFilter == "-" {
		// Explicitly clear
		triggerFilterUUID = nil
	} else if targetUpdateArgs.triggerFilter != "" {
		triggerFilterID, err := parseFilterFlag(targetUpdateArgs.triggerFilter)
		if err != nil {
			return err
		}
		parsed := uuid.MustParse(triggerFilterID)
		triggerFilterUUID = &parsed
	}

	// Build patch data using consolidated function with target enhancer
	targetEnhancer := func(patchMap map[string]interface{}) {
		// Add Options if provided
		if len(option) > 0 {
			optionMap := make(map[string]interface{})
			if existingOptions, ok := patchMap["Options"]; ok {
				if optionMapInterface, ok := existingOptions.(map[string]interface{}); ok {
					for k, v := range optionMapInterface {
						optionMap[k] = v
					}
				}
			}
			_ = patchKeyValues(optionMap, splitOptionsBySemicolon(option))
			patchMap["Options"] = optionMap
		}
		// Add Facts if provided
		if len(fact) > 0 {
			factMap := make(map[string]interface{})
			if existingFacts, ok := patchMap["Facts"]; ok {
				if factMapInterface, ok := existingFacts.(map[string]interface{}); ok {
					for k, v := range factMapInterface {
						factMap[k] = v
					}
				}
			}
			_ = patchKeyValues(factMap, fact)
			patchMap["Facts"] = factMap
		}
		// Add LiveStateType if provided
		if targetUpdateArgs.liveStateType == "-" {
			patchMap["LiveStateType"] = ""
		} else if targetUpdateArgs.liveStateType != "" {
			patchMap["LiveStateType"] = targetUpdateArgs.liveStateType
		}
		// Add WhereTrigger if provided
		if targetUpdateArgs.whereTrigger == "-" {
			patchMap["WhereTrigger"] = ""
		} else if targetUpdateArgs.whereTrigger != "" {
			patchMap["WhereTrigger"] = targetUpdateArgs.whereTrigger
		}
		// Add TriggerFilterID if provided
		if targetUpdateArgs.triggerFilter == "-" {
			patchMap["TriggerFilterID"] = nil
		} else if triggerFilterUUID != nil {
			patchMap["TriggerFilterID"] = triggerFilterUUID.String()
		}
	}

	patchJSON, err := BuildPatchDataWithPermissions(targetEnhancer, targetUpdateArgs.permissions)
	if err != nil {
		return err
	}

	if len(patchJSON) == 0 || string(patchJSON) == "null" {
		return errors.New("no updates specified")
	}

	patchParams := &goclientnew.PatchTargetParams{}
	if targetUpdateArgs.refreshTriggers {
		patchParams.RefreshTriggers = &targetUpdateArgs.refreshTriggers
	}
	targetRes, err := cubClientNew.PatchTargetWithBodyWithResponse(ctx, spaceID, currentTarget.Target.TargetID, patchParams, "application/merge-patch+json", bytes.NewReader(patchJSON))
	if cubapi.IsAPIError(err, targetRes) {
		return cubapi.InterpretErrorGeneric(err, targetRes)
	}

	targetDetails := targetRes.JSON200
	extendedDetails := &goclientnew.ExtendedTarget{Target: targetDetails}
	displayUpdateResults(extendedDetails, "target", args[0], targetDetails.TargetID.String(), displayTargetDetails)
	return nil
}

func targetBulkPatchCmdRun(cmd *cobra.Command, args []string) error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from target identifiers or use provided where clause
	var effectiveWhere string
	if len(targetIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromTargets(targetIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Parse TriggerFilterID if provided
	var triggerFilterUUID *uuid.UUID
	if targetUpdateArgs.triggerFilter == "-" {
		// Explicitly clear
		triggerFilterUUID = nil
	} else if targetUpdateArgs.triggerFilter != "" {
		triggerFilterID, err := parseFilterFlag(targetUpdateArgs.triggerFilter)
		if err != nil {
			return err
		}
		parsed := uuid.MustParse(triggerFilterID)
		triggerFilterUUID = &parsed
	}

	// Build patch data with target enhancer
	targetEnhancer := func(patchMap map[string]interface{}) {
		// Add Options if provided
		if len(option) > 0 {
			optionMap := make(map[string]interface{})
			if existingOptions, ok := patchMap["Options"]; ok {
				if optionMapInterface, ok := existingOptions.(map[string]interface{}); ok {
					for k, v := range optionMapInterface {
						optionMap[k] = v
					}
				}
			}
			_ = patchKeyValues(optionMap, splitOptionsBySemicolon(option))
			patchMap["Options"] = optionMap
		}
		// Add Facts if provided
		if len(fact) > 0 {
			factMap := make(map[string]interface{})
			if existingFacts, ok := patchMap["Facts"]; ok {
				if factMapInterface, ok := existingFacts.(map[string]interface{}); ok {
					for k, v := range factMapInterface {
						factMap[k] = v
					}
				}
			}
			_ = patchKeyValues(factMap, fact)
			patchMap["Facts"] = factMap
		}
		// Add LiveStateType if provided
		if targetUpdateArgs.liveStateType == "-" {
			patchMap["LiveStateType"] = ""
		} else if targetUpdateArgs.liveStateType != "" {
			patchMap["LiveStateType"] = targetUpdateArgs.liveStateType
		}
		// Add WhereTrigger if provided
		if targetUpdateArgs.whereTrigger == "-" {
			patchMap["WhereTrigger"] = ""
		} else if targetUpdateArgs.whereTrigger != "" {
			patchMap["WhereTrigger"] = targetUpdateArgs.whereTrigger
		}
		// Add TriggerFilterID if provided
		if targetUpdateArgs.triggerFilter == "-" {
			patchMap["TriggerFilterID"] = nil
		} else if triggerFilterUUID != nil {
			patchMap["TriggerFilterID"] = triggerFilterUUID.String()
		}
	}

	patchJSON, err := BuildPatchDataWithPermissions(targetEnhancer, targetUpdateArgs.permissions)
	if err != nil {
		return err
	}

	if len(patchJSON) == 0 || string(patchJSON) == "null" {
		return errors.New("no updates specified for bulk patch")
	}

	params := &goclientnew.BulkPatchTargetsParams{}
	if effectiveWhere != "" {
		params.Where = &effectiveWhere
	}
	if filterID != "" {
		params.Filter = &filterID
	}
	if targetUpdateArgs.refreshTriggers {
		params.RefreshTriggers = &targetUpdateArgs.refreshTriggers
	}
	include := "SpaceID,BridgeWorkerID"
	params.Include = &include

	res, err := cubClientNew.BulkPatchTargetsWithBodyWithResponse(ctx, params, "application/merge-patch+json", bytes.NewReader(patchJSON))
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}

	// Handle 207 Multi-Status or 200 OK
	var responses []goclientnew.TargetCreateOrUpdateResponse
	if res.JSON200 != nil {
		responses = *res.JSON200
	} else if res.JSON207 != nil {
		responses = *res.JSON207
	} else {
		return errors.New("unexpected response from server")
	}

	// Display results
	successCount := 0
	failureCount := 0
	for _, resp := range responses {
		if resp.Error != nil {
			failureCount++
			if resp.Target != nil {
				fmt.Printf("Failed to update target %s: %s\n", resp.Target.Slug, resp.Error.Message)
			} else {
				fmt.Printf("Failed to update target: %s\n", resp.Error.Message)
			}
		} else if resp.Target != nil {
			successCount++
			if verbose {
				extendedDetails := &goclientnew.ExtendedTarget{Target: resp.Target}
				displayTargetDetails(extendedDetails)
			}
		}
	}

	fmt.Printf("\nBulk patch completed: %d succeeded, %d failed\n", successCount, failureCount)

	if failureCount > 0 {
		return errors.New("some targets failed to update")
	}

	return nil
}
