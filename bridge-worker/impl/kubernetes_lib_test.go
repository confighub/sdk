// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"

	"github.com/fluxcd/pkg/ssa"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func setupMockKubernetesClient(kubeContext string) (KubernetesClient, ResourceManager, error) {
	mockClient := &MockK8sClient{}
	mockManager := &MockResourceManager{}

	// Configure mock behavior for Kubernetes client
	mockClient.On("Get", mock.MatchedBy(func(ctx context.Context) bool {
		return ctx != nil
	}), mock.MatchedBy(func(key client.ObjectKey) bool {
		return key.Name == "test-configmap" && key.Namespace == "default"
	}), mock.MatchedBy(func(obj client.Object) bool {
		u, ok := obj.(*unstructured.Unstructured)
		return ok && u != nil
	})).Return(nil)

	// Configure mock behavior for ResourceManager
	mockManager.On("ApplyAllStaged", mock.MatchedBy(func(ctx context.Context) bool {
		return ctx != nil
	}), mock.MatchedBy(func(objects []*unstructured.Unstructured) bool {
		return len(objects) == 1 && objects[0].GetName() == "test-configmap"
	}), mock.Anything).Return(&ssa.ChangeSet{}, nil)

	return mockClient, mockManager, nil
}
