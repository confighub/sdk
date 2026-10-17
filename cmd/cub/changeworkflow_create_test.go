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
	}
	for _, test := range tests {
		err := checkChangeWorkflowDefinition(test.fromFile, test.stages, test.prerequisites, test.listsGiven)
		switch {
		case test.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error %v", test.name, err)
		case test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)):
			t.Errorf("%s: error = %v, want one containing %q", test.name, err, test.wantErr)
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
