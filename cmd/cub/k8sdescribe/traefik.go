// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8sdescribe

import (
	"sort"
	"strings"
)

// ingressRouteView renders a traefik.io IngressRoute (HTTP).
func ingressRouteView(doc record) []*Section {
	spec := asRecord(doc["spec"])

	s := newSection("Ingress Route")
	s.field("Entry Points", joinList(spec["entryPoints"]))
	s.field("TLS Secret", str(spec, "tls", "secretName"))
	s.field("Cert Resolver", str(spec, "tls", "certResolver"))

	routes := newSection("Routes")
	var rows [][]string
	for _, r := range asArray(spec["routes"]) {
		rows = append(rows, []string{
			str(r, "match"),
			serviceList(get(r, "services")),
			strings.Join(nameRefs(get(r, "middlewares")), ", "),
			str(r, "priority"),
		})
	}
	routes.table([]string{"Match", "Services", "Middlewares", "Priority"}, rows)
	if routes.Empty() {
		routes.note("No routes defined.")
	}

	return add([]*Section{s}, routes)
}

// ingressRouteTCPView renders a traefik.io IngressRouteTCP / IngressRouteUDP.
func ingressRouteTCPView(doc record) []*Section {
	spec := asRecord(doc["spec"])

	s := newSection("Ingress Route")
	s.field("Entry Points", joinList(spec["entryPoints"]))
	s.field("TLS Secret", str(spec, "tls", "secretName"))
	s.field("TLS Passthrough", str(spec, "tls", "passthrough"))

	routes := newSection("Routes")
	var rows [][]string
	for _, r := range asArray(spec["routes"]) {
		rows = append(rows, []string{str(r, "match"), serviceList(get(r, "services"))})
	}
	routes.table([]string{"Match", "Services"}, rows)
	if routes.Empty() {
		routes.note("No routes defined.")
	}

	return add([]*Section{s}, routes)
}

func serviceList(v any) string {
	var labels []string
	for _, s := range asArray(v) {
		label := str(s, "name")
		if label == "" {
			continue
		}
		if port := str(s, "port"); port != "" {
			label += ":" + port
		}
		if weight := str(s, "weight"); weight != "" {
			label += " (w=" + weight + ")"
		}
		labels = append(labels, label)
	}
	return strings.Join(labels, ", ")
}

// middlewareView renders a traefik.io Middleware. Its spec contains exactly one
// key naming the middleware type (stripPrefix, rateLimit, headers, ...) whose
// value is that type's configuration.
func middlewareView(doc record) []*Section {
	spec := asRecord(doc["spec"])
	if len(spec) == 0 {
		return []*Section{newSection("Middleware").note("No middleware configuration.")}
	}

	types := make([]string, 0, len(spec))
	for k := range spec {
		types = append(types, k)
	}
	sort.Strings(types)

	sections := make([]*Section, 0, len(types))
	for _, middlewareType := range types {
		s := newSection("Middleware: " + middlewareType)
		config := asRecord(spec[middlewareType])
		if len(config) == 0 {
			s.note("No options set.")
		} else {
			scalarsAndNested(s, config, nil)
		}
		sections = append(sections, s)
	}
	return sections
}

// traefikServiceView renders a traefik.io TraefikService: weighted round-robin
// or mirroring.
func traefikServiceView(doc record) []*Section {
	spec := asRecord(doc["spec"])

	if weighted := asRecord(spec["weighted"]); weighted != nil {
		s := newSection("Weighted Service")
		var rows [][]string
		for _, svc := range asArray(weighted["services"]) {
			name := str(svc, "name")
			if port := str(svc, "port"); port != "" {
				name += ":" + port
			}
			rows = append(rows, []string{name, str(svc, "weight")})
		}
		s.table([]string{"Service", "Weight"}, rows)
		return []*Section{s}
	}

	if mirroring := asRecord(spec["mirroring"]); mirroring != nil {
		s := newSection("Mirroring Service")
		s.field("Primary", str(mirroring, "name"))
		s.field("Port", str(mirroring, "port"))

		mirrors := newSection("Mirrors")
		var rows [][]string
		for _, m := range asArray(mirroring["mirrors"]) {
			rows = append(rows, []string{str(m, "name"), str(m, "percent")})
		}
		mirrors.table([]string{"Service", "Percent"}, rows)
		return add([]*Section{s}, mirrors)
	}

	return []*Section{newSection("Traefik Service").note("No weighted or mirroring configuration found.")}
}
