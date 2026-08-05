// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
)

// These tests cover how ComputeMutations pairs the resources of one revision with the
// resources of the next — the level above the per-path diff. Getting a pairing wrong is
// expensive: two unrelated resources paired as a rename produce a patch that rewrites one
// resource into the other, and a resource paired twice produces a patch that claims to
// update a resource that is really being replaced.

// configMapDoc builds a ConfigMap with the given data keys, as "key=value" strings.
func configMapDoc(name string, entries ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s\n  namespace: ns\ndata:\n", name)
	for _, e := range entries {
		key, value, _ := strings.Cut(e, "=")
		fmt.Fprintf(&b, "  %s: \"%s\"\n", key, value)
	}
	return b.String()
}

// secretDoc builds a Secret, which is never type-compatible with a ConfigMap.
func secretDoc(name string) string {
	return fmt.Sprintf("apiVersion: v1\nkind: Secret\nmetadata:\n  name: %s\n  namespace: ns\ntype: Opaque\ndata:\n  token: \"abc\"\n", name)
}

// deploymentDoc builds a Deployment big enough to dominate a unit's line count, which is
// what the resource-matching score used to be normalized by.
func deploymentDoc(name string, image string) string {
	return fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: ns
spec:
  replicas: 2
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
    spec:
      containers:
      - name: app
        image: %s
        ports:
        - containerPort: 8080
        env:
        - name: LOG_LEVEL
          value: info
        - name: REGION
          value: us-east
`, name, name, name, image)
}

func joinDocs(docs ...string) string {
	return strings.Join(docs, "---\n")
}

// resourceMutationTypes returns resource name -> resource-level MutationType.
func resourceMutationTypes(mutations api.ResourceMutationList) map[string]api.MutationType {
	types := map[string]api.MutationType{}
	for _, m := range mutations {
		types[string(m.Resource.ResourceName)] = m.ResourceMutationInfo.MutationType
	}
	return types
}

// TestResourceMatchScoreIsPerResource covers the score normalization. The cost of pairing
// two resources used to be divided by the line count of the *whole* unit rather than of the
// resources being paired, so the rejection threshold got weaker as a unit got larger: in a
// multi-resource unit almost any type-compatible resource passed, and a resource that was
// really new was recorded as a rename of an unrelated one.
func TestResourceMatchScoreIsPerResource(t *testing.T) {
	// The Deployment is unchanged and matches by name; it is here only to make the unit
	// large. The two ConfigMaps have nothing in common but their type.
	previous := parseDocs(t, joinDocs(
		deploymentDoc("web", "nginx:1.19"),
		configMapDoc("old-settings", "timeout=30", "retries=3", "region=us-east"),
	))
	modified := parseDocs(t, joinDocs(
		deploymentDoc("web", "nginx:1.19"),
		configMapDoc("new-flags", "featureA=on", "featureB=off", "featureC=on", "featureD=off"),
	))

	mutations, err := ComputeMutations(previous, modified, 1, testProvider)
	require.NoError(t, err)

	assert.Equal(t, map[string]api.MutationType{
		"ns/web":          api.MutationTypeNone,
		"ns/new-flags":    api.MutationTypeAdd,
		"ns/old-settings": api.MutationTypeDelete,
	}, resourceMutationTypes(mutations),
		"two unrelated ConfigMaps must not be paired as a rename just because the unit is large")
}

// TestResourceMatchIsNotReused covers pairing the same previous resource with more than one
// modified resource. The search used to scan from the first unmatched previous resource
// without skipping the ones already claimed, so when a match happened out of order the same
// resource could be claimed twice — recording two Updates of one resource and never
// recording that the second modified resource was new.
func TestResourceMatchIsNotReused(t *testing.T) {
	// The Secret holds the "first unmatched" position and can never be paired with a
	// ConfigMap, so the scan reaches alpha for both modified resources. Only one of them
	// may claim it.
	previous := parseDocs(t, joinDocs(
		secretDoc("zeta"),
		configMapDoc("alpha", "timeout=30", "retries=3", "region=us-east"),
	))
	modified := parseDocs(t, joinDocs(
		configMapDoc("alpha-primary", "timeout=30", "retries=3", "region=us-east"),
		configMapDoc("alpha-secondary", "timeout=30", "retries=3", "region=us-west"),
	))

	mutations, err := ComputeMutations(previous, modified, 1, testProvider)
	require.NoError(t, err)

	types := resourceMutationTypes(mutations)
	assert.Equal(t, api.MutationTypeUpdate, types["ns/alpha-primary"],
		"the closest pairing is a rename of alpha")
	assert.Equal(t, api.MutationTypeAdd, types["ns/alpha-secondary"],
		"alpha is already claimed, so the second resource is new")
	assert.Equal(t, api.MutationTypeDelete, types["ns/zeta"])
	assert.Len(t, mutations, 3, "one mutation per resource on either side")
}

// TestResourceMatchPrefersExactName covers the ordering of the two passes. A resource that
// matches by name must not have its counterpart taken by a rename candidate that happens to
// be considered first.
func TestResourceMatchPrefersExactName(t *testing.T) {
	previous := parseDocs(t, configMapDoc("settings", "timeout=30", "retries=3"))
	modified := parseDocs(t, joinDocs(
		// Considered first, and a near-perfect content match for "settings".
		configMapDoc("settings-copy", "timeout=30", "retries=3"),
		// Matches by name, and must win.
		configMapDoc("settings", "timeout=45", "retries=3"),
	))

	mutations, err := ComputeMutations(previous, modified, 1, testProvider)
	require.NoError(t, err)

	assert.Equal(t, map[string]api.MutationType{
		"ns/settings":      api.MutationTypeUpdate,
		"ns/settings-copy": api.MutationTypeAdd,
	}, resourceMutationTypes(mutations))
}

// TestResourceMatchDetectsRename is the case the fuzzy pass exists for: the same resource
// under a new name, which must stay one Update carrying both names as aliases rather than
// becoming a Delete plus an Add.
func TestResourceMatchDetectsRename(t *testing.T) {
	previous := parseDocs(t, configMapDoc("settings", "timeout=30", "retries=3", "region=us-east"))
	modified := parseDocs(t, configMapDoc("settings-v2", "timeout=30", "retries=3", "region=us-east"))

	mutations, err := ComputeMutations(previous, modified, 1, testProvider)
	require.NoError(t, err)
	require.Len(t, mutations, 1)
	assert.Equal(t, api.MutationTypeUpdate, mutations[0].ResourceMutationInfo.MutationType)
	assert.Contains(t, mutations[0].Aliases, api.ResourceName("ns/settings"))
	assert.Contains(t, mutations[0].Aliases, api.ResourceName("ns/settings-v2"))
}
