// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func stringArgument(parameterName, value, evaluator string) goclientnew.FunctionArgument {
	argument := goclientnew.FunctionArgument{Value: &goclientnew.FunctionArgument_Value{}}
	_ = argument.Value.FromFunctionArgumentValue0(value)
	if parameterName != "" {
		argument.ParameterName = &parameterName
	}
	if evaluator != "" {
		argument.Evaluator = &evaluator
	}
	return argument
}

// A TransformPaths Link's definition reads as rows: where each value comes from, and the call or
// expression that writes it.
func TestLinkDefinitionFormatting(t *testing.T) {
	setter := &goclientnew.FunctionInvocation{
		FunctionName: "set-image-reference-by-uri",
		Arguments: []goclientnew.FunctionArgument{
			stringArgument("repository-uri", "ghcr.io/confighubai/confighub", ""),
			stringArgument("image-reference", ":{{.Params.tag}}", "template"),
		},
	}
	if got, want := formatFunctionInvocationLine(setter),
		`set-image-reference-by-uri repository-uri="ghcr.io/confighubai/confighub" image-reference=":{{.Params.tag}}" (template)`; got != want {
		t.Errorf("setter:\n got %s\nwant %s", got, want)
	}

	getter := &goclientnew.FunctionInvocation{
		FunctionName:  "get-hash",
		Arguments:     []goclientnew.FunctionArgument{stringArgument("", "data", "")},
		WhereResource: "ConfigHub.ResourceType = 'v1/ConfigMap'",
	}
	if got, want := formatFunctionInvocationLine(getter),
		`get-hash "data" where "ConfigHub.ResourceType = 'v1/ConfigMap'"`; got != want {
		t.Errorf("getter:\n got %s\nwant %s", got, want)
	}

	deployment := &goclientnew.ResourceInfo{ResourceType: "apps/v1/Deployment", ResourceName: "default/web"}
	down := &goclientnew.PathExpression{
		Path: "spec.replicas", Resource: deployment,
		Expression: "{{.Params.replicas}}", Evaluator: "template", DataType: "int",
		Parameters: []string{"replicas"},
	}
	if got, want := formatPathExpression(down),
		`spec.replicas in apps/v1/Deployment default/web = "{{.Params.replicas}}" (template, int) [uses replicas]`; got != want {
		t.Errorf("downstream path:\n got %s\nwant %s", got, want)
	}

	if got, want := formatPathIn("streams.stable.tag",
		&goclientnew.ResourceInfo{ResourceType: "registrybot.confighub.com/v1alpha3", ResourceName: "confighub"}),
		"streams.stable.tag in registrybot.confighub.com/v1alpha3 confighub"; got != want {
		t.Errorf("upstream path:\n got %s\nwant %s", got, want)
	}
}

// A Binding names both ends for NeedsProvides, and only the insertion point for Insert, whose
// upstream data goes in whole.
func TestLinkBindingFormatting(t *testing.T) {
	needsProvides := &goclientnew.Binding{
		NeededPath:       "metadata.namespace",
		NeededResource:   &goclientnew.ResourceInfo{ResourceType: "v1/ServiceAccount", ResourceName: "confighubplaceholder/app"},
		ProvidedPath:     "metadata.name",
		ProvidedResource: &goclientnew.ResourceInfo{ResourceType: "v1/Namespace", ResourceName: "/prod"},
		AttributeName:    "resource-name",
		DataType:         "string",
	}
	if got, want := formatBinding(needsProvides),
		"metadata.namespace in v1/ServiceAccount confighubplaceholder/app <- metadata.name in v1/Namespace /prod (resource-name, string)"; got != want {
		t.Errorf("needs/provides binding:\n got %s\nwant %s", got, want)
	}

	insert := &goclientnew.Binding{
		NeededPath:     "spec.policy",
		NeededResource: &goclientnew.ResourceInfo{ResourceType: "ecr.services.k8s.aws/v1alpha1/Repository", ResourceName: "ns/repo"},
	}
	if got, want := formatBinding(insert), "spec.policy in ecr.services.k8s.aws/v1alpha1/Repository ns/repo"; got != want {
		t.Errorf("insert binding:\n got %s\nwant %s", got, want)
	}

	if got := formatMergedRevision(0); got != "none" {
		t.Errorf("a Link that has not merged should read none, got %s", got)
	}
	if got := numberedLabel("Downstream Setter", 1, 2); got != "Downstream Setter 2" {
		t.Errorf("numbered label: got %s", got)
	}
	if got := numberedLabel("Downstream Setter", 0, 1); got != "Downstream Setter" {
		t.Errorf("single label: got %s", got)
	}
	if got := keyedLabel("Downstream Setter", "replicas", 1, 2); got != "Downstream Setter replicas" {
		t.Errorf("keyed label: got %s", got)
	}
	if got := keyedLabel("Downstream Setter", "", 1, 2); got != "Downstream Setter 2" {
		t.Errorf("unkeyed label: got %s", got)
	}
}
