// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8sdescribe

import "strings"

func ingressView(doc record) []*Section {
	spec := asRecord(doc["spec"])

	s := newSection("Ingress")
	s.field("Ingress Class", str(spec, "ingressClassName"))
	s.field("Default Backend", backendLabel(get(spec, "defaultBackend")))
	sections := []*Section{s}

	// Flatten host x path into one row per path, so the table reads like the
	// routing decisions the controller will actually make.
	var pathRows [][]string
	for _, r := range asArray(spec["rules"]) {
		host := str(r, "host")
		if host == "" {
			host = "*"
		}
		for _, p := range asArray(get(r, "http", "paths")) {
			path := str(p, "path")
			if path == "" {
				path = "/"
			}
			pathRows = append(pathRows, []string{host, path, str(p, "pathType"), backendLabel(get(p, "backend"))})
		}
	}
	routing := newSection("Routing")
	routing.table([]string{"Host", "Path", "Path Type", "Backend"}, pathRows)
	if routing.Empty() {
		routing.note("No HTTP rules defined.")
	}
	sections = add(sections, routing)

	tls := newSection("TLS")
	var tlsRows [][]string
	for _, t := range asArray(spec["tls"]) {
		tlsRows = append(tlsRows, []string{joinList(get(t, "hosts")), str(t, "secretName")})
	}
	tls.table([]string{"Hosts", "Secret"}, tlsRows)

	return add(sections, tls)
}

// backendLabel renders an IngressBackend as "service-name:port".
func backendLabel(backend any) string {
	service := get(backend, "service")
	if asRecord(service) == nil {
		return ""
	}
	name := str(service, "name")
	port := str(service, "port", "number")
	if port == "" {
		port = str(service, "port", "name")
	}
	if port != "" {
		return name + ":" + port
	}
	return name
}

func networkPolicyView(doc record) []*Section {
	spec := asRecord(doc["spec"])

	s := newSection("Network Policy")
	podSelector := selectorString(asStringMap(get(spec, "podSelector", "matchLabels")))
	if podSelector == "" {
		podSelector = "all pods in namespace"
	}
	s.field("Pod Selector", podSelector)
	s.field("Policy Types", joinList(spec["policyTypes"]))
	sections := []*Section{s}

	ingress := newSection("Ingress Rules")
	var ingressRows [][]string
	for _, r := range asArray(spec["ingress"]) {
		ingressRows = append(ingressRows, []string{peerSummary(get(r, "from")), portSummary(get(r, "ports"))})
	}
	ingress.table([]string{"From", "Ports"}, ingressRows)
	sections = add(sections, ingress)

	egress := newSection("Egress Rules")
	var egressRows [][]string
	for _, r := range asArray(spec["egress"]) {
		egressRows = append(egressRows, []string{peerSummary(get(r, "to")), portSummary(get(r, "ports"))})
	}
	egress.table([]string{"To", "Ports"}, egressRows)

	return add(sections, egress)
}

func peerSummary(v any) string {
	peers := asArray(v)
	if len(peers) == 0 {
		return "any"
	}
	summaries := make([]string, 0, len(peers))
	for _, p := range peers {
		if cidr := str(p, "ipBlock", "cidr"); cidr != "" {
			summaries = append(summaries, "cidr "+cidr)
			continue
		}
		var parts []string
		if ns := asStringMap(get(p, "namespaceSelector", "matchLabels")); ns != nil {
			parts = append(parts, "ns("+mapSummary(ns)+")")
		}
		if pod := asStringMap(get(p, "podSelector", "matchLabels")); pod != nil {
			parts = append(parts, "pods("+mapSummary(pod)+")")
		}
		if len(parts) == 0 {
			summaries = append(summaries, "any")
		} else {
			summaries = append(summaries, strings.Join(parts, " "))
		}
	}
	return strings.Join(summaries, "; ")
}

func mapSummary(m map[string]string) string {
	if len(m) == 0 {
		return "all"
	}
	return selectorString(m)
}

func portSummary(v any) string {
	ports := asArray(v)
	if len(ports) == 0 {
		return "all"
	}
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		protocol := str(p, "protocol")
		if protocol == "" {
			protocol = "TCP"
		}
		port := protocol + "/" + str(p, "port")
		if end := str(p, "endPort"); end != "" {
			port += "-" + end
		}
		parts = append(parts, port)
	}
	return strings.Join(parts, ", ")
}
