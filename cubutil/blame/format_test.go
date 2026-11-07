// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

func TestAuthoredPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"a plain path is unchanged", "spec.replicas", "spec.replicas"},
		{"an associative segment loses its anchor",
			"spec.template.spec.containers.?name=server;@0.image",
			"spec.template.spec.containers.?name=server.image"},
		{"every associative segment loses its anchor",
			"spec.template.spec.containers.?name=server;@0.env.?name=PORT;@3.value",
			"spec.template.spec.containers.?name=server.env.?name=PORT.value"},
		{"a numeric index is not an anchor and stays",
			"spec.template.spec.containers.?name=server;@0.ports.0.containerPort",
			"spec.template.spec.containers.?name=server.ports.0.containerPort"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AuthoredPath(tt.path); got != tt.want {
				t.Errorf("AuthoredPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestFormatNeededAndProvided(t *testing.T) {
	bound := NeededPath{
		AttributeName: "container-image",
		Value:         "confighubplaceholder",
		Details: &goclientnew.AttributeDetails{
			BoundLinkID:     uuid.New(),
			Description:     "the image to run",
			NeededRequired:  map[string]string{"registry": "ghcr", "arch": "arm64"},
			NeededPreferred: map[string]string{"tier": "stable"},
		},
		BoundLink: "web-to-registry",
	}
	want := `container-image, bound by link web-to-registry: the image to run; requires arch=arm64,registry=ghcr; ` +
		`prefers tier=stable; placeholder "confighubplaceholder"`
	if got := FormatNeeded(bound); got != want {
		t.Errorf("FormatNeeded =\n  %s\nwant\n  %s", got, want)
	}
	if got := FormatNeeded(NeededPath{AttributeName: "port"}); got != "port, not bound by a link" {
		t.Errorf("unbound FormatNeeded = %q", got)
	}

	provided := goclientnew.AttributeInfo{
		AttributeName: "container-image",
		Details:       &goclientnew.AttributeDetails{ProvidedProperties: map[string]string{"registry": "ghcr"}},
	}
	if got := FormatProvided(provided); got != "container-image: offers registry=ghcr" {
		t.Errorf("FormatProvided = %q", got)
	}
}

func TestFormatGuardsIsSortedAndFlagShaped(t *testing.T) {
	// Rendered the way --guard takes them, and sorted, so a listing is diffable between runs.
	got := FormatGuards(map[string]string{"owner": "platform", "policy-exception": "pinned"})
	if got != "owner=platform policy-exception=pinned" {
		t.Errorf("unexpected rendering: %q", got)
	}
	if FormatGuards(nil) != "" {
		t.Error("no guards should render as nothing at all")
	}
}

func TestFormatValidation(t *testing.T) {
	warning := Validation{
		Gate:    "prod/replicas/vet-cel",
		Warning: true,
		Path:    "spec.template.spec.containers.0.image",
		Score:   "High",
		Issues:  []goclientnew.Issue{{Message: "image must be pinned"}},
	}
	if got, want := FormatValidation(warning),
		"warning prod/replicas/vet-cel (High) at spec.template.spec.containers.0.image: image must be pinned"; got != want {
		t.Errorf("FormatValidation = %q, want %q", got, want)
	}
	// A function that says nothing about a field, as vet-placeholders does, still says what
	// it found there.
	placeholder := Validation{Gate: "dev/placeholders/vet-placeholders", Value: "confighubplaceholder"}
	if got, want := FormatValidation(placeholder),
		`error dev/placeholders/vet-placeholders: found "confighubplaceholder"`; got != want {
		t.Errorf("FormatValidation = %q, want %q", got, want)
	}
}

func TestLettersAndFlagsUsedFollowTheLegend(t *testing.T) {
	// The server lists codes in its own order; the column prints letters in the legend's.
	if got := Letters(&Field{Flags: []string{"Unreleased", "Protected", "SetByLink"}}); got != "PLN" {
		t.Errorf("Letters = %q, want PLN", got)
	}
	// A code this version does not know is left out rather than printed wrongly.
	if got := Letters(&Field{Flags: []string{"SomethingNew", "Provided"}}); got != "p" {
		t.Errorf("Letters = %q, want p", got)
	}
	legend := FlagsUsed([]*Field{
		{Flags: []string{"SetByLink", "Unreleased", "Provided"}},
		{Flags: []string{"Protected"}},
		{Flags: []string{"Upgraded"}},
	})
	var letters []string
	for _, flag := range legend {
		letters = append(letters, flag.Letter)
	}
	if got := strings.Join(letters, ""); got != "PULNp" {
		t.Errorf("legend letters = %q, want PULNp in column order", got)
	}
}

func TestEveryFlagHasACodeAndALetter(t *testing.T) {
	codes, letters := map[string]bool{}, map[string]bool{}
	for _, flag := range FlagLegend {
		if flag.Code == "" || flag.Letter == "" || codes[flag.Code] || letters[flag.Letter] {
			t.Errorf("flag %+v is missing a code or letter, or repeats one", flag)
		}
		codes[flag.Code], letters[flag.Letter] = true, true
	}
}

func TestFormatConflictDismissal(t *testing.T) {
	for _, tt := range []struct {
		dismissal ConflictDismissal
		want      string
	}{
		{ConflictDismissal{Reason: "Subtracted", Path: "spec.replicas"}, "Subtracted, at spec.replicas"},
		{ConflictDismissal{Reason: "Guarded", Guard: "owner", GuardValue: "platform", Path: "spec.template.spec.containers.*.image"},
			"Guarded by guard owner=platform, at spec.template.spec.containers.*.image"},
		{ConflictDismissal{Reason: "Guarded", Guard: "owner"}, "Guarded by guard owner, on the resource as a whole"},
	} {
		if got := FormatConflictDismissal(tt.dismissal); got != tt.want {
			t.Errorf("FormatConflictDismissal(%+v) = %q, want %q", tt.dismissal, got, tt.want)
		}
	}
}

func TestFormatFieldGuardsMarksInheritedGuards(t *testing.T) {
	got := FormatFieldGuards(map[string]string{"Scale": "RegionSpecific", "RunAsRoot": "LegacyImage"}, []string{"RunAsRoot"})
	if want := "RunAsRoot=LegacyImage (inherited) Scale=RegionSpecific"; got != want {
		t.Errorf("FormatFieldGuards = %q, want %q", got, want)
	}
	if got := FormatFieldGuards(nil, []string{"RunAsRoot"}); got != "" {
		t.Errorf("FormatFieldGuards of no guards = %q, want empty", got)
	}
}
