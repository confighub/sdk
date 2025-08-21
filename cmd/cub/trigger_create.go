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

Config Types:
  - Kubernetes/YAML: For Kubernetes YAML configurations

Example Functions:
  - cel-validate: Validate resources using CEL expressions
  - is-approved: Check if resource is approved
  - no-placeholders: Ensure no placeholders exist
  - set-default-names: Set default names for cloned resources
  - set-annotation: Set annotations on resources
  - ensure-context: Ensure context annotations are present

Function arguments can be provided as positional arguments or as named arguments using --argumentname=value syntax.
Once a named argument is used, all subsequent arguments must be named. Use "--" to separate command flags from function arguments when using named function arguments.

BULK TRIGGER CREATION:
When no positional arguments are provided, bulk create mode is activated. This mode clones existing
triggers based on filters and creates multiple new triggers with optional modifications.

Single Trigger Examples:
  # Create a trigger to validate replicas > 1 for Deployments
  cub trigger create --space my-space --json replicated Mutation Kubernetes/YAML cel-validate 'r.kind != "Deployment" || r.spec.replicas > 1'

  # Create a trigger to enforce low resource usage (replicas < 10)
  cub trigger create --space my-space --json lowcost Mutation Kubernetes/YAML cel-validate 'r.kind != "Deployment" || r.spec.replicas < 10'

  # Create a trigger to ensure no placeholders exist in resources
  cub trigger create --space my-space --json complete Mutation Kubernetes/YAML no-placeholders

  # Create a trigger requiring approval before applying changes
  cub trigger create --space my-space --json require-approval Mutation Kubernetes/YAML is-approved 1

  # Create a trigger to ensure context annotations
  cub trigger create --space my-space --json annotate-resources Mutation Kubernetes/YAML ensure-context true

  # Create a trigger to set default names for cloned resources
  cub trigger create --space my-space --json rename PostClone Kubernetes/YAML set-default-names

  # Create a trigger to add a "cloned=true" annotation after cloning
  cub trigger create --space my-space --json stamp PostClone Kubernetes/YAML set-annotation cloned true

  # Using named arguments for clarity (note the "--" separator)
  cub trigger create --space my-space --json stamp PostClone Kubernetes/YAML -- set-annotation --key=cloned --value=true

Bulk Create Examples:
  # Clone all triggers matching a pattern with name prefixes
  cub trigger create --where "Slug LIKE 'app-%'" --name-prefix dev-,staging- --dest-space dev-space

  # Clone specific triggers to multiple spaces
  cub trigger create --trigger my-trigger --dest-space dev-space,staging-space

  # Clone triggers using a where expression for destination spaces
  cub trigger create --where "Slug LIKE 'app-%'" --where-space "Labels.Environment IN ('dev', 'staging')"

  # Clone triggers with modifications via JSON patch
  echo '{"Disabled": false}' | cub trigger create --where "Event = 'Mutation'" --name-prefix active- --from-stdin

  # Clone triggers matching specific criteria
  cub trigger create --where "ToolchainType = 'Kubernetes/YAML' AND FunctionName = 'cel-validate'" --name-prefix v2-`

	return baseHelp
}

var triggerCreateArgs struct {
	destSpaces   []string
	whereSpace   string
	namePrefixes []string
	triggerSlugs []string
}

func init() {
	addStandardCreateFlags(triggerCreateCmd)
	triggerCreateCmd.Flags().BoolVar(&disableTrigger, "disable", false, "Disable trigger")
	triggerCreateCmd.Flags().BoolVar(&enforceTrigger, "enforce", false, "Enforce trigger")
	triggerCreateCmd.Flags().StringVar(&workerSlug, "worker", "", "worker to execute the trigger function")
	enableWhereFlag(triggerCreateCmd)

	// Bulk create specific flags
	triggerCreateCmd.Flags().StringSliceVar(&triggerCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	triggerCreateCmd.Flags().StringVar(&triggerCreateArgs.whereSpace, "where-space", "", "where expression to select destination spaces for bulk create")
	triggerCreateCmd.Flags().StringSliceVar(&triggerCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	triggerCreateCmd.Flags().StringSliceVar(&triggerCreateArgs.triggerSlugs, "trigger", []string{}, "target specific triggers by slug or UUID for bulk create (can be repeated or comma-separated)")

	triggerCmd.AddCommand(triggerCreateCmd)
}

func checkTriggerCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args and has bulk-specific flags
	isBulkCreateMode := len(args) == 0 && (where != "" || len(triggerCreateArgs.triggerSlugs) > 0 || len(triggerCreateArgs.destSpaces) > 0 || triggerCreateArgs.whereSpace != "" || len(triggerCreateArgs.namePrefixes) > 0)

	if isBulkCreateMode {
		// Validate bulk create requirements
		if where == "" && len(triggerCreateArgs.triggerSlugs) == 0 {
			return false, errors.New("bulk create mode requires --where or --trigger flags")
		}

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
		if len(args) < 4 {
			return false, errors.New("single trigger creation requires: <slug> <event> <config type> <function> [arguments...]")
		}

		if where != "" || len(triggerCreateArgs.triggerSlugs) > 0 || len(triggerCreateArgs.destSpaces) > 0 || triggerCreateArgs.whereSpace != "" || len(triggerCreateArgs.namePrefixes) > 0 {
			return false, errors.New("bulk create flags (--where, --trigger, --dest-space, --where-space, --name-prefix) can only be used without positional arguments")
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
	err := setLabels(&newBody.Labels)
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
	if enforceTrigger {
		newBody.Enforced = true
	}
	if workerSlug != "" {
		worker, err := apiGetBridgeWorkerFromSlug(workerSlug, "*") // get all fields for now
		if err != nil {
			return err
		}
		newBody.BridgeWorkerID = &worker.BridgeWorkerID
	}

	// TODO: update with overriden string type TriggerEvent
	// params.Trigger.Event = models.ModelsTriggerEvent(args[1])
	newBody.Event = args[1]
	newBody.ToolchainType = args[2]
	newBody.FunctionName = args[3]
	invokeArgs := args[4:]
	newArgs := parseFunctionArguments(invokeArgs)
	newBody.Arguments = newArgs
	triggerRes, err := cubClientNew.CreateTriggerWithResponse(ctx, spaceID, newBody)
	if IsAPIError(err, triggerRes) {
		return InterpretErrorGeneric(err, triggerRes)
	}

	triggerDetails := triggerRes.JSON200
	displayCreateResults(triggerDetails, "trigger", args[0], triggerDetails.TriggerID.String(), displayTriggerDetails)
	return nil
}

func runBulkTriggerCreate() error {
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
