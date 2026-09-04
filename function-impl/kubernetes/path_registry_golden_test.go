// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/confighub/sdk/core/function/api"
	"github.com/stretchr/testify/require"
)

// The path registry is what every attribute-reading function looks through: which attribute
// a type has, where it lives, what type the value is, and which getter and setter reach it.
// Today it is populated by nine hand-rolled loops over the k8skit tables making thirty
// RegisterPathsByAttributeName calls. docs/design/resource-type-specs.md §9.1 replaces those
// loops with compiled resource-type specs and asks for a differential test written first,
// covering both halves of the output.
//
// This is the registry half; public/configkit/k8skit/structure_golden_test.go is the
// structure half. The capture includes the per-target reference attribute names that
// attributeNameForResourceType mints (resource-name-apps-v1-Deployment and its hundred-odd
// siblings), which is what proves the compiler's `target:` expansion is exact.
//
// Each registration is captured as the JSON the function server publishes for it on its
// /paths route, one per line under the key it is registered at, so a difference here is a
// difference a worker would serve and it shows up as one changed line rather than as a
// reindented block. Marshaling rather than rendering by hand is what keeps a field nobody
// thought about from being silently omitted from the comparison; Enricher and
// TypeExceptionFunc are function values and are not serialized, in the capture or on the
// wire.
//
// Regenerate with: go test ./public/function-impl/kubernetes/ -run TestPathRegistryGolden -update-golden

var updatePathRegistryGolden = flag.Bool("update-golden", false, "rewrite the path-registry golden file")

const pathRegistryGoldenPath = "testdata/path_registry.golden"

func TestPathRegistryGolden(t *testing.T) {
	got := renderRegistries(
		t,
		testResourceProvider.GetPathRegistry(),
		testResourceProvider.GetAttributeRegistry(),
	)

	if *updatePathRegistryGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(pathRegistryGoldenPath), 0o755))
		require.NoError(t, os.WriteFile(pathRegistryGoldenPath, []byte(got), 0o644))
		t.Logf("wrote %s (%d bytes)", pathRegistryGoldenPath, len(got))
		return
	}

	want, err := os.ReadFile(pathRegistryGoldenPath)
	require.NoError(t, err, "golden file missing; run with -update-golden")
	require.Equal(t, string(want), got,
		"path registry differs from the golden capture; if the change is intended, rerun with -update-golden")
}

func renderRegistries(
	t *testing.T,
	pathRegistry api.AttributeNameToResourceTypeToPathToVisitorInfoType,
	attributeRegistry api.AttributeNameToAttributeDescriptor,
) string {
	t.Helper()
	var b strings.Builder

	b.WriteString("# attribute descriptors: attributeName\tjson\n")
	for _, name := range sortedAttributeNames(attributeRegistry) {
		fmt.Fprintf(&b, "descriptor\t%s\t%s\n", name, marshalCompact(t, attributeRegistry[name]))
	}

	b.WriteString("\n# registered paths: attributeName\tresourceType\tregisteredPath\tjson\n")
	for _, name := range sortedAttributeNames(pathRegistry) {
		byResourceType := pathRegistry[name]
		for _, resourceType := range sortedRegistryResourceTypes(byResourceType) {
			byPath := byResourceType[resourceType]
			for _, path := range sortedPaths(byPath) {
				fmt.Fprintf(&b, "path\t%s\t%s\t%s\t%s\n",
					name, resourceType, path, marshalCompact(t, byPath[path]))
			}
		}
	}

	return b.String()
}

func marshalCompact(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}

func sortedAttributeNames[V any](m map[api.AttributeName]V) []api.AttributeName {
	out := make([]api.AttributeName, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sortedRegistryResourceTypes[V any](m map[api.ResourceType]V) []api.ResourceType {
	out := make([]api.ResourceType, 0, len(m))
	for rt := range m {
		out = append(out, rt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sortedPaths[V any](m map[api.UnresolvedPath]V) []api.UnresolvedPath {
	out := make([]api.UnresolvedPath, 0, len(m))
	for path := range m {
		out = append(out, path)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
