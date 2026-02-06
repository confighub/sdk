// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var invocationUpdateCmd = &cobra.Command{
	Use:   "update [<slug or id>] [<toolchain type> <function> [<arg1> ...]]",
	Short: "Update an invocation or multiple invocations",
	Long: getCommandHelp(`Update an invocation or multiple invocations using bulk operations.

Single invocation update:

Function arguments can be provided as positional arguments or as named arguments using --argumentname=value syntax.
Once a named argument is used, all subsequent arguments must be named. Use "--" to separate command flags from function arguments when using named function arguments.

Example with named arguments:
`+"```"+`
  cub invocation update --space my-space my-invocation Kubernetes/YAML -- set-annotation --key=cloned --value=true
`+"```"+`

Bulk update with --patch:

Update multiple invocations at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
`+"```"+`
  # Update worker for all invocations of a certain type using JSON patch
  echo '{"BridgeWorkerID": "worker-uuid"}' | cub invocation update --patch --where "ToolchainType = 'Kubernetes/YAML'" --from-stdin

  # Update function for specific invocations
  echo '{"FunctionName": "no-placeholders"}' | cub invocation update --patch --where "FunctionName = 'cel-validate'" --from-stdin

  # Update specific invocations by slug
  cub invocation update --patch --invocation my-invocation,another-invocation --worker new-worker
`+"```"+`
`, ""),
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
	enableFilterFlag(invocationUpdateCmd)
	invocationUpdateCmd.Flags().StringSliceVar(&invocationIdentifiers, "invocation", []string{}, "target specific invocations by slug or UUID for bulk patch (can be repeated or comma-separated)")
	invocationCmd.AddCommand(invocationUpdateCmd)
}

func checkInvocationConflictingArgs(args []string) bool {
	// Check for bulk patch mode: no positional args
	isBulkPatchMode := len(args) == 0

	if isBulkPatchMode {
		if !invocationPatch {
			failOnError(errors.New("--patch is required in bulk mode"))
		}

		// Check for mutual exclusivity between --invocation and --where flags
		if len(invocationIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--invocation and --where flags are mutually exclusive"))
		}

	} else {
		// Single create mode validation
		if len(args) < 3 {
			failOnError(errors.New("single invocation update requires: <slug> <toolchain type> <function> [arguments...]"))
		}

		if filter != "" || where != "" || len(invocationIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --invocation can only be specified with --patch and no positional arguments"))
		}
	}

	if invocationPatch && flagReplace {
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

func runBulkInvocationUpdate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

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

	// Validate and resolve worker early if specified
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

	// Create enhancer function for invocation-specific fields
	enhancer := func(patchMap map[string]interface{}) {
		// Add worker if specified
		if workerUUID != nil {
			patchMap["BridgeWorkerID"] = workerUUID.String()
		}
	}

	// Build patch data using consolidated function
	patchJSON, err := BuildPatchData(enhancer)
	if err != nil {
		return err
	}

	// Build bulk patch parameters
	include := "SpaceID"
	params := &goclientnew.BulkPatchInvocationsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
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
		// Single invocation patch mode

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

		// Parse function arguments if needed
		var newArgs []goclientnew.FunctionArgument
		if len(args) > 3 {
			invokeArgs := args[3:]
			newArgs = parseFunctionArguments(invokeArgs)
		}

		// Build patch data using BuildPatchData with invocation enhancer
		invocationEnhancer := func(patchData map[string]interface{}) {
			// Add invocation-specific fields
			if workerID != nil {
				patchData["BridgeWorkerID"] = *workerID
			}

			// Add function details from args
			patchData["ToolchainType"] = args[1]
			patchData["FunctionName"] = args[2]
			if newArgs != nil {
				patchData["Arguments"] = newArgs
			}
		}

		patchData, err := BuildPatchData(invocationEnhancer)
		if err != nil {
			return fmt.Errorf("failed to build patch data: %w", err)
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
	err = setAnnotations(&currentInvocation.Annotations)
	if err != nil {
		return err
	}
	err = setLabels(&currentInvocation.Labels)
	if err != nil {
		return err
	}

	// If this was set from stdin, it will be overridden
	currentInvocation.SpaceID = spaceID
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
		currentInvocation.BridgeWorkerID = &workerID
	}

	currentInvocation.ToolchainType = args[1]
	currentInvocation.FunctionName = args[2]
	invokeArgs := args[3:]
	newArgs := parseFunctionArguments(invokeArgs)
	currentInvocation.Arguments = newArgs
	invocationRes, err := cubClientNew.UpdateInvocationWithResponse(ctx, spaceID, currentInvocation.InvocationID, *currentInvocation)
	if cubapi.IsAPIError(err, invocationRes) {
		return cubapi.InterpretErrorGeneric(err, invocationRes)
	}

	invocationDetails := invocationRes.JSON200
	displayUpdateResults(invocationDetails, "invocation", args[0], invocationDetails.InvocationID.String(), displayInvocationDetails)
	return nil
}

func handleBulkInvocationCreateOrUpdateResponse(responses200 *[]goclientnew.InvocationCreateOrUpdateResponse, responses207 *[]goclientnew.InvocationCreateOrUpdateResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkGenericCreateOrUpdateResults(
		responses200, responses207, statusCode, "invocation", operationName, contextInfo,
		func(r *goclientnew.InvocationCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.InvocationCreateOrUpdateResponse) string {
			if r.Invocation != nil {
				return fmt.Sprintf("%s (ID: %s)", r.Invocation.Slug, r.Invocation.InvocationID)
			}
			return ""
		},
	)
}

func patchInvocation(spaceID uuid.UUID, invocationID uuid.UUID, patchData []byte) (*goclientnew.Invocation, error) {
	invocationRes, err := cubClientNew.PatchInvocationWithBodyWithResponse(
		ctx,
		spaceID,
		invocationID,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if cubapi.IsAPIError(err, invocationRes) {
		return nil, cubapi.InterpretErrorGeneric(err, invocationRes)
	}

	return invocationRes.JSON200, nil
}
