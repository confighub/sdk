// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"
)

// changeWorkflowListFlags are the flags a change workflow's create and update share for its
// lists: the stages, and the prerequisites a stage or the final stage may gate on.
type changeWorkflowListFlags struct {
	stages, custom, attestation *listFlags
	finalPrerequisites          string
}

// addChangeWorkflowListFlags registers them. stages holds the values of the command's own
// --stage.
func addChangeWorkflowListFlags(cmd *cobra.Command, stages *[]string) *changeWorkflowListFlags {
	flags := &changeWorkflowListFlags{}
	flags.stages = addListFlags(cmd, listFlagsSpec{
		entityType: "ChangeWorkflow", list: "Stages", key: "Name", element: "stage", elements: stages, noun: "stage",
		newMember: newChangeWorkflowStageMember,
	})
	cmd.Flags().StringVar(&flags.finalPrerequisites, "final-prerequisites", "",
		"gates the last stage must satisfy for the rollout to read as completed, as <name>[;<name>...] (use '-' to clear)")
	flags.custom = addListFlags(cmd, listFlagsSpec{
		entityType: "ChangeWorkflow", list: "CustomPrerequisites", key: "Name", element: "custom-prerequisite",
		primary: "Expression", noun: "custom prerequisite",
	})
	flags.attestation = addListFlags(cmd, listFlagsSpec{
		entityType: "ChangeWorkflow", list: "AttestationPrerequisites", key: "Name", element: "attestation-prerequisite",
		noun: "attestation prerequisite",
	})
	return flags
}

// newChangeWorkflowStageMember makes the stage --stage names when the workflow does not have it.
func newChangeWorkflowStageMember(name string) (map[string]any, error) {
	stage, err := changeWorkflowStage(name)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(stage)
	if err != nil {
		return nil, err
	}
	member := map[string]any{}
	return member, json.Unmarshal(data, &member)
}

// queue adds the flags' edits to the ones the command's write will make. With stageMembers, the
// stages --stage names become the workflow's stages, each keeping what it had, and prerequisites,
// if any, become the gates of each of them and of the final stage. Without it the command has
// set the stages itself.
func (flags *changeWorkflowListFlags) queue(stageMembers bool, prerequisites []string) error {
	if stageMembers {
		edits, err := flags.stages.elementEdits()
		if err != nil {
			return err
		}
		pendingFieldEdits = append(pendingFieldEdits, edits...)
		if len(prerequisites) > 0 {
			gates := strings.Join(prerequisites, listValueSeparator)
			for _, name := range *flags.stages.elements {
				pendingFieldEdits = append(pendingFieldEdits, fieldEdit{
					path: elementPath("Stages", "Name", strings.TrimSpace(name)) + ".Prerequisites", value: gates, flag: "--prerequisites", mustExist: true})
			}
			pendingFieldEdits = append(pendingFieldEdits, fieldEdit{path: "Final.Prerequisites", value: gates, flag: "--prerequisites"})
		}
	}
	if err := flags.stages.queueFields(); err != nil {
		return err
	}
	switch flags.finalPrerequisites {
	case "":
	case clearFlagValue:
		pendingFieldEdits = append(pendingFieldEdits, fieldEdit{path: "Final.Prerequisites", unset: true, flag: "--final-prerequisites"})
	default:
		pendingFieldEdits = append(pendingFieldEdits, fieldEdit{path: "Final.Prerequisites", value: flags.finalPrerequisites, flag: "--final-prerequisites"})
	}
	if err := flags.custom.queue(); err != nil {
		return err
	}
	return flags.attestation.queue()
}
