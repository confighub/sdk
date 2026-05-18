// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import "strings"

// builtInClusterRoles is the four user-facing default ClusterRoles
// Kubernetes ships in every cluster
// (https://kubernetes.io/docs/reference/access-authn-authz/rbac/).
// Tooling that inspects RoleBinding / ClusterRoleBinding references can
// use this list to recognize references that pre-exist in every cluster
// and therefore don't need to be represented as ConfigHub Units.
var builtInClusterRoles = map[string]struct{}{
	"cluster-admin": {},
	"admin":         {},
	"edit":          {},
	"view":          {},
}

// IsBuiltInClusterRole reports whether name is a Kubernetes built-in
// ClusterRole — either one of the four user-facing roles (cluster-admin,
// admin, edit, view) or any role under the `system:` prefix Kubernetes
// reserves for its core-component roles (system:node, system:kube-*,
// system:controller:*, etc.). See
// https://kubernetes.io/docs/reference/access-authn-authz/rbac/.
func IsBuiltInClusterRole(name string) bool {
	if _, ok := builtInClusterRoles[name]; ok {
		return true
	}
	return strings.HasPrefix(name, "system:")
}
