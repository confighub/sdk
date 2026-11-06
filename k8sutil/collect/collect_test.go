// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package collect

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	fakemetadata "k8s.io/client-go/metadata/fake"
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
	meta := nodeMetadataClient(t,
		map[string]string{"kubernetes.io/hostname": "node-a", zoneLabel: "us-east-1a", "pool": "general"},
		map[string]string{"kubernetes.io/hostname": "node-b", zoneLabel: "us-east-1b"})

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

	got, err := facts(context.Background(), disc, dyn, meta)
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
		"Cluster.Zones":                        "us-east-1a,us-east-1b",
		"Cluster.NodeLabelKeys":                "kubernetes.io/hostname,pool,topology.kubernetes.io/zone",
		"Cluster.Distribution":                 "",
	}, got)

	// Without permission to list Nodes, the facts read from them are left out,
	// and so is the distribution, which the version alone does not settle here.
	meta.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(nodeGVR.GroupResource(), "", nil)
	})
	got, err = facts(context.Background(), disc, dyn, meta)
	require.NoError(t, err)
	assert.NotContains(t, got, "Cluster.Zones")
	assert.NotContains(t, got, "Cluster.NodeLabelKeys")
	assert.NotContains(t, got, "Cluster.Distribution", "unknown is not reported as none")

	// An EKS server version names the service without the Nodes.
	disc.FakedServerVersion = &version.Info{GitVersion: "v1.31.2-eks-7f9249a"}
	got, err = facts(context.Background(), disc, dyn, meta)
	require.NoError(t, err)
	assert.Equal(t, "eks", got["Cluster.Distribution"])
}

// nodeMetadataClient serves one Node per label set, as metadata only.
func nodeMetadataClient(t *testing.T, labels ...map[string]string) *fakemetadata.FakeMetadataClient {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, metav1.AddMetaToScheme(scheme))
	var objs []runtime.Object
	for i, l := range labels {
		objs = append(objs, &metav1.PartialObjectMetadata{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Node"},
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("node-%d", i), Labels: l},
		})
	}
	return fakemetadata.NewSimpleMetadataClient(scheme, objs...)
}

func TestDistribution(t *testing.T) {
	cases := []struct {
		version     string
		labels      []map[string]string
		nodesListed bool
		want        string
		known       bool
	}{
		{"v1.31.2-eks-7f9249a", nil, false, "eks", true},
		{"v1.31.2-gke.1146000", nil, false, "gke", true},
		{"v1.31.2", []map[string]string{{}, {aksClusterLabel: "mc_rg_cluster"}}, true, "aks", true},
		{"v1.31.2+k3s1", []map[string]string{{"kubernetes.io/hostname": "n"}}, true, "", true},
		{"v1.31.2", nil, false, "", false},
	}
	for _, c := range cases {
		got, known := distribution(c.version, c.labels, c.nodesListed)
		assert.Equal(t, c.want, got, c.version)
		assert.Equal(t, c.known, known, c.version)
	}
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
