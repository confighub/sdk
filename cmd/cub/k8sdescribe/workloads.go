// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8sdescribe

import "strings"

// Views for pod-based workloads. They differ only in their top-of-spec fields
// (replicas/strategy vs schedule vs completions); the pod template rendering
// is shared.

func deploymentView(doc record) []*Section {
	spec := asRecord(doc["spec"])
	s := newSection("Deployment")
	s.field("Replicas", str(spec, "replicas"))
	s.field("Strategy", str(spec, "strategy", "type"))
	s.field("Max Surge", str(spec, "strategy", "rollingUpdate", "maxSurge"))
	s.field("Max Unavailable", str(spec, "strategy", "rollingUpdate", "maxUnavailable"))
	s.field("Selector", selectorString(asStringMap(get(spec, "selector", "matchLabels"))))
	return append([]*Section{s}, podTemplateSections(asRecord(spec["template"]))...)
}

func statefulSetView(doc record) []*Section {
	spec := asRecord(doc["spec"])
	s := newSection("StatefulSet")
	s.field("Replicas", str(spec, "replicas"))
	s.field("Service Name", str(spec, "serviceName"))
	s.field("Update Strategy", str(spec, "updateStrategy", "type"))
	s.field("Pod Management", str(spec, "podManagementPolicy"))
	s.field("Selector", selectorString(asStringMap(get(spec, "selector", "matchLabels"))))
	sections := []*Section{s}

	claims := newSection("Volume Claim Templates")
	var rows [][]string
	for _, c := range asArray(spec["volumeClaimTemplates"]) {
		claimSpec := get(c, "spec")
		rows = append(rows, []string{
			str(c, "metadata", "name"),
			str(claimSpec, "storageClassName"),
			joinList(get(claimSpec, "accessModes")),
			str(claimSpec, "resources", "requests", "storage"),
		})
	}
	claims.table([]string{"Name", "Storage Class", "Access Modes", "Storage"}, rows)
	sections = add(sections, claims)

	return append(sections, podTemplateSections(asRecord(spec["template"]))...)
}

func daemonSetView(doc record) []*Section {
	spec := asRecord(doc["spec"])
	s := newSection("DaemonSet")
	s.field("Update Strategy", str(spec, "updateStrategy", "type"))
	s.field("Max Unavailable", str(spec, "updateStrategy", "rollingUpdate", "maxUnavailable"))
	s.field("Selector", selectorString(asStringMap(get(spec, "selector", "matchLabels"))))
	return append([]*Section{s}, podTemplateSections(asRecord(spec["template"]))...)
}

func jobView(doc record) []*Section {
	spec := asRecord(doc["spec"])
	s := newSection("Job")
	jobSpecFields(s, spec)
	return append([]*Section{s}, podTemplateSections(asRecord(spec["template"]))...)
}

func cronJobView(doc record) []*Section {
	spec := asRecord(doc["spec"])
	jobSpec := asRecord(get(spec, "jobTemplate", "spec"))

	s := newSection("CronJob")
	s.field("Schedule", str(spec, "schedule"))
	s.field("Time Zone", str(spec, "timeZone"))
	s.field("Suspend", str(spec, "suspend"))
	s.field("Concurrency", str(spec, "concurrencyPolicy"))
	s.field("History Limits", historyLimits(spec))
	s.field("Starting Deadline", str(spec, "startingDeadlineSeconds"))
	sections := []*Section{s}

	template := newSection("Job Template")
	jobSpecFields(template, jobSpec)
	sections = add(sections, template)

	return append(sections, podTemplateSections(asRecord(jobSpec["template"]))...)
}

func jobSpecFields(s *Section, spec record) {
	s.field("Completions", str(spec, "completions"))
	s.field("Parallelism", str(spec, "parallelism"))
	s.field("Backoff Limit", str(spec, "backoffLimit"))
	s.field("Active Deadline", str(spec, "activeDeadlineSeconds"))
	s.field("TTL After Finished", str(spec, "ttlSecondsAfterFinished"))
}

func historyLimits(spec record) string {
	ok := str(spec, "successfulJobsHistoryLimit")
	failed := str(spec, "failedJobsHistoryLimit")
	if ok == "" && failed == "" {
		return ""
	}
	if ok == "" {
		ok = "default"
	}
	if failed == "" {
		failed = "default"
	}
	return ok + " successful / " + failed + " failed"
}

// podTemplateSections renders the containers, volumes, and scheduling settings
// shared by every pod-based workload.
func podTemplateSections(template record) []*Section {
	podSpec := asRecord(template["spec"])
	if podSpec == nil {
		return nil
	}
	var sections []*Section

	initContainers := newSection("Init Containers")
	initContainers.table(containerColumns, containerRows(podSpec["initContainers"]))
	sections = add(sections, initContainers)

	containers := newSection("Containers")
	containers.table(containerColumns, containerRows(podSpec["containers"]))
	sections = add(sections, containers)

	volumes := newSection("Volumes")
	var volumeRows [][]string
	for _, v := range asArray(podSpec["volumes"]) {
		volumeRows = append(volumeRows, []string{str(v, "name"), volumeSource(asRecord(v))})
	}
	volumes.table([]string{"Name", "Source"}, volumeRows)
	sections = add(sections, volumes)

	settings := newSection("Pod Settings")
	settings.field("Service Account", str(podSpec, "serviceAccountName"))
	settings.field("Restart Policy", str(podSpec, "restartPolicy"))
	settings.field("Priority Class", str(podSpec, "priorityClassName"))
	settings.field("Host Network", str(podSpec, "hostNetwork"))
	settings.field("Node Selector", selectorString(asStringMap(podSpec["nodeSelector"])))
	if tolerations := asArray(podSpec["tolerations"]); len(tolerations) > 0 {
		settings.field("Tolerations", asScalar(len(tolerations)))
	}
	settings.field("Image Pull Secrets", strings.Join(nameRefs(podSpec["imagePullSecrets"]), ", "))
	sections = add(sections, settings)

	return sections
}

var containerColumns = []string{"Name", "Image", "Ports", "CPU", "Memory"}

func containerRows(v any) [][]string {
	var rows [][]string
	for _, c := range asArray(v) {
		rows = append(rows, []string{
			str(c, "name"),
			str(c, "image"),
			containerPorts(get(c, "ports")),
			resourceRange(c, "cpu"),
			resourceRange(c, "memory"),
		})
	}
	return rows
}

func containerPorts(v any) string {
	var parts []string
	for _, p := range asArray(v) {
		num := str(p, "containerPort")
		name := str(p, "name")
		switch {
		case name != "" && num != "":
			parts = append(parts, name+":"+num)
		case name != "":
			parts = append(parts, name)
		case num != "":
			parts = append(parts, num)
		}
	}
	return strings.Join(parts, ", ")
}

// resourceRange renders "request → limit" for one resource name, omitting
// absent halves.
func resourceRange(container any, name string) string {
	request := str(container, "resources", "requests", name)
	limit := str(container, "resources", "limits", name)
	switch {
	case request != "" && limit != "":
		return request + " -> " + limit
	case request != "":
		return request + " (req)"
	case limit != "":
		return limit + " (limit)"
	}
	return ""
}

func volumeSource(vol record) string {
	if vol == nil {
		return ""
	}
	if name := str(vol, "configMap", "name"); name != "" {
		return "ConfigMap " + name
	}
	if name := str(vol, "secret", "secretName"); name != "" {
		return "Secret " + name
	}
	if name := str(vol, "persistentVolumeClaim", "claimName"); name != "" {
		return "PVC " + name
	}
	if asRecord(vol["emptyDir"]) != nil {
		return "EmptyDir"
	}
	if asRecord(vol["hostPath"]) != nil {
		return strings.TrimSpace("HostPath " + str(vol, "hostPath", "path"))
	}
	if asRecord(vol["projected"]) != nil {
		return "Projected"
	}
	if asRecord(vol["downwardAPI"]) != nil {
		return "Downward API"
	}
	// Unknown source type: name it by the first key that isn't `name`.
	rest := make(record, len(vol))
	for k, v := range vol {
		if k != "name" {
			rest[k] = v
		}
	}
	return firstKey(rest)
}
