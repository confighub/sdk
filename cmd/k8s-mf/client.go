// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/confighub/sdk/cmd/k8s-mf/mfclass"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/openapi3"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kube-openapi/pkg/validation/spec"
	"sigs.k8s.io/yaml"
)

// cluster bundles the clients needed to read and patch a single resource.
type cluster struct {
	dynamic dynamic.Interface
	mapper  meta.RESTMapper
	disco   discovery.DiscoveryInterface
}

// newCluster builds clients from the kubeconfig/context flags, mirroring
// kubectl's loading precedence (--kubeconfig, then KUBECONFIG, then the
// default path).
func newCluster() (*cluster, error) {
	restConfig, err := buildRESTConfig()
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}
	disco, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create discovery client: %w", err)
	}
	groupResources, err := restmapper.GetAPIGroupResources(disco)
	if err != nil {
		return nil, fmt.Errorf("discover API groups: %w", err)
	}
	mapper := restmapper.NewShortcutExpander(
		restmapper.NewDiscoveryRESTMapper(groupResources), disco, func(string) {})
	return &cluster{dynamic: dyn, mapper: mapper, disco: disco}, nil
}

// schemaProjector builds a schema-aware mfclass.Projector for obj from the
// cluster's OpenAPI v3, fetching only the object's group-version (self-contained
// in Kubernetes OpenAPI v3). It returns a schemaless projector when the schema
// can't be fetched or can't type the object, so callers always get a usable
// projector.
func (c *cluster) schemaProjector(obj *unstructured.Unstructured) *mfclass.Projector {
	return mfclass.NewProjector(c.typeConverter(obj))
}

func (c *cluster) typeConverter(obj *unstructured.Unstructured) managedfields.TypeConverter {
	gvSpec, err := openapi3.NewRoot(c.disco.OpenAPIV3()).GVSpec(obj.GroupVersionKind().GroupVersion())
	if err != nil || gvSpec == nil || gvSpec.Components == nil {
		return nil
	}
	schemas := make(map[string]*spec.Schema, len(gvSpec.Components.Schemas))
	for name, s := range gvSpec.Components.Schemas {
		schemas[name] = s
	}
	tc, err := managedfields.NewTypeConverter(schemas, false)
	if err != nil {
		return nil
	}
	// Verify the converter can actually type this object; otherwise the caller
	// should fall back to the schemaless path for consistent key handling.
	if _, err := tc.ObjectToTyped(obj); err != nil {
		return nil
	}
	return tc
}

func buildRESTConfig() (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if flagKubeconfig != "" {
		loadingRules.ExplicitPath = flagKubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if flagContext != "" {
		overrides.CurrentContext = flagContext
	}
	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	restConfig, err := cfg.ClientConfig()
	if err != nil {
		return nil, kubeconfigError(cfg, err)
	}
	return restConfig, nil
}

// kubeconfigError turns clientcmd's opaque empty-config error into actionable
// guidance. The most common cause is a kubeconfig with no current-context set
// (and no --context given), which clientcmd reports only as "no configuration
// has been provided" — so name the available contexts instead.
func kubeconfigError(cfg clientcmd.ClientConfig, err error) error {
	if raw, rawErr := cfg.RawConfig(); rawErr == nil && flagContext == "" && raw.CurrentContext == "" && len(raw.Contexts) > 0 {
		names := make([]string, 0, len(raw.Contexts))
		for name := range raw.Contexts {
			names = append(names, name)
		}
		sort.Strings(names)
		return fmt.Errorf("no current context is set in the kubeconfig; pass --context (available: %s)", strings.Join(names, ", "))
	}
	return fmt.Errorf("load kubeconfig: %w", err)
}

// mappingFor resolves a user-supplied type argument (kind, resource, plural,
// short name, or resource.group form — e.g. "deployment", "deployments",
// "deploy", "deployments.apps") to a REST mapping.
func (c *cluster) mappingFor(typeArg string) (*meta.RESTMapping, error) {
	fullySpecified, gr := schema.ParseResourceArg(strings.ToLower(typeArg))
	var gvk schema.GroupVersionKind
	var err error
	if fullySpecified != nil {
		gvk, err = c.mapper.KindFor(*fullySpecified)
	}
	if gvk.Empty() {
		gvk, err = c.mapper.KindFor(gr.WithVersion(""))
	}
	if err != nil {
		return nil, fmt.Errorf("resolve resource type %q: %w", typeArg, err)
	}
	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("REST mapping for %s: %w", gvk.String(), err)
	}
	return mapping, nil
}

// mappingForObject resolves the REST mapping for an object using its
// apiVersion and kind.
func (c *cluster) mappingForObject(obj *unstructured.Unstructured) (*meta.RESTMapping, error) {
	gvk := obj.GroupVersionKind()
	if gvk.Kind == "" {
		return nil, fmt.Errorf("object has no kind")
	}
	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("REST mapping for %s: %w", gvk.String(), err)
	}
	return mapping, nil
}

// resourceInterface returns the dynamic interface scoped to the right namespace.
// For namespaced resources an empty namespace falls back to flagNamespace;
// cluster-scoped resources ignore namespace entirely.
func (c *cluster) resourceInterface(mapping *meta.RESTMapping, namespace string) dynamic.ResourceInterface {
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if namespace == "" {
			namespace = flagNamespace
		}
		return c.dynamic.Resource(mapping.Resource).Namespace(namespace)
	}
	return c.dynamic.Resource(mapping.Resource)
}

// getLiveObject fetches a resource by type and name from the cluster.
func (c *cluster) getLiveObject(ctx context.Context, typeArg, name string) (*unstructured.Unstructured, error) {
	mapping, err := c.mappingFor(typeArg)
	if err != nil {
		return nil, err
	}
	obj, err := c.resourceInterface(mapping, flagNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s/%s: %w", typeArg, name, err)
	}
	return obj, nil
}

// inputObject resolves the object a read-only command operates on and the
// projector to analyze it with: from a file (-f, or "-" for stdin) when file is
// set — schemaless, since there is no cluster to fetch a schema from — otherwise
// from the live cluster, with a schema-aware projector built from the cluster's
// OpenAPI.
func inputObject(ctx context.Context, file string, args []string) (*unstructured.Unstructured, *mfclass.Projector, error) {
	if file != "" {
		if len(args) > 0 {
			return nil, nil, fmt.Errorf("provide either -f/--file or TYPE NAME, not both")
		}
		obj, err := loadObjectFromFile(file)
		if err != nil {
			return nil, nil, err
		}
		return obj, mfclass.NewProjector(nil), nil
	}
	if len(args) != 2 {
		return nil, nil, fmt.Errorf("specify a resource as TYPE NAME (e.g. \"deployment my-app\") or read one with -f")
	}
	c, err := newCluster()
	if err != nil {
		return nil, nil, err
	}
	obj, err := c.getLiveObject(ctx, args[0], args[1])
	if err != nil {
		return nil, nil, err
	}
	return obj, c.schemaProjector(obj), nil
}

// loadObjectFromFile reads a single Kubernetes object (YAML or JSON) from a
// file, or from stdin when path is "-". The object is expected to carry
// metadata.managedFields (e.g. `kubectl get -o yaml --show-managed-fields`).
func loadObjectFromFile(path string) (*unstructured.Unstructured, error) {
	var (
		data []byte
		err  error
	)
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m map[string]interface{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("%s contains no object", path)
	}
	return &unstructured.Unstructured{Object: m}, nil
}
