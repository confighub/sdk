// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"strings"

	"github.com/cockroachdb/errors"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var invocationCreateCmd = &cobra.Command{
	Use:         "create [<slug> <toolchain type> <function> [<arg1> ...]]",
	Short:       "Create a new invocation or bulk create invocations",
	Long:        getInvocationCreateHelp(),
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        invocationCreateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func getInvocationCreateHelp() string {
	baseHelp := `Create a new invocation or bulk create multiple invocations by cloning existing ones.

SINGLE INVOCATION CREATION:
Create a new invocation to define a function invocation.

Toolchain Types:
  - Kubernetes/YAML: For Kubernetes YAML configurations

Example Functions:
  - cel-validate: Validate resources using CEL expressions
  - is-approved: Check if resource is approved
  - no-placeholders: Ensure no placeholders exist
  - set-default-names: Set default names for resources
  - set-annotation: Set annotations on resources
  - ensure-context: Ensure context annotations are present

Function arguments can be provided as positional arguments or as named arguments using --argumentname=value syntax.
Once a named argument is used, all subsequent arguments must be named. Use "--" to separate command flags from function arguments when using named function arguments.

BULK INVOCATION CREATION:
When no positional arguments are provided, bulk create mode is activated. This mode clones existing
invocations based on filters and creates multiple new invocations with optional modifications.

Single Invocation Examples:
  # Create an invocation to validate replicas > 1 for Deployments
  cub invocation create --space my-space --json replicated Kubernetes/YAML cel-validate 'r.kind != "Deployment" || r.spec.replicas > 1'

  # Create an invocation to enforce low resource usage (replicas < 10)
  cub invocation create --space my-space --json lowcost Kubernetes/YAML cel-validate 'r.kind != "Deployment" || r.spec.replicas < 10'

  # Create an invocation to ensure no placeholders exist in resources
  cub invocation create --space my-space --json complete Kubernetes/YAML no-placeholders

  # Create an invocation requiring approval before applying changes
  cub invocation create --space my-space --json require-approval Kubernetes/YAML is-approved 1

  # Create an invocation to add a "cloned=true" annotation
  cub invocation create --space my-space --json stamp Kubernetes/YAML set-annotation cloned true

  # Using named arguments for clarity (note the "--" separator)
  cub invocation create --space my-space --json stamp Kubernetes/YAML -- set-annotation --key=cloned --value=true

Bulk Create Examples:
  # Clone all invocations matching a pattern with name prefixes
  cub invocation create --where "FunctionName = 'cel-validate'" --name-prefix dev-,staging- --dest-space dev-space

  # Clone specific invocations to multiple spaces
  cub invocation create --invocation my-invocation --dest-space dev-space,staging-space

  # Clone invocations using a where expression for destination spaces
  cub invocation create --where "ToolchainType = 'Kubernetes/YAML'" --where-space "Labels.Environment IN ('dev', 'staging')"

  # Clone invocations with modifications via JSON patch
  echo '{"FunctionName": "no-placeholders"}' | cub invocation create --where "FunctionName = 'cel-validate'" --name-prefix v2- --from-stdin`

	return baseHelp
}

var invocationCreateArgs struct {
	destSpaces      []string
	whereSpace      string
	namePrefixes    []string
	invocationSlugs []string
}

func init() {
	addStandardCreateFlags(invocationCreateCmd)
	invocationCreateCmd.Flags().StringVar(&workerSlug, "worker", "", "worker to execute the invocation function")
	enableWhereFlag(invocationCreateCmd)

	// Bulk create specific flags
	invocationCreateCmd.Flags().StringSliceVar(&invocationCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	invocationCreateCmd.Flags().StringVar(&invocationCreateArgs.whereSpace, "where-space", "", "where expression to select destination spaces for bulk create")
	invocationCreateCmd.Flags().StringSliceVar(&invocationCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	invocationCreateCmd.Flags().StringSliceVar(&invocationCreateArgs.invocationSlugs, "invocation", []string{}, "target specific invocations by slug or UUID for bulk create (can be repeated or comma-separated)")

	invocationCmd.AddCommand(invocationCreateCmd)
}

func checkInvocationCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args and has bulk-specific flags
	isBulkCreateMode := len(args) == 0 && (where != "" || len(invocationCreateArgs.invocationSlugs) > 0 || len(invocationCreateArgs.destSpaces) > 0 || invocationCreateArgs.whereSpace != "" || len(invocationCreateArgs.namePrefixes) > 0)

	if isBulkCreateMode {
		// Validate bulk create requirements
		if where == "" && len(invocationCreateArgs.invocationSlugs) == 0 {
			return false, errors.New("bulk create mode requires --where or --invocation flags")
		}

		if len(invocationCreateArgs.invocationSlugs) > 0 && where != "" {
			return false, errors.New("--invocation and --where flags are mutually exclusive")
		}

		if len(invocationCreateArgs.destSpaces) > 0 && invocationCreateArgs.whereSpace != "" {
			return false, errors.New("--dest-space and --where-space flags are mutually exclusive")
		}

		if len(invocationCreateArgs.destSpaces) == 0 && invocationCreateArgs.whereSpace == "" && len(invocationCreateArgs.namePrefixes) == 0 {
			return false, errors.New("bulk create mode requires at least one of --dest-space, --where-space, or --name-prefix")
		}
	} else {
		// Single create mode validation
		if len(args) < 3 {
			return false, errors.New("single invocation creation requires: <slug> <toolchain type> <function> [arguments...]")
		}

		if where != "" || len(invocationCreateArgs.invocationSlugs) > 0 || len(invocationCreateArgs.destSpaces) > 0 || invocationCreateArgs.whereSpace != "" || len(invocationCreateArgs.namePrefixes) > 0 {
			return false, errors.New("bulk create flags (--where, --invocation, --dest-space, --where-space, --name-prefix) can only be used without positional arguments")
		}
	}

	if err := validateSpaceFlag(isBulkCreateMode); err != nil {
		failOnError(err)
	}

	if err := validateStdinFlags(); err != nil {
		return isBulkCreateMode, err
	}

	return isBulkCreateMode, nil
}

func invocationCreateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkCreateMode, err := checkInvocationCreateConflictingArgs(args)
	if err != nil {
		return err
	}

	if isBulkCreateMode {
		return runBulkInvocationCreate()
	}

	return runSingleInvocationCreate(args)
}

func runSingleInvocationCreate(args []string) error {
	spaceID := uuid.MustParse(selectedSpaceID)
	newBody := goclientnew.Invocation{}
	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(&newBody); err != nil {
			return err
		}
	}
	err := setLabels(&newBody.Labels)
	if err != nil {
		return err
	}
	newBody.SpaceID = spaceID
	newBody.Slug = makeSlug(args[0])
	if newBody.DisplayName == "" {
		newBody.DisplayName = args[0]
	}
	if workerSlug != "" {
		worker, err := apiGetBridgeWorkerFromSlug(workerSlug, "*") // get all fields for now
		if err != nil {
			return err
		}
		newBody.BridgeWorkerID = &worker.BridgeWorkerID
	}

	newBody.ToolchainType = args[1]
	newBody.FunctionName = args[2]
	invokeArgs := args[3:]
	newArgs := parseFunctionArguments(invokeArgs)
	newBody.Arguments = newArgs
	invocationRes, err := cubClientNew.CreateInvocationWithResponse(ctx, spaceID, newBody)
	if IsAPIError(err, invocationRes) {
		return InterpretErrorGeneric(err, invocationRes)
	}

	invocationDetails := invocationRes.JSON200
	displayCreateResults(invocationDetails, "invocation", args[0], invocationDetails.InvocationID.String(), displayInvocationDetails)
	return nil
}

func runBulkInvocationCreate() error {
	// Build WHERE clause from invocation identifiers or use provided where clause
	var effectiveWhere string
	if len(invocationCreateArgs.invocationSlugs) > 0 {
		whereClause, err := buildWhereClauseFromInvocations(invocationCreateArgs.invocationSlugs)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Build patch data using consolidated function (no entity-specific fields for invocation)
	patchJSON, err := BuildPatchData(nil)
	if err != nil {
		return err
	}

	// Build bulk create parameters
	include := "SpaceID"
	params := &goclientnew.BulkCreateInvocationsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}

	// Add name prefixes if specified
	if len(invocationCreateArgs.namePrefixes) > 0 {
		namePrefixesStr := strings.Join(invocationCreateArgs.namePrefixes, ",")
		params.NamePrefixes = &namePrefixesStr
	}

	// Set where_space parameter - either from direct where-space flag or converted from dest-space
	var whereSpaceExpr string
	if invocationCreateArgs.whereSpace != "" {
		whereSpaceExpr = invocationCreateArgs.whereSpace
	} else if len(invocationCreateArgs.destSpaces) > 0 {
		// Convert dest-space identifiers to a where expression
		whereSpaceExpr, err = buildWhereClauseForSpaces(invocationCreateArgs.destSpaces)
		if err != nil {
			return errors.Wrapf(err, "error converting destination spaces to where expression")
		}
	}

	if whereSpaceExpr != "" {
		params.WhereSpace = &whereSpaceExpr
	}

	// Call the bulk create API
	bulkRes, err := cubClientNew.BulkCreateInvocationsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkInvocationCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "create", effectiveWhere)
}
