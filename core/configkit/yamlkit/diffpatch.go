// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"bytes"
	"fmt"

	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/kio/filters"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// StripComments removes all comments from YAML data while preserving the structure and values.
// This is useful when comparing YAML documents where comments should be ignored.
func StripComments(yamlData []byte) ([]byte, error) {
	if len(yamlData) == 0 {
		return yamlData, nil
	}

	// Use kustomize's StripCommentsFilter to remove comments
	reader := &kio.ByteReader{Reader: bytes.NewReader(yamlData)}
	var buf bytes.Buffer
	writer := &kio.ByteWriter{Writer: &buf}

	err := kio.Pipeline{
		Inputs:  []kio.Reader{reader},
		Filters: []kio.Filter{filters.StripCommentsFilter{}},
		Outputs: []kio.Writer{writer},
	}.Execute()
	if err != nil {
		return nil, fmt.Errorf("failed to strip comments: %v", err)
	}

	return buf.Bytes(), nil
}

// DiffPatch compares original and modified YAML content, generates a patch, and applies it to target data
func DiffPatch(original, modified, targetData []byte, resourceProvider ResourceProvider) ([]byte, bool, error) {
	return DiffPatchWithOptions(original, modified, targetData, resourceProvider, false, nil)
}

// DiffPatchWithOptions compares original and modified YAML content, generates a patch, and applies it to target data
// If omitAdditions is true, mutations of type MutationTypeAdd are filtered out before applying the patch
func DiffPatchWithOptions(original, modified, targetData []byte, resourceProvider ResourceProvider, omitAdditions bool, whereExpressions []*api.VisitorRelationalExpression) ([]byte, bool, error) {
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

	// Filter out Add mutations if omitAdditions is true
	if omitAdditions {
		filteredMutations := make(api.ResourceMutationList, 0, len(mutations))
		for _, mutation := range mutations {
			// Check if this is a resource-level Add mutation
			if mutation.ResourceMutationInfo.MutationType == api.MutationTypeAdd {
				continue // Skip this mutation
			}

			// For other mutations, filter out Add mutations from PathMutationMap
			filteredPathMap := make(api.MutationMap)
			for path, pathMutation := range mutation.PathMutationMap {
				if pathMutation.MutationType != api.MutationTypeAdd {
					filteredPathMap[path] = pathMutation
				}
			}

			// Create a new mutation with filtered path mutations
			filteredMutation := mutation
			filteredMutation.PathMutationMap = filteredPathMap
			filteredMutations = append(filteredMutations, filteredMutation)
		}
		mutations = filteredMutations
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
	patchedResult, err := PatchMutations(parsedTargetData, nil, mutations, resourceProvider, whereExpressions)
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
