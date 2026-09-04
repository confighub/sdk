// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/confighub/sdk/core/workerapi"
	"github.com/stretchr/testify/require"
)

// The structure lookups -- merge keys, exclusive fields, and map-key paths -- are what
// decide what a stored path *is*: with a merge key a container is `containers.?name=runner`,
// without one it is `containers.0`. docs/design/resource-type-specs.md §9.1 replaces the
// per-concern tables that fed them with compiled resource-type specs, and asks for a
// differential test written first, so the conversion is provably lossless rather than
// merely plausible.
//
// This is that test's structure half. It renders the compiled snapshot and compares against
// a golden file captured from the tables before the conversion. The registry half lives in
// public/function-impl/kubernetes/path_registry_golden_test.go, because the path registry is
// only populated once the functions register into it.
//
// Regenerate with: go test ./public/configkit/k8skit/ -run TestStructureLookupsGolden -update-golden

var updateStructureGolden = flag.Bool("update-golden", false, "rewrite the golden captures")

const (
	structureGoldenPath  = "testdata/structure_lookups.golden"
	attributesGoldenPath = "testdata/declared_attributes.golden"
)

func TestStructureLookupsGolden(t *testing.T) {
	compareGolden(t, structureGoldenPath, compiledK8sSpecs.RenderStructure(workerapi.ToolchainKubernetesYAML))
}

// Not every declared attribute reaches the path registry: workload-labels is handed straight
// to a visitor, so the registry capture in function-impl says nothing about it. This covers
// what a type declares, registered or not.
func TestDeclaredAttributesGolden(t *testing.T) {
	compareGolden(t, attributesGoldenPath, compiledK8sSpecs.RenderAttributes(workerapi.ToolchainKubernetesYAML))
}

func compareGolden(t *testing.T, goldenPath, got string) {
	t.Helper()

	if *updateStructureGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(goldenPath), 0o755))
		require.NoError(t, os.WriteFile(goldenPath, []byte(got), 0o644))
		t.Logf("wrote %s (%d bytes)", goldenPath, len(got))
		return
	}

	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "golden file missing; run with -update-golden")
	require.Equal(t, string(want), got,
		"%s differs from the golden capture; if the change is intended, rerun with -update-golden", goldenPath)
}
