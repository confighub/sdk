// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"fmt"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// KubernetesConfigFactory creates a Kubernetes rest.Config for the given
// context. Override in tests to inject a fake config.
var KubernetesConfigFactory = setupKubernetesConfig

func setupKubernetesConfig(kubeContext string) (*rest.Config, error) {
	return config.GetConfigWithContext(kubeContext)
}

// KubernetesClientFactory creates a controller-runtime client bound to the
// given context. Override in tests to inject a fake client.
var KubernetesClientFactory = setupKubernetesClient

func setupKubernetesClient(kubeContext string) (KubernetesClient, error) {
	cfg, err := KubernetesConfigFactory(kubeContext)
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes config: %w", err)
	}
	log.Log.Info("✅ Got Kubernetes config")

	k8sClient, err := client.New(cfg, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}
	log.Log.Info("✅ Created Kubernetes client")

	return k8sClient, nil
}
