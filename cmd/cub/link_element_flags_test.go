// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func resource(resourceType, name string) *goclientnew.ResourceInfo {
	return &goclientnew.ResourceInfo{ResourceType: resourceType, ResourceName: name}
}

func TestParseUpstreamPath(t *testing.T) {
	got, err := parseUpstreamPath("replicas=apps/v1/Deployment:default/web:spec.replicas")
	want := &goclientnew.NamedPath{Name: "replicas", Resource: resource("apps/v1/Deployment", "default/web"), Path: "spec.replicas"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %s, %v", asJSON(t, got), err)
	}
	for _, bad := range []string{"apps/v1/Deployment:default/web:spec.replicas", "replicas=apps/v1/Deployment:default/web", "=a:b:c"} {
		if _, err := parseUpstreamPath(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestParseDownstreamPath(t *testing.T) {
	deployment := resource("apps/v1/Deployment", "default/web")
	tests := []struct {
		value string
		want  goclientnew.PathExpression
	}{
		{
			value: "apps/v1/Deployment:default/web:metadata.annotations.description=replicas={{.Params.replicas}} unit={{.UnitSlug}}",
			want: goclientnew.PathExpression{Resource: deployment, Path: "metadata.annotations.description",
				Expression: "replicas={{.Params.replicas}} unit={{.UnitSlug}}", Evaluator: "template", DataType: "string", Parameters: []string{"replicas"}},
		},
		{
			value: "doubled=apps/v1/Deployment:default/web:spec.replicas=int:cel:params.replicas * 2 + params.surge",
			want: goclientnew.PathExpression{Key: "doubled", Resource: deployment, Path: "spec.replicas",
				Expression: "params.replicas * 2 + params.surge", Evaluator: "cel", DataType: "int", Parameters: []string{"replicas", "surge"}},
		},
		{
			// The path has an "=" of its own, and the annotation key a dot.
			value: "apps/v1/Deployment:default/web:spec.template.spec.containers.?name=web.image=template:{{.Params.image}}",
			want: goclientnew.PathExpression{Resource: deployment, Path: "spec.template.spec.containers.?name=web.image",
				Expression: "{{.Params.image}}", Evaluator: "template", DataType: "string", Parameters: []string{"image"}},
		},
		{
			value: "apps/v1/Deployment:default/web:metadata.annotations.confighub~1com/hash=cel:params.configHash",
			want: goclientnew.PathExpression{Resource: deployment, Path: "metadata.annotations.confighub~1com/hash",
				Expression: "params.configHash", Evaluator: "cel", DataType: "string", Parameters: []string{"configHash"}},
		},
	}
	for _, test := range tests {
		got, err := parseDownstreamPath(test.value)
		if err != nil || !reflect.DeepEqual(*got, test.want) {
			t.Errorf("%q:\n got %s, %v\nwant %s", test.value, asJSON(t, got), err, asJSON(t, test.want))
		}
	}
	for _, bad := range []string{"apps/v1/Deployment:default/web:spec.replicas", "apps/v1/Deployment:spec.replicas=x", "a:b:c="} {
		if got, err := parseDownstreamPath(bad); err == nil {
			t.Errorf("%q was accepted as %s", bad, asJSON(t, got))
		}
	}
}

func TestParseManualBinding(t *testing.T) {
	subnet := resource("confighub.services.k8s.aws/v1alpha1/Subnet", "/confighub-public-2")
	routeTable := resource("confighub.services.k8s.aws/v1alpha1/RouteTable", "/confighub-public-rt")
	tests := []struct {
		value string
		want  goclientnew.Binding
	}{
		{
			value: "confighub.services.k8s.aws/v1alpha1/Subnet:/confighub-public-2:spec.routeTableRefs.0.from.name=confighub.services.k8s.aws/v1alpha1/RouteTable:/confighub-public-rt:metadata.name",
			want: goclientnew.Binding{DataType: "string", NeededResource: subnet, NeededPath: "spec.routeTableRefs.0.from.name",
				ProvidedResource: routeTable, ProvidedPath: "metadata.name"},
		},
		{
			value: "rt:resource-name=confighub.services.k8s.aws/v1alpha1/Subnet:/confighub-public-2:spec.count=int:confighub.services.k8s.aws/v1alpha1/RouteTable:/confighub-public-rt:spec.count",
			want: goclientnew.Binding{Key: "rt", AttributeName: "resource-name", DataType: "int", NeededResource: subnet, NeededPath: "spec.count",
				ProvidedResource: routeTable, ProvidedPath: "spec.count"},
		},
		{
			// An Insert link's binding: only where the upstream unit goes, and as what.
			value: "v1/ConfigMap:default/app:data.config~1yaml=yaml",
			want:  goclientnew.Binding{DataType: "yaml", NeededResource: resource("v1/ConfigMap", "default/app"), NeededPath: "data.config~1yaml"},
		},
		{
			value: "v1/ConfigMap:default/app:data.config",
			want:  goclientnew.Binding{DataType: "string", NeededResource: resource("v1/ConfigMap", "default/app"), NeededPath: "data.config"},
		},
	}
	for _, test := range tests {
		got, err := parseManualBinding(test.value)
		if err != nil || !reflect.DeepEqual(*got, test.want) {
			t.Errorf("%q:\n got %s, %v\nwant %s", test.value, asJSON(t, got), err, asJSON(t, test.want))
		}
	}
	for _, bad := range []string{"v1/ConfigMap:default/app", "key=v1/ConfigMap", "v1/ConfigMap:default/app:data.x="} {
		if got, err := parseManualBinding(bad); err == nil {
			t.Errorf("%q was accepted as %s", bad, asJSON(t, got))
		}
	}
}

func TestParseNamedFunction(t *testing.T) {
	name, invocation, err := parseNamedFunction("upstream-getter", "configHash=get-hash --path=data", true)
	if err != nil || name != "configHash" || invocation.FunctionName != "get-hash" || len(invocation.Arguments) != 1 {
		t.Errorf("got %q, %s, %v", name, asJSON(t, invocation), err)
	}
	// A setter's key is optional, and an "=" inside an argument is not one.
	key, invocation, err := parseNamedFunction("downstream-setter", "set-annotation hash --value='template:{{.Params.configHash}} from {{.Params.source}}'", false)
	if err != nil || key != "" || invocation.FunctionName != "set-annotation" || len(invocation.Arguments) != 2 {
		t.Fatalf("got %q, %s, %v", key, asJSON(t, invocation), err)
	}
	if parameters := referencedParameters(functionArgumentTexts(invocation)...); !reflect.DeepEqual(parameters, []string{"configHash", "source"}) {
		t.Errorf("the setter's parameters are %v", parameters)
	}
	if _, _, err := parseNamedFunction("upstream-getter", "get-hash --path=data", true); err == nil {
		t.Error("a getter with no name was accepted")
	}
}

func TestLinkElementFlagsStateTheirLists(t *testing.T) {
	previous := linkElementArgs
	t.Cleanup(func() { linkElementArgs = previous })
	linkElementArgs.upstreamPaths = []string{"replicas=apps/v1/Deployment:default/web:spec.replicas"}
	linkElementArgs.downstreamPaths = []string{"apps/v1/Deployment:default/web:metadata.annotations.description=replicas={{.Params.replicas}}"}

	useFieldEditFlags(t, nil, nil)
	if err := queueLinkElementEdits(); err != nil {
		t.Fatal(err)
	}
	link := &goclientnew.Link{
		UpstreamPaths:   []goclientnew.NamedPath{{Name: "old"}},
		DownstreamPaths: []goclientnew.PathExpression{{Key: "old"}, {Key: "older"}},
		UpstreamGetters: []goclientnew.NamedFunctionResult{{Name: "kept"}},
	}
	if err := applyFieldEdits("Link", link); err != nil {
		t.Fatal(err)
	}
	if len(link.UpstreamPaths) != 1 || link.UpstreamPaths[0].Name != "replicas" || len(link.DownstreamPaths) != 1 ||
		link.DownstreamPaths[0].Path != "metadata.annotations.description" {
		t.Errorf("paths are %s and %s", asJSON(t, link.UpstreamPaths), asJSON(t, link.DownstreamPaths))
	}
	// A list no flag named is left as it was.
	if len(link.UpstreamGetters) != 1 || link.UpstreamGetters[0].Name != "kept" {
		t.Errorf("getters are %s", asJSON(t, link.UpstreamGetters))
	}
}
