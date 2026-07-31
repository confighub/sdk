// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8sdescribe

import "strings"

// refLabel renders a Flux cross-resource reference as "Kind name" or
// "Kind namespace/name".
func refLabel(ref any) string {
	rec := asRecord(ref)
	if rec == nil {
		return ""
	}
	qualified := str(rec, "name")
	if namespace := str(rec, "namespace"); namespace != "" {
		qualified = namespace + "/" + qualified
	}
	return strings.TrimSpace(str(rec, "kind") + " " + qualified)
}

// helmReleaseView renders a helm.toolkit.fluxcd.io HelmRelease.
func helmReleaseView(doc record) []*Section {
	spec := asRecord(doc["spec"])
	chartSpec := asRecord(get(spec, "chart", "spec"))
	install := asRecord(spec["install"])
	upgrade := asRecord(spec["upgrade"])

	s := newSection("Helm Release")
	s.field("Release Name", str(spec, "releaseName"))
	s.field("Target Namespace", str(spec, "targetNamespace"))
	s.field("Storage Namespace", str(spec, "storageNamespace"))
	s.field("Interval", str(spec, "interval"))
	s.field("Timeout", str(spec, "timeout"))
	s.field("Suspend", str(spec, "suspend"))
	s.field("Service Account", str(spec, "serviceAccountName"))
	s.field("Depends On", dependsOnLabel(spec["dependsOn"]))

	chart := newSection("Chart")
	chart.field("Chart", str(chartSpec, "chart"))
	chart.field("Version", str(chartSpec, "version"))
	chart.field("Source", refLabel(chartSpec["sourceRef"]))
	// Newer HelmReleases may reference an OCIRepository/HelmChart directly via
	// chartRef instead of an inline chart template.
	chart.field("Chart Ref", refLabel(spec["chartRef"]))
	chart.field("Reconcile Strategy", str(chartSpec, "reconcileStrategy"))

	remediation := newSection("Remediation")
	remediation.field("Install Retries", str(install, "remediation", "retries"))
	remediation.field("Create Namespace", str(install, "createNamespace"))
	remediation.field("Upgrade Retries", str(upgrade, "remediation", "retries"))
	remediation.field("Cleanup On Fail", str(upgrade, "cleanupOnFail"))

	values := newSection("Values")
	var rows [][]string
	for _, v := range asArray(spec["valuesFrom"]) {
		rows = append(rows, []string{str(v, "kind"), str(v, "name"), str(v, "valuesKey")})
	}
	values.table([]string{"Kind", "Name", "Key"}, rows)
	if inline := asRecord(spec["values"]); inline != nil {
		values.block("values", yamlText(inline))
	}

	sections := add([]*Section{s}, chart)
	sections = add(sections, remediation)
	return add(sections, values)
}

// fluxKustomizationView renders a kustomize.toolkit.fluxcd.io Kustomization.
func fluxKustomizationView(doc record) []*Section {
	spec := asRecord(doc["spec"])

	s := newSection("Kustomization")
	s.field("Source", refLabel(spec["sourceRef"]))
	s.field("Path", str(spec, "path"))
	s.field("Interval", str(spec, "interval"))
	s.field("Prune", str(spec, "prune"))
	s.field("Suspend", str(spec, "suspend"))
	s.field("Target Namespace", str(spec, "targetNamespace"))
	s.field("Service Account", str(spec, "serviceAccountName"))
	s.field("Wait", str(spec, "wait"))
	s.field("Timeout", str(spec, "timeout"))
	s.field("Depends On", dependsOnLabel(spec["dependsOn"]))
	if patches := asArray(spec["patches"]); len(patches) > 0 {
		s.field("Patches", asScalar(len(patches)))
	}
	sections := []*Section{s}

	images := newSection("Image Overrides")
	var imageRows [][]string
	for _, i := range asArray(spec["images"]) {
		newTag := str(i, "newTag")
		if newTag == "" {
			newTag = str(i, "digest")
		}
		imageRows = append(imageRows, []string{str(i, "name"), str(i, "newName"), newTag})
	}
	images.table([]string{"Name", "New Name", "New Tag"}, imageRows)
	sections = add(sections, images)

	postBuild := newSection("Post-Build Substitutions")
	substitute := asStringMap(get(spec, "postBuild", "substitute"))
	var substRows [][]string
	for _, k := range sortedKeys(substitute) {
		substRows = append(substRows, []string{k, substitute[k]})
	}
	postBuild.table([]string{"Variable", "Value"}, substRows)
	var from []string
	for _, sf := range asArray(get(spec, "postBuild", "substituteFrom")) {
		if label := refLabel(sf); label != "" {
			from = append(from, label)
		}
	}
	postBuild.field("Substitute From", strings.Join(from, ", "))
	sections = add(sections, postBuild)

	health := newSection("Health Checks")
	var healthRows [][]string
	for _, h := range asArray(spec["healthChecks"]) {
		healthRows = append(healthRows, []string{str(h, "kind"), str(h, "name"), str(h, "namespace")})
	}
	health.table([]string{"Kind", "Name", "Namespace"}, healthRows)

	return add(sections, health)
}

func dependsOnLabel(v any) string {
	var deps []string
	for _, d := range asArray(v) {
		name := str(d, "name")
		if namespace := str(d, "namespace"); namespace != "" {
			name = namespace + "/" + name
		}
		if name != "" {
			deps = append(deps, name)
		}
	}
	return strings.Join(deps, ", ")
}

// fluxSourceView renders the source.toolkit.fluxcd.io sources (GitRepository,
// HelmRepository, HelmChart, OCIRepository, Bucket). Their specs are similar
// enough — URL-ish locator, interval, ref/version selector, auth secret — to
// share one view.
func fluxSourceView(doc record) []*Section {
	kind := asScalar(doc["kind"])
	if kind == "" {
		kind = "Source"
	}
	spec := asRecord(doc["spec"])
	ref := asRecord(spec["ref"])

	s := newSection(kind)
	s.field("URL", str(spec, "url"))
	// HelmRepository: "oci" for OCI registries, default is HTTP.
	s.field("Type", str(spec, "type"))
	// Bucket fields.
	s.field("Bucket", str(spec, "bucketName"))
	s.field("Endpoint", str(spec, "endpoint"))
	s.field("Provider", str(spec, "provider"))
	// HelmChart fields.
	s.field("Chart", str(spec, "chart"))
	s.field("Version", str(spec, "version"))
	s.field("Source", refLabel(spec["sourceRef"]))
	// GitRepository / OCIRepository ref selectors.
	s.field("Branch", str(ref, "branch"))
	s.field("Tag", str(ref, "tag"))
	s.field("SemVer", str(ref, "semver"))
	s.field("Commit", str(ref, "commit"))
	s.field("Digest", str(ref, "digest"))
	s.field("Interval", str(spec, "interval"))
	s.field("Timeout", str(spec, "timeout"))
	s.field("Suspend", str(spec, "suspend"))
	s.field("Auth Secret", str(spec, "secretRef", "name"))
	s.field("Ignore Paths", ignoreSummary(asString(spec["ignore"])))
	return []*Section{s}
}

// ignoreSummary collapses a multi-line .sourceignore-style value into one line.
func ignoreSummary(ignore string) string {
	var patterns []string
	for line := range strings.SplitSeq(ignore, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}
	return strings.Join(patterns, " ")
}

// fluxAlertView renders a notification.toolkit.fluxcd.io Alert.
func fluxAlertView(doc record) []*Section {
	spec := asRecord(doc["spec"])

	s := newSection("Alert")
	s.field("Provider", refLabel(spec["providerRef"]))
	s.field("Severity", str(spec, "eventSeverity"))
	s.field("Suspend", str(spec, "suspend"))

	sources := newSection("Event Sources")
	var rows [][]string
	for _, es := range asArray(spec["eventSources"]) {
		rows = append(rows, []string{str(es, "kind"), str(es, "name"), str(es, "namespace")})
	}
	sources.table([]string{"Kind", "Name", "Namespace"}, rows)

	return add([]*Section{s}, sources)
}
