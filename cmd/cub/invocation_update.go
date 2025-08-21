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

var invocationUpdateCmd = &cobra.Command{
	Use:   "update [<slug or id>] [<toolchain type> <function> [<arg1> ...]]",
	Short: "Update an invocation or multiple invocations",
	Long: `Update an invocation or multiple invocations using bulk operations.

Single invocation update:
Function arguments can be provided as positional arguments or as named arguments using --argumentname=value syntax.
Once a named argument is used, all subsequent arguments must be named. Use "--" to separate command flags from function arguments when using named function arguments.

Example with named arguments:
  cub invocation update --space my-space my-invocation Kubernetes/YAML -- set-annotation --key=cloned --value=true

Bulk update with --patch:
Update multiple invocations at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
  # Update worker for all invocations of a certain type using JSON patch
  echo '{"BridgeWorkerID": "worker-uuid"}' | cub invocation update --patch --where "ToolchainType = 'Kubernetes/YAML'" --from-stdin

  # Update function for specific invocations
  echo '{"FunctionName": "no-placeholders"}' | cub invocation update --patch --where "FunctionName = 'cel-validate'" --from-stdin

  # Update specific invocations by slug
  cub invocation update --patch --invocation my-invocation,another-invocation --worker new-worker`,
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        invocationUpdateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	invocationPatch       bool
	invocationIdentifiers []string
)

func init() {
	addStandardUpdateFlags(invocationUpdateCmd)
	invocationUpdateCmd.Flags().StringVar(&workerSlug, "worker", "", "worker to execute the invocation function")
	invocationUpdateCmd.Flags().BoolVar(&invocationPatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(invocationUpdateCmd)
	invocationUpdateCmd.Flags().StringSliceVar(&invocationIdentifiers, "invocation", []string{}, "target specific invocations by slug or UUID for bulk patch (can be repeated or comma-separated)")
	invocationCmd.AddCommand(invocationUpdateCmd)
}

func checkInvocationConflictingArgs(args []string) bool {
	// Check for bulk patch mode (no positional args with --patch)
	isBulkPatchMode := invocationPatch && len(args) == 0

	if !isBulkPatchMode && (where != "" || len(invocationIdentifiers) > 0) {
		failOnError(fmt.Errorf("--where or --invocation can only be specified with --patch and no positional arguments"))
	}

	// Single create mode validation
	if !isBulkPatchMode && len(args) < 3 {
		failOnError(errors.New("single invocation update requires: <slug> <toolchain type> <function> [arguments...]"))
	}

	// Check for mutual exclusivity between --invocation and --where flags
	if len(invocationIdentifiers) > 0 && where != "" {
		failOnError(fmt.Errorf("--invocation and --where flags are mutually exclusive"))
	}

	if invocationPatch && flagReplace {
		failOnError(fmt.Errorf("only one of --patch and --replace should be specified"))
	}

	if isBulkPatchMode && (where == "" && len(invocationIdentifiers) == 0) {
		failOnError(fmt.Errorf("bulk patch mode requires --where or --invocation flags"))
	}

	if err := validateSpaceFlag(isBulkPatchMode); err != nil {
		failOnError(err)
	}

	if err := validateStdinFlags(); err != nil {
		failOnError(err)
	}

	return isBulkPatchMode
}

func runBulkInvocationUpdate() error {
	// Build WHERE clause from invocation identifiers or use provided where clause
	var effectiveWhere string
	if len(invocationIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromInvocations(invocationIdentifiers)
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

	// Add worker if specified
	if workerSlug != "" {
		worker, err := apiGetBridgeWorkerFromSlug(workerSlug, "*") // get all fields for now
		if err != nil {
			return err
		}
		patchData["BridgeWorkerID"] = worker.BridgeWorkerID.String()
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
	params := &goclientnew.BulkPatchInvocationsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}

	// Call the bulk patch API
	bulkRes, err := cubClientNew.BulkPatchInvocationsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkInvocationCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "update", effectiveWhere)
}

func invocationUpdateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkPatchMode := checkInvocationConflictingArgs(args)

	if isBulkPatchMode {
		return runBulkInvocationUpdate()
	}

	// Single invocation update logic
	if len(args) < 3 {
		return errors.New("single invocation update requires: <slug or id> <toolchain type> <function> [arguments...]")
	}

	currentInvocation, err := apiGetInvocationFromSlug(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)

	if invocationPatch {
		// Single invocation patch mode - we'll apply changes directly to the invocation object
		// Handle --from-stdin or --filename
		if flagPopulateModelFromStdin || flagFilename != "" {
			existingInvocation := currentInvocation
			if err := populateModelFromFlags(currentInvocation); err != nil {
				return err
			}
			// Ensure essential fields can't be clobbered
			currentInvocation.OrganizationID = existingInvocation.OrganizationID
			currentInvocation.SpaceID = existingInvocation.SpaceID
			currentInvocation.InvocationID = existingInvocation.InvocationID
		}

		// Add flags to patch
		if workerSlug != "" {
			worker, err := apiGetBridgeWorkerFromSlug(workerSlug, "*") // get all fields for now
			if err != nil {
				return err
			}
			currentInvocation.BridgeWorkerID = &worker.BridgeWorkerID
		}

		// Add labels if specified
		if len(label) > 0 {
			err := setLabels(&currentInvocation.Labels)
			if err != nil {
				return err
			}
		}

		// Add function details from args
		currentInvocation.ToolchainType = args[1]
		currentInvocation.FunctionName = args[2]
		if len(args) > 3 {
			invokeArgs := args[3:]
			newArgs := parseFunctionArguments(invokeArgs)
			currentInvocation.Arguments = newArgs
		}

		// Convert invocation to patch data
		patchData, err := json.Marshal(currentInvocation)
		if err != nil {
			return fmt.Errorf("failed to marshal patch data: %w", err)
		}

		invocationDetails, err := patchInvocation(spaceID, currentInvocation.InvocationID, patchData)
		if err != nil {
			return err
		}

		displayUpdateResults(invocationDetails, "invocation", args[0], invocationDetails.InvocationID.String(), displayInvocationDetails)
		return nil
	}

	// Traditional update mode
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingInvocation := currentInvocation
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			currentInvocation = new(goclientnew.Invocation)
			currentInvocation.Version = existingInvocation.Version
		}

		if err := populateModelFromFlags(currentInvocation); err != nil {
			return err
		}

		// Ensure essential fields can't be clobbered
		currentInvocation.OrganizationID = existingInvocation.OrganizationID
		currentInvocation.SpaceID = existingInvocation.SpaceID
		currentInvocation.InvocationID = existingInvocation.InvocationID
	}
	err = setLabels(&currentInvocation.Labels)
	if err != nil {
		return err
	}

	// If this was set from stdin, it will be overridden
	currentInvocation.SpaceID = spaceID
	if workerSlug != "" {
		worker, err := apiGetBridgeWorkerFromSlug(workerSlug, "*") // get all fields for now
		if err != nil {
			return err
		}
		currentInvocation.BridgeWorkerID = &worker.BridgeWorkerID
	}

	currentInvocation.ToolchainType = args[1]
	currentInvocation.FunctionName = args[2]
	invokeArgs := args[3:]
	newArgs := parseFunctionArguments(invokeArgs)
	currentInvocation.Arguments = newArgs
	invocationRes, err := cubClientNew.UpdateInvocationWithResponse(ctx, spaceID, currentInvocation.InvocationID, *currentInvocation)
	if IsAPIError(err, invocationRes) {
		return InterpretErrorGeneric(err, invocationRes)
	}

	invocationDetails := invocationRes.JSON200
	displayUpdateResults(invocationDetails, "invocation", args[0], invocationDetails.InvocationID.String(), displayInvocationDetails)
	return nil
}

func handleBulkInvocationCreateOrUpdateResponse(responses200 *[]goclientnew.InvocationCreateOrUpdateResponse, responses207 *[]goclientnew.InvocationCreateOrUpdateResponse, statusCode int, operationName, contextInfo string) error {
	var responses *[]goclientnew.InvocationCreateOrUpdateResponse
	if statusCode == 200 && responses200 != nil {
		responses = responses200
	} else if statusCode == 207 && responses207 != nil {
		responses = responses207
	} else {
		return fmt.Errorf("unexpected status code %d or no response data", statusCode)
	}

	if responses == nil {
		return fmt.Errorf("no response data received")
	}

	successCount := 0
	failureCount := 0
	var failures []string

	for _, resp := range *responses {
		if resp.Error == nil && resp.Invocation != nil {
			successCount++
			if verbose {
				fmt.Printf("Successfully %sd invocation: %s (ID: %s)\n", operationName, resp.Invocation.Slug, resp.Invocation.InvocationID)
			}
		} else {
			failureCount++
			errorMsg := "unknown error"
			if resp.Error != nil && resp.Error.Message != "" {
				errorMsg = resp.Error.Message
			}
			if resp.Invocation != nil {
				failures = append(failures, fmt.Sprintf("  - %s: %s", resp.Invocation.Slug, errorMsg))
			} else {
				failures = append(failures, fmt.Sprintf("  - (unknown invocation): %s", errorMsg))
			}
		}
	}

	// Display summary
	if !jsonOutput {
		fmt.Printf("\nBulk %s operation completed:\n", operationName)
		fmt.Printf("  Success: %d invocation(s)\n", successCount)
		if failureCount > 0 {
			fmt.Printf("  Failed: %d invocation(s)\n", failureCount)
			if verbose && len(failures) > 0 {
				fmt.Println("\nFailures:")
				for _, failure := range failures {
					fmt.Println(failure)
				}
			}
		}
		if contextInfo != "" {
			fmt.Printf("  Context: %s\n", contextInfo)
		}
	}

	// Return success only if all operations succeeded
	if statusCode == 207 || failureCount > 0 {
		return fmt.Errorf("bulk %s partially failed: %d succeeded, %d failed", operationName, successCount, failureCount)
	}

	return nil
}

func patchInvocation(spaceID uuid.UUID, invocationID uuid.UUID, patchData []byte) (*goclientnew.Invocation, error) {
	invocationRes, err := cubClientNew.PatchInvocationWithBodyWithResponse(
		ctx,
		spaceID,
		invocationID,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if IsAPIError(err, invocationRes) {
		return nil, InterpretErrorGeneric(err, invocationRes)
	}

	return invocationRes.JSON200, nil
}
