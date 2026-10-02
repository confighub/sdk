// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/confighub/sdk/core/configkit/cubkit"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// useFieldEditFlags gives --set, --unset and the pending edits for one test.
func useFieldEditFlags(t *testing.T, sets, unsets []string, pending ...fieldEdit) {
	t.Helper()
	previousSet, previousUnset, previousPending := setFlags, unsetFlags, pendingFieldEdits
	setFlags, unsetFlags, pendingFieldEdits = sets, unsets, pending
	t.Cleanup(func() { setFlags, unsetFlags, pendingFieldEdits = previousSet, previousUnset, previousPending })
}

func TestSplitSetExpression(t *testing.T) {
	tests := []struct {
		expression, path, value string
		wantErr                 bool
	}{
		{expression: "GroupBy=Unit.Slug", path: "GroupBy", value: "Unit.Slug"},
		{expression: "Where=Labels.Tier = 'web'", path: "Where", value: "Labels.Tier = 'web'"},
		{expression: "Stages.?Name=prod.WhereSpace=Labels.Stage = 'prod'", path: "Stages.?Name=prod.WhereSpace", value: "Labels.Stage = 'prod'"},
		{expression: "Stages.?Name=prod={WhereSpace: x}", path: "Stages.?Name=prod", value: "{WhereSpace: x}"},
		{expression: "Final.Prerequisites.0=Healthy", path: "Final.Prerequisites.0", value: "Healthy"},
		{expression: "Description=", path: "Description", value: ""},
		{expression: "Labels.a~1b=c", path: "Labels.a~1b", value: "c"},
		{expression: "NoValue", wantErr: true},
		{expression: "=value", wantErr: true},
		{expression: "Stages.?Name", wantErr: true},
	}
	for _, test := range tests {
		path, value, err := splitSetExpression(test.expression)
		if test.wantErr {
			if err == nil {
				t.Errorf("%q was split into %q and %q", test.expression, path, value)
			}
			continue
		}
		if err != nil || path != test.path || value != test.value {
			t.Errorf("%q: got %q, %q, %v; want %q, %q", test.expression, path, value, err, test.path, test.value)
		}
	}
}

func asJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestApplyFieldEditsToChangeWorkflow(t *testing.T) {
	workflow := func() *goclientnew.ChangeWorkflow {
		return &goclientnew.ChangeWorkflow{
			Slug: "rollout",
			Stages: []goclientnew.ChangeWorkflowStage{
				{Name: "dev", WhereSpace: "Labels.Stage = 'dev'"},
				{Name: "prod", WhereSpace: "Labels.Stage = 'prod'", Prerequisites: []string{"Released"}},
			},
		}
	}
	tests := []struct {
		name         string
		sets, unsets []string
		check        func(t *testing.T, w *goclientnew.ChangeWorkflow)
	}{
		{
			name: "a field of a list element, by key",
			sets: []string{"Stages.?Name=prod.WhereSpace=Labels.Stage = 'prod' AND Labels.Region = 'us'"},
			check: func(t *testing.T, w *goclientnew.ChangeWorkflow) {
				if w.Stages[1].WhereSpace != "Labels.Stage = 'prod' AND Labels.Region = 'us'" || w.Stages[0].WhereSpace != "Labels.Stage = 'dev'" {
					t.Errorf("stages are %s", asJSON(t, w.Stages))
				}
				if !reflect.DeepEqual(w.Stages[1].Prerequisites, []string{"Released"}) {
					t.Errorf("the stage's other fields changed: %s", asJSON(t, w.Stages[1]))
				}
			},
		},
		{
			name: "a list of plain values",
			sets: []string{"Stages.?Name=prod.Prerequisites=Released; Healthy", "Final.Prerequisites=Healthy"},
			check: func(t *testing.T, w *goclientnew.ChangeWorkflow) {
				if !reflect.DeepEqual(w.Stages[1].Prerequisites, []string{"Released", "Healthy"}) {
					t.Errorf("prerequisites are %v", w.Stages[1].Prerequisites)
				}
				if !reflect.DeepEqual(w.Final.Prerequisites, []string{"Healthy"}) {
					t.Errorf("final prerequisites are %v", w.Final.Prerequisites)
				}
			},
		},
		{
			name: "an element that is not there is added, carrying its key",
			sets: []string{"Stages.?Name=canary.WhereSpace=Labels.Stage = 'canary'", "CustomPrerequisites.?Name=freeze={Expression: 'cel:true'}"},
			check: func(t *testing.T, w *goclientnew.ChangeWorkflow) {
				if len(w.Stages) != 3 || w.Stages[2].Name != "canary" || w.Stages[2].WhereSpace != "Labels.Stage = 'canary'" {
					t.Errorf("stages are %s", asJSON(t, w.Stages))
				}
				if len(w.CustomPrerequisites) != 1 || w.CustomPrerequisites[0].Name != "freeze" || w.CustomPrerequisites[0].Expression != "cel:true" {
					t.Errorf("custom prerequisites are %s", asJSON(t, w.CustomPrerequisites))
				}
			},
		},
		{
			name: "by position",
			sets: []string{"Stages.0.WhereSpace=Slug = 'dev'", "Stages.1.Prerequisites.1=Healthy"},
			check: func(t *testing.T, w *goclientnew.ChangeWorkflow) {
				if w.Stages[0].WhereSpace != "Slug = 'dev'" || !reflect.DeepEqual(w.Stages[1].Prerequisites, []string{"Released", "Healthy"}) {
					t.Errorf("stages are %s", asJSON(t, w.Stages))
				}
			},
		},
		{
			name:   "unset a field, and remove an element",
			unsets: []string{"Stages.?Name=prod.Prerequisites", "Stages.?Name=dev", "Stages.?Name=absent"},
			check: func(t *testing.T, w *goclientnew.ChangeWorkflow) {
				if len(w.Stages) != 1 || w.Stages[0].Name != "prod" || len(w.Stages[0].Prerequisites) != 0 {
					t.Errorf("stages are %s", asJSON(t, w.Stages))
				}
			},
		},
		{
			name: "maps and the entity's own fields",
			sets: []string{"Labels.team=platform", "DisplayName=Roll out"},
			check: func(t *testing.T, w *goclientnew.ChangeWorkflow) {
				if w.Labels["team"] != "platform" || w.DisplayName != "Roll out" || w.Slug != "rollout" {
					t.Errorf("workflow is %s", asJSON(t, w))
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			useFieldEditFlags(t, test.sets, test.unsets)
			w := workflow()
			if err := applyFieldEdits("ChangeWorkflow", w); err != nil {
				t.Fatal(err)
			}
			test.check(t, w)
		})
	}
}

func TestApplyFieldEditsConvertsToTheFieldsType(t *testing.T) {
	useFieldEditFlags(t, []string{
		"Columns.?Name=Unit~1Slug.GroupBy=true",
		"AttestationPrerequisites.?Name=two.Count=2",
	}, nil)
	view := &goclientnew.View{Columns: []goclientnew.Column{{Name: "Unit.Slug"}}}
	// The second edit names a field a View does not have, so nothing is applied.
	err := applyFieldEdits("View", view)
	if err == nil || !strings.Contains(err.Error(), `no field "AttestationPrerequisites"`) || !strings.Contains(err.Error(), "Columns") {
		t.Fatalf("the error does not name the field and what the View has: %v", err)
	}

	useFieldEditFlags(t, []string{"Columns.?Name=Unit~1Slug.GroupBy=true"}, nil)
	if err := applyFieldEdits("View", view); err != nil {
		t.Fatal(err)
	}
	if len(view.Columns) != 1 || !view.Columns[0].GroupBy {
		t.Errorf("columns are %s", asJSON(t, view.Columns))
	}

	useFieldEditFlags(t, []string{"AttestationPrerequisites.?Name=two.Count=2"}, nil)
	workflow := &goclientnew.ChangeWorkflow{}
	if err := applyFieldEdits("ChangeWorkflow", workflow); err != nil {
		t.Fatal(err)
	}
	if len(workflow.AttestationPrerequisites) != 1 || workflow.AttestationPrerequisites[0].Count != 2 {
		t.Errorf("attestation prerequisites are %s", asJSON(t, workflow.AttestationPrerequisites))
	}

	for _, bad := range []string{"Columns.?Name=x.GroupBy=perhaps", "Columns.GroupBy=true", "Columns.?Nome=x.GroupBy=true", "GroupBy.x=1"} {
		useFieldEditFlags(t, []string{bad}, nil)
		if err := applyFieldEdits("View", &goclientnew.View{}); err == nil {
			t.Errorf("--set %s was accepted", bad)
		}
	}
}

func TestFieldEditMembersKeepWhatAnElementHas(t *testing.T) {
	useFieldEditFlags(t, nil, nil,
		fieldEdit{path: "Columns", flag: "--column", memberKey: "Name", members: []string{"Unit.CreatedAt", "Unit.Slug"}},
		fieldEdit{path: elementPath("Columns", "Name", "Unit.Slug") + ".OrderByDirection", value: "ASC", flag: "--column-order-by-direction", mustExist: true},
	)
	view := &goclientnew.View{Columns: []goclientnew.Column{
		{Name: "Unit.Slug", GroupBy: true, OrderByDirection: "DESC"}, {Name: "Unit.Dropped"},
	}}
	if err := applyFieldEdits("View", view); err != nil {
		t.Fatal(err)
	}
	want := []goclientnew.Column{{Name: "Unit.CreatedAt"}, {Name: "Unit.Slug", GroupBy: true, OrderByDirection: "ASC"}}
	if !reflect.DeepEqual(view.Columns, want) {
		t.Errorf("columns are %s, want %s", asJSON(t, view.Columns), asJSON(t, want))
	}

	// A field flag naming an element the list does not have is a misspelling, not a new element.
	useFieldEditFlags(t, nil, nil,
		fieldEdit{path: elementPath("Columns", "Name", "Unit.Slig") + ".GroupBy", value: "true", flag: "--column-group-by", mustExist: true})
	err := applyFieldEdits("View", view)
	if err == nil || !strings.Contains(err.Error(), "has no element") {
		t.Errorf("an unknown element was accepted: %v", err)
	}
}

func TestFieldEditsInAPatch(t *testing.T) {
	previousPath := runningCommandPath
	runningCommandPath = viewUpdateCmd.CommandPath()
	t.Cleanup(func() { runningCommandPath = previousPath })

	useFieldEditFlags(t, []string{"GroupBy=Unit.Slug", "Labels.team=platform"}, []string{"OrderBy"})
	patch, err := BuildPatchDataWithPermissions(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fields := patchFields(t, patch)
	labels, _ := fields["Labels"].(map[string]any)
	if fields["GroupBy"] != "Unit.Slug" || labels["team"] != "platform" {
		t.Errorf("patch is %s", patch)
	}
	if orderBy, present := fields["OrderBy"]; !present || orderBy != nil {
		t.Errorf("--unset should send a null for OrderBy: %s", patch)
	}

	// One element of a list is beyond a merge patch, and the error says what to do instead.
	useFieldEditFlags(t, []string{"Columns.?Name=x.GroupBy=true"}, nil)
	_, err = BuildPatchDataWithPermissions(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "without --patch") {
		t.Errorf("an element edit in a patch was accepted: %v", err)
	}
}

// A reference is named in the document and held as an ID in the entity. The resolvers are
// replaced so that nothing is looked up.
func TestFieldEditResolvesReferences(t *testing.T) {
	filterID, workflowA, workflowB := uuid.New(), uuid.New(), uuid.New()
	previous := referenceResolvers
	referenceResolvers = map[string]func(string) (uuid.UUID, error){
		"Filter": func(ref string) (uuid.UUID, error) {
			if ref != "other-space/units" {
				t.Errorf("resolving %q", ref)
			}
			return filterID, nil
		},
		"ChangeWorkflow": func(ref string) (uuid.UUID, error) {
			return map[string]uuid.UUID{"a": workflowA, "ops/b": workflowB}[ref], nil
		},
	}
	t.Cleanup(func() { referenceResolvers = previous })

	useFieldEditFlags(t, []string{"Filter=other-space/units"}, nil)
	view := &goclientnew.View{}
	if err := applyFieldEdits("View", view); err != nil {
		t.Fatal(err)
	}
	if view.FilterID == nil || *view.FilterID != filterID {
		t.Errorf("FilterID is %v, want %s", view.FilterID, filterID)
	}

	useFieldEditFlags(t, []string{"AllowedChangeWorkflows=a; ops/b"}, nil)
	component := &goclientnew.Component{}
	if err := applyFieldEdits("Component", component); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(component.AllowedChangeWorkflowIDs, []uuid.UUID{workflowA, workflowB}) {
		t.Errorf("AllowedChangeWorkflowIDs is %v", component.AllowedChangeWorkflowIDs)
	}

	useFieldEditFlags(t, nil, []string{"Filter"})
	view.FilterID = &filterID
	if err := applyFieldEdits("View", view); err != nil {
		t.Fatal(err)
	}
	if view.FilterID != nil {
		t.Errorf("FilterID is still %v", view.FilterID)
	}
}

// Every command that takes --set writes a type that has a document, and every such type's create
// and update take it.
func TestFieldEditFlagsCoverTheDocumentTypes(t *testing.T) {
	covered := map[string]int{}
	for cmd, entityType := range fieldEditCommands {
		if _, err := entityDocumentSchema(entityType); err != nil {
			t.Errorf("%s: %v", cmd.CommandPath(), err)
		}
		if cmd.Flags().Lookup("set") == nil || cmd.Flags().Lookup("unset") == nil {
			t.Errorf("%s is registered without the flags", cmd.CommandPath())
		}
		covered[entityType]++
	}
	for _, entityType := range cubkit.DocumentSchemaTypes() {
		if covered[string(entityType)] != 2 {
			t.Errorf("%s has --set on %d commands, want its create and its update", entityType, covered[string(entityType)])
		}
	}
}
