// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"testing"
)

func TestParseResourceRef(t *testing.T) {
	for _, tc := range []struct {
		ref     string
		want    resourceRef
		wantErr bool
	}{
		{ref: "Deployment/backend", want: resourceRef{Kind: "Deployment", Name: "backend"}},
		{ref: "deployment/backend", want: resourceRef{Kind: "deployment", Name: "backend"}},
		{ref: "Deployment/shop/backend", want: resourceRef{Kind: "Deployment", Namespace: "shop", Name: "backend"}},
		// kubectl's group suffix, with a group that has dots of its own.
		{ref: "Ingress.networking.k8s.io/web", want: resourceRef{Kind: "Ingress", Group: "networking.k8s.io", Name: "web"}},
		{ref: "Deployment.apps/shop/backend", want: resourceRef{Kind: "Deployment", Group: "apps", Namespace: "shop", Name: "backend"}},
		{ref: "backend", wantErr: true},
		{ref: "Deployment/shop/backend/extra", wantErr: true},
		{ref: "/backend", wantErr: true},
		{ref: "Deployment/", wantErr: true},
		{ref: "Deployment//backend", wantErr: true},
		{ref: "Deployment./backend", wantErr: true},
		// The parts go into a filter literal, which has no way to express a quote.
		{ref: "Deployment/back'end", wantErr: true},
	} {
		got, err := parseResourceRef(tc.ref)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%q parsed as %+v", tc.ref, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tc.ref, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q = %+v, want %+v", tc.ref, got, tc.want)
		}
		if got.String() != tc.ref {
			t.Errorf("%q renders as %q", tc.ref, got.String())
		}
	}
}

func TestResourceRefMatches(t *testing.T) {
	for _, tc := range []struct {
		ref                        string
		resourceType, resourceName string
		want                       bool
	}{
		{"Deployment/backend", "apps/v1/Deployment", "shop/backend", true},
		// The kind matches in any case, and only as a whole.
		{"deployment/backend", "apps/v1/Deployment", "shop/backend", true},
		{"Deploy/backend", "apps/v1/Deployment", "shop/backend", false},
		// The name matches as a whole, in whatever namespace when none is given.
		{"Deployment/backend", "apps/v1/Deployment", "shop/backend-canary", false},
		{"Deployment/shop/backend", "apps/v1/Deployment", "shop/backend", true},
		{"Deployment/shop/backend", "apps/v1/Deployment", "other/backend", false},
		// A cluster-scoped resource is stored with an empty namespace.
		{"Namespace/shop", "v1/Namespace", "/shop", true},
		{"Namespace/x/shop", "v1/Namespace", "/shop", false},
		// The group qualifies the kind; a core type has no group.
		{"Ingress.networking.k8s.io/web", "networking.k8s.io/v1/Ingress", "shop/web", true},
		{"Ingress.extensions/web", "networking.k8s.io/v1/Ingress", "shop/web", false},
		{"ConfigMap.apps/settings", "v1/ConfigMap", "shop/settings", false},
	} {
		ref, err := parseResourceRef(tc.ref)
		if err != nil {
			t.Fatalf("%q: %v", tc.ref, err)
		}
		if got := ref.matches(tc.resourceType, tc.resourceName); got != tc.want {
			t.Errorf("%q matches %s %s = %v, want %v", tc.ref, tc.resourceType, tc.resourceName, got, tc.want)
		}
	}
}

func TestResourceRefListWhere(t *testing.T) {
	for _, tc := range []struct{ ref, want string }{
		{"Deployment/backend", "ResourceType ~* '/Deployment$' AND ResourceName ~ '/backend$'"},
		{"Deployment/shop/backend", "ResourceType ~* '/Deployment$' AND ResourceName = 'shop/backend'"},
		{"Ingress.networking.k8s.io/web", "ResourceType ~* '^networking.k8s.io/.*/Ingress$' AND ResourceName ~ '/web$'"},
	} {
		ref, err := parseResourceRef(tc.ref)
		if err != nil {
			t.Fatalf("%q: %v", tc.ref, err)
		}
		where := ref.listWhere()
		if err := where.Err(); err != nil {
			t.Errorf("%q: %v", tc.ref, err)
		}
		if got := where.String(); got != tc.want {
			t.Errorf("%q where = %q, want %q", tc.ref, got, tc.want)
		}
	}
}

func TestResolvedResourceWhereResource(t *testing.T) {
	for _, tc := range []struct {
		resourceType, resourceName, want string
	}{
		{"apps/v1/Deployment", "shop/backend", "apiVersion = 'apps/v1' AND kind = 'Deployment' AND metadata.name = 'backend' AND metadata.namespace = 'shop'"},
		{"v1/Namespace", "/shop", "apiVersion = 'v1' AND kind = 'Namespace' AND metadata.name = 'shop'"},
		{"networking.k8s.io/v1/Ingress", "shop/web", "apiVersion = 'networking.k8s.io/v1' AND kind = 'Ingress' AND metadata.name = 'web' AND metadata.namespace = 'shop'"},
	} {
		r := resolvedResource{ResourceType: tc.resourceType, ResourceName: tc.resourceName}
		if got := r.whereResource(); got != tc.want {
			t.Errorf("%s %s: %q, want %q", tc.resourceType, tc.resourceName, got, tc.want)
		}
	}
}

// TestQualifiedVariant: --variant names its component inline or takes it from
// --component; both given, they agree; a variant name has no slash of its own.
func TestQualifiedVariant(t *testing.T) {
	for _, tc := range []struct {
		componentFlag, value string
		component, variant   string
		wantErr              bool
	}{
		{value: "web/base", component: "web", variant: "base"},
		{value: "web/prod-us", component: "web", variant: "prod-us"},
		{componentFlag: "web", value: "base", component: "web", variant: "base"},
		{componentFlag: "web", value: "web/base", component: "web", variant: "base"},
		{componentFlag: "api", value: "web/base", wantErr: true},
		{value: "base", wantErr: true},
		{value: "", wantErr: true},
		{componentFlag: "web", value: "", wantErr: true},
		{value: "web/", wantErr: true},
		{value: "/base", wantErr: true},
		{value: "web/base/extra", wantErr: true},
		{componentFlag: "web", value: "base/extra", wantErr: true},
	} {
		component, variant, err := qualifiedVariant(tc.componentFlag, tc.value)
		if tc.wantErr {
			if err == nil {
				t.Errorf("--component %q --variant %q parsed as %q/%q", tc.componentFlag, tc.value, component, variant)
			}
			continue
		}
		if err != nil || component != tc.component || variant != tc.variant {
			t.Errorf("--component %q --variant %q = %q, %q, %v; want %q, %q", tc.componentFlag, tc.value, component, variant, err, tc.component, tc.variant)
		}
	}
}
