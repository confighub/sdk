// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package reconcile

import (
	"strings"
	"testing"
)

func TestStripANSI(t *testing.T) {
	in := "\x1b[94mhello\x1b[0m \x1b[32mworld\x1b[0m"
	if got := stripANSI(in); got != "hello world" {
		t.Errorf("got %q", got)
	}
}

func TestFilterBookkeepingMutations(t *testing.T) {
	// Single bookkeeping-only mutation (the convergence-bug case): the only
	// diff after a successful merge-external-source apply is the
	// confighub.com/ResourceMergeID annotation cub injected.
	bookkeepingOnly := `New changes from update from /tmp/x.yaml:
Resource: apps/v1/Deployment demo/hello
  - [Delete] metadata.annotations  (#4)
    confighub.com/ResourceMergeID: 31ad11e2-4838-4722-bbaa-7d04dea842e6
`
	got := filterBookkeepingMutations(bookkeepingOnly)
	if !isNoChange(got) {
		t.Errorf("expected no-change after filter, got:\n%s", got)
	}

	// Real change + bookkeeping noise: keep the real change, drop the noise.
	mixed := `New changes from update from /tmp/x.yaml:
Resource: apps/v1/Deployment demo/hello
  ~ [Update] spec.replicas  (#5)
    1 →     5
  - [Delete] metadata.annotations  (#4)
    confighub.com/ResourceMergeID: 31ad11e2-4838-4722-bbaa-7d04dea842e6
`
	got = filterBookkeepingMutations(mixed)
	if isNoChange(got) {
		t.Errorf("expected real change to survive filter, got no-change")
	}
	if !strings.Contains(got, "[Update] spec.replicas") {
		t.Errorf("real Update mutation got dropped:\n%s", got)
	}
	if strings.Contains(got, "ResourceMergeID") {
		t.Errorf("bookkeeping mutation should have been dropped:\n%s", got)
	}

	// Empty-body mutation: the "value" is encoded in the header path itself
	// (e.g., adding a single label). Must NOT be dropped.
	emptyBody := `New changes from update from /tmp/x.yaml:
Resource: apps/v1/Deployment demo/hello-app
  + [Add] metadata.labels.installer-e2e-marker  (#3)
`
	got = filterBookkeepingMutations(emptyBody)
	if isNoChange(got) {
		t.Errorf("empty-body mutation got dropped (it's a real change):\n%s", got)
	}
	if !strings.Contains(got, "installer-e2e-marker") {
		t.Errorf("empty-body mutation should survive filter:\n%s", got)
	}

	// Two resources, one with only bookkeeping, one with a real diff: only
	// the second resource's header should survive.
	twoResources := `New changes from update from /tmp/x.yaml:
Resource: v1/ConfigMap demo/cm
  - [Delete] metadata.annotations  (#4)
    confighub.com/ResourceMergeID: aaa
Resource: apps/v1/Deployment demo/hello
  ~ [Update] spec.replicas  (#5)
    1 →     5
`
	got = filterBookkeepingMutations(twoResources)
	if strings.Contains(got, "ConfigMap") {
		t.Errorf("ConfigMap header should be dropped (only bookkeeping):\n%s", got)
	}
	if !strings.Contains(got, "Deployment") {
		t.Errorf("Deployment header should survive (has real change):\n%s", got)
	}

	// AppConfig flat-key bookkeeping: a [Delete] on configHub.resourceMergeID
	// is dropped via the header path, but a legitimate configHub.configName
	// update is kept.
	appConfig := `New changes from update from /tmp/env:
Resource: AppConfig demo/env
  - [Delete] configHub.resourceMergeID  (#2)
    abc123
`
	if !isNoChange(filterBookkeepingMutations(appConfig)) {
		t.Errorf("configHub.resourceMergeID delete should be filtered as bookkeeping")
	}
	legitConfigHub := `New changes from update from /tmp/env:
Resource: AppConfig demo/env
  ~ [Update] configHub.configName  (#2)
    a → b
`
	if isNoChange(filterBookkeepingMutations(legitConfigHub)) {
		t.Errorf("configHub.configName is real config and must not be filtered")
	}
}

func TestIsNoChange(t *testing.T) {
	cases := map[string]bool{
		"":                                       true,
		"  \n":                                   true,
		"No new changes":                         true,
		"... No new changes from update from x.": true,
		// Preamble alone, no Resource: blocks → no change. Happens after
		// filterBookkeepingMutations strips a confighub.com-only diff.
		"New changes from update from x.yaml:": true,
		"New changes from update from x.yaml:\nResource: apps/v1/Deployment x/y\n  ~ [Update] spec.replicas (#3)": false,
	}
	for in, want := range cases {
		if got := isNoChange(in); got != want {
			t.Errorf("isNoChange(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestBaseName(t *testing.T) {
	cases := map[string]string{
		"/tmp/x/foo.yaml":       "foo.yaml",
		"foo.yaml":              "foo.yaml",
		`C:\path\to\thing.yaml`: "thing.yaml",
		"":                      "",
		"/trailing/":            "",
	}
	for in, want := range cases {
		if got := baseName(in); got != want {
			t.Errorf("baseName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSourceNameDefault(t *testing.T) {
	cases := []struct {
		u    DesiredUnit
		want string
	}{
		{DesiredUnit{Slug: "s", SourceName: "explicit", Path: "/tmp/a.yaml"}, "explicit"},
		{DesiredUnit{Slug: "s", Path: "/tmp/dir/a.yaml"}, "a.yaml"},
		{DesiredUnit{Slug: "only-slug"}, "only-slug"},
	}
	for _, tc := range cases {
		if got := tc.u.sourceName(); got != tc.want {
			t.Errorf("sourceName(%+v) = %q, want %q", tc.u, got, tc.want)
		}
	}
}
