// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

import (
	"slices"
	"strings"
	"testing"
)

func TestMatchResource(t *testing.T) {
	all := []unitResource{
		{resourceType: "apps/v1/Deployment", resourceName: "shop/web"},
		{resourceType: "v1/Service", resourceName: "shop/web"},
		{resourceType: "serving.knative.dev/v1/Service", resourceName: "shop/web"},
		{resourceType: "v1/Service", resourceName: "prod/api"},
		{resourceType: "rbac.authorization.k8s.io/v1/ClusterRole", resourceName: "/admin"},
	}
	for _, tc := range []struct {
		spec, want, err string
	}{
		{spec: "apps/v1/Deployment/shop/web", want: "apps/v1/Deployment/shop/web"},
		{spec: "Deployment/web", want: "apps/v1/Deployment/shop/web"},
		{spec: "deployment/shop/web", want: "apps/v1/Deployment/shop/web"},
		{spec: "v1/Service/shop/web", want: "v1/Service/shop/web"},
		{spec: "serving.knative.dev/v1/Service/shop/web", want: "serving.knative.dev/v1/Service/shop/web"},
		{spec: "serving.knative.dev/v1/Service/web", want: "serving.knative.dev/v1/Service/shop/web"},
		{spec: "Service/api", want: "v1/Service/prod/api"},
		{spec: "ClusterRole/admin", want: "rbac.authorization.k8s.io/v1/ClusterRole/admin"},
		{spec: "rbac.authorization.k8s.io/v1/ClusterRole/admin", want: "rbac.authorization.k8s.io/v1/ClusterRole/admin"},
		{spec: "Service/shop/web", err: "names 2 resources; name one in full: v1/Service/shop/web, serving.knative.dev/v1/Service/shop/web"},
		{spec: "Deployment/Web", err: "no resource Deployment/Web"},
		{spec: "Deployment", err: "must be <type>/[<namespace>/]<name>"},
	} {
		got, err := matchResource(all, tc.spec)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s: err = %v, want %q", tc.spec, err, tc.err)
			}
			continue
		}
		if err != nil || len(got) != 1 || got[0].ref() != tc.want {
			t.Errorf("%s: got %v, %v; want %s", tc.spec, refs(got), err, tc.want)
		}
	}
}

func TestPickResources(t *testing.T) {
	one := []unitResource{{resourceType: "apps/v1/Deployment", resourceName: "shop/web"}}
	if got, err := pickResources("prod/web", one, ""); err != nil || len(got) != 1 {
		t.Errorf("one resource: got %v, %v", got, err)
	}
	two := append(slices.Clone(one), unitResource{resourceType: "v1/Service", resourceName: "shop/web"})
	if _, err := pickResources("prod/web", two, ""); err == nil || !strings.Contains(err.Error(), "--resource") {
		t.Errorf("two resources without --resource: err = %v", err)
	}
	if got, err := pickResources("prod/web", two, "Service/web"); err != nil || got[0].ref() != "v1/Service/shop/web" {
		t.Errorf("two resources with --resource: got %v, %v", got, err)
	}
}

func TestParseRules(t *testing.T) {
	rules := []string{"a", "b", "c"}
	if got, _ := parseRules("", rules); !slices.Equal(got, rules) {
		t.Errorf("empty: got %v", got)
	}
	if got, _ := parseRules("c, a,c", rules); !slices.Equal(got, []string{"a", "c"}) {
		t.Errorf("list: got %v", got)
	}
	if _, err := parseRules("a,d", rules); err == nil || !strings.Contains(err.Error(), `"d"`) {
		t.Errorf("unknown: err = %v", err)
	}
}

func TestAnnotationPath(t *testing.T) {
	want := "metadata.annotations.exceptions~1guardrails~1confighub~1com/rbac-no-wildcards"
	if got := annotationPath(ExceptionAnnotation("rbac-no-wildcards")); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
