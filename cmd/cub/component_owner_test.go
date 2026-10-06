// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// ownerSpaces builds the Spaces of a Component, one per Owner label value; "" is a Space with
// no Owner label.
func ownerSpaces(owners ...string) []*goclientnew.ExtendedSpace {
	spaces := make([]*goclientnew.ExtendedSpace, 0, len(owners))
	for _, owner := range owners {
		labels := map[string]string{labelVariant: "v"}
		if owner != "" {
			labels[labelOwner] = owner
		}
		spaces = append(spaces, &goclientnew.ExtendedSpace{Space: &goclientnew.Space{Labels: labels}})
	}
	return spaces
}

func ownerLabels(owner string) map[string]string {
	if owner == "" {
		return nil
	}
	return map[string]string{labelOwner: owner}
}

func TestComponentOwner(t *testing.T) {
	tests := []struct {
		name           string
		componentOwner string
		spaceOwners    []string
		want           string
	}{
		{name: "component label set", componentOwner: "platform", spaceOwners: []string{"platform"}, want: "platform"},
		{name: "component label set, no spaces", componentOwner: "platform", want: "platform"},
		{name: "all spaces agree", spaceOwners: []string{"web", "web", "web"}, want: "web"},
		{name: "spaces disagree", spaceOwners: []string{"web", "api"}, want: ""},
		{name: "one space missing owner", spaceOwners: []string{"web", ""}, want: ""},
		{name: "first space missing owner", spaceOwners: []string{"", "web"}, want: ""},
		{name: "no spaces", want: ""},
		{name: "component label beats disagreeing spaces", componentOwner: "platform", spaceOwners: []string{"web", "api"}, want: "platform"},
		{name: "component label beats agreeing spaces", componentOwner: "platform", spaceOwners: []string{"web", "web"}, want: "platform"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := componentOwner(ownerLabels(tt.componentOwner), ownerSpaces(tt.spaceOwners...))
			if got != tt.want {
				t.Errorf("componentOwner() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOwnerWrite(t *testing.T) {
	tests := []struct {
		name           string
		componentOwner string
		spaceOwners    []string
		requested      string
		wantSet        bool
		wantErr        []string
	}{
		{name: "nothing requested", componentOwner: "platform", requested: "", wantSet: false},
		{name: "unset, no spaces", requested: "web", wantSet: true},
		{name: "unset, spaces disagree", spaceOwners: []string{"web", "api"}, requested: "web", wantSet: true},
		{name: "unset, one space missing owner", spaceOwners: []string{"web", ""}, requested: "api", wantSet: true},
		{name: "same, from spaces, component label missing", spaceOwners: []string{"web", "web"}, requested: "web", wantSet: true},
		{name: "same, component label present", componentOwner: "web", spaceOwners: []string{"api"}, requested: "web", wantSet: false},
		{
			name: "different component label", componentOwner: "web", requested: "api",
			wantErr: []string{`already has owner "web"`, `--owner "api"`, "cub component update --patch my-app --label Owner=api"},
		},
		{
			name: "different, from spaces", spaceOwners: []string{"web", "web"}, requested: "api",
			wantErr: []string{`already has owner "web"`, "cub component update --patch my-app --label Owner=api"},
		},
		{
			name: "different, quoted in the hint", componentOwner: "web", requested: "Team A",
			wantErr: []string{"cub component update --patch my-app --label 'Owner=Team A'"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set, err := ownerWrite("my-app", ownerLabels(tt.componentOwner), ownerSpaces(tt.spaceOwners...), tt.requested)
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatalf("ownerWrite() = %v, nil; want an error", set)
				}
				for _, want := range tt.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("ownerWrite() error %q does not contain %q", err, want)
					}
				}
				if set {
					t.Errorf("ownerWrite() set = true with an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ownerWrite() error = %v", err)
			}
			if set != tt.wantSet {
				t.Errorf("ownerWrite() set = %v, want %v", set, tt.wantSet)
			}
		})
	}
}

func TestValidateOwnerValue(t *testing.T) {
	valid := []string{"Engineering", "Team A", "team-web", "a", "lead@example.com", "team/web.1", "(core) [infra] {ops} <x>|y",
		"a-_@/#$%&~+=!?.,:;b", strings.Repeat("a", ownerValueMaxLength)}
	for _, owner := range valid {
		if err := validateOwnerValue(owner); err != nil {
			t.Errorf("validateOwnerValue(%q) = %v, want nil", owner, err)
		}
	}
	invalid := []string{"", "Team*", " lead", "lead ", "it's", `a"b`, "a`b", `a\b`, "a^b", "tab\there", "line\nbreak", "caf\u00e9",
		strings.Repeat("a", ownerValueMaxLength+1)}
	for _, owner := range invalid {
		err := validateOwnerValue(owner)
		if err == nil {
			t.Errorf("validateOwnerValue(%q) = nil, want an error", owner)
			continue
		}
		if !strings.Contains(err.Error(), "not a valid label value") {
			t.Errorf("validateOwnerValue(%q) error %q does not say the value is not a valid label value", owner, err)
		}
	}
}

// TestOwnerRecheck covers the second ownerWrite that writeComponentOwner makes right before the
// write, on a Component another writer changed after the first check.
func TestOwnerRecheck(t *testing.T) {
	set, err := ownerWrite("my-app", nil, ownerSpaces("web", "api"), "web")
	if err != nil || !set {
		t.Fatalf("first check: ownerWrite() = %v, %v; want true, nil", set, err)
	}

	// Another writer set a different owner: the re-check refuses, and the error after the
	// variant was written gives the command that changes the owner once.
	set, err = ownerWrite("my-app", ownerLabels("api"), ownerSpaces("web", "api"), "web")
	if set {
		t.Errorf("re-check after a different owner was set: set = true")
	}
	var conflict *ownerConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("re-check after a different owner was set: error = %v, want an ownerConflictError", err)
	}
	msg := componentOwnerNotSet("variant space my-app-prod", "my-app", "web", err).Error()
	hint := "cub component update --patch my-app --label Owner=web"
	for _, want := range []string{"variant space my-app-prod exists", `already has owner "api"`, hint} {
		if !strings.Contains(msg, want) {
			t.Errorf("componentOwnerNotSet() = %q, does not contain %q", msg, want)
		}
	}
	if n := strings.Count(msg, hint); n != 1 {
		t.Errorf("componentOwnerNotSet() gives the hint %d times, want once: %q", n, msg)
	}

	// Another writer set the same owner: the re-check passes and writes nothing.
	set, err = ownerWrite("my-app", ownerLabels("web"), ownerSpaces("web", "api"), "web")
	if err != nil || set {
		t.Errorf("re-check after the same owner was set: ownerWrite() = %v, %v; want false, nil", set, err)
	}

	// Any other failure keeps the hint.
	msg = componentOwnerNotSet("the uploaded variant", "my-app", "web", errors.New("forbidden")).Error()
	if !strings.Contains(msg, "forbidden") || !strings.Contains(msg, "set it with: "+hint) {
		t.Errorf("componentOwnerNotSet() = %q, want the cause and the hint", msg)
	}
}

func TestComponentOwnerSkipped(t *testing.T) {
	cause := errors.New("target dev/target not found")
	err := componentOwnerSkipped(cause, "my-app", "Team A")
	if !errors.Is(err, cause) {
		t.Errorf("componentOwnerSkipped() does not wrap the cause")
	}
	for _, want := range []string{"target dev/target not found", "Owner label of component my-app was not set",
		"cub component update --patch my-app --label 'Owner=Team A'"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("componentOwnerSkipped() = %q, does not contain %q", err, want)
		}
	}
}

func TestShellQuoteArg(t *testing.T) {
	tests := map[string]string{
		"Owner=web":        "Owner=web",
		"Owner=Team A":     "'Owner=Team A'",
		"Owner=it's":       `'Owner=it'\''s'`,
		"":                 "''",
		"Owner=a$b":        "'Owner=a$b'",
		"Owner=team/web.1": "Owner=team/web.1",
	}
	for in, want := range tests {
		if got := shellQuoteArg(in); got != want {
			t.Errorf("shellQuoteArg(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUploadSpaceLabels(t *testing.T) {
	labels, err := uploadSpaceLabels(&variantUploadOptions{
		variant:     "prod",
		stage:       "Canary",
		environment: "Prod",
		region:      "us-east1",
		layer:       "App",
		owner:       "platform",
		spaceLabels: []string{"tier=web"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"Variant": "prod", "Stage": "Canary", "Environment": "Prod", "Region": "us-east1", "Layer": "App", "tier": "web",
	}
	if len(labels) != len(want) {
		t.Errorf("uploadSpaceLabels() = %v, want %v", labels, want)
	}
	for k, v := range want {
		if labels[k] != v {
			t.Errorf("uploadSpaceLabels()[%q] = %q, want %q", k, labels[k], v)
		}
	}
	if _, ok := labels[labelOwner]; ok {
		t.Errorf("uploadSpaceLabels() carries Owner %q; --owner belongs to the Component", labels[labelOwner])
	}

	if _, err := uploadSpaceLabels(&variantUploadOptions{variant: "base", spaceLabels: []string{"tier"}}); err == nil {
		t.Error("uploadSpaceLabels() accepted a --space-label with no value")
	}
}
