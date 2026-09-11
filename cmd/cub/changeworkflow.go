// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var changeworkflowCmd = &cobra.Command{
	Use:   "changeworkflow",
	Short: "ChangeWorkflow commands",
	Long: getCommandHelp(`The changeworkflow subcommands manage ChangeWorkflows.

A ChangeWorkflow declares how a change is promoted: the ordered stages it moves through, which
Spaces each stage selects, and the gates that have to pass before it enters one. It is the
definition; a ChangeOrder is the invocation, and one workflow governs many of them.

A workflow names no component and no base Space. Both come from the ChangeOrder being promoted --
the base is the Space the change order lives in, and the component is that Space's Component label
-- so one workflow resolves to different Spaces under different change orders. That is what lets a
workflow be cloned into another component's Space and give it the same shape of rollout, which
"cub changeworkflow create" does in bulk mode.

"cub changeorder create --change-workflow" names the workflow a change order is promoted under.
`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(changeworkflowCmd)
	rootCmd.AddCommand(changeworkflowCmd)
	addExplainCmd(changeworkflowCmd, "ChangeWorkflow")
}

// buildWhereClauseFromChangeWorkflows generates a WHERE clause from change workflow identifiers
func buildWhereClauseFromChangeWorkflows(changeWorkflowIDs []string) (string, error) {
	return buildWhereClauseFromIdentifiers(changeWorkflowIDs, "ChangeWorkflowID", "Slug")
}

// changeWorkflowFinalPrerequisites is what the last stage must satisfy for the rollout to be
// complete. Final is optional -- a workflow that declares nothing is complete once its last stage
// has taken the change -- so it is read through here rather than dereferenced at each caller.
func changeWorkflowFinalPrerequisites(final *goclientnew.ChangeWorkflowFinalStage) []string {
	if final == nil {
		return nil
	}
	return final.Prerequisites
}
