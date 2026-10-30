// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package profiles

import (
	"testing"

	api "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var lib = Library{
	Tool:   "autoscale-manager",
	Noun:   "autoscaling profile",
	Target: "an HPA Unit",
}

// The install and list commands have to agree on the annotation key, or a listed
// profile shows no description. One key for every tool keeps them agreeing even
// across tools sharing the library Space.
func TestDescriptionAnnotationIsShared(t *testing.T) {
	if DescriptionAnnotation != "Description" {
		t.Fatalf("annotation key = %q", DescriptionAnnotation)
	}
	other := Library{Tool: "workload-manager", Target: "a workload"}
	inv := other.buildInvocation(goclientnew.UUID{}, Spec{Slug: "x", Description: "theirs"})
	// A profile another tool installed describes itself to this one.
	if got := inv.Annotations[DescriptionAnnotation]; got != "theirs" {
		t.Errorf("cross-tool description = %q", got)
	}
}

func TestBuildInvocation(t *testing.T) {
	spaceID := goclientnew.UUID{}
	inv := lib.buildInvocation(spaceID, Spec{
		Slug:        "hpa-range",
		Description: "set minReplicas/maxReplicas",
		Function:    "set-yq",
		Args:        []api.FunctionArgument{Arg("yq-expression", ".spec.minReplicas = 1")},
		Params:      []Param{{Name: "min"}, {Name: "max", DataType: "int"}},
	})

	if inv.Slug != "hpa-range" || inv.DisplayName != "hpa-range" {
		t.Errorf("slug/displayName = %q/%q", inv.Slug, inv.DisplayName)
	}
	if inv.Annotations[DescriptionAnnotation] != "set minReplicas/maxReplicas" {
		t.Errorf("description annotation = %v", inv.Annotations)
	}
	if inv.ToolchainType != "Kubernetes/YAML" {
		t.Errorf("toolchain = %q", inv.ToolchainType)
	}
	// A parameter with no DataType is a string, and every parameter is required:
	// a profile applied without one would substitute an empty value rather than
	// fail, and write that into the config.
	if len(inv.Parameters) != 2 {
		t.Fatalf("parameters = %+v", inv.Parameters)
	}
	if inv.Parameters[0].DataType != "string" || !inv.Parameters[0].Required {
		t.Errorf("min = %+v, want a required string", inv.Parameters[0])
	}
	if inv.Parameters[1].DataType != "int" {
		t.Errorf("max DataType = %q, want the declared int", inv.Parameters[1].DataType)
	}
}

// A profile parameter is substituted at apply time, so its argument has to carry
// the template evaluator; a plain value would be written literally.
func TestParamArgsAreTemplates(t *testing.T) {
	tmpl := TmplArg("container-name", "container")
	if tmpl.Value != "{{ .Params.container }}" || tmpl.Evaluator != api.EvaluatorTemplate {
		t.Errorf("TmplArg = %+v", tmpl)
	}
	// set-yq takes its parameters through a `param` varArg as name=value, not as
	// named arguments, so the whole binding is the value.
	yq := YQParamArg("min")
	if yq.ParameterName != "param" || yq.Value != "min={{ .Params.min }}" || yq.Evaluator != api.EvaluatorTemplate {
		t.Errorf("YQParamArg = %+v", yq)
	}
	if fixed := Arg("cpu", "100m"); fixed.Evaluator != "" {
		t.Errorf("a fixed Arg should not be evaluated: %+v", fixed)
	}
}

func TestParseParams(t *testing.T) {
	got, err := ParseParams([]string{"min=3", "max=10", "expr=a=b"})
	if err != nil {
		t.Fatalf("ParseParams: %v", err)
	}
	// Only the first = separates, so a value may contain one.
	want := map[string]any{"min": "3", "max": "10", "expr": "a=b"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	for _, bad := range []string{"nope", "=novalue"} {
		if _, err := ParseParams([]string{bad}); err == nil {
			t.Errorf("ParseParams(%q) was accepted", bad)
		}
	}
}

func TestArticle(t *testing.T) {
	for in, want := range map[string]string{
		"autoscaling profile": "an autoscaling profile",
		"placement profile":   "a placement profile",
		"profile":             "a profile",
		"":                    "a profile",
	} {
		if got := article(in); got != want {
			t.Errorf("article(%q) = %q, want %q", in, got, want)
		}
	}
}

// Noun defaults so a tool that has no special word for a profile need not say so.
func TestNounDefaults(t *testing.T) {
	if got := (Library{}).noun(); got != "profile" {
		t.Errorf("default noun = %q", got)
	}
	if got := lib.noun(); got != "autoscaling profile" {
		t.Errorf("noun = %q", got)
	}
}
