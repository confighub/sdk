// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package upload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleManifests = `apiVersion: v1
kind: Namespace
metadata:
  name: web
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: web
data:
  KEY: value
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: web
spec:
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: web
          image: nginx:1.27
          envFrom:
            - configMapRef:
                name: app-config
---
apiVersion: v1
kind: Secret
metadata:
  name: web-tls
  namespace: web
type: Opaque
data:
  tls.crt: Zm9v
`

func parseSample(t *testing.T) []Resource {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "manifests.yaml")
	if err := os.WriteFile(path, []byte(sampleManifests), 0o644); err != nil {
		t.Fatal(err)
	}
	resources, err := Parse([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return resources
}

func unitBySlug(plan *Plan, slug string) *Unit {
	for i := range plan.Units {
		if plan.Units[i].Slug == slug {
			return &plan.Units[i]
		}
	}
	return nil
}

func TestBuildPlan_MinimalSplitsInfraAndSkipsSecrets(t *testing.T) {
	resources := parseSample(t)
	plan, err := BuildPlan(resources, Minimal, "web", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.SkippedSecrets) != 1 || plan.SkippedSecrets[0].Name != "web-tls" {
		t.Fatalf("expected the Secret to be skipped, got %+v", plan.SkippedSecrets)
	}
	// Namespace, ConfigMap, and the workload land in three separate Units.
	ns := unitBySlug(plan, "web-namespaces")
	cm := unitBySlug(plan, "web-configmaps")
	main := unitBySlug(plan, "web")
	if ns == nil || cm == nil || main == nil {
		t.Fatalf("expected web-namespaces, web-configmaps, web Units; got %v", unitSlugs(plan))
	}
	if !strings.Contains(ns.Content, "kind: Namespace") {
		t.Fatalf("Namespace not in its own Unit: %q", ns.Content)
	}
	if !strings.Contains(cm.Content, "kind: ConfigMap") {
		t.Fatalf("ConfigMap not in its own Unit: %q", cm.Content)
	}
	if !strings.Contains(main.Content, "kind: Deployment") || strings.Contains(main.Content, "kind: ConfigMap") {
		t.Fatalf("main Unit should hold the Deployment but not the ConfigMap: %q", main.Content)
	}
	for _, u := range plan.Units {
		if strings.Contains(u.Content, "kind: Secret") {
			t.Fatalf("Secret leaked into Unit %s", u.Slug)
		}
	}
	// No cycle should be broken for this acyclic input.
	if len(plan.BrokenOrdering) != 0 || len(plan.BrokenLinks) != 0 {
		t.Fatalf("unexpected broken edges: ordering=%v links=%v", plan.BrokenOrdering, plan.BrokenLinks)
	}
	// The workload Unit depends on both the ConfigMap and Namespace Units.
	wantLink := func(from, to string) {
		for _, l := range plan.Links {
			if l.FromUnit == from && l.ToUnit == to {
				return
			}
		}
		t.Fatalf("expected link %s -> %s, got %v", from, to, plan.Links)
	}
	wantLink("web", "web-configmaps")
	wantLink("web", "web-namespaces")
	wantLink("web-configmaps", "web-namespaces")
}

func TestBuildPlan_PerResourceInfersConfigMapLink(t *testing.T) {
	resources := parseSample(t)
	plan, err := BuildPlan(resources, PerResource, "web", "")
	if err != nil {
		t.Fatal(err)
	}
	// One Unit per non-Secret resource: Namespace, ConfigMap, Deployment.
	if len(plan.Units) != 3 {
		t.Fatalf("expected 3 Units, got %d: %v", len(plan.Units), unitSlugs(plan))
	}
	// The Deployment references the ConfigMap, so a link deployment-unit ->
	// configmap-unit should be inferred.
	depSlug := "web-deployment-web"
	cmSlug := "web-configmap-app-config"
	if unitBySlug(plan, depSlug) == nil || unitBySlug(plan, cmSlug) == nil {
		t.Fatalf("expected Units %q and %q, got %v", depSlug, cmSlug, unitSlugs(plan))
	}
	found := false
	for _, l := range plan.Links {
		if l.FromUnit == depSlug && l.ToUnit == cmSlug {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a link %s -> %s, got %v", depSlug, cmSlug, plan.Links)
	}
}

func TestBuildPlan_EnsuresNamespace(t *testing.T) {
	// Manifests without a Namespace resource; --namespace should synthesize one.
	res, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = res
	dir := t.TempDir()
	path := filepath.Join(dir, "m.yaml")
	body := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: c\n  namespace: web\ndata:\n  a: b\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	resources, err := Parse([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(resources, Minimal, "web", "web")
	if err != nil {
		t.Fatal(err)
	}
	ns := unitBySlug(plan, "web-namespaces")
	if ns == nil || !strings.Contains(ns.Content, "kind: Namespace") {
		t.Fatalf("expected a synthesized Namespace in web-namespaces, got %v", unitSlugs(plan))
	}
}

func TestResourceSlug_OmitsPlaceholders(t *testing.T) {
	cases := []struct {
		r    Resource
		want string
	}{
		{Resource{Type: "v1/Namespace", Namespace: "", Name: "confighubplaceholder"}, "namespace"},
		{Resource{Type: "apps/v1/Deployment", Namespace: "confighubplaceholder", Name: "web"}, "deployment-web"},
		{Resource{Type: "apps/v1/Deployment", Namespace: "shop", Name: "web"}, "shop-deployment-web"},
		{Resource{Type: "v1/Service", Namespace: "", Name: "web"}, "service-web"},
	}
	for _, c := range cases {
		if got := resourceSlug(c.r); got != c.want {
			t.Errorf("resourceSlug(%+v) = %q, want %q", c.r, got, c.want)
		}
	}
}

func unitSlugs(plan *Plan) []string {
	var s []string
	for _, u := range plan.Units {
		s = append(s, u.Slug)
	}
	return s
}
