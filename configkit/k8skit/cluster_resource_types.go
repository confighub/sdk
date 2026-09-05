// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
)

// Crossplane encodes scope in the API group rather than per type, which is the only tractable way
// to classify it: a provider family ships thousands of managed resource types and adds more with
// each release, so enumerating them in the specs would dwarf everything declared there and go
// stale immediately.
//
// Managed resources under *.upbound.io and *.crossplane.io are cluster-scoped, except the
// Crossplane v2 namespaced variants, which carry a ".m." infix (eks.aws.m.upbound.io). This is a
// generalization of data already in the map — every Crossplane control-plane type enumerated there
// (Composition, Provider, …) is cluster-scoped and matches the rule.
//
// It does not classify user-defined composite resources, whose API groups are arbitrary; those
// still need an explicit registration or a where-expression.
const (
	crossplaneUpboundSuffix   = ".upbound.io"
	crossplaneCoreSuffix      = ".crossplane.io"
	crossplaneNamespacedInfix = ".m."
)

// IsCrossplaneClusterScopedGroup reports whether a Crossplane API group is cluster-scoped by the
// group-suffix rule.
func IsCrossplaneClusterScopedGroup(group string) bool {
	if !strings.HasSuffix(group, crossplaneUpboundSuffix) && !strings.HasSuffix(group, crossplaneCoreSuffix) {
		return false
	}
	return !strings.Contains(group, crossplaneNamespacedInfix)
}

// IsResourceTypeClusterScoped reports whether a resource type is cluster-scoped: either the
// resource-type specs declare it so, or its API group matches the Crossplane group-suffix rule
// (see IsCrossplaneClusterScopedGroup).
//
// A type the specs say nothing about is treated as namespaced, which is what the enumeration
// this replaced meant by leaving a type out. The specs declare the namespaced ones they know as
// well, so that a spec can state the fact rather than rely on the absence of one.
func IsResourceTypeClusterScoped(rt api.ResourceType) bool {
	switch ScopeOf(rt) {
	case yamlkit.ScopeCluster:
		return true
	case yamlkit.ScopeNamespaced:
		return false
	}
	return IsCrossplaneClusterScopedGroup(apiGroupOf(rt))
}

// apiGroupOf extracts the API group from a "[group/]version/Kind" resource type. A core type
// ("v1/Pod") has no group and yields "".
func apiGroupOf(rt api.ResourceType) string {
	parts := strings.Split(string(rt), "/")
	if len(parts) < 3 {
		return ""
	}
	return parts[0]
}

// IsClusterScoped returns true if the given apiVersion and kind represent a cluster-scoped resource.
// It checks the scope the resource-type specs declare, and the Crossplane group-suffix rule.
func IsClusterScoped(apiVersion, kind string) bool {
	resourceType := api.ResourceType(apiVersion + "/" + kind)
	return IsResourceTypeClusterScoped(resourceType)
}
