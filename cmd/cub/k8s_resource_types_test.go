// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"regexp"
	"strings"
	"testing"
)

func TestParseResourceTypesMatches(t *testing.T) {
	tests := []struct {
		name    string
		arg     string
		matches []string
		rejects []string
	}{
		{
			name:    "short name",
			arg:     "deploy",
			matches: []string{"apps/v1/Deployment"},
			rejects: []string{"v1/Service", "extensions/v1beta1/Deployment"},
		},
		{
			name:    "plural, singular, and Kind all resolve",
			arg:     "networkpolicies,networkpolicy,NetworkPolicy",
			matches: []string{"networking.k8s.io/v1/NetworkPolicy"},
			rejects: []string{"v1/Service"},
		},
		{
			name:    "core group type is not matched by a same-named group type",
			arg:     "svc",
			matches: []string{"v1/Service", "v1beta1/Service"},
			rejects: []string{"serving.knative.dev/v1/Service"},
		},
		{
			name:    "version is not part of the match",
			arg:     "hpa",
			matches: []string{"autoscaling/v1/HorizontalPodAutoscaler", "autoscaling/v2/HorizontalPodAutoscaler"},
			rejects: []string{"v1/HorizontalPodAutoscaler"},
		},
		{
			name:    "qualified name",
			arg:     "deployments.apps",
			matches: []string{"apps/v1/Deployment"},
			rejects: []string{"extensions/v1beta1/Deployment"},
		},
		{
			name:    "full resource type is matched exactly",
			arg:     "apps/v1/Deployment",
			matches: []string{"apps/v1/Deployment"},
			rejects: []string{"apps/v1beta1/Deployment"},
		},
		{
			name:    "core-group resource type is matched exactly",
			arg:     "v1/Service",
			matches: []string{"v1/Service"},
			rejects: []string{"v1beta1/Service"},
		},
		{
			name:    "group and Kind without a version",
			arg:     "apps/Deployment",
			matches: []string{"apps/v1/Deployment", "apps/v1beta2/Deployment"},
			rejects: []string{"extensions/v1beta1/Deployment"},
		},
		{
			name:    "unknown Kind matches any group, case-insensitively",
			arg:     "widget",
			matches: []string{"example.com/v1/Widget", "other.io/v1alpha1/widget"},
			rejects: []string{"example.com/v1/Gadget", "example.com/v1/SuperWidget"},
		},
		{
			name:    "unknown Kind accepts a plural spelling",
			arg:     "widgets",
			matches: []string{"example.com/v1/Widget"},
			rejects: []string{"example.com/v1/Widge"},
		},
		{
			name:    "unknown Kind qualified by group",
			arg:     "widgets.example.com",
			matches: []string{"example.com/v1/Widget"},
			rejects: []string{"other.io/v1/Widget"},
		},
		{
			name:    "all excludes CustomResourceDefinition",
			arg:     "all",
			matches: []string{"apps/v1/Deployment", "v1/ConfigMap", "example.com/v1/Widget"},
			rejects: []string{"apiextensions.k8s.io/v1/CustomResourceDefinition"},
		},
		{
			name:    "all combined with crd includes CustomResourceDefinition",
			arg:     "all,crd",
			matches: []string{"apps/v1/Deployment", "apiextensions.k8s.io/v1/CustomResourceDefinition"},
		},
		{
			name:    "several types at once",
			arg:     "deploy,sts,cm",
			matches: []string{"apps/v1/Deployment", "apps/v1/StatefulSet", "v1/ConfigMap"},
			rejects: []string{"v1/Secret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := parseResourceTypes(tt.arg)
			if err != nil {
				t.Fatalf("parseResourceTypes(%q) failed: %v", tt.arg, err)
			}
			for _, resourceType := range tt.matches {
				if !filter.matches(resourceType) {
					t.Errorf("%q should match %q", tt.arg, resourceType)
				}
			}
			for _, resourceType := range tt.rejects {
				if filter.matches(resourceType) {
					t.Errorf("%q should not match %q", tt.arg, resourceType)
				}
			}
		})
	}
}

func TestParseResourceTypesRejectsEmpty(t *testing.T) {
	if _, err := parseResourceTypes(" , "); err == nil {
		t.Error("expected an error for an argument naming no type")
	}
}

// The server-side WhereResource clause may be broader than the client-side
// match, but never narrower: anything matches() accepts must survive it, or
// resources would silently go missing.
func TestWhereResourceExpressionIsNotNarrowerThanMatches(t *testing.T) {
	resourceTypes := []string{
		"apps/v1/Deployment",
		"apps/v1beta2/Deployment",
		"apps/v1/StatefulSet",
		"v1/Service",
		"v1/ConfigMap",
		"v1beta1/Service",
		"serving.knative.dev/v1/Service",
		"networking.k8s.io/v1/NetworkPolicy",
		"autoscaling/v2/HorizontalPodAutoscaler",
		"apiextensions.k8s.io/v1/CustomResourceDefinition",
		"example.com/v1/Widget",
		"traefik.io/v1alpha1/IngressRoute",
	}
	args := []string{
		"deploy", "svc", "hpa", "netpol", "all", "crd", "deploy,svc",
		"apps/v1/Deployment", "apps/Deployment", "v1/Service",
		"widget", "widgets.example.com", "ingressroutes",
	}

	for _, arg := range args {
		t.Run(arg, func(t *testing.T) {
			filter, err := parseResourceTypes(arg)
			if err != nil {
				t.Fatalf("parseResourceTypes(%q) failed: %v", arg, err)
			}
			predicate := compileWhereResourceExpression(t, filter.whereResourceExpression())
			for _, resourceType := range resourceTypes {
				if filter.matches(resourceType) && !predicate(resourceType) {
					t.Errorf("%q matches %q but the server-side clause %q filters it out",
						arg, resourceType, filter.whereResourceExpression())
				}
			}
		})
	}
}

// compileWhereResourceExpression turns the generated clause back into a
// predicate, mirroring how the function executor evaluates it. It understands
// only the two shapes whereResourceExpression emits.
func compileWhereResourceExpression(t *testing.T, expression string) func(string) bool {
	t.Helper()
	if expression == "" {
		return func(string) bool { return true }
	}
	negated := false
	rest, found := strings.CutPrefix(expression, "ConfigHub.ResourceType ~* ")
	if !found {
		rest, found = strings.CutPrefix(expression, "ConfigHub.ResourceType !~ ")
		if !found {
			t.Fatalf("unexpected WhereResource shape: %q", expression)
		}
		negated = true
	}
	pattern := strings.Trim(rest, "'")
	if !negated {
		pattern = "(?i)" + pattern
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("clause %q has an invalid pattern: %v", expression, err)
	}
	return func(resourceType string) bool {
		return compiled.MatchString(resourceType) != negated
	}
}

// WhereResource string literals may not contain quotes or backslashes, so a
// generated clause containing either would be rejected by the parser.
func TestWhereResourceExpressionUsesSafeLiterals(t *testing.T) {
	for _, arg := range []string{"deploy", "all", "widgets.example.com", "apps/v1/Deployment", "deploy,svc,crd"} {
		filter, err := parseResourceTypes(arg)
		if err != nil {
			t.Fatalf("parseResourceTypes(%q) failed: %v", arg, err)
		}
		expression := filter.whereResourceExpression()
		if strings.ContainsAny(strings.Trim(expression, "'"), `"\`) {
			t.Errorf("%q produced a clause with an unsupported character: %q", arg, expression)
		}
	}
}

func TestSplitResourceType(t *testing.T) {
	tests := []struct {
		resourceType string
		group        string
		kind         string
	}{
		{"apps/v1/Deployment", "apps", "Deployment"},
		{"v1/ConfigMap", "", "ConfigMap"},
		{"traefik.containo.us/v1alpha1/IngressRoute", "traefik.containo.us", "IngressRoute"},
		{"Deployment", "", "Deployment"},
	}
	for _, tt := range tests {
		group, kind := splitResourceType(tt.resourceType)
		if group != tt.group || kind != tt.kind {
			t.Errorf("splitResourceType(%q) = (%q, %q), want (%q, %q)",
				tt.resourceType, group, kind, tt.group, tt.kind)
		}
	}
}

func TestIsAPIVersion(t *testing.T) {
	for _, segment := range []string{"v1", "v2", "v1beta1", "v2alpha3", "v10"} {
		if !isAPIVersion(segment) {
			t.Errorf("%q should be recognized as an API version", segment)
		}
	}
	for _, segment := range []string{"apps", "v", "version", "traefik.io", "vault", ""} {
		if isAPIVersion(segment) {
			t.Errorf("%q should not be recognized as an API version", segment)
		}
	}
}
