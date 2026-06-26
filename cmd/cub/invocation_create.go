// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
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
- ConfigHub/YAML: For ConfigHub YAML configurations
- AppConfig/Properties: For application Java Properties configurations
- AppConfig/YAML: For application YAML configurations
- AppConfig/TOML: For application TOML configurations
- AppConfig/INI: For application INI configurations
- AppConfig/Env: For application Env configurations
- AppConfig/YAML: For application YAML configurations
- AppConfig/JSON: For application JSON configurations

Example Functions:

  - vet-celexpr: Validate resources using CEL expressions
  - vet-approvedby: Check if resource is approved
  - vet-placeholders: Ensure no placeholders exist
  - set-default-names: Set default names for resources
  - set-annotation: Set annotations on resources
  - ensure-context: Ensure context annotations are present

Function arguments can be provided as positional arguments or as named arguments using --argumentname=value syntax.
Once a named argument is used, all subsequent arguments must be named. Use "--" to separate command flags from function arguments when using named function arguments.

BULK INVOCATION CREATION:

When no positional arguments are provided, bulk create mode is activated. This mode clones existing
invocations based on filters and creates multiple new invocations with optional modifications.

Single Invocation Examples:
` + "```" + `
  # Create an invocation to validate replicas > 1 for Deployments
  cub invocation create --space my-space -o json replicated Kubernetes/YAML vet-celexpr 'r.kind != "Deployment" || r.spec.replicas > 1'

  # Create an invocation to enforce low resource usage (replicas < 10)
  cub invocation create --space my-space -o json lowcost Kubernetes/YAML vet-celexpr 'r.kind != "Deployment" || r.spec.replicas < 10'

  # Create an invocation to ensure no placeholders exist in resources
  cub invocation create --space my-space -o json complete Kubernetes/YAML vet-placeholders

  # Create an invocation requiring approval before applying changes
  cub invocation create --space my-space -o json require-approval Kubernetes/YAML vet-approvedby 1

  # Create an invocation to add a "cloned=true" annotation
  cub invocation create --space my-space -o json stamp Kubernetes/YAML set-annotation cloned true

  # Using named arguments for clarity (note the "--" separator)
  cub invocation create --space my-space -o json stamp Kubernetes/YAML -- set-annotation --key=cloned --value=true
` + "```" + `

Bulk Create Examples:
` + "```" + `
  # Clone all invocations matching a pattern with name prefixes
  cub invocation create --where "FunctionName = 'vet-celexpr'" --name-prefix dev-,staging- --dest-space dev-space

  # Clone specific invocations to multiple spaces
  cub invocation create --invocation my-invocation --dest-space dev-space,staging-space

  # Clone invocations using a where expression for destination spaces
  cub invocation create --where "ToolchainType = 'Kubernetes/YAML'" --where-space "Labels.Environment IN ('dev', 'staging')"

  # Clone invocations with modifications via JSON patch
  echo '{"FunctionName": "vet-placeholders"}' | cub invocation create --where "FunctionName = 'vet-celexpr'" --name-prefix v2- --from-stdin
` + "```" + `
`

	return getCommandHelp(baseHelp, "")
}

var invocationCreateArgs struct {
	destSpaces      []string
	whereSpace      string
	namePrefixes    []string
	invocationSlugs []string
	filterSpace     string
	variantLabels   []string
	namePattern     string
}

var invocationDeclaredParameterFlags []string

func init() {
	addStandardCreateFlags(invocationCreateCmd)
	invocationCreateCmd.Flags().StringVar(&workerSlug, "worker", "", "worker to execute the invocation function")
	invocationCreateCmd.Flags().StringArrayVar(&invocationDeclaredParameterFlags, "parameter", nil, "declare a parameter as name[:datatype[:required]] (datatype defaults to string, required defaults to true; can be repeated). Reference declared parameters from templated argument values via {{ .Params.<name> }}.")
	enableWhereFlag(invocationCreateCmd)
	enableFilterFlag(invocationCreateCmd)

	// Bulk create specific flags
	invocationCreateCmd.Flags().StringSliceVar(&invocationCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	invocationCreateCmd.Flags().StringVar(&invocationCreateArgs.whereSpace, "where-space", "", "where expression to select destination spaces for bulk create")
	invocationCreateCmd.Flags().StringSliceVar(&invocationCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	invocationCreateCmd.Flags().StringSliceVar(&invocationCreateArgs.variantLabels, "variant-labels", []string{}, "labels for bulk create in the format of key1=value1|value2,key2=value1|value2|value3")
	invocationCreateCmd.Flags().StringVar(&invocationCreateArgs.namePattern, "name-pattern", "", "a pattern string for name generation of clones, prefix 'template:' to use a Go template with .SourceEntity to access the original Invocation and .Labels to access variant labels, example: 'template:{{.SourceEntitySlug}}-{{.Labels.env}}'")
	invocationCreateCmd.Flags().StringSliceVar(&invocationCreateArgs.invocationSlugs, "invocation", []string{}, "target specific invocations by slug or UUID for bulk create (can be repeated or comma-separated)")
	invocationCreateCmd.Flags().StringVar(&invocationCreateArgs.filterSpace, "filter-space", "", "filter entity containing WHERE expression to select destination spaces for bulk create (slug or UUID)")

	invocationCmd.AddCommand(invocationCreateCmd)
}

func checkInvocationCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args
	isBulkCreateMode := len(args) == 0

	if isBulkCreateMode {
		// Validate bulk create requirements
		if len(invocationCreateArgs.invocationSlugs) > 0 && where != "" {
			return false, errors.New("--invocation and --where flags are mutually exclusive")
		}

		if len(invocationCreateArgs.destSpaces) > 0 && invocationCreateArgs.whereSpace != "" {
			return false, errors.New("--dest-space and --where-space flags are mutually exclusive")
		}

		if len(invocationCreateArgs.destSpaces) == 0 && invocationCreateArgs.whereSpace == "" && len(invocationCreateArgs.namePrefixes) == 0 && len(invocationCreateArgs.variantLabels) == 0 {
			return false, errors.New("bulk create mode requires at least one of --dest-space, --where-space, --name-prefix, or --variant-labels")
		}

		if len(invocationCreateArgs.namePrefixes) > 0 && len(invocationCreateArgs.variantLabels) > 0 {
			return false, errors.New("--name-prefix and --variant-labels cannot be used together")
		}

		if invocationCreateArgs.namePattern != "" && len(invocationCreateArgs.namePrefixes) > 0 {
			return false, errors.New("--name-pattern cannot be used with --name-prefix")
		}

		if invocationCreateArgs.namePattern != "" && len(invocationCreateArgs.variantLabels) == 0 {
			return false, errors.New("--variant-labels needs to be set when using --name-pattern")
		}
	} else {
		// Single create mode validation
		if len(args) < 3 {
			return false, errors.New("single invocation creation requires: <slug> <toolchain type> <function> [arguments...]")
		}

		if filter != "" || where != "" ||
			invocationCreateArgs.namePattern != "" || len(invocationCreateArgs.invocationSlugs) > 0 ||
			len(invocationCreateArgs.destSpaces) > 0 || invocationCreateArgs.whereSpace != "" ||
			len(invocationCreateArgs.namePrefixes) > 0 || len(invocationCreateArgs.variantLabels) > 0 {
			return false, errors.New(
				"bulk create flags (--filter, --where, --invocation, --dest-space, --where-space, --name-prefix, --variant-labels, --name-pattern) can only be used without positional arguments",
			)
		}
	}

	if err := validateSpaceFlag(isBulkCreateMode); err != nil {
		return isBulkCreateMode, err
	}

	if err := validateStdinFlags(); err != nil {
		return isBulkCreateMode, err
	}

	// Validate no label removal
	if err := ValidateLabelRemoval(label, false); err != nil {
		return isBulkCreateMode, err
	}
	// Validate no delete gate removal
	if err := ValidateDeleteGateRemoval(deleteGate, false); err != nil {
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
	err := setAnnotations(&newBody.Annotations)
	if err != nil {
		return err
	}
	err = setLabels(&newBody.Labels)
	if err != nil {
		return err
	}
	err = setDeleteGates(&newBody.DeleteGates)
	if err != nil {
		return err
	}
	newBody.SpaceID = spaceID
	newBody.Slug = makeSlug(args[0])
	if newBody.DisplayName == "" {
		newBody.DisplayName = args[0]
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
		newBody.BridgeWorkerID = &workerID
	}

	newBody.ToolchainType = args[1]
	newBody.FunctionName = args[2]
	invokeArgs := args[3:]
	newArgs := parseFunctionArguments(invokeArgs)
	newBody.Arguments = newArgs
	declaredParams, err := parseDeclaredParameterFlags(invocationDeclaredParameterFlags)
	if err != nil {
		return err
	}
	newBody.Parameters = declaredParams
	// Create params with AllowExists if needed
	params := &goclientnew.CreateInvocationParams{}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	invocationRes, err := cubClientNew.CreateInvocationWithResponse(ctx, spaceID, params, newBody)
	if cubapi.IsAPIError(err, invocationRes) {
		return cubapi.InterpretErrorGeneric(err, invocationRes)
	}

	invocationDetails := invocationRes.JSON200
	displayCreateResults(invocationDetails, "invocation", args[0], invocationDetails.InvocationID.String(), displayInvocationDetails)
	return nil
}

// parseDeclaredParameterFlags parses --parameter flags of the form
// name[:datatype[:required]] into the Invocation's declared parameter namespace.
// datatype defaults to "string"; required defaults to true.
func parseDeclaredParameterFlags(flags []string) ([]goclientnew.FunctionParameter, error) {
	if len(flags) == 0 {
		return nil, nil
	}
	params := make([]goclientnew.FunctionParameter, 0, len(flags))
	for _, spec := range flags {
		parts := strings.Split(spec, ":")
		name := strings.TrimSpace(parts[0])
		if name == "" {
			return nil, errors.Newf("--parameter %q: name is required", spec)
		}
		dataType := "string"
		if len(parts) > 1 && parts[1] != "" {
			dataType = parts[1]
		}
		required := true
		if len(parts) > 2 && parts[2] != "" {
			switch strings.ToLower(parts[2]) {
			case "true", "required", "yes":
				required = true
			case "false", "optional", "no":
				required = false
			default:
				return nil, errors.Newf("--parameter %q: required must be true or false, got %q", spec, parts[2])
			}
		}
		params = append(params, goclientnew.FunctionParameter{
			ParameterName: name,
			DataType:      dataType,
			Required:      required,
		})
	}
	return params, nil
}

func runBulkInvocationCreate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

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
	if filterID != "" {
		params.Filter = &filterID
	}

	// Set allow_exists parameter if flag is set
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	// Add name prefixes if specified
	if len(invocationCreateArgs.namePrefixes) > 0 {
		namePrefixesStr := strings.Join(invocationCreateArgs.namePrefixes, ",")
		params.NamePrefixes = &namePrefixesStr
	}

	// Add variant labels if specified
	if len(invocationCreateArgs.variantLabels) > 0 {
		variantLabelsStr := strings.Join(invocationCreateArgs.variantLabels, ",")
		params.VariantLabels = &variantLabelsStr
	}

	// Add name pattern if specified
	if invocationCreateArgs.namePattern != "" {
		params.NamePattern = &invocationCreateArgs.namePattern
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

	// Parse and set filter_space parameter if specified
	if invocationCreateArgs.filterSpace != "" {
		filterSpaceID, err := parseFilterFlag(invocationCreateArgs.filterSpace)
		if err != nil {
			return errors.Wrapf(err, "error parsing filter-space")
		}
		params.FilterSpace = &filterSpaceID
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
