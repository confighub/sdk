// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var triggerCreateCmd = &cobra.Command{
	Use:   "create <slug> <event> <config type> <function> [<arg1> ...]",
	Short: "Create a new trigger",
	Long: `Create a new trigger to automate actions on resources.

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

Examples:
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
  cub trigger create --space my-space --json stamp PostClone Kubernetes/YAML -- set-annotation --key=cloned --value=true`,
	Args: cobra.MinimumNArgs(4),
	RunE: triggerCreateCmdRun,
}

func init() {
	addStandardCreateFlags(triggerCreateCmd)
	triggerCreateCmd.Flags().BoolVar(&disableTrigger, "disable", false, "Disable trigger")
	triggerCreateCmd.Flags().BoolVar(&enforceTrigger, "enforce", false, "Enforce trigger")
	triggerCreateCmd.Flags().StringVar(&workerSlug, "worker", "", "worker to execute the trigger function")
	triggerCmd.AddCommand(triggerCreateCmd)
}

func triggerCreateCmdRun(cmd *cobra.Command, args []string) error {
	if err := validateStdinFlags(); err != nil {
		return err
	}

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
		worker, err := apiGetBridgeWorkerFromSlug(workerSlug)
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
