// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

func TestCheckEntityInputReportsWhatWouldNotBeWritten(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string // each a line of the error; none means the input passes
	}{
		{
			name: "fields the schema has, through lists and maps",
			input: `
ManualBindings:
  - AttributeName: resource-name
    NeededResource: {ResourceType: apps/v1/Deployment, ResourceName: /web}
    NeededPath: spec.replicas
Labels: {app.kubernetes.io/name: web}
Permissions: {View: {UserIDs: {5d1e2a6c-1c55-4a8e-9a2b-0e1c2d3e4f50: true}}}
`,
		},
		{
			name: "a field a list element does not have",
			input: `
ManualBindings:
  - AttributeName: resource-name
    AutoUpdate: false
`,
			want: []string{`ManualBindings.0.AutoUpdate: Binding has no field "AutoUpdate"`},
		},
		{
			name:  "a field in a map value",
			input: `Permissions: {View: {UserIds: {}}}`,
			want:  []string{`Permissions.View.UserIds: Subjects has no field "UserIds"; did you mean "UserIDs"?`},
		},
		{
			name:  "a misspelled field",
			input: `ManualBinding: []`,
			want:  []string{`ManualBinding: Link has no field "ManualBinding"; did you mean "ManualBindings"?`},
		},
		{
			name:  "a field only the server sets",
			input: `Bindings: [{AttributeName: resource-name}]`,
			want:  []string{"Bindings: set by the server, so a write ignores it"},
		},
		{
			name:  "a field only the server sets, in an entity read from the server",
			input: `{LinkID: 5d1e2a6c-1c55-4a8e-9a2b-0e1c2d3e4f50, Bindings: [], CreatedAt: "2026-01-01T00:00:00Z"}`,
		},
		{
			name:  "every problem, in path order",
			input: `{Zzz: 1, Bindings: [], Aaa: 2}`,
			want: []string{
				`Aaa: Link has no field "Aaa"`,
				"Bindings: set by the server",
				`Zzz: Link has no field "Zzz"`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mergeEntityWithData(&goclientnew.Link{}, []byte(tt.input))
			if len(tt.want) == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("no error, want %q", tt.want)
			}
			lines := strings.Split(err.Error(), "\n")
			if len(lines) != len(tt.want)+2 {
				t.Fatalf("error has %d lines, want a heading, %d problems and a hint:\n%v", len(lines), len(tt.want), err)
			}
			for i, want := range tt.want {
				if !strings.HasPrefix(strings.TrimSpace(lines[i+1]), want) {
					t.Errorf("problem %d is %q, want it to start %q", i, lines[i+1], want)
				}
			}
			if !strings.Contains(lines[len(lines)-1], "cub link explain") {
				t.Errorf("hint %q does not name cub link explain", lines[len(lines)-1])
			}
		})
	}
}

func TestCheckPatchInput(t *testing.T) {
	saved := runningCommandPath
	t.Cleanup(func() { runningCommandPath = saved })
	runningCommandPath = "cub link update"

	patch, err := checkPatchInput([]byte("ManualBindings:\n  - AttributeName: resource-name\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(patch), `{"ManualBindings":[{"AttributeName":"resource-name"}]}`; got != want {
		t.Errorf("YAML patch became %s, want %s", got, want)
	}

	json := []byte(`{"Labels": {"tier": null}}`)
	if patch, err := checkPatchInput(json); err != nil || string(patch) != string(json) {
		t.Errorf("JSON patch became %s, %v, want it unchanged", patch, err)
	}

	if _, err := checkPatchInput([]byte(`{"ManualBinding": []}`)); err == nil ||
		!strings.Contains(err.Error(), `did you mean "ManualBindings"`) {
		t.Errorf("patch with an unknown field: %v", err)
	}
}

// Every command that reads an entity from --from-stdin or --filename has to know the entity's
// schema to check it against. A patch learns it from the explain command beside it.
func TestEntityInputCommandsHaveASchema(t *testing.T) {
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd.Flags().Lookup("from-stdin") != nil && cmd.HasParent() {
			if _, ok := explainSchemas[cmd.Parent()]; !ok {
				t.Errorf("%s reads an entity, but %s has no explain command naming its schema",
					cmd.CommandPath(), cmd.Parent().CommandPath())
			}
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(rootCmd)
}
