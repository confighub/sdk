// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package argocd

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Common test data for ArgoCD tests
var (
	testConfigMapYAML = []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-configmap
  namespace: default
data:
  key: value
`)

	testConfigMap = &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test-configmap",
				"namespace": "default",
			},
			"data": map[string]interface{}{
				"key": "value",
			},
		},
	}
)
