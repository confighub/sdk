// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
)

// The released revision carries comments; the live object never can. Keeping them here is
// what makes the comment cases below real rather than hypothetical.
//
// Both shapes that actually misbehave are represented. A comment block after a document
// separator -- which is exactly what "cub variant upload" stores, down to the "# Source:"
// line -- becomes a $comment$head$ path that reads as a deletion. An inline comment is
// worse: it makes the commented field itself compare unequal, so spec.replicas reads as
// changed when both sides say 2. A comment at the very start of a file with no leading
// separator produces neither, which is why the fixture does not rely on that shape.
const refreshReleasedData = `---
# Source: workload.yaml
# what this workload is for
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: prod
spec:
  replicas: 2 # sized for prod
---
apiVersion: v1
kind: Service
metadata:
  name: my-app
  namespace: prod
spec:
  ports:
  - port: 80
`

// A single cleaned live object, which is all a refresh ever has: one resource, read from
// the cluster, with the controller-owned fields already stripped.
const refreshLiveData = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: prod
spec:
  replicas: 5
`

// The direction of the diff is the thing most easily gotten backwards, and getting it
// backwards would quietly write the released value back over the cluster's. The recorded
// value has to be the cluster's.
func TestComputeClusterDriftRecordsTheClusterValue(t *testing.T) {
	drift, err := computeClusterDrift(refreshReleasedData, refreshLiveData, "apps/v1/Deployment", "my-app")
	if err != nil {
		t.Fatalf("computeClusterDrift: %v", err)
	}
	if len(drift) != 1 {
		t.Fatalf("expected the one refreshed resource, got %d", len(drift))
	}
	info, ok := drift[0].PathMutationMap["spec.replicas"]
	if !ok {
		t.Fatalf("spec.replicas not reported as drifted; got %v", drift[0].PathMutationMap)
	}
	// The value is YAML, so a scalar carries a trailing newline.
	if strings.TrimSpace(info.Value) != "5" {
		t.Errorf("expected the cluster's value 5, got %q -- the diff is running released <- live", info.Value)
	}
	if info.MutationType != api.MutationTypeUpdate {
		t.Errorf("expected an Update, got %s", info.MutationType)
	}
}

// Comments live in the unit and never reach the cluster, so a diff that saw them would
// report each one as a deletion and a refresh would strip the unit's comments. The base
// side is stripped before the diff, so no comment path can reach the patch at all.
func TestComputeClusterDriftIgnoresComments(t *testing.T) {
	drift, err := computeClusterDrift(refreshReleasedData, refreshLiveData, "apps/v1/Deployment", "my-app")
	if err != nil {
		t.Fatalf("computeClusterDrift: %v", err)
	}
	for _, rm := range drift {
		for path := range rm.PathMutationMap {
			if strings.Contains(string(path), yamlkit.CommentKeyPrefix) {
				t.Errorf("comment path %q reached the patch; refreshing would strip the unit's comments", path)
			}
		}
	}
}

// The released revision holds resources the single live object does not, and
// compute-mutations reports each of those as a whole-resource Delete. Refresh touches only
// the resource it was asked about, so those must not reach the patch -- a --dry-run that
// listed them would be describing a deletion refresh will never make.
func TestComputeClusterDriftDropsTheOtherResources(t *testing.T) {
	drift, err := computeClusterDrift(refreshReleasedData, refreshLiveData, "apps/v1/Deployment", "my-app")
	if err != nil {
		t.Fatalf("computeClusterDrift: %v", err)
	}
	for _, rm := range drift {
		if string(rm.Resource.ResourceType) != "apps/v1/Deployment" {
			t.Errorf("resource %s %s leaked into the patch", rm.Resource.ResourceType, rm.Resource.ResourceName)
		}
	}
}

// A cluster that still matches what was released has nothing to bring back. The released
// side is commented and the live side is not, so this also covers the case that would
// otherwise report every comment as a deletion.
func TestComputeClusterDriftEmptyWhenTheClusterMatches(t *testing.T) {
	unchanged := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: prod
spec:
  replicas: 2
`
	drift, err := computeClusterDrift(refreshReleasedData, unchanged, "apps/v1/Deployment", "my-app")
	if err != nil {
		t.Fatalf("computeClusterDrift: %v", err)
	}
	if len(drift) != 0 {
		t.Errorf("expected no drift, got %v", drift)
	}
}

// A unit's stored resource often carries no metadata.namespace -- the applier supplies it --
// so its ResourceName is "/my-app" where the live object's is "prod/my-app". Matching on the
// scopeless name is what lets refresh find it anyway.
func TestMatchesResourceName(t *testing.T) {
	cases := []struct {
		name string
		info api.ResourceInfo
		want bool
	}{
		{"scoped", api.ResourceInfo{ResourceName: "prod/my-app", ResourceNameWithoutScope: "my-app"}, true},
		{"unscoped", api.ResourceInfo{ResourceName: "/my-app", ResourceNameWithoutScope: "my-app"}, true},
		{"scope only in the full name", api.ResourceInfo{ResourceName: "prod/my-app"}, true},
		{"different resource", api.ResourceInfo{ResourceName: "prod/other", ResourceNameWithoutScope: "other"}, false},
		// A name that only looks like a prefix of the target must not match.
		{"prefix", api.ResourceInfo{ResourceName: "prod/my-app-canary", ResourceNameWithoutScope: "my-app-canary"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesResourceName(tc.info, "my-app"); got != tc.want {
				t.Errorf("matchesResourceName(%+v) = %v, want %v", tc.info, got, tc.want)
			}
		})
	}
}

// WhereResource literals cannot contain quotes or backslashes, and the clause is what keeps
// a refresh from writing the unit's other resources.
func TestWhereResourceForOneResource(t *testing.T) {
	got := whereResourceForOneResource("apps/v1/Deployment", "my-app")
	want := "ConfigHub.ResourceType = 'apps/v1/Deployment' AND ConfigHub.ResourceNameWithoutScope = 'my-app'"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if _, err := api.ParseAndValidateWhereResource(got); err != nil {
		t.Errorf("the clause refresh builds does not parse: %v", err)
	}
}
