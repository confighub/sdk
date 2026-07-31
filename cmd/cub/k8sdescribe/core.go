// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8sdescribe

import (
	"encoding/base64"
	"fmt"
	"strings"
)

func namespaceView(doc record) []*Section {
	s := newSection("Namespace")
	s.field("Finalizers", joinList(get(doc, "spec", "finalizers")))
	if s.Empty() {
		s.note("Namespaces carry no spec beyond their metadata.")
	}
	return []*Section{s}
}

func serviceView(doc record) []*Section {
	spec := asRecord(doc["spec"])

	s := newSection("Service")
	svcType := str(spec, "type")
	if svcType == "" {
		svcType = "ClusterIP"
	}
	s.field("Type", svcType)
	s.field("Cluster IP", str(spec, "clusterIP"))
	s.field("External Name", str(spec, "externalName"))
	s.field("Load Balancer IP", str(spec, "loadBalancerIP"))
	s.field("Session Affinity", str(spec, "sessionAffinity"))
	s.field("Selector", selectorString(asStringMap(spec["selector"])))

	ports := newSection("Ports")
	var rows [][]string
	for _, p := range asArray(spec["ports"]) {
		protocol := str(p, "protocol")
		if protocol == "" {
			protocol = "TCP"
		}
		rows = append(rows, []string{
			str(p, "name"),
			str(p, "port"),
			str(p, "targetPort"),
			protocol,
			str(p, "nodePort"),
		})
	}
	ports.table([]string{"Name", "Port", "Target Port", "Protocol", "Node Port"}, rows)

	return add([]*Section{s}, ports)
}

func configMapView(doc record) []*Section {
	var sections []*Section

	s := newSection("ConfigMap")
	s.field("Immutable", asScalar(doc["immutable"]))
	sections = add(sections, s)

	data := asStringMap(doc["data"])
	binary := asStringMap(doc["binaryData"])
	entries := newSection("Data")
	for _, k := range sortedKeys(data) {
		// One-line entries read better as rows; file-shaped ones need a block.
		if strings.Contains(data[k], "\n") {
			entries.block(k, data[k])
		} else {
			entries.field(k, data[k])
		}
	}
	if len(binary) > 0 {
		entries.field("Binary Keys", strings.Join(sortedKeys(binary), ", "))
	}
	if entries.Empty() {
		entries.note("No data entries.")
	}
	return add(sections, entries)
}

// secretView reports the shape of a Secret — type and per-key sizes — the way
// `kubectl describe secret` does. The values are present in the Unit's data,
// but printing them belongs to an explicit request for the raw configuration
// (`cub k8s get ... --show data`), not to a summary.
func secretView(doc record) []*Section {
	data := asStringMap(doc["data"])
	stringData := asStringMap(doc["stringData"])

	s := newSection("Secret")
	secretType := asScalar(doc["type"])
	if secretType == "" {
		secretType = "Opaque"
	}
	s.field("Type", secretType)
	s.field("Immutable", asScalar(doc["immutable"]))

	keys := newSection("Keys")
	var rows [][]string
	for _, k := range sortedKeys(data) {
		rows = append(rows, []string{k, fmt.Sprintf("%d bytes", decodedLen(data[k]))})
	}
	for _, k := range sortedKeys(stringData) {
		rows = append(rows, []string{k, fmt.Sprintf("%d bytes (stringData)", len(stringData[k]))})
	}
	keys.table([]string{"Key", "Size"}, rows)
	if keys.Empty() {
		keys.note("No keys.")
	}

	return add([]*Section{s}, keys)
}

// decodedLen is the size of a base64 Secret value, falling back to the raw
// length when the value isn't valid base64 (e.g. a ConfigHub placeholder).
func decodedLen(value string) int {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return len(value)
	}
	return len(decoded)
}

func serviceAccountView(doc record) []*Section {
	s := newSection("Service Account")
	s.field("Automount Token", asScalar(doc["automountServiceAccountToken"]))
	s.field("Image Pull Secrets", strings.Join(nameRefs(doc["imagePullSecrets"]), ", "))
	s.field("Secrets", strings.Join(nameRefs(doc["secrets"]), ", "))
	if s.Empty() {
		s.note("This ServiceAccount has no settings beyond its metadata.")
	}
	return []*Section{s}
}

func persistentVolumeClaimView(doc record) []*Section {
	spec := asRecord(doc["spec"])
	s := newSection("Persistent Volume Claim")
	s.field("Storage Class", str(spec, "storageClassName"))
	s.field("Access Modes", joinList(spec["accessModes"]))
	s.field("Requested Storage", str(spec, "resources", "requests", "storage"))
	s.field("Volume Mode", str(spec, "volumeMode"))
	s.field("Volume Name", str(spec, "volumeName"))
	return []*Section{s}
}

func horizontalPodAutoscalerView(doc record) []*Section {
	spec := asRecord(doc["spec"])

	s := newSection("Autoscaler")
	s.field("Target", strings.TrimSpace(str(spec, "scaleTargetRef", "kind")+" "+str(spec, "scaleTargetRef", "name")))
	s.field("Min Replicas", str(spec, "minReplicas"))
	s.field("Max Replicas", str(spec, "maxReplicas"))

	metrics := newSection("Metrics")
	var rows [][]string
	for _, m := range asArray(spec["metrics"]) {
		metricType := str(m, "type")
		// The detail object's key is the lower-camel form of the type
		// (resource/pods/object/external/containerResource).
		detail := get(m, lowerFirst(metricType))
		name := str(detail, "name")
		if name == "" {
			name = str(detail, "metric", "name")
		}
		var target string
		switch {
		case str(detail, "target", "averageUtilization") != "":
			target = str(detail, "target", "averageUtilization") + "%"
		case str(detail, "target", "averageValue") != "":
			target = str(detail, "target", "averageValue")
		default:
			target = str(detail, "target", "value")
		}
		rows = append(rows, []string{metricType, name, target})
	}
	metrics.table([]string{"Type", "Metric", "Target"}, rows)

	return add([]*Section{s}, metrics)
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
