// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package collect

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func object(apiVersion, kind, name string, fields map[string]any) *unstructured.Unstructured {
	obj := map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   map[string]any{"name": name},
	}
	for k, v := range fields {
		obj[k] = v
	}
	return &unstructured.Unstructured{Object: obj}
}

func TestFacts(t *testing.T) {
	widget := object("apiextensions.k8s.io/v1", "CustomResourceDefinition", "widgets.example.com", map[string]any{
		"spec": map[string]any{
			"group": "example.com",
			"scope": "Namespaced",
			"names": map[string]any{"kind": "Widget"},
			"versions": []any{
				map[string]any{"name": "v1", "served": true},
				map[string]any{"name": "v1beta1", "served": false},
			},
		},
	})
	gadget := object("apiextensions.k8s.io/v1", "CustomResourceDefinition", "gadgets.example.com", map[string]any{
		"spec": map[string]any{
			"group":    "example.com",
			"scope":    "Cluster",
			"names":    map[string]any{"kind": "Gadget"},
			"versions": []any{map[string]any{"name": "v1", "served": true}},
		},
	})
	standard := object("storage.k8s.io/v1", "StorageClass", "standard", nil)
	standard.SetAnnotations(map[string]string{defaultStorageClassAnnotation: "true"})
	fast := object("storage.k8s.io/v1", "StorageClass", "fast", nil)
	nginx := object("networking.k8s.io/v1", "IngressClass", "nginx", nil)

	dyn := fakedynamic.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		crdGVR:          "CustomResourceDefinitionList",
		storageClassGVR: "StorageClassList",
		ingressClassGVR: "IngressClassList",
	}, widget, gadget, standard, fast, nginx)

	disc := &fakediscovery.FakeDiscovery{
		Fake:               &k8stesting.Fake{},
		FakedServerVersion: &version.Info{GitVersion: "v1.31.2"},
	}
	disc.Resources = []*metav1.APIResourceList{
		{GroupVersion: "v1", APIResources: []metav1.APIResource{
			{Name: "pods", Kind: "Pod", Namespaced: true},
			{Name: "pods/status", Kind: "Pod", Namespaced: true},
			{Name: "namespaces", Kind: "Namespace"},
		}},
		{GroupVersion: "apps/v1", APIResources: []metav1.APIResource{
			{Name: "deployments", Kind: "Deployment", Namespaced: true},
		}},
		{GroupVersion: "example.com/v1", APIResources: []metav1.APIResource{
			{Name: "widgets", Kind: "Widget", Namespaced: true},
			{Name: "gadgets", Kind: "Gadget"},
		}},
	}

	got, err := facts(context.Background(), disc, dyn)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"Cluster.KubernetesVersion":            "1.31.2",
		"Cluster.ClusterScopedCRDs":            "example.com/v1/Gadget",
		"Cluster.NamespaceScopedCRDs":          "example.com/v1/Widget",
		"Cluster.ClusterScopedResourceTypes":   "v1/Namespace",
		"Cluster.NamespaceScopedResourceTypes": "apps/v1/Deployment,v1/Pod",
		"Cluster.StorageClasses":               "fast,standard",
		"Cluster.DefaultStorageClass":          "standard",
		"Cluster.IngressClasses":               "nginx",
	}, got)
}

func TestPatch(t *testing.T) {
	current := map[string]string{
		"Cluster.KubernetesVersion": "1.30.0",
		"Cluster.IngressClasses":    "nginx",
		"Cluster.Name":              "prod-eu",
		"Team":                      "payments",
	}
	collected := map[string]string{"Cluster.KubernetesVersion": "1.31.2"}

	assert.Equal(t, map[string]any{
		"Cluster.KubernetesVersion": "1.31.2",
		"Cluster.IngressClasses":    nil,
	}, Patch(current, collected), "a stale collected fact is removed; the name and user facts are kept")

	collected[NameFact] = "prod-us"
	assert.Equal(t, "prod-us", Patch(current, collected)[NameFact], "a collected name replaces the existing one")
}
