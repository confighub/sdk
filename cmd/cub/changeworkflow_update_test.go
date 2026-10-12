// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// A patch naming the stages again must send each one the workflow has as it has it. Built by the
// create rule, it rewrote a stage's WhereSpace from its name -- Labels.Stage = 'Test' for a stage
// selecting Labels.Stage = 'test' -- and dropped its gates, while removing another stage.
func TestChangeWorkflowStagesForPatchKeepsExistingStages(t *testing.T) {
	current := []goclientnew.ChangeWorkflowStage{
		{Name: "Base", WhereSpace: "Labels.Stage = 'base'"},
		{Name: "Test", WhereSpace: "Labels.Stage = 'test'"},
		{Name: "Public", WhereSpace: "Labels.Stage = 'public'", Prerequisites: []string{"Released", "Healthy"},
			ReleasePrerequisites: []string{"two-approvers"}},
	}

	stages, final, err := changeWorkflowStagesForPatch(current, []string{"Test", "Public"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := current[1:]; !reflect.DeepEqual(stages, want) {
		t.Errorf("stages = %+v, want %+v", stages, want)
	}
	if final != nil {
		t.Errorf("final = %+v, want nil: without --prerequisites the patch leaves the final stage alone", final)
	}
}

// A stage the workflow does not have is made as create makes it, and --prerequisites replaces the
// gates of every stage after the first and of the final stage.
func TestChangeWorkflowStagesForPatchAddsStagesAndGates(t *testing.T) {
	current := []goclientnew.ChangeWorkflowStage{
		{Name: "dev", WhereSpace: "Labels.Stage = 'dev'", Prerequisites: []string{"Healthy"}},
		{Name: "prod", WhereSpace: "Labels.Region = 'us-east1'", Prerequisites: []string{"Healthy"}},
	}

	stages, final, err := changeWorkflowStagesForPatch(current, []string{"dev", "canary", "prod"}, []string{"Released"})
	if err != nil {
		t.Fatal(err)
	}
	want := []goclientnew.ChangeWorkflowStage{
		{Name: "dev", WhereSpace: "Labels.Stage = 'dev'", Prerequisites: []string{"Healthy"}},
		{Name: "canary", WhereSpace: "Labels.Stage = 'canary'", Prerequisites: []string{"Released"}},
		{Name: "prod", WhereSpace: "Labels.Region = 'us-east1'", Prerequisites: []string{"Released"}},
	}
	if !reflect.DeepEqual(stages, want) {
		t.Errorf("stages = %+v, want %+v", stages, want)
	}
	if final == nil || !reflect.DeepEqual(final.Prerequisites, []string{"Released"}) {
		t.Errorf("final = %+v, want Prerequisites [Released]", final)
	}
}
