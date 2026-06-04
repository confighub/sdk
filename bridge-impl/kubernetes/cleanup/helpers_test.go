// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cleanup

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

func createTestDeployment(name, namespace string, replicas int64) *unstructured.Unstructured {
	deployment := &unstructured.Unstructured{}
	deployment.SetAPIVersion("apps/v1")
	deployment.SetKind("Deployment")
	deployment.SetName(name)
	deployment.SetNamespace(namespace)
	deployment.Object["spec"] = map[string]interface{}{
		"replicas": replicas,
		"template": map[string]interface{}{
			"spec": map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{
						"name":  "test",
						"image": "nginx",
					},
				},
			},
		},
	}
	return deployment
}

func createTestService(name, namespace, serviceType string, port int64) *unstructured.Unstructured {
	service := &unstructured.Unstructured{}
	service.SetAPIVersion("v1")
	service.SetKind("Service")
	service.SetName(name)
	service.SetNamespace(namespace)
	service.Object["spec"] = map[string]interface{}{
		"type": serviceType,
		"ports": []interface{}{
			map[string]interface{}{
				"port": port,
			},
		},
	}
	return service
}
