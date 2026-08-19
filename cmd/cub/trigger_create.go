// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var triggerCreateCmd = &cobra.Command{
	Use:         "create [<slug> <event> <config type> <function> [<arg1> ...]]",
	Short:       "Create a new trigger or bulk create triggers",
	Long:        getTriggerCreateHelp(),
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        triggerCreateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func getTriggerCreateHelp() string {
	baseHelp := `Create a new trigger or bulk create multiple triggers by cloning existing ones.

SINGLE TRIGGER CREATION:

Create a new trigger to automate actions on resources.

Events:

  - Mutation: Triggered when a resource is being modified
  - PostClone: Triggered after a resource is cloned

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

  - vet-cel: Validate resources using CEL expressions
  - vet-approvedby: Check if resource is approved
  - vet-placeholders: Ensure no placeholders exist
  - vet-schemas: Validate resources against their OpenAPI schemas
  - vet-no-merge-conflicts: Fail while a merge has changes still withheld on the unit
  - set-default-names: Set default names for cloned resources
  - set-annotation: Set annotations on resources
  - ensure-context: Ensure context annotations are present

Function arguments can be provided as positional arguments or as named arguments using --argumentname=value syntax.
Once a named argument is used, all subsequent arguments must be named. Use "--" to separate command flags from function arguments when using named function arguments.

BULK TRIGGER CREATION:

When no positional arguments are provided, bulk create mode is activated. This mode clones existing
triggers based on filters and creates multiple new triggers with optional modifications.

Single Trigger Examples:
` + "```" + `
  # Create a trigger to validate replicas > 1 for Deployments
  cub trigger create --space my-space --json replicated Mutation Kubernetes/YAML vet-cel 'r.kind != "Deployment" || r.spec.replicas > 1'

  # Create a trigger to enforce low resource usage (replicas < 10)
  cub trigger create --space my-space --json lowcost Mutation Kubernetes/YAML vet-cel 'r.kind != "Deployment" || r.spec.replicas < 10'

  # Create a trigger to ensure no placeholders exist in resources
  cub trigger create --space my-space --json complete Mutation Kubernetes/YAML vet-placeholders

  # Create a trigger requiring approval before applying changes
  cub trigger create --space my-space --json require-approval Mutation Kubernetes/YAML vet-approvedby 1

  # Create a trigger to ensure context annotations
  cub trigger create --space my-space --json annotate-resources Mutation Kubernetes/YAML ensure-context true

  # Create a trigger to set default names for cloned resources
  cub trigger create --space my-space --json rename PostClone Kubernetes/YAML set-default-names

  # Create a trigger to add a "cloned=true" annotation after cloning
  cub trigger create --space my-space --json stamp PostClone Kubernetes/YAML set-annotation cloned true

  # Using named arguments for clarity (note the "--" separator)
  cub trigger create --space my-space --json stamp PostClone Kubernetes/YAML -- set-annotation --key=cloned --value=true
` + "```" + `

Bulk Create Examples:
` + "```" + `
  # Clone all triggers matching a pattern with name prefixes
  cub trigger create --where "Slug LIKE 'app-%'" --name-prefix dev-,staging- --dest-space dev-space

  # Clone specific triggers to multiple spaces
  cub trigger create --trigger my-trigger --dest-space dev-space,staging-space

  # Clone triggers using a where expression for destination spaces
  cub trigger create --where "Slug LIKE 'app-%'" --where-space "Labels.Environment IN ('dev', 'staging')"

  # Clone triggers with modifications via JSON patch
  echo '{"Disabled": false}' | cub trigger create --where "Event = 'Mutation'" --name-prefix active- --from-stdin

  # Clone triggers matching specific criteria
  cub trigger create --where "ToolchainType = 'Kubernetes/YAML' AND FunctionName = 'vet-cel'" --name-prefix v2-
` + "```" + `
`

	return getCommandHelp(baseHelp, "")
}

var triggerCreateArgs struct {
	destSpaces     []string
	whereSpace     string
	namePrefixes   []string
	triggerSlugs   []string
	filterSpace    string
	invocationSlug string
}

func init() {
	addStandardCreateFlags(triggerCreateCmd)
	triggerCreateCmd.Flags().BoolVar(&disableTrigger, "disable", false, "Disable trigger")
	triggerCreateCmd.Flags().BoolVar(&warnTrigger, "warn", false, "Set trigger to produce ApplyWarnings instead of ApplyGates")
	addTriggerClearanceFlag(triggerCreateCmd)
	triggerCreateCmd.Flags().BoolVar(&protectTrigger, "protect", false, "record the paths this trigger's function writes as protected local overrides, so a later merge from upstream does not overwrite them; for a trigger that decides a value the unit then owns, such as a PostClone trigger customizing a variant")
	triggerCreateCmd.Flags().StringVar(&workerSlug, "worker", "", "worker to execute the trigger function")
	enableWhereFlag(triggerCreateCmd)
	enableFilterFlag(triggerCreateCmd)

	// Bulk create specific flags
	triggerCreateCmd.Flags().StringSliceVar(&triggerCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	triggerCreateCmd.Flags().StringVar(&triggerCreateArgs.whereSpace, "where-space", "", "where expression to select destination spaces for bulk create")
	triggerCreateCmd.Flags().StringSliceVar(&triggerCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	triggerCreateCmd.Flags().StringSliceVar(&triggerCreateArgs.triggerSlugs, "trigger", []string{}, "target specific triggers by slug or UUID for bulk create (can be repeated or comma-separated)")
	triggerCreateCmd.Flags().StringVar(&triggerCreateArgs.filterSpace, "filter-space", "", "filter entity containing WHERE expression to select destination spaces for bulk create (slug or UUID)")
	triggerCreateCmd.Flags().StringVar(&triggerCreateArgs.invocationSlug, "invocation", "", "invocation to execute (alternative to specifying function and arguments)")
	triggerCreateCmd.Flags().StringVar(&triggerDescription, "description", "", "description explaining the trigger's purpose and how to fix failures")
	triggerCreateCmd.Flags().StringVar(&triggerWhereUnit, "where-unit", "", "filter expression to restrict which Units this trigger applies to")
	triggerCreateCmd.Flags().StringVar(&triggerUnitFilter, "unit-filter", "", "filter entity (slug or UUID) to restrict which Units this trigger applies to")
	triggerCreateCmd.Flags().StringVar(&triggerWhereResource, "where-resource", "", "metadata path expression to restrict which resources the trigger operates on")
	triggerCreateCmd.Flags().StringVar(&triggerFailOpenAfter, "fail-open-after", "", "duration after which disconnected worker triggers fail open (e.g., 6h, 30m)")
	triggerCreateCmd.Flags().StringVar(&triggerOtherDataSource, "other-data-source", "", "source of additional data to pass to the function (e.g., LiveRevisionNum)")

	triggerCmd.AddCommand(triggerCreateCmd)
}

func checkTriggerCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args
	isBulkCreateMode := len(args) == 0

	if isBulkCreateMode {
		// Validate bulk create requirements
		if len(triggerCreateArgs.triggerSlugs) > 0 && where != "" {
			return false, errors.New("--trigger and --where flags are mutually exclusive")
		}

		if len(triggerCreateArgs.destSpaces) > 0 && triggerCreateArgs.whereSpace != "" {
			return false, errors.New("--dest-space and --where-space flags are mutually exclusive")
		}

		if len(triggerCreateArgs.destSpaces) == 0 && triggerCreateArgs.whereSpace == "" && len(triggerCreateArgs.namePrefixes) == 0 {
			return false, errors.New("bulk create mode requires at least one of --dest-space, --where-space, or --name-prefix")
		}
	} else {
		// Single create mode validation
		if triggerCreateArgs.invocationSlug != "" {
			// When using invocation, we need 3 args: slug, event, config type
			if len(args) != 3 {
				return false, errors.New("single trigger creation with --invocation requires: <slug> <event> <config type>")
			}
		} else {
			// Traditional mode requires function and arguments
			if len(args) < 4 {
				return false, errors.New("single trigger creation requires: <slug> <event> <config type> <function> [arguments...]")
			}
		}

		if filter != "" || where != "" || len(triggerCreateArgs.triggerSlugs) > 0 || len(triggerCreateArgs.destSpaces) > 0 || triggerCreateArgs.whereSpace != "" || len(triggerCreateArgs.namePrefixes) > 0 {
			return false, errors.New("bulk create flags (--filter, --where, --trigger, --dest-space, --where-space, --name-prefix) can only be used without positional arguments")
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

func triggerCreateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkCreateMode, err := checkTriggerCreateConflictingArgs(args)
	if err != nil {
		return err
	}

	if isBulkCreateMode {
		return runBulkTriggerCreate()
	}

	return runSingleTriggerCreate(args)
}

func runSingleTriggerCreate(args []string) error {
	spaceID := uuid.MustParse(selectedSpaceID)
	newBody := goclientnew.Trigger{}
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
	if disableTrigger {
		newBody.Disabled = true
	}
	if warnTrigger {
		newBody.Warn = true
	}
	if protectTrigger {
		newBody.Protect = true
	}
	if len(triggerClearance) > 0 {
		clearance, err := parseClearanceSpecs(triggerClearance)
		if err != nil {
			return err
		}
		newBody.Clearance = &clearance
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

	if triggerDescription != "" {
		newBody.Description = triggerDescription
	}
	if triggerWhereUnit != "" {
		newBody.WhereUnit = triggerWhereUnit
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
		newBody.UnitFilterID = &filterID
	}
	if triggerWhereResource != "" {
		newBody.WhereResource = triggerWhereResource
	}
	if triggerOtherDataSource != "" {
		newBody.OtherDataSource = triggerOtherDataSource
	}
	if triggerFailOpenAfter != "" {
		duration, err := time.ParseDuration(triggerFailOpenAfter)
		if err != nil {
			return fmt.Errorf("invalid --fail-open-after duration: %w", err)
		}
		newBody.FailOpenAfter = int(duration)
	}

	// TODO: update with overriden string type TriggerEvent
	// params.Trigger.Event = models.ModelsTriggerEvent(args[1])
	newBody.Event = args[1]
	newBody.ToolchainType = args[2]

	if triggerCreateArgs.invocationSlug != "" {
		// Use invocation instead of function and arguments
		invocationID, err := parseInvocationSlug(triggerCreateArgs.invocationSlug)
		if err != nil {
			return err
		}
		newBody.InvocationID = &invocationID
		// Clear function-related fields when using invocation
		newBody.FunctionName = ""
		newBody.Arguments = nil
	} else {
		// Traditional function and arguments approach
		newBody.FunctionName = args[3]
		invokeArgs := args[4:]
		newArgs := parseFunctionArguments(invokeArgs)
		newBody.Arguments = newArgs
	}
	// Create params with AllowExists if needed
	params := &goclientnew.CreateTriggerParams{}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	triggerRes, err := cubClientNew.CreateTriggerWithResponse(ctx, spaceID, params, newBody)
	if cubapi.IsAPIError(err, triggerRes) {
		return cubapi.InterpretErrorGeneric(err, triggerRes)
	}

	triggerDetails := triggerRes.JSON200
	displayCreateResults(triggerDetails, "trigger", args[0], triggerDetails.TriggerID.String(), displayTriggerDetails)
	return nil
}

func runBulkTriggerCreate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from trigger identifiers or use provided where clause
	var effectiveWhere string
	if len(triggerCreateArgs.triggerSlugs) > 0 {
		whereClause, err := buildWhereClauseFromTriggers(triggerCreateArgs.triggerSlugs)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Build patch data using consolidated function (no entity-specific fields for trigger)
	patchJSON, err := BuildPatchData(nil)
	if err != nil {
		return err
	}

	// Build bulk create parameters
	include := "SpaceID"
	params := &goclientnew.BulkCreateTriggersParams{
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
	if len(triggerCreateArgs.namePrefixes) > 0 {
		namePrefixesStr := strings.Join(triggerCreateArgs.namePrefixes, ",")
		params.NamePrefixes = &namePrefixesStr
	}

	// Set where_space parameter - either from direct where-space flag or converted from dest-space
	var whereSpaceExpr string
	if triggerCreateArgs.whereSpace != "" {
		whereSpaceExpr = triggerCreateArgs.whereSpace
	} else if len(triggerCreateArgs.destSpaces) > 0 {
		// Convert dest-space identifiers to a where expression
		whereSpaceExpr, err = buildWhereClauseForSpaces(triggerCreateArgs.destSpaces)
		if err != nil {
			return errors.Wrapf(err, "error converting destination spaces to where expression")
		}
	}

	if whereSpaceExpr != "" {
		params.WhereSpace = &whereSpaceExpr
	}

	// Parse and set filter_space parameter if specified
	if triggerCreateArgs.filterSpace != "" {
		filterSpaceID, err := parseFilterFlag(triggerCreateArgs.filterSpace)
		if err != nil {
			return errors.Wrapf(err, "error parsing filter-space")
		}
		params.FilterSpace = &filterSpaceID
	}

	// Call the bulk create API
	bulkRes, err := cubClientNew.BulkCreateTriggersWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkTriggerCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "create", effectiveWhere)
}
