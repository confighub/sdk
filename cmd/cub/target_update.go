// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"

	"github.com/cockroachdb/errors"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var targetUpdateCmd = &cobra.Command{
	Use:   "update [<slug or id>]",
	Short: "Update a target or multiple targets",
	Long: `Update a target or multiple targets using bulk operations.

Single target update:
  cub target update --space my-space my-target

Bulk update with --patch:
Update multiple targets at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
  # Update all targets with a specific toolchain type using JSON patch
  echo '{"Parameters": "{}"}' | cub target update --patch --where "ToolchainType = 'Kubernetes/YAML'" --from-stdin

  # Update specific targets by slug
  cub target update --patch --target my-target,another-target --from-stdin < patch.json`,
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
	targetUpdateCmd.Flags().StringSliceVar(&targetIdentifiers, "target", []string{}, "target specific targets by slug or UUID for bulk patch (can be repeated or comma-separated)")
	targetCmd.AddCommand(targetUpdateCmd)
}

func targetUpdateCmdRun(cmd *cobra.Command, args []string) error {
	if err := validateStdinFlags(); err != nil {
		return err
	}

	// Validate label removal only works with patch
	if err := ValidateLabelRemoval(label, targetPatch); err != nil {
		return err
	}

	// Check for bulk patch mode (no positional args with --patch)
	isBulkPatchMode := targetPatch && len(args) == 0

	if isBulkPatchMode {
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
	if where != "" || len(targetIdentifiers) > 0 {
		return fmt.Errorf("--where or --target can only be specified with --patch")
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

	err = validateToolchainAndProvider(currentTarget.Target.ToolchainType, currentTarget.Target.ProviderType)
	if err != nil {
		return err
	}

	err = setLabels(&currentTarget.Target.Labels)
	if err != nil {
		return err
	}
	// If this was set from stdin, it will be overridden
	currentTarget.Target.SpaceID = spaceID

	targetRes, err := cubClientNew.UpdateTargetWithResponse(ctx, spaceID, currentTarget.Target.TargetID, *currentTarget.Target)
	if IsAPIError(err, targetRes) {
		return InterpretErrorGeneric(err, targetRes)
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

	// Build patch data using consolidated function (no entity-specific fields for target)
	patchJSON, err := BuildPatchData(nil)
	if err != nil {
		return err
	}

	if len(patchJSON) == 0 || string(patchJSON) == "null" {
		return errors.New("no updates specified")
	}

	targetRes, err := cubClientNew.PatchTargetWithBodyWithResponse(ctx, spaceID, currentTarget.Target.TargetID, "application/merge-patch+json", bytes.NewReader(patchJSON))
	if IsAPIError(err, targetRes) {
		return InterpretErrorGeneric(err, targetRes)
	}

	targetDetails := targetRes.JSON200
	extendedDetails := &goclientnew.ExtendedTarget{Target: targetDetails}
	displayUpdateResults(extendedDetails, "target", args[0], targetDetails.TargetID.String(), displayTargetDetails)
	return nil
}

func targetBulkPatchCmdRun(cmd *cobra.Command, args []string) error {
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

	// Build patch data using consolidated function (no entity-specific fields for target)
	patchJSON, err := BuildPatchData(nil)
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
	include := "SpaceID,BridgeWorkerID"
	params.Include = &include

	res, err := cubClientNew.BulkPatchTargetsWithBodyWithResponse(ctx, params, "application/merge-patch+json", bytes.NewReader(patchJSON))
	if IsAPIError(err, res) {
		return InterpretErrorGeneric(err, res)
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
