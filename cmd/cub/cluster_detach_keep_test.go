// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// Detaching a cluster whose Secrets a secret store keeps deletes argobot's
// resources but not its Namespace, which holds the store's Secrets.
func TestClusterWithoutNamespaces(t *testing.T) {
	data := "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: argobot\n---\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: argobot\n  namespace: argobot\n---\n# a comment only\n"
	got := clusterWithoutNamespaces(data)
	var kinds []string
	for _, doc := range strings.Split(got, "\n---") {
		var meta struct {
			Kind string `json:"kind"`
		}
		if err := yaml.Unmarshal([]byte(doc), &meta); err != nil {
			t.Fatalf("document %q: %v", doc, err)
		}
		if meta.Kind != "" {
			kinds = append(kinds, meta.Kind)
		}
	}
	if len(kinds) != 1 || kinds[0] != "Deployment" {
		t.Errorf("kinds = %v, want [Deployment]; got:\n%s", kinds, got)
	}
}
