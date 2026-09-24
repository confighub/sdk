// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var componentUpdateArgs struct {
	permissions            []string
	allowedChangeWorkflows []string
	changeWorkflowRequired bool
}

var componentUpdateCmd = &cobra.Command{
	Use:   "update <name or id>",
	Short: "Update a component",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Update a Component entity.

Changing which ChangeWorkflows a component allows, whether one is required, or its permissions
requires Manage permission on the component. ChangeWorkflows are addressed as <space>/<slug> or by
UUID; prefix one with '-' to stop allowing it.

Examples:
`+"```"+`
  # Allow another ChangeWorkflow and stop allowing one
  cub component update --patch my-app \
    --allowed-change-workflow platform/canary-rollout \
    --allowed-change-workflow -platform/hotfix

  # Stop requiring a ChangeWorkflow to promote and release
  cub component update --patch my-app --change-workflow-required=false

  # Replace the component from a JSON definition
  cub component update my-app --from-stdin --replace < component.json
`+"```"+`
`, ""),
	RunE: componentUpdateCmdRun,
}

func init() {
	addStandardUpdateFlags(componentUpdateCmd)
	componentUpdateCmd.Flags().BoolVar(&isPatch, "patch", false, "use patch API")
	componentUpdateCmd.Flags().StringSliceVar(&componentUpdateArgs.permissions, "permission", []string{}, "permission in format Action:UserIDOrUsername to add, or -Action:UserIDOrUsername to remove (e.g., Manage:user@example.com, -View:user@example.com, can be repeated)")
	componentUpdateCmd.Flags().StringSliceVar(&componentUpdateArgs.allowedChangeWorkflows, "allowed-change-workflow", []string{}, "ChangeWorkflow, as <space>/<slug> or UUID, to allow, or -<ChangeWorkflow> to stop allowing (can be repeated or comma-separated)")
	componentUpdateCmd.Flags().BoolVar(&componentUpdateArgs.changeWorkflowRequired, "change-workflow-required", false, "require a ChangeWorkflow to promote and release")
	componentCmd.AddCommand(componentUpdateCmd)
}

func componentUpdateCmdRun(cmd *cobra.Command, args []string) error {
	if isPatch && flagReplace {
		return errors.New("only one of --patch and --replace should be specified")
	}
	if err := ValidateLabelRemoval(label, isPatch); err != nil {
		return err
	}
	if err := ValidateDeleteGateRemoval(deleteGate, isPatch); err != nil {
		return err
	}
	if err := validateStdinFlags(); err != nil {
		return err
	}

	currentEnvelope, err := resolveComponent(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}
	currentComponent := currentEnvelope.Component
	componentID := currentComponent.ComponentID

	added, removed, err := parseAllowedChangeWorkflows(componentUpdateArgs.allowedChangeWorkflows)
	if err != nil {
		return err
	}
	requiredChanged := cmd.Flags().Changed("change-workflow-required")

	if isPatch {
		componentEnhancer := func(patchMap map[string]interface{}) {
			if len(added) > 0 || len(removed) > 0 {
				// A merge patch replaces an array whole, so send the complete resulting set.
				patchMap["AllowedChangeWorkflowIDs"] = applyAllowedChangeWorkflows(currentComponent.AllowedChangeWorkflowIDs, added, removed)
			}
			if requiredChanged {
				patchMap["ChangeWorkflowRequired"] = componentUpdateArgs.changeWorkflowRequired
			}
		}
		patchData, err := BuildPatchDataWithPermissions(componentEnhancer, componentUpdateArgs.permissions)
		if err != nil {
			return fmt.Errorf("failed to build patch data: %w", err)
		}
		componentRes, err := cubClientNew.PatchComponentWithBodyWithResponse(
			ctx,
			componentID,
			"application/merge-patch+json",
			bytes.NewReader(patchData),
		)
		if cubapi.IsAPIError(err, componentRes) {
			return cubapi.InterpretErrorGeneric(err, componentRes)
		}
		componentDetails := componentRes.JSON200
		displayUpdateResults(componentDetails, "component", args[0], componentDetails.ComponentID.String(), displayComponentEntityDetails)
		return nil
	}

	newBody := currentComponent
	if flagPopulateModelFromStdin || flagFilename != "" {
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			newBody = new(goclientnew.Component)
			newBody.Version = currentComponent.Version
		}
		if err := populateModelFromFlags(newBody); err != nil {
			return err
		}
		// Ensure essential fields can't be clobbered
		newBody.OrganizationID = currentComponent.OrganizationID
		newBody.ComponentID = currentComponent.ComponentID
	}
	if err := setAnnotations(&newBody.Annotations); err != nil {
		return err
	}
	if err := setLabels(&newBody.Labels); err != nil {
		return err
	}
	if err := setDeleteGates(&newBody.DeleteGates); err != nil {
		return err
	}
	if len(componentUpdateArgs.permissions) > 0 && newBody.Permissions == nil {
		newBody.Permissions = &goclientnew.Permissions{}
	}
	if err := parsePermissions(componentUpdateArgs.permissions, newBody.Permissions); err != nil {
		return err
	}
	if len(added) > 0 || len(removed) > 0 {
		newBody.AllowedChangeWorkflowIDs = applyAllowedChangeWorkflows(newBody.AllowedChangeWorkflowIDs, added, removed)
	}
	if requiredChanged {
		newBody.ChangeWorkflowRequired = componentUpdateArgs.changeWorkflowRequired
	}

	componentRes, err := cubClientNew.UpdateComponentWithResponse(ctx, componentID, *newBody)
	if cubapi.IsAPIError(err, componentRes) {
		return cubapi.InterpretErrorGeneric(err, componentRes)
	}
	componentDetails := componentRes.JSON200
	displayUpdateResults(componentDetails, "component", args[0], componentDetails.ComponentID.String(), displayComponentEntityDetails)
	return nil
}
