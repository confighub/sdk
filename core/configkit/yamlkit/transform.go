// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"fmt"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// TransformConfig applies a mutation function to configuration data, preserving
// YAML comments by diffing the changes and patching them onto the original data.
// This is a general-purpose mechanism that can be reused for any config transformation
// that operates on comment-stripped data (Starlark, CEL, etc.).
//
// The transform function receives comment-stripped parsed YAML and returns modified YAML bytes.
// If the transform function returns nil bytes, the original data is returned unchanged.
func TransformConfig(
	originalData []byte,
	resourceProvider ResourceProvider,
	transform func(parsedData gaby.Container) ([]byte, error),
	whereExpressions []*api.VisitorRelationalExpression,
) ([]byte, bool, error) {
	// Strip comments for the transformation
	strippedData, err := StripComments(originalData)
	if err != nil {
		return nil, false, fmt.Errorf("failed to strip comments: %w", err)
	}

	// Parse the stripped data
	parsedData, err := gaby.ParseAll(strippedData)
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Run the transformation
	modifiedData, err := transform(parsedData)
	if err != nil {
		return nil, false, err
	}

	// If nil, the transformation made no changes
	if modifiedData == nil {
		return originalData, false, nil
	}

	// Patch changes onto the original data (which has comments)
	patched, changed, err := DiffPatchWithOptions(strippedData, modifiedData, originalData, resourceProvider, false, whereExpressions)
	if err != nil {
		return nil, false, fmt.Errorf("failed to apply transformation: %w", err)
	}
	if !changed {
		return originalData, false, nil
	}

	return patched, true, nil
}
