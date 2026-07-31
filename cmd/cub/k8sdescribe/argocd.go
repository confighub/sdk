// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8sdescribe

import (
	"fmt"
	"strings"
)

// argoApplicationView renders an argoproj.io Application.
func argoApplicationView(doc record) []*Section {
	spec := asRecord(doc["spec"])

	s := newSection("Application")
	s.field("Project", str(spec, "project"))
	destination := str(spec, "destination", "name")
	if destination == "" {
		destination = str(spec, "destination", "server")
	}
	s.field("Destination", destination)
	s.field("Dest Namespace", str(spec, "destination", "namespace"))
	s.field("Revision History", str(spec, "revisionHistoryLimit"))
	sections := []*Section{s}

	// Single-source apps use spec.source; multi-source apps use spec.sources.
	var sources []record
	if source := asRecord(spec["source"]); source != nil {
		sources = append(sources, source)
	}
	for _, src := range asArray(spec["sources"]) {
		if source := asRecord(src); source != nil {
			sources = append(sources, source)
		}
	}
	for i, source := range sources {
		title := "Source"
		if len(sources) > 1 {
			title = fmt.Sprintf("Source %d", i+1)
		}
		sections = add(sections, sourceSection(title, source))
	}

	if syncPolicy := asRecord(spec["syncPolicy"]); syncPolicy != nil {
		sync := newSection("Sync Policy")
		if asRecord(syncPolicy["automated"]) != nil {
			sync.field("Automated", "yes")
		} else {
			sync.field("Automated", "no (manual sync)")
		}
		sync.field("Prune", str(syncPolicy, "automated", "prune"))
		sync.field("Self Heal", str(syncPolicy, "automated", "selfHeal"))
		sync.field("Retry Limit", str(syncPolicy, "retry", "limit"))
		sync.field("Sync Options", joinList(syncPolicy["syncOptions"]))
		sections = add(sections, sync)
	}

	ignore := newSection("Ignore Differences")
	for i, d := range asArray(spec["ignoreDifferences"]) {
		kind := str(d, "kind")
		label := kind
		if name := str(d, "name"); name != "" {
			label = strings.TrimSpace(kind + " " + name)
		}
		if label == "" {
			label = fmt.Sprintf("#%d", i+1)
		}
		paths := append(asStringSlice(get(d, "jsonPointers")), asStringSlice(get(d, "jqPathExpressions"))...)
		ignore.field(label, strings.Join(paths, ", "))
	}

	return add(sections, ignore)
}

func sourceSection(title string, source record) *Section {
	helm := asRecord(source["helm"])
	kustomize := asRecord(source["kustomize"])

	s := newSection(title)
	s.field("Repo URL", str(source, "repoURL"))
	s.field("Chart", str(source, "chart"))
	s.field("Path", str(source, "path"))
	s.field("Target Revision", str(source, "targetRevision"))
	s.field("Ref", str(source, "ref"))
	s.field("Release Name", str(helm, "releaseName"))
	s.field("Value Files", joinList(helm["valueFiles"]))
	s.field("Kustomize Images", joinList(kustomize["images"]))
	s.field("Name Prefix", str(kustomize, "namePrefix"))
	s.field("Recurse", str(source, "directory", "recurse"))
	s.block("helm values", asString(helm["values"]))
	return s
}
