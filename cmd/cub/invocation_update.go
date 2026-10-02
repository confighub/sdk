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

var invocationUpdateCmd = &cobra.Command{
	Use:   "update [<slug or id>] [<toolchain type> [<function> [<arg1> ...]]]",
	Short: "Update an invocation or multiple invocations",
	Long: getCommandHelp(`Update an invocation or multiple invocations using bulk operations.

Single invocation update:

Function arguments can be provided as positional arguments or as named arguments using --argumentname=value syntax.
Once a named argument is used, all subsequent arguments must be named. Use "--" to separate command flags from function arguments when using named function arguments.

Example with named arguments:
`+"```"+`
  cub invocation update --space my-space my-invocation Kubernetes/YAML -- set-annotation --annotation-key=cloned --annotation-value=true
`+"```"+`

An Invocation can call several functions, which are executed in the order they are listed.
The positional form replaces the list with a single function; to set a list of several,
supply FunctionInvocations with --from-stdin or --filename and omit the function positional
arguments:
`+"```"+`
  echo '{"FunctionInvocations": [
           {"FunctionName": "set-default-names"},
           {"FunctionName": "set-annotation", "Arguments": [{"Value": "cloned"}, {"Value": "true"}]}
         ]}' | cub invocation update --space my-space --from-stdin my-invocation Kubernetes/YAML
`+"```"+`

Bulk update with --patch:

Update multiple invocations at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
`+"```"+`
  # Update worker for all invocations of a certain type using JSON patch
  echo '{"BridgeWorkerID": "worker-uuid"}' | cub invocation update --patch --where "ToolchainType = 'Kubernetes/YAML'" --from-stdin

  # Repoint every invocation still on a deprecated function to its replacement
  echo '{"FunctionInvocations": [{"FunctionName": "vet-cel"}]}' | cub invocation update --patch --where "FunctionInvocations.*.FunctionName = 'cel-validate'" --from-stdin

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
	addFieldEditFlags(invocationUpdateCmd, "Invocation")
	addInvocationFunctionFlag(invocationUpdateCmd)
	enableUpdatePermissionFlag(invocationUpdateCmd)
	invocationUpdateCmd.Flags().StringVar(&workerSlug, "worker", "", "worker to execute the invocation function")
	invocationUpdateCmd.Flags().StringArrayVar(&invocationDeclaredParameterFlags, "parameter", nil, "replace the declared parameters with the ones given, each as name[:datatype[:required]] (datatype defaults to string, required defaults to true; can be repeated). Reference declared parameters from templated argument values via {{ .Params.<name> }}.")
	invocationUpdateCmd.Flags().BoolVar(&invocationPatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(invocationUpdateCmd)
	enableFilterFlag(invocationUpdateCmd)
	invocationUpdateCmd.Flags().StringSliceVar(&invocationIdentifiers, "invocation", []string{}, "target specific invocations by slug or UUID for bulk patch (can be repeated or comma-separated)")
	addBackingUnitFlags(invocationUpdateCmd, "Invocation", false, false)
	addFromBackingUnitsFlags(invocationUpdateCmd, "Invocation", false)
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
		// Single update mode validation. The function positional argument is required unless the
		// body supplies the function list, which is how a multi-function Invocation is set.
		if len(args) < invocationUpdateMinArgs() {
			failOnError(errors.New("single invocation update requires: <slug> <toolchain type> <function> [arguments...], or <slug> <toolchain type> with the functions supplied by --function, --from-stdin or --filename, or left as they are by an update that sets something else"))
		}

		if filter != "" || where != "" || len(invocationIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --invocation can only be specified with --patch and no positional arguments"))
		}
	}

	if invocationPatch && flagReplace {
		failOnError(fmt.Errorf("only one of --patch and --replace should be specified"))
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
		workerID, err := resolveWorkerID(workerSlug)
		if err != nil {
			return err
		}
		workerUUID = &workerID
	}

	declaredParams, err := parseDeclaredParameterFlags(invocationDeclaredParameterFlags)
	if err != nil {
		return err
	}

	// Create enhancer function for invocation-specific fields
	enhancer := func(patchMap map[string]interface{}) {
		// Add worker if specified
		if workerUUID != nil {
			patchMap["BridgeWorkerID"] = workerUUID.String()
		}
		if len(declaredParams) > 0 {
			patchMap["Parameters"] = declaredParams
		}
	}

	// Build patch data using consolidated function
	patchJSON, err := BuildPatchDataWithPermissions(enhancer, permissionFlag)
	if err != nil {
		return err
	}

	// Build bulk patch parameters
	include := "SpaceID"
	params := &goclientnew.BulkPatchInvocationsParams{
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
	bulkRes, err := cubClientNew.BulkPatchInvocationsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkInvocationCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "update", effectiveWhere)
}

// invocationUpdateMinArgs is how many arguments a single update needs. The function is required
// unless something else supplies the functions, or the update changes something else and leaves
// them alone.
func invocationUpdateMinArgs() int {
	if flagPopulateModelFromStdin || flagFilename != "" || len(invocationFunctionLines) > 0 ||
		len(invocationDeclaredParameterFlags) > 0 || workerSlug != "" || hasMetadataFlags() ||
		len(label) > 0 || len(deleteGate) > 0 || len(permissionFlag) > 0 {
		return 2
	}
	return 3
}

func invocationUpdateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkPatchMode := checkInvocationConflictingArgs(args)

	if err := queueInvocationFunctionEdits(args); err != nil {
		return err
	}

	if isBulkPatchMode {
		return runBulkInvocationUpdate()
	}

	// Single invocation update logic
	if len(args) < invocationUpdateMinArgs() {
		return errors.New("single invocation update requires: <slug or id> <toolchain type> <function> [arguments...]")
	}

	currentInvocationEnvelope, err := resolveInvocation(args[0], selectedSpaceID, "*") // get all fields for RMW
	if err != nil {
		return err
	}

	currentInvocation := currentInvocationEnvelope.Invocation

	spaceID := currentInvocation.SpaceID

	if invocationPatch {
		// Single invocation patch mode

		// Handle error-prone operations before enhancer
		var workerID *goclientnew.UUID
		if workerSlug != "" {
			workerUUID, err := resolveWorkerID(workerSlug)
			if err != nil {
				return err
			}
			workerUUIDConverted := goclientnew.UUID(workerUUID)
			workerID = &workerUUIDConverted
		}

		declaredParams, err := parseDeclaredParameterFlags(invocationDeclaredParameterFlags)
		if err != nil {
			return err
		}

		// Build patch data using BuildPatchData with invocation enhancer
		invocationEnhancer := func(patchData map[string]interface{}) {
			// Add invocation-specific fields
			if workerID != nil {
				patchData["BridgeWorkerID"] = *workerID
			}
			if len(declaredParams) > 0 {
				patchData["Parameters"] = declaredParams
			}

			// Add function details from args
			patchData["ToolchainType"] = args[1]
			if len(args) > 2 {
				// A named function replaces the whole list: the positional form is the
				// one-function form.
				patchData["FunctionInvocations"] = goclientnew.FunctionInvocationList{{
					FunctionName: args[2],
					Arguments:    parseFunctionArguments(args[3:]),
				}}
			}
		}

		patchData, err := BuildPatchDataWithPermissions(invocationEnhancer, permissionFlag)
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
	setDisplayNameAndHiddenReason(&currentInvocation.DisplayName, &currentInvocation.HiddenReason)
	if err := setDeleteGates(&currentInvocation.DeleteGates); err != nil {
		return err
	}
	err = setAnnotations(&currentInvocation.Annotations)
	if err != nil {
		return err
	}
	err = setLabels(&currentInvocation.Labels)
	if err != nil {
		return err
	}
	if err := setPermissions(&currentInvocation.Permissions); err != nil {
		return err
	}

	// If this was set from stdin, it will be overridden
	currentInvocation.SpaceID = spaceID
	if workerSlug != "" {
		workerUUID, err := resolveWorkerID(workerSlug)
		if err != nil {
			return err
		}
		workerID := goclientnew.UUID(workerUUID)
		currentInvocation.BridgeWorkerID = &workerID
	}

	currentInvocation.ToolchainType = args[1]
	if len(args) > 2 {
		// A named function replaces the whole list: the positional form is the one-function form.
		currentInvocation.FunctionInvocations = &goclientnew.FunctionInvocationList{{
			FunctionName: args[2],
			Arguments:    parseFunctionArguments(args[3:]),
		}}
	}
	declaredParams, err := parseDeclaredParameterFlags(invocationDeclaredParameterFlags)
	if err != nil {
		return err
	}
	if len(declaredParams) > 0 {
		// --parameter declares the whole parameter namespace. Without it, the invocation
		// keeps the parameters it has, or the ones a file or stdin supplied.
		currentInvocation.Parameters = declaredParams
	}
	if err := applyFieldEdits("Invocation", currentInvocation); err != nil {
		return err
	}
	invocationRes, err := cubClientNew.UpdateInvocationWithResponse(ctx, spaceID, currentInvocation.InvocationID, &goclientnew.UpdateInvocationParams{DryRun: dryRunParam()}, *currentInvocation)
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
		&goclientnew.PatchInvocationParams{DryRun: dryRunParam()},
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if cubapi.IsAPIError(err, invocationRes) {
		return nil, cubapi.InterpretErrorGeneric(err, invocationRes)
	}

	return invocationRes.JSON200, nil
}
