// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8sdescribe

import "strings"

// roleView renders Role and ClusterRole: the policy rules table.
func roleView(doc record) []*Section {
	s := newSection("Rules")
	var rows [][]string
	for _, r := range asArray(doc["rules"]) {
		resources := joinList(get(r, "resources"))
		if resources == "" {
			resources = joinList(get(r, "nonResourceURLs"))
		}
		rows = append(rows, []string{
			joinOrCore(get(r, "apiGroups")),
			resources,
			joinList(get(r, "verbs")),
			joinList(get(r, "resourceNames")),
		})
	}
	s.table([]string{"API Groups", "Resources", "Verbs", "Resource Names"}, rows)
	if s.Empty() {
		s.note("This role grants no permissions.")
	}
	return []*Section{s}
}

// joinOrCore renders an apiGroups list, spelling the empty-string group — which
// means the core group — as "(core)" rather than dropping it.
func joinOrCore(v any) string {
	entries := asArray(v)
	groups := make([]string, 0, len(entries))
	for _, e := range entries {
		if group := asString(e); group == "" {
			groups = append(groups, "(core)")
		} else {
			groups = append(groups, group)
		}
	}
	return strings.Join(groups, ", ")
}

// roleBindingView renders RoleBinding and ClusterRoleBinding.
func roleBindingView(doc record) []*Section {
	ref := newSection("Role Reference")
	ref.field("Kind", str(doc, "roleRef", "kind"))
	ref.field("Name", str(doc, "roleRef", "name"))
	ref.field("API Group", str(doc, "roleRef", "apiGroup"))

	subjects := newSection("Subjects")
	var rows [][]string
	for _, s := range asArray(doc["subjects"]) {
		rows = append(rows, []string{str(s, "kind"), str(s, "name"), str(s, "namespace")})
	}
	subjects.table([]string{"Kind", "Name", "Namespace"}, rows)
	if subjects.Empty() {
		subjects.note("No subjects bound.")
	}

	return add([]*Section{ref}, subjects)
}
