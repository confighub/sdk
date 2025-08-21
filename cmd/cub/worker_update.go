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

var bridgeworkerUpdateCmd = &cobra.Command{
	Use:   "update [<name>]",
	Short: "Update a bridge worker or multiple bridge workers",
	Long: `Update a bridge worker or multiple bridge workers using bulk operations.

Single bridge worker update:
  cub worker update --space my-space my-worker

Bulk update with --patch:
Update multiple bridge workers at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
  # Update all bridge workers with specific labels using JSON patch
  echo '{"Labels": {"env": "prod"}}' | cub worker update --patch --where "Labels.tier = 'backend'" --from-stdin

  # Update specific bridge workers by slug
  cub worker update --patch --worker my-worker,another-worker --from-stdin < patch.json`,
	Args:        cobra.MaximumNArgs(1), // Allow 0 args for bulk mode
	RunE:        bridgeworkerUpdateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	workerPatch       bool
	workerIdentifiers []string
)

func init() {
	addStandardUpdateFlags(bridgeworkerUpdateCmd)
	bridgeworkerUpdateCmd.Flags().BoolVar(&workerPatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(bridgeworkerUpdateCmd)
	bridgeworkerUpdateCmd.Flags().StringSliceVar(&workerIdentifiers, "worker", []string{}, "target specific bridge workers by slug or UUID for bulk patch (can be repeated or comma-separated)")
	workerCmd.AddCommand(bridgeworkerUpdateCmd)
}

func bridgeworkerUpdateCmdRun(cmd *cobra.Command, args []string) error {
	if err := validateStdinFlags(); err != nil {
		return err
	}

	// Validate label removal only works with patch
	if err := ValidateLabelRemoval(label, workerPatch); err != nil {
		return err
	}

	// Check for bulk patch mode (no positional args with --patch)
	isBulkPatchMode := workerPatch && len(args) == 0

	if isBulkPatchMode {
		return workerBulkPatchCmdRun(cmd, args)
	}

	// Check for individual patch mode (single worker with --patch)
	if workerPatch && len(args) == 1 {
		return workerIndividualPatchCmdRun(cmd, args)
	}

	// Regular update mode validation
	if len(args) != 1 {
		return errors.New("single bridge worker update requires: <name>")
	}

	// Check that bulk-only flags are not used in single mode
	if where != "" || len(workerIdentifiers) > 0 {
		return fmt.Errorf("--where or --worker can only be specified with --patch")
	}

	currentBridgeworker, err := apiGetBridgeWorkerFromSlug(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}
	spaceID := uuid.MustParse(selectedSpaceID)
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingBridgeworker := currentBridgeworker
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			currentBridgeworker = new(goclientnew.BridgeWorker)
			currentBridgeworker.Version = existingBridgeworker.Version
		}

		if err := populateModelFromFlags(currentBridgeworker); err != nil {
			return err
		}

		// Ensure essential fields can't be clobbered
		currentBridgeworker.OrganizationID = existingBridgeworker.OrganizationID
		currentBridgeworker.SpaceID = existingBridgeworker.SpaceID
		currentBridgeworker.BridgeWorkerID = existingBridgeworker.BridgeWorkerID
	}
	err = setLabels(&currentBridgeworker.Labels)
	if err != nil {
		return err
	}
	// If this was set from stdin, it will be overridden
	currentBridgeworker.SpaceID = spaceID

	bridgeWorkerRes, err := cubClientNew.UpdateBridgeWorkerWithResponse(ctx, spaceID, currentBridgeworker.BridgeWorkerID, *currentBridgeworker)
	if IsAPIError(err, bridgeWorkerRes) {
		return InterpretErrorGeneric(err, bridgeWorkerRes)
	}

	bridgeworkerDetails := bridgeWorkerRes.JSON200
	displayUpdateResults(bridgeworkerDetails, "bridgeworker", args[0], bridgeworkerDetails.BridgeWorkerID.String(), displayWorkerDetails)
	return nil
}

func workerIndividualPatchCmdRun(cmd *cobra.Command, args []string) error {
	currentWorker, err := apiGetBridgeWorkerFromSlug(args[0], "*")
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)

	// Build patch data using consolidated function (no entity-specific fields for worker)
	patchJSON, err := BuildPatchData(nil)
	if err != nil {
		return err
	}

	if len(patchJSON) == 0 || string(patchJSON) == "null" {
		return errors.New("no updates specified")
	}

	workerRes, err := cubClientNew.PatchBridgeWorkerWithBodyWithResponse(ctx, spaceID, currentWorker.BridgeWorkerID, "application/merge-patch+json", bytes.NewReader(patchJSON))
	if IsAPIError(err, workerRes) {
		return InterpretErrorGeneric(err, workerRes)
	}

	workerDetails := workerRes.JSON200
	displayUpdateResults(workerDetails, "bridge worker", args[0], workerDetails.BridgeWorkerID.String(), displayWorkerDetails)
	return nil
}

func workerBulkPatchCmdRun(cmd *cobra.Command, args []string) error {
	// Build WHERE clause from worker identifiers or use provided where clause
	var effectiveWhere string
	if len(workerIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromWorkers(workerIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Build patch data using consolidated function (no entity-specific fields for worker)
	patchJSON, err := BuildPatchData(nil)
	if err != nil {
		return err
	}

	if len(patchJSON) == 0 || string(patchJSON) == "null" {
		return errors.New("no updates specified for bulk patch")
	}

	params := &goclientnew.BulkPatchBridgeWorkersParams{}
	if effectiveWhere != "" {
		params.Where = &effectiveWhere
	}
	include := "SpaceID"
	params.Include = &include

	res, err := cubClientNew.BulkPatchBridgeWorkersWithBodyWithResponse(ctx, params, "application/merge-patch+json", bytes.NewReader(patchJSON))
	if IsAPIError(err, res) {
		return InterpretErrorGeneric(err, res)
	}

	// Handle 207 Multi-Status or 200 OK
	var responses []goclientnew.BridgeWorkerCreateOrUpdateResponse
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
			if resp.BridgeWorker != nil {
				fmt.Printf("Failed to update bridge worker %s: %s\n", resp.BridgeWorker.Slug, resp.Error.Message)
			} else {
				fmt.Printf("Failed to update bridge worker: %s\n", resp.Error.Message)
			}
		} else if resp.BridgeWorker != nil {
			successCount++
			if verbose {
				displayWorkerDetails(resp.BridgeWorker)
			}
		}
	}

	fmt.Printf("\nBulk patch completed: %d succeeded, %d failed\n", successCount, failureCount)

	if failureCount > 0 {
		return errors.New("some bridge workers failed to update")
	}

	return nil
}
