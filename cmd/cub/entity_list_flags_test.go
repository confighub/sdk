// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/confighub/sdk/core/configkit/cubkit"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

func TestKebabCase(t *testing.T) {
	for name, want := range map[string]string{
		"OrderByDirection":     "order-by-direction",
		"FromUserIDs":          "from-user-ids",
		"MaxAge":               "max-age",
		"Type":                 "type",
		"WhereSpace":           "where-space",
		"ReleasePrerequisites": "release-prerequisites",
		"FilterID":             "filter-id",
	} {
		if got := kebabCase(name); got != want {
			t.Errorf("%s is %s, want %s", name, got, want)
		}
	}
}

// The flags for a list are made from the entity's document schema, so a field added to an element
// gets its flag without anyone writing one. This pins what they are today.
func TestListFlagsAreMadeFromTheSchema(t *testing.T) {
	for path, want := range map[string][]string{
		"view create":           {"column", "column-data-type", "column-type", "column-group-by", "column-order-by-direction"},
		"view update":           {"column", "column-data-type", "column-type", "column-group-by", "column-order-by-direction"},
		"changeworkflow create": {"stage", "stage-where-space", "stage-prerequisites", "stage-release-prerequisites", "final-prerequisites", "custom-prerequisite", "custom-prerequisite-description", "attestation-prerequisite", "attestation-prerequisite-count", "attestation-prerequisite-type", "attestation-prerequisite-from-user-ids", "attestation-prerequisite-max-age", "attestation-prerequisite-allow-authors", "attestation-prerequisite-ignore-fail"},
		"changeworkflow update": {"stage", "stage-where-space", "stage-prerequisites", "stage-release-prerequisites", "final-prerequisites", "custom-prerequisite", "attestation-prerequisite", "attestation-prerequisite-count"},
		"link create":           {"upstream-path", "downstream-path", "manual-binding", "upstream-getter", "downstream-setter"},
		"link update":           {"upstream-path", "downstream-path", "manual-binding", "upstream-getter", "downstream-setter"},
		"invocation create":     {"function", "parameter"},
		"invocation update":     {"function", "parameter"},
		"attribute create":      {"parameter"},
		"attribute update":      {"parameter"},
	} {
		cmd := findCommand(t, path)
		for _, flag := range want {
			if cmd.Flags().Lookup(flag) == nil {
				t.Errorf("cub %s has no --%s", path, flag)
			}
		}
	}
	// A field that is an object is left to --set.
	if findCommand(t, "view update").Flags().Lookup("column-source") != nil {
		t.Error("--column-source would have to hold an object")
	}
}

// The key each family is told a list's elements are identified by is the list's merge key.
func TestListFlagKeysAreTheMergeKeys(t *testing.T) {
	specs, err := cubkit.BuiltinSpecSet()
	if err != nil {
		t.Fatal(err)
	}
	mergeKeys := map[string]string{}
	for _, resourceType := range specs.ResourceTypes {
		for _, mergeKey := range resourceType.MergeKeys {
			mergeKeys[string(resourceType.Type)+"."+mergeKey.Path] = mergeKey.Key
		}
	}
	for _, flags := range []*listFlags{viewCreateColumns, viewUpdateColumns,
		changeworkflowCreateLists.stages, changeworkflowCreateLists.custom, changeworkflowCreateLists.attestation,
		changeworkflowUpdateLists.stages, changeworkflowUpdateLists.custom, changeworkflowUpdateLists.attestation} {
		list := flags.spec.entityType + "." + flags.spec.list
		if mergeKeys[list] != flags.spec.key {
			t.Errorf("%s is keyed by %q in its flags and by %q in the spec", list, flags.spec.key, mergeKeys[list])
		}
	}
}

func TestListFlagEdits(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	stages := []string{}
	flags := addChangeWorkflowListFlags(cmd, &stages)
	if err := cmd.Flags().Parse([]string{
		"--stage-where-space", "prod=Labels.Stage = 'prod' AND Labels.Region = 'us'",
		"--stage-prerequisites", "prod=Released; Healthy",
		"--stage-prerequisites", "dev=-",
		"--final-prerequisites", "Healthy",
		"--custom-prerequisite", "freeze=cel:Space.Annotations['code-freeze'] != 'true'",
		"--attestation-prerequisite", "two-approvers",
		"--attestation-prerequisite-count", "two-approvers=2",
		"--attestation-prerequisite-allow-authors", "two-approvers=true",
	}); err != nil {
		t.Fatal(err)
	}
	stages = append(stages, "dev", "prod", "canary")

	useFieldEditFlags(t, nil, nil)
	if err := flags.queue(true, []string{"Released"}); err != nil {
		t.Fatal(err)
	}
	workflow := &goclientnew.ChangeWorkflow{
		Stages: []goclientnew.ChangeWorkflowStage{
			{Name: "prod", WhereSpace: "Slug = 'p'", ReleasePrerequisites: []string{"release-manager"}},
			{Name: "dev", WhereSpace: "Slug = 'd'", Prerequisites: []string{"Old"}},
			{Name: "gone"},
		},
		CustomPrerequisites: []goclientnew.ChangeWorkflowPrerequisite{{Name: "old", Expression: "cel:true"}},
	}
	if err := applyFieldEdits("ChangeWorkflow", workflow); err != nil {
		t.Fatal(err)
	}

	wantStages := []goclientnew.ChangeWorkflowStage{
		// Named again: kept, with its gates cleared by the field flag after --prerequisites set them.
		{Name: "dev", WhereSpace: "Slug = 'd'"},
		// Named again: its other fields are kept, and the field flags win over --prerequisites.
		{Name: "prod", WhereSpace: "Labels.Stage = 'prod' AND Labels.Region = 'us'", Prerequisites: []string{"Released", "Healthy"},
			ReleasePrerequisites: []string{"release-manager"}},
		// New: it selects the Spaces labeled with its name, and takes --prerequisites.
		{Name: "canary", WhereSpace: "Labels.Stage = 'canary'", Prerequisites: []string{"Released"}},
	}
	if !reflect.DeepEqual(workflow.Stages, wantStages) {
		t.Errorf("stages are\n%s\nwant\n%s", asJSON(t, workflow.Stages), asJSON(t, wantStages))
	}
	if workflow.Final == nil || !reflect.DeepEqual(workflow.Final.Prerequisites, []string{"Healthy"}) {
		t.Errorf("final is %s", asJSON(t, workflow.Final))
	}
	wantCustom := []goclientnew.ChangeWorkflowPrerequisite{{Name: "freeze", Expression: "cel:Space.Annotations['code-freeze'] != 'true'"}}
	if !reflect.DeepEqual(workflow.CustomPrerequisites, wantCustom) {
		t.Errorf("custom prerequisites are %s", asJSON(t, workflow.CustomPrerequisites))
	}
	if len(workflow.AttestationPrerequisites) != 1 || workflow.AttestationPrerequisites[0].Count != 2 || !workflow.AttestationPrerequisites[0].AllowAuthors {
		t.Errorf("attestation prerequisites are %s", asJSON(t, workflow.AttestationPrerequisites))
	}
}

func TestListFieldFlagNeedsAnElement(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	columns := []string{}
	flags := addListFlags(cmd, viewColumnFlags(&columns))
	if err := cmd.Flags().Parse([]string{"--column-group-by", "Unit.Slug"}); err != nil {
		t.Fatal(err)
	}
	useFieldEditFlags(t, nil, nil)
	if err := flags.queue(); err == nil || !strings.Contains(err.Error(), "<name>=<value>") {
		t.Errorf("a field flag with no element was accepted: %v", err)
	}
}
