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
	Use:   "update [<name or id>]",
	Short: "Update a component or multiple components",
	Args:  cobra.MaximumNArgs(1),
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

  # Give every component a backing Unit, in the platform space, holding its configuration
  cub component update --patch --where "Slug LIKE '%'" --with-backing-units --backing-unit-space platform
`+"```"+`

With no name, --patch updates every component --where selects.
`, ""),
	RunE: componentUpdateCmdRun,
}

func init() {
	addStandardUpdateFlags(componentUpdateCmd)
	addFieldEditFlags(componentUpdateCmd, "Component")
	componentUpdateCmd.Flags().BoolVar(&isPatch, "patch", false, "use patch API")
	componentUpdateCmd.Flags().StringSliceVar(&componentUpdateArgs.permissions, "permission", []string{}, permissionUpdateHelp)
	componentUpdateCmd.Flags().StringSliceVar(&componentUpdateArgs.allowedChangeWorkflows, "allowed-change-workflow", []string{}, "ChangeWorkflow, as <space>/<slug> or UUID, to allow, or -<ChangeWorkflow> to stop allowing (can be repeated or comma-separated)")
	componentUpdateCmd.Flags().BoolVar(&componentUpdateArgs.changeWorkflowRequired, "change-workflow-required", false, "require a ChangeWorkflow to promote and release")
	enableWhereFlag(componentUpdateCmd)
	addBackingUnitFlags(componentUpdateCmd, "Component", true, false)
	addFromBackingUnitsFlags(componentUpdateCmd, "Component", false)
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
	if len(args) == 0 {
		return runBulkComponentUpdate(cmd)
	}
	if where != "" {
		return errors.New("--where selects the components to update, so it takes no component name")
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
			&goclientnew.PatchComponentParams{DryRun: dryRunParam()},
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
	setDisplayNameAndHiddenReason(&newBody.DisplayName, &newBody.HiddenReason)
	if err := setAnnotations(&newBody.Annotations); err != nil {
		return err
	}
	if err := setLabels(&newBody.Labels); err != nil {
		return err
	}
	if err := setDeleteGates(&newBody.DeleteGates); err != nil {
		return err
	}
	if err := applyPermissions(componentUpdateArgs.permissions, &newBody.Permissions); err != nil {
		return err
	}
	if len(added) > 0 || len(removed) > 0 {
		newBody.AllowedChangeWorkflowIDs = applyAllowedChangeWorkflows(newBody.AllowedChangeWorkflowIDs, added, removed)
	}
	if requiredChanged {
		newBody.ChangeWorkflowRequired = componentUpdateArgs.changeWorkflowRequired
	}

	if err := applyFieldEdits("Component", newBody); err != nil {
		return err
	}
	componentRes, err := cubClientNew.UpdateComponentWithResponse(ctx, componentID, &goclientnew.UpdateComponentParams{DryRun: dryRunParam()}, *newBody)
	if cubapi.IsAPIError(err, componentRes) {
		return cubapi.InterpretErrorGeneric(err, componentRes)
	}
	componentDetails := componentRes.JSON200
	displayUpdateResults(componentDetails, "component", args[0], componentDetails.ComponentID.String(), displayComponentEntityDetails)
	return nil
}

// runBulkComponentUpdate patches every component --where selects.
func runBulkComponentUpdate(cmd *cobra.Command) error {
	if !isPatch {
		return errors.New("--patch is required to update multiple components")
	}
	if where == "" {
		return errors.New("--where is required to update multiple components")
	}
	if len(componentUpdateArgs.allowedChangeWorkflows) > 0 {
		// A merge patch replaces the list whole, and each component's list is its own.
		return errors.New("--allowed-change-workflow updates one component at a time")
	}
	enhancer := func(patchMap map[string]interface{}) {
		if cmd.Flags().Changed("change-workflow-required") {
			patchMap["ChangeWorkflowRequired"] = componentUpdateArgs.changeWorkflowRequired
		}
	}
	patchData, err := BuildPatchDataWithPermissions(enhancer, componentUpdateArgs.permissions)
	if err != nil {
		return fmt.Errorf("failed to build patch data: %w", err)
	}
	params := &goclientnew.BulkPatchComponentsParams{Where: &where}
	params.IncludeHidden = includeHiddenParam()
	params.WithBackingUnits = withBackingUnitsParam()
	params.FromBackingUnits = fromBackingUnitsParam()
	params.BackingUnitSpace = backingUnitSpaceParam()
	params.DryRun = dryRunParam()
	bulkRes, err := cubClientNew.BulkPatchComponentsWithBodyWithResponse(ctx, params,
		"application/merge-patch+json", bytes.NewReader(patchData))
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}
	return displayBulkGenericCreateOrUpdateResults(
		bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "component", "update", where,
		func(r *goclientnew.ComponentCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.ComponentCreateOrUpdateResponse) string {
			if r.Component != nil {
				return fmt.Sprintf("%s (ID: %s)", r.Component.Slug, r.Component.ComponentID)
			}
			return ""
		},
	)
}
