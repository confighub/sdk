// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// A workflow governing ChangeSets has no stages, so its Final prerequisites alone have to be
// enough to create one from flags; it was refused as needing stages, leaving only a file.
func TestCheckChangeWorkflowDefinition(t *testing.T) {
	tests := []struct {
		name          string
		fromFile      bool
		stages        []string
		stagePerSpace bool
		prerequisites []string
		listsGiven    bool
		wantErr       string
	}{
		{name: "stages", stages: []string{"dev", "prod"}, prerequisites: []string{"Released"}},
		{name: "a file", fromFile: true},
		{name: "a file edited by the list flags", fromFile: true, listsGiven: true},
		{name: "Final prerequisites alone, governing ChangeSets", listsGiven: true},
		{name: "nothing", wantErr: "needs stages, or, to govern ChangeSets, Final prerequisites"},
		{name: "a file and stages", fromFile: true, stages: []string{"dev"}, wantErr: "mutually exclusive"},
		{name: "prerequisites without stages", prerequisites: []string{"Validated"}, listsGiven: true,
			wantErr: "--prerequisites needs at least one --stage"},
		{name: "a stage per space", stagePerSpace: true, prerequisites: []string{"Released"}},
		{name: "a stage per space and stages", stagePerSpace: true, stages: []string{"dev"}, wantErr: "mutually exclusive"},
		{name: "a file and a stage per space", fromFile: true, stagePerSpace: true, wantErr: "mutually exclusive"},
	}
	for _, test := range tests {
		err := checkChangeWorkflowDefinition(test.fromFile, test.stages, test.stagePerSpace, test.prerequisites, test.listsGiven)
		switch {
		case test.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error %v", test.name, err)
		case test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)):
			t.Errorf("%s: error = %v, want one containing %q", test.name, err, test.wantErr)
		}
	}
}

func TestCheckChangeWorkflowComponent(t *testing.T) {
	tests := []struct {
		name                             string
		component                        string
		standalone, stagePerSpace, allow bool
		wantErr                          string
	}{
		{name: "nothing"},
		{name: "--component qualifying --variant", component: "my-app"},
		{name: "--component selecting Spaces", component: "my-app", standalone: true, stagePerSpace: true},
		{name: "--component to allow on", component: "my-app", standalone: true, allow: true},
		{name: "--variant <component>/<variant> to allow on", component: "my-app", allow: true},
		{name: "--component doing nothing", component: "my-app", standalone: true, wantErr: "needs one of them"},
		{name: "allowing on no Component", allow: true, wantErr: "--allow-on-component needs the Component"},
	}
	for _, test := range tests {
		err := checkChangeWorkflowComponent(test.component, test.standalone, test.stagePerSpace, test.allow)
		switch {
		case test.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error %v", test.name, err)
		case test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)):
			t.Errorf("%s: error = %v, want one containing %q", test.name, err, test.wantErr)
		}
	}
}

// The qualified --variant names the same Component --component does.
func TestScopedComponent(t *testing.T) {
	defer func(component, variant string) { componentFlag, variantFlag = component, variant }(componentFlag, variantFlag)
	for _, test := range []struct{ component, variant, want string }{
		{"", "", ""},
		{"", "base", ""},
		{"", "my-app/base", "my-app"},
		{"my-app", "base", "my-app"},
		{"my-app", "", "my-app"},
	} {
		componentFlag, variantFlag = test.component, test.variant
		if got := scopedComponent(); got != test.want {
			t.Errorf("scopedComponent() with --component %q --variant %q = %q, want %q", test.component, test.variant, got, test.want)
		}
	}
}

// --final-prerequisites without --stage builds a workflow with no stages whose Final holds them.
func TestChangeWorkflowFinalPrerequisitesWithoutStages(t *testing.T) {
	useFieldEditFlags(t, nil, nil)
	var stages []string
	lists := addChangeWorkflowListFlags(&cobra.Command{}, &stages)
	lists.finalPrerequisites = "Validated;one-approver"
	if !lists.given() {
		t.Fatal("given() = false with --final-prerequisites")
	}
	if err := lists.queue(false, nil); err != nil {
		t.Fatal(err)
	}
	workflow := &goclientnew.ChangeWorkflow{}
	if err := applyFieldEdits("ChangeWorkflow", workflow); err != nil {
		t.Fatal(err)
	}
	if len(workflow.Stages) != 0 {
		t.Errorf("Stages = %+v, want none", workflow.Stages)
	}
	if workflow.Final == nil || !reflect.DeepEqual(workflow.Final.Prerequisites, []string{"Validated", "one-approver"}) {
		t.Errorf("Final = %+v, want Prerequisites [Validated one-approver]", workflow.Final)
	}
}
