// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import "slices"

// builtInNamespaces are the namespaces the API server creates in every cluster
// (https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/#initial-namespaces).
// Kubernetes reserves the whole kube- prefix, but only these four are
// guaranteed to exist.
var builtInNamespaces = []string{
	"default",
	"kube-node-lease",
	"kube-public",
	"kube-system",
}

// BuiltInNamespaces returns the namespaces every Kubernetes cluster has from
// the start: default, kube-node-lease, kube-public and kube-system. Tooling
// can use it to avoid authoring a Namespace object for one, which would make
// whoever applies it the owner of a namespace the cluster depends on, and to
// avoid reporting one as missing.
func BuiltInNamespaces() []string {
	return slices.Clone(builtInNamespaces)
}

// IsBuiltInNamespace reports whether name is one of BuiltInNamespaces.
func IsBuiltInNamespace(name string) bool {
	return slices.Contains(builtInNamespaces, name)
}
