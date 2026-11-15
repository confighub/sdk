// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"testing"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
)

// The stanzas are pasted into the built-in spec file, so they have to load back as what was read:
// a deprecated version's warning included, quoted where YAML needs it.
func TestCRDSpecStanzasLoadBack(t *testing.T) {
	const crd = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: widgets.example.com}
spec:
  group: example.com
  names: {kind: Widget}
  scope: Namespaced
  versions:
    - name: v1beta1
      deprecated: true
      deprecationWarning: "example.com/v1beta1 Widget is deprecated: use example.com/v1 Widget, #2"
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                ports:
                  type: array
                  x-kubernetes-list-type: map
                  x-kubernetes-list-map-keys: [name]
                  items: {type: object, properties: {name: {type: string}}}
    - name: v1
      schema: {openAPIV3Schema: {type: object}}
`
	specs, err := k8skit.SpecsFromCRD([]byte(crd), k8skit.DefaultCRDSpecOptions())
	if err != nil {
		t.Fatal(err)
	}

	stanzas := renderCRDSpecStanzas(specs, "widgets.yaml")
	set, err := yamlkit.LoadSpecSet([]byte("toolchainType: Kubernetes/YAML\nresourceTypes:\n" + stanzas))
	if err != nil {
		t.Fatalf("the rendered stanzas do not load: %v\n%s", err, stanzas)
	}
	if len(set.ResourceTypes) != 2 {
		t.Fatalf("loaded %d types, want 2:\n%s", len(set.ResourceTypes), stanzas)
	}

	deprecated := set.ResourceTypes[0]
	if deprecated.Type != specs[0].Spec.Type || !deprecated.Deprecated {
		t.Errorf("loaded %s, deprecated %v; want %s, deprecated", deprecated.Type, deprecated.Deprecated, specs[0].Spec.Type)
	}
	if want := "example.com/v1beta1 Widget is deprecated: use example.com/v1 Widget, #2"; deprecated.DeprecationWarning != want {
		t.Errorf("warning = %q, want %q", deprecated.DeprecationWarning, want)
	}
	if !reflect.DeepEqual(deprecated.MergeKeys, specs[0].Spec.MergeKeys) {
		t.Errorf("merge keys = %v, want %v", deprecated.MergeKeys, specs[0].Spec.MergeKeys)
	}
	if set.ResourceTypes[1].Deprecated {
		t.Errorf("%s was rendered deprecated", set.ResourceTypes[1].Type)
	}
	if _, err := yamlkit.CompileSpecSets(set); err != nil {
		t.Errorf("the loaded stanzas do not compile: %v", err)
	}
}
