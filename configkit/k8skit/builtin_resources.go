// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import "strings"

// builtInPriorityClasses are the PriorityClasses the API server creates in every cluster.
var builtInPriorityClasses = map[string]bool{
	"system-cluster-critical": true,
	"system-node-critical":    true,
}

// IsBuiltInResource reports whether every Kubernetes cluster has a resource of the type, in the
// namespace (ignored for a cluster-scoped type), with the name, without anyone authoring it: a
// built-in namespace, a namespace's default ServiceAccount or its kube-root-ca.crt ConfigMap, a
// built-in or system: ClusterRole, a Role the bootstrap RBAC policy creates, or a built-in
// PriorityClass. A reference to one names something that exists although no configuration
// declares it. The type is <group>/<version>/<kind>, or <version>/<kind> for the core group, as
// ConfigHub writes resource types; the version is not compared.
func IsBuiltInResource(resourceType, namespace, name string) bool {
	switch groupKindOf(resourceType) {
	case "Namespace":
		return IsBuiltInNamespace(name)
	case "ServiceAccount":
		return name == "default"
	case "ConfigMap":
		return name == "kube-root-ca.crt"
	case "rbac.authorization.k8s.io/ClusterRole":
		return IsBuiltInClusterRole(name)
	case "rbac.authorization.k8s.io/Role":
		return IsBuiltInRole(namespace, name)
	case "scheduling.k8s.io/PriorityClass":
		return builtInPriorityClasses[name]
	}
	return false
}

// groupKindOf returns <group>/<kind>, or <kind> for the core group, of a resource type written
// <group>/<version>/<kind> or <version>/<kind>.
func groupKindOf(resourceType string) string {
	segments := strings.Split(resourceType, "/")
	if len(segments) < 2 {
		return resourceType
	}
	kind := segments[len(segments)-1]
	if len(segments) == 2 {
		return kind
	}
	return strings.Join(segments[:len(segments)-2], "/") + "/" + kind
}
