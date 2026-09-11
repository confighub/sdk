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

var changeworkflowUpdateCmd = &cobra.Command{
	Use:   "update [<slug or id>] [options...]",
	Short: "Update a change workflow or multiple change workflows",
	Long: getCommandHelp(`Update a change workflow or multiple change workflows using bulk operations.

A workflow is edited in place rather than versioned, and a promotion reads it as it stands. Editing
one part way through a rollout therefore changes the rules a change already started under; a
rollout that must not move is served by cloning the workflow and pointing the new change orders at
the clone.

Single change workflow update:
`+"```"+`
  # Replace the stages with the ones named, gating each of them alike
  cub changeworkflow update --space workflows myapp-main-line \
    --stage dev --stage staging --stage prod --prerequisites Released

  # Replace the whole workflow from a file
  cub changeworkflow update --space workflows myapp-main-line --filename workflow.yaml
`+"```"+`

Bulk update with --patch:

Update multiple change workflows at once based on search criteria. Requires --patch with no
positional arguments.

Examples:
`+"```"+`
  # Label every workflow whose last stage gates on live status
  cub changeworkflow update --patch --where "Final.Prerequisites ? 'Healthy'" --label Gated=true

  # Patch specific workflows from JSON
  echo '{"Final": {"Prerequisites": ["Released"]}}' | cub changeworkflow update --patch \
    --changeworkflow myapp-main-line --from-stdin
`+"```"+`
`, ""),
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        changeworkflowUpdateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	changeworkflowPatch       bool
	changeworkflowIdentifiers []string
	changeworkflowUpdateArgs  struct {
		stages        []string
		prerequisites []string
	}
)

func init() {
	addStandardUpdateFlags(changeworkflowUpdateCmd)
	changeworkflowUpdateCmd.Flags().BoolVar(&changeworkflowPatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(changeworkflowUpdateCmd)
	enableFilterFlag(changeworkflowUpdateCmd)
	changeworkflowUpdateCmd.Flags().StringSliceVar(&changeworkflowIdentifiers, "changeworkflow", []string{}, "target specific change workflows by slug or UUID for bulk patch (can be repeated or comma-separated)")

	// Single update specific flags
	changeworkflowUpdateCmd.Flags().StringSliceVar(&changeworkflowUpdateArgs.stages, "stage", nil, "replace the stages with the ones named (can be repeated or comma-separated), given in the order a change is promoted through them")
	changeworkflowUpdateCmd.Flags().StringSliceVar(&changeworkflowUpdateArgs.prerequisites, "prerequisites", nil, "gates given to every stage and to the final stage (can be repeated or comma-separated); requires --stage")

	changeworkflowCmd.AddCommand(changeworkflowUpdateCmd)
}

func checkChangeWorkflowConflictingArgs(args []string) bool {
	// Check for bulk patch mode (no positional args)
	isBulkPatchMode := len(args) == 0

	if isBulkPatchMode {
		if !changeworkflowPatch {
			failOnError(errors.New("--patch is required in bulk mode"))
		}

		// Check for mutual exclusivity between --changeworkflow and --where flags
		if len(changeworkflowIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--changeworkflow and --where flags are mutually exclusive"))
		}

		// The stages are one workflow's own shape, so there is nothing sensible to give a
		// selection of them all at once.
		if len(changeworkflowUpdateArgs.stages) > 0 || len(changeworkflowUpdateArgs.prerequisites) > 0 {
			failOnError(errors.New("--stage and --prerequisites can only be used with a single change workflow update"))
		}
	} else {
		// Single update mode validation
		if len(args) != 1 {
			failOnError(errors.New("single change workflow update requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(changeworkflowIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --changeworkflow can only be specified with --patch and no positional arguments"))
		}
	}

	if changeworkflowPatch && flagReplace {
		failOnError(fmt.Errorf("only one of --patch and --replace should be specified"))
	}

	// The gates are carried by the stages, so replacing the gates alone would leave them attached
	// to whatever stages happen to be there.
	if len(changeworkflowUpdateArgs.prerequisites) > 0 && len(changeworkflowUpdateArgs.stages) == 0 {
		failOnError(errors.New("--prerequisites needs at least one --stage"))
	}

	if err := validateStdinFlags(); err != nil {
		failOnError(err)
	}

	// Validate label removal only works with patch
	if err := ValidateLabelRemoval(label, changeworkflowPatch); err != nil {
		failOnError(err)
	}
	// Validate delete gate removal only works with patch
	if err := ValidateDeleteGateRemoval(deleteGate, changeworkflowPatch); err != nil {
		failOnError(err)
	}

	return isBulkPatchMode
}

// changeWorkflowStagesFromUpdateFlags builds the stages --stage and --prerequisites describe on an
// update. It is the create path's rule applied to a workflow that already exists: the stages named
// replace the ones there, and the gates go to each of them and to the final stage alike.
func changeWorkflowStagesFromUpdateFlags() ([]goclientnew.ChangeWorkflowStage, *goclientnew.ChangeWorkflowFinalStage, error) {
	return changeWorkflowStagesFromFlags(changeworkflowUpdateArgs.stages, changeworkflowUpdateArgs.prerequisites)
}

func runBulkChangeWorkflowUpdate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from changeworkflow identifiers or use provided where clause
	var effectiveWhere string
	if len(changeworkflowIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromChangeWorkflows(changeworkflowIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	patchJSON, err := BuildPatchData(nil)
	if err != nil {
		return err
	}

	// Build bulk patch parameters
	include := "SpaceID"
	params := &goclientnew.BulkPatchChangeWorkflowsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Call the bulk patch API
	bulkRes, err := cubClientNew.BulkPatchChangeWorkflowsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	return handleBulkChangeWorkflowCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "update", effectiveWhere)
}

func changeworkflowUpdateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkPatchMode := checkChangeWorkflowConflictingArgs(args)

	if isBulkPatchMode {
		return runBulkChangeWorkflowUpdate()
	}

	currentChangeWorkflowEnvelope, err := resolveChangeWorkflow(args[0], selectedSpaceID, "*") // get all fields for RMW
	if err != nil {
		return err
	}

	currentChangeWorkflow := currentChangeWorkflowEnvelope.ChangeWorkflow
	spaceID := currentChangeWorkflow.SpaceID

	if changeworkflowPatch {
		// Add annotations if specified
		if len(annotation) > 0 {
			if err := setAnnotations(&currentChangeWorkflow.Annotations); err != nil {
				return err
			}
		}

		// Add labels if specified
		if len(label) > 0 {
			if err := setLabels(&currentChangeWorkflow.Labels); err != nil {
				return err
			}
		}

		var stages []goclientnew.ChangeWorkflowStage
		var final *goclientnew.ChangeWorkflowFinalStage
		if len(changeworkflowUpdateArgs.stages) > 0 {
			stages, final, err = changeWorkflowStagesFromUpdateFlags()
			if err != nil {
				return err
			}
		}

		changeWorkflowEnhancer := func(patchData map[string]interface{}) {
			if stages != nil {
				patchData["Stages"] = stages
				patchData["Final"] = final
			}
		}

		patchData, err := BuildPatchData(changeWorkflowEnhancer)
		if err != nil {
			return fmt.Errorf("failed to build patch data: %w", err)
		}

		changeWorkflowDetails, err := patchChangeWorkflow(spaceID, currentChangeWorkflow.ChangeWorkflowID, patchData)
		if err != nil {
			return err
		}

		displayUpdateResults(changeWorkflowDetails, "changeworkflow", args[0],
			changeWorkflowDetails.ChangeWorkflowID.String(), displayChangeWorkflowDetails)
		return nil
	}

	// Traditional update mode
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingChangeWorkflow := currentChangeWorkflow
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			currentChangeWorkflow = new(goclientnew.ChangeWorkflow)
			currentChangeWorkflow.Version = existingChangeWorkflow.Version
		}

		if err := populateModelFromFlags(currentChangeWorkflow); err != nil {
			return err
		}

		// Ensure essential fields can't be clobbered
		currentChangeWorkflow.OrganizationID = existingChangeWorkflow.OrganizationID
		currentChangeWorkflow.SpaceID = existingChangeWorkflow.SpaceID
		currentChangeWorkflow.ChangeWorkflowID = existingChangeWorkflow.ChangeWorkflowID
		currentChangeWorkflow.Slug = existingChangeWorkflow.Slug
	}
	if err := setAnnotations(&currentChangeWorkflow.Annotations); err != nil {
		return err
	}
	if err := setLabels(&currentChangeWorkflow.Labels); err != nil {
		return err
	}

	// If this was set from stdin, it will be overridden
	currentChangeWorkflow.SpaceID = spaceID

	if len(changeworkflowUpdateArgs.stages) > 0 {
		stages, final, err := changeWorkflowStagesFromUpdateFlags()
		if err != nil {
			return err
		}
		currentChangeWorkflow.Stages = stages
		currentChangeWorkflow.Final = final
	}

	changeWorkflowRes, err := cubClientNew.UpdateChangeWorkflowWithResponse(ctx, spaceID,
		currentChangeWorkflow.ChangeWorkflowID, *currentChangeWorkflow)
	if cubapi.IsAPIError(err, changeWorkflowRes) {
		return cubapi.InterpretErrorGeneric(err, changeWorkflowRes)
	}

	changeWorkflowDetails := changeWorkflowRes.JSON200
	displayUpdateResults(changeWorkflowDetails, "changeworkflow", args[0],
		changeWorkflowDetails.ChangeWorkflowID.String(), displayChangeWorkflowDetails)
	return nil
}

func handleBulkChangeWorkflowCreateOrUpdateResponse(responses200 *[]goclientnew.ChangeWorkflowCreateOrUpdateResponse, responses207 *[]goclientnew.ChangeWorkflowCreateOrUpdateResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkGenericCreateOrUpdateResults(
		responses200, responses207, statusCode, "changeworkflow", operationName, contextInfo,
		func(r *goclientnew.ChangeWorkflowCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.ChangeWorkflowCreateOrUpdateResponse) string {
			if r.ChangeWorkflow != nil {
				return fmt.Sprintf("%s (ID: %s)", r.ChangeWorkflow.Slug, r.ChangeWorkflow.ChangeWorkflowID)
			}
			return ""
		},
	)
}

func patchChangeWorkflow(spaceID uuid.UUID, changeWorkflowID uuid.UUID, patchData []byte) (*goclientnew.ChangeWorkflow, error) {
	changeWorkflowRes, err := cubClientNew.PatchChangeWorkflowWithBodyWithResponse(
		ctx,
		spaceID,
		changeWorkflowID,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if cubapi.IsAPIError(err, changeWorkflowRes) {
		return nil, cubapi.InterpretErrorGeneric(err, changeWorkflowRes)
	}

	return changeWorkflowRes.JSON200, nil
}
