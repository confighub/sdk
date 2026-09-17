// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// TestReferenceCompletionsNameRealCommands pins every refArgKinds entry to a
// command that exists under the name the table uses, and checks that installing
// gave each one a completer. A renamed command would otherwise silently lose
// its completion.
func TestReferenceCompletionsNameRealCommands(t *testing.T) {
	installReferenceCompletions(rootCmd)
	for path := range refArgKinds {
		cmd := findCommand(t, path)
		if cmd.ValidArgsFunction == nil {
			t.Errorf("%q is in refArgKinds but has no ValidArgsFunction after install", path)
		}
	}
}

// TestReferenceFlagsRegistered checks the tree walk reached the flags the map
// names, on a persistent flag defined on a parent and on a local one.
func TestReferenceFlagsRegistered(t *testing.T) {
	installReferenceCompletions(rootCmd)
	for _, tc := range []struct{ path, flag string }{
		{"unit get", "space"},
		{"unit list", "filter"},
		{"unit create", "target"},
		{"unit diff", "with-unit"},
		{"function do", "unit"},
	} {
		if _, ok := findCommand(t, tc.path).GetFlagCompletionFunc(tc.flag); !ok {
			t.Errorf("%q --%s has no completion function", tc.path, tc.flag)
		}
	}
	if _, ok := findCommand(t, "worker install").GetFlagCompletionFunc("unit"); ok {
		t.Error("worker install --unit names a unit to create, so it must not complete existing units")
	}
}

func fakeCompleter(spaceFlag string) (*refCompleter, *refKind) {
	ids := map[string]goclientnew.UUID{
		"alpha": goclientnew.UUID{1},
		"beta":  goclientnew.UUID{2},
	}
	units := map[goclientnew.UUID][]string{
		{1}: {"api", "app"},
		{2}: {"app", "batch"},
		{}:  {"api", "app", "app", "batch"},
	}
	c := &refCompleter{
		ctx:       context.Background(),
		spaceFlag: spaceFlag,
		spaces:    func(context.Context) ([]string, error) { return []string{"alpha", "beta"}, nil },
		spaceID: func(_ context.Context, ref string) (goclientnew.UUID, error) {
			id, ok := ids[ref]
			if !ok {
				return goclientnew.UUID{}, fmt.Errorf("space %q not found", ref)
			}
			return id, nil
		},
	}
	kind := &refKind{name: "unit", slugs: func(_ context.Context, spaceID goclientnew.UUID) ([]string, error) {
		return units[spaceID], nil
	}}
	return c, kind
}

func TestCompleteReference(t *testing.T) {
	noFile := cobra.ShellCompDirectiveNoFileComp
	for _, tc := range []struct {
		name, spaceFlag, toComplete string
		want                        []string
		directive                   cobra.ShellCompDirective
	}{
		{"spaces first, held open", "", "", []string{"alpha/", "beta/"}, noFile | cobra.ShellCompDirectiveNoSpace},
		{"space prefix narrows", "", "b", []string{"beta/"}, noFile | cobra.ShellCompDirectiveNoSpace},
		{"slugs in the named space", "", "alpha/", []string{"alpha/api", "alpha/app"}, noFile},
		{"slug prefix narrows", "", "beta/b", []string{"beta/batch"}, noFile},
		{"organization-wide", "", "*/ba", []string{"*/batch"}, noFile},
		{"unknown space completes to nothing", "", "gamma/a", nil, noFile},
		{"--space gives bare slugs", "beta", "", []string{"app", "batch"}, noFile},
		{"--space with a qualified word still honors the word", "beta", "alpha/ap", []string{"alpha/api", "alpha/app"}, noFile},
		{"--space '*' is no space", "*", "a", []string{"alpha/"}, noFile | cobra.ShellCompDirectiveNoSpace},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, kind := fakeCompleter(tc.spaceFlag)
			got, directive := c.complete(kind, tc.toComplete)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("candidates = %v, want %v", got, tc.want)
			}
			if directive != tc.directive {
				t.Errorf("directive = %v, want %v", directive, tc.directive)
			}
		})
	}
}

// TestCompleteSpaceReference: a space is not itself space-resident, so it
// completes flat, whatever --space says.
func TestCompleteSpaceReference(t *testing.T) {
	c, _ := fakeCompleter("beta")
	got, directive := c.complete(kindSpace, "al")
	if !reflect.DeepEqual(got, []string{"alpha"}) || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("got %v, %v", got, directive)
	}
}

// TestCompleteWithoutClient: a completer that could not reach a context offers
// nothing rather than failing, since a completion failure is invisible.
func TestCompleteWithoutClient(t *testing.T) {
	got, directive := (&refCompleter{}).complete(kindUnit, "a")
	if got != nil || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("got %v, %v", got, directive)
	}
}

// TestCompletionScriptsHideQualifier pins the anchors the qualifier-hiding patch
// is inserted at. A cobra upgrade that rewrites either script fails here rather
// than silently shipping lists that repeat the space on every line.
func TestCompletionScriptsHideQualifier(t *testing.T) {
	installCompletionCommand(rootCmd)
	for _, shell := range []string{"zsh", "bash"} {
		var buf bytes.Buffer
		cmd := findCommand(t, "completion "+shell)
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("%s: %v", shell, err)
		}
		if !strings.Contains(buf.String(), "Hiding qualifier") {
			t.Errorf("%s completion script was not patched; cobra's script no longer contains the anchor", shell)
		}
		if strings.Contains(buf.String(), "warning:") {
			t.Errorf("%s: %s", shell, buf.String()[:200])
		}
	}
}
