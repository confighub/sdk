// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"strings"

	"github.com/confighub/sdk/core/function/api"
)

// Helper functions to extract fields from ResourceNames

// GetNameFromResourceName extracts name from "namespace/name" or "/name" format
func GetNameFromResourceName(resourceName api.ResourceName) string {
	nameStr := string(resourceName)
	if strings.HasPrefix(nameStr, "/") {
		// Cluster-scoped: "/name"
		return nameStr[1:]
	} else {
		// Namespaced: "namespace/name"
		parts := strings.SplitN(nameStr, "/", 2)
		if len(parts) == 2 {
			return parts[1]
		}
	}
	return ""
}

// GetNamespaceFromResourceName extracts namespace from "namespace/name" format (empty for cluster-scoped)
func GetNamespaceFromResourceName(resourceName api.ResourceName) string {
	nameStr := string(resourceName)
	if strings.HasPrefix(nameStr, "/") {
		// Cluster-scoped: "/name" - no namespace
		return ""
	} else {
		// Namespaced: "namespace/name"
		parts := strings.SplitN(nameStr, "/", 2)
		if len(parts) == 2 {
			return parts[0]
		}
	}
	return ""
}
