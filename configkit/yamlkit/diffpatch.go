// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"bytes"
	"fmt"

	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
)

// TODO: move this to somewhere more appropriate; maybe yamlkit

// DiffPatch compares original and modified YAML content, generates a patch, and applies it to target data
func DiffPatch(original, modified, targetData []byte, resourceProvider ResourceProvider) ([]byte, bool, error) {
	// Parse original YAML content
	originalYAML, err := gaby.ParseAll(original)
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse original YAML: %v", err)
	}

	// Parse modified YAML content
	modifiedYAML, err := gaby.ParseAll(modified)
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse modified YAML: %v", err)
	}

	// Compute mutations from original and modified YAML
	mutations, err := ComputeMutations(originalYAML, modifiedYAML, 0, resourceProvider)
	if err != nil {
		return nil, false, fmt.Errorf("failed to diff YAML resources: %v", err)
	}

	// If no differences found, return original target data unchanged
	if api.NoMutations(mutations) {
		return targetData, false, nil
	}

	// Parse target data to apply patch
	parsedTargetData, err := gaby.ParseAll(targetData)
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse target YAML: %v", err)
	}

	// Apply patch to target data
	patchedResult, err := PatchMutations(parsedTargetData, nil, mutations, resourceProvider)
	if err != nil {
		return nil, false, fmt.Errorf("failed to apply patch: %v", err)
	}

	// Filter out nil/empty documents before serializing
	var buf bytes.Buffer
	count := 0
	for _, doc := range patchedResult {
		if doc == nil || doc.IsEmptyDoc() {
			continue
		}
		out, err := doc.MarshalYAML()
		if err != nil {
			return nil, false, fmt.Errorf("failed to marshal patched YAML: %v", err)
		}
		if count > 0 {
			buf.WriteString("---\n")
		}
		buf.Write(out)
		if len(out) > 0 && out[len(out)-1] != '\n' {
			buf.WriteByte('\n')
		}
		count++
	}
	return buf.Bytes(), true, nil
}
