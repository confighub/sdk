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
		{"unit list", "variant"},
		{"function set", "variant"},
		{"function set", "resource"},
		{"function exec", "resource"},
	} {
		if _, ok := findCommand(t, tc.path).GetFlagCompletionFunc(tc.flag); !ok {
			t.Errorf("%q --%s has no completion function", tc.path, tc.flag)
		}
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

// TestVariantLabelFlagsDoNotCompleteAsVariants: the --variant of "cub space create" and
// "cub variant upload" is a label value, not a <component>/<variant> reference, so the
// completer attached to the scope flag must not reach them.
func TestVariantLabelFlagsDoNotCompleteAsVariants(t *testing.T) {
	installReferenceCompletions(rootCmd)
	for _, path := range []string{"space create", "space update", "space list", "variant upload", "component open"} {
		if _, ok := findCommand(t, path).GetFlagCompletionFunc("variant"); ok {
			t.Errorf("%q --variant takes a label value but has a completion function", path)
		}
	}
}

func fakeScopeCompleter(spaceFlag, variantFlag string) *refCompleter {
	c, _ := fakeCompleter(spaceFlag)
	c.variantFlag = variantFlag
	c.components = func(context.Context) ([]string, error) { return []string{"api", "web"}, nil }
	c.variants = func(_ context.Context, component string) ([]string, error) {
		switch component {
		case "web":
			return []string{"base", "dev", "prod"}, nil
		case "api":
			return []string{"base"}, nil
		}
		return nil, fmt.Errorf("component %q not found", component)
	}
	c.variantSpaceID = func(_ context.Context, ref string) (goclientnew.UUID, error) {
		if ref == "web/dev" {
			return goclientnew.UUID{2}, nil
		}
		return goclientnew.UUID{}, fmt.Errorf("variant %q not found", ref)
	}
	resources := map[goclientnew.UUID][]resourceCandidate{
		{1}: {{"Deployment", "backend"}, {"Service", "backend"}},
		{2}: {{"Deployment", "backend"}, {"Deployment", "frontend"}, {"Namespace", "shop"}},
		{}:  {{"Deployment", "backend"}, {"Service", "backend"}, {"Deployment", "backend"}, {"Deployment", "frontend"}, {"Namespace", "shop"}},
	}
	c.resources = func(_ context.Context, spaceID goclientnew.UUID) ([]resourceCandidate, error) {
		return resources[spaceID], nil
	}
	return c
}

func TestCompleteVariant(t *testing.T) {
	noFile := cobra.ShellCompDirectiveNoFileComp
	for _, tc := range []struct {
		name, toComplete string
		want             []string
		directive        cobra.ShellCompDirective
	}{
		{"components first, held open", "", []string{"api/", "web/"}, noFile | cobra.ShellCompDirectiveNoSpace},
		{"component prefix narrows", "w", []string{"web/"}, noFile | cobra.ShellCompDirectiveNoSpace},
		{"variants of the named component", "web/", []string{"web/base", "web/dev", "web/prod"}, noFile},
		{"variant prefix narrows", "web/p", []string{"web/prod"}, noFile},
		{"unknown component completes to nothing", "db/", nil, noFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, directive := fakeScopeCompleter("", "").completeVariant(tc.toComplete)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("candidates = %v, want %v", got, tc.want)
			}
			if directive != tc.directive {
				t.Errorf("directive = %v, want %v", directive, tc.directive)
			}
		})
	}
	if got, _ := (&refCompleter{}).completeVariant("w"); got != nil {
		t.Errorf("no context completed to %v", got)
	}
}

// TestCompleteVariantOrSpace: a variant argument offers the space slugs and the components
// held open, then a component's variants once one is typed.
func TestCompleteVariantOrSpace(t *testing.T) {
	noFile := cobra.ShellCompDirectiveNoFileComp
	for _, tc := range []struct {
		name, toComplete string
		want             []string
		directive        cobra.ShellCompDirective
	}{
		{"spaces and components, the latter held open", "", []string{"alpha", "api/", "beta", "web/"}, noFile | cobra.ShellCompDirectiveNoSpace},
		{"a prefix matching only spaces", "al", []string{"alpha"}, noFile},
		{"a prefix matching only components", "w", []string{"web/"}, noFile | cobra.ShellCompDirectiveNoSpace},
		{"variants of the typed component", "web/d", []string{"web/dev"}, noFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, directive := fakeScopeCompleter("", "").complete(kindVariantOrSpace, tc.toComplete)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("candidates = %v, want %v", got, tc.want)
			}
			if directive != tc.directive {
				t.Errorf("directive = %v, want %v", directive, tc.directive)
			}
		})
	}
}

func TestCompleteResource(t *testing.T) {
	noFile := cobra.ShellCompDirectiveNoFileComp
	for _, tc := range []struct {
		name, spaceFlag, variantFlag, toComplete string
		want                                     []string
		directive                                cobra.ShellCompDirective
	}{
		{"kinds first, held open, once each", "", "", "", []string{"Deployment/", "Namespace/", "Service/"}, noFile | cobra.ShellCompDirectiveNoSpace},
		{"kind prefix narrows", "", "", "Dep", []string{"Deployment/"}, noFile | cobra.ShellCompDirectiveNoSpace},
		{"names of the kind, once each", "", "", "Deployment/", []string{"Deployment/backend", "Deployment/frontend"}, noFile},
		{"the kind is matched in any case and kept as typed", "", "", "deployment/f", []string{"deployment/frontend"}, noFile},
		{"a namespace is typed by hand", "", "", "Deployment/shop/", nil, noFile},
		{"--space scopes", "alpha", "", "", []string{"Deployment/", "Service/"}, noFile | cobra.ShellCompDirectiveNoSpace},
		{"--variant scopes", "", "web/dev", "Deployment/", []string{"Deployment/backend", "Deployment/frontend"}, noFile},
		{"an unknown scope completes to nothing", "", "web/qa", "", nil, noFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, directive := fakeScopeCompleter(tc.spaceFlag, tc.variantFlag).completeResource(tc.toComplete)
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
