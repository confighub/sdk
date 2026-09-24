// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"slices"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var componentCreateArgs struct {
	permissions            []string
	allowedChangeWorkflows []string
	changeWorkflowRequired bool
}

var componentCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a component",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Create a Component entity, which decides which ChangeWorkflows promotions and releases of its Variants may use, and whether one is required.

ChangeWorkflows are addressed as <space>/<slug> or by UUID.

Examples:
`+"```"+`
  # Create a component
  cub component create my-app

  # Create a component that requires one of two ChangeWorkflows to promote and release
  cub component create my-app --change-workflow-required \
    --allowed-change-workflow platform/standard-rollout \
    --allowed-change-workflow platform/hotfix

  # Create a component from a JSON definition
  cub component create my-app --from-stdin < component.json
`+"```"+`
`, ""),
	RunE: componentCreateCmdRun,
}

func init() {
	addStandardCreateFlags(componentCreateCmd)
	componentCreateCmd.Flags().StringSliceVar(&componentCreateArgs.permissions, "permission", []string{}, "permission in format Action:UserIDOrUsername (e.g., Manage:user@example.com, can be repeated)")
	componentCreateCmd.Flags().StringSliceVar(&componentCreateArgs.allowedChangeWorkflows, "allowed-change-workflow", []string{}, "ChangeWorkflow, as <space>/<slug> or UUID, that promotions and releases may use (can be repeated or comma-separated)")
	componentCreateCmd.Flags().BoolVar(&componentCreateArgs.changeWorkflowRequired, "change-workflow-required", false, "require a ChangeWorkflow to promote and release")
	componentCmd.AddCommand(componentCreateCmd)
}

// parseAllowedChangeWorkflows resolves --allowed-change-workflow values into the
// ChangeWorkflow IDs to add and, for values prefixed with "-", to remove.
func parseAllowedChangeWorkflows(refs []string) (added []uuid.UUID, removed []uuid.UUID, err error) {
	for _, ref := range refs {
		isRemoval := strings.HasPrefix(ref, "-")
		ref = strings.TrimPrefix(ref, "-")
		changeWorkflowID, err := resolveChangeWorkflowID(ref)
		if err != nil {
			return nil, nil, err
		}
		if isRemoval {
			removed = append(removed, changeWorkflowID)
		} else {
			added = append(added, changeWorkflowID)
		}
	}
	return added, removed, nil
}

// applyAllowedChangeWorkflows returns the allowed ChangeWorkflow IDs with added appended, unless
// already present, and removed dropped.
func applyAllowedChangeWorkflows(allowed, added, removed []uuid.UUID) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(allowed)+len(added))
	for _, changeWorkflowID := range append(slices.Clone(allowed), added...) {
		if !slices.Contains(result, changeWorkflowID) && !slices.Contains(removed, changeWorkflowID) {
			result = append(result, changeWorkflowID)
		}
	}
	return result
}

func componentCreateCmdRun(cmd *cobra.Command, args []string) error {
	if err := validateStdinFlags(); err != nil {
		return err
	}
	if err := ValidateLabelRemoval(label, false); err != nil {
		return err
	}
	if err := ValidateDeleteGateRemoval(deleteGate, false); err != nil {
		return err
	}

	newBody := &goclientnew.Component{}
	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(newBody); err != nil {
			return err
		}
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
	if len(componentCreateArgs.permissions) > 0 && newBody.Permissions == nil {
		newBody.Permissions = &goclientnew.Permissions{}
	}
	if err := parsePermissions(componentCreateArgs.permissions, newBody.Permissions); err != nil {
		return err
	}

	added, removed, err := parseAllowedChangeWorkflows(componentCreateArgs.allowedChangeWorkflows)
	if err != nil {
		return err
	}
	if len(removed) > 0 {
		return errors.New("--allowed-change-workflow values cannot be removed with '-' on create")
	}
	if len(added) > 0 {
		newBody.AllowedChangeWorkflowIDs = applyAllowedChangeWorkflows(newBody.AllowedChangeWorkflowIDs, added, nil)
	}
	if cmd.Flags().Changed("change-workflow-required") {
		newBody.ChangeWorkflowRequired = componentCreateArgs.changeWorkflowRequired
	}

	// Even if slug was set in stdin, we override it with the one from args
	if args[0] == "-" {
		newBody.Slug = randomUnusedSlug()
	} else {
		newBody.Slug = makeSlug(args[0])
	}

	params := &goclientnew.CreateComponentParams{}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}
	componentRes, err := cubClientNew.CreateComponentWithResponse(ctx, params, *newBody)
	if cubapi.IsAPIError(err, componentRes) {
		return cubapi.InterpretErrorGeneric(err, componentRes)
	}

	componentDetails := componentRes.JSON200
	displayCreateResults(componentDetails, "component", newBody.Slug, componentDetails.ComponentID.String(), displayComponentEntityDetails)
	return nil
}
