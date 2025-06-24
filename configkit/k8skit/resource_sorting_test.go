// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"testing"

	"github.com/confighub/sdk/function/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSortResourcesByPriority(t *testing.T) {
	tests := []struct {
		name     string
		input    []resourceDoc
		expected []api.ResourceType
	}{
		{
			name: "Basic K8s resources ordering",
			input: []resourceDoc{
				{resourceType: testResourceTypeDeployment, resourceName: "test-ns/my-deployment"},
				{resourceType: testResourceTypeNamespace, resourceName: "/test-ns"},
				{resourceType: testResourceTypeServiceAccount, resourceName: "test-ns/my-sa"},
				{resourceType: testResourceTypeCRD, resourceName: "/my-crd"},
				{resourceType: testResourceTypeService, resourceName: "test-ns/my-service"},
			},
			// Based on actual priority-based ordering (CRDs must come first)
			expected: []api.ResourceType{
				testResourceTypeCRD,
				testResourceTypeNamespace,
				testResourceTypeServiceAccount,
				testResourceTypeService,
				testResourceTypeDeployment,
			},
		},
		{
			name: "RBAC resources ordering",
			input: []resourceDoc{
				{resourceType: testResourceTypeRoleBinding, resourceName: "test-ns/my-binding"},
				{resourceType: testResourceTypeClusterRole, resourceName: "/my-cluster-role"},
				{resourceType: testResourceTypeRole, resourceName: "test-ns/my-role"},
				{resourceType: testResourceTypeServiceAccount, resourceName: "test-ns/my-sa"},
			},
			expected: []api.ResourceType{
				testResourceTypeServiceAccount,
				testResourceTypeClusterRole,
				testResourceTypeRole,
				testResourceTypeRoleBinding,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sorted := sortResourcesByPriority(tt.input)
			require.Len(t, sorted, len(tt.expected))

			// Verify actual ordering matches expected
			actualTypes := make([]api.ResourceType, len(sorted))
			for i, doc := range sorted {
				actualTypes[i] = doc.resourceType
			}

			assert.Equal(t, tt.expected, actualTypes, "Resource ordering should match cli-utils expected order")
		})
	}
}
