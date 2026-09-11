// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var changeworkflowGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a change workflow",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a change workflow: its stages in the order a change is
promoted through them, the Spaces each selects, the gates that have to pass before a change enters
one, and the checks the last stage must satisfy for the rollout to be complete.

Examples:
`+"```"+`
  # Get details about a change workflow
  cub changeworkflow get --space workflows myapp-main-line

  # Get a change workflow in JSON format, which is also what --filename takes
  cub changeworkflow get --space workflows --json myapp-main-line
`+"```"+`
`, ""),
	RunE: changeworkflowGetCmdRun,
}

func init() {
	addStandardGetFlags(changeworkflowGetCmd)
	enableOptionalSpace(changeworkflowGetCmd)
	changeworkflowCmd.AddCommand(changeworkflowGetCmd)
}

func changeworkflowGetCmdRun(cmd *cobra.Command, args []string) error {
	changeWorkflowDetails, err := resolveChangeWorkflow(args[0], selectedSpaceID, selectFields)
	if err != nil {
		return err
	}

	displayGetResults(changeWorkflowDetails, displayExtendedChangeWorkflowDetails)
	return nil
}

// formatChangeWorkflowStage renders one stage as its selector and its entry gates. A stage with no
// gates is not gateless in effect: having taken the change is checked whatever is declared.
func formatChangeWorkflowStage(stage goclientnew.ChangeWorkflowStage) string {
	parts := []string{}
	if stage.WhereSpace != "" {
		parts = append(parts, stage.WhereSpace)
	} else {
		parts = append(parts, "every Space of the component")
	}
	if len(stage.Prerequisites) > 0 {
		parts = append(parts, "gates: "+strings.Join(stage.Prerequisites, ", "))
	}
	return strings.Join(parts, "; ")
}

func displayChangeWorkflowDetails(changeWorkflowDetails *goclientnew.ChangeWorkflow) {
	// Create an ExtendedChangeWorkflow wrapper with just the ChangeWorkflow set. The other fields
	// stay nil, which makes the Extended display show IDs.
	displayExtendedChangeWorkflowDetails(&goclientnew.ExtendedChangeWorkflow{
		ChangeWorkflow: changeWorkflowDetails,
	})
}

func displayExtendedChangeWorkflowDetails(extendedChangeWorkflow *goclientnew.ExtendedChangeWorkflow) {
	changeWorkflowDetails := extendedChangeWorkflow.ChangeWorkflow
	view := tableView()
	view.Append([]string{"ID", changeWorkflowDetails.ChangeWorkflowID.String()})
	view.Append([]string{"Name", changeWorkflowDetails.Slug})

	// Show Space slug instead of Space ID when available
	if extendedChangeWorkflow.Space != nil {
		view.Append([]string{"Space", extendedChangeWorkflow.Space.Slug})
	} else {
		view.Append([]string{"Space ID", changeWorkflowDetails.SpaceID.String()})
	}

	view.Append([]string{"Created At", changeWorkflowDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", changeWorkflowDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(changeWorkflowDetails.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(changeWorkflowDetails.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(changeWorkflowDetails.Annotations)})
	view.Append([]string{"Organization ID", changeWorkflowDetails.OrganizationID.String()})

	// The stages are numbered, since their order is what a change is promoted through.
	for i, stage := range changeWorkflowDetails.Stages {
		view.Append([]string{fmt.Sprintf("Stage %d: %s", i+1, stage.Name), formatChangeWorkflowStage(stage)})
	}

	if final := changeWorkflowFinalPrerequisites(changeWorkflowDetails.Final); len(final) > 0 {
		view.Append([]string{"Final Gates", strings.Join(final, ", ")})
	}

	// The description is what a gate is for, in the author's words, and the expression is how it
	// is checked. A declaration that carries both shows both.
	for _, custom := range changeWorkflowDetails.CustomPrerequisites {
		detail := custom.Expression
		if custom.Description != "" {
			detail = custom.Description + " (" + custom.Expression + ")"
		}
		view.Append([]string{"Prerequisite " + custom.Name, detail})
	}

	view.Render()
}
