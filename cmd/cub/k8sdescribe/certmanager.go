// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8sdescribe

import "strings"

// certificateView renders a cert-manager.io Certificate.
func certificateView(doc record) []*Section {
	spec := asRecord(doc["spec"])
	privateKey := asRecord(spec["privateKey"])

	s := newSection("Certificate")
	s.field("Secret Name", str(spec, "secretName"))
	if issuerRef := asRecord(spec["issuerRef"]); issuerRef != nil {
		kind := str(issuerRef, "kind")
		if kind == "" {
			kind = "Issuer"
		}
		s.field("Issuer", strings.TrimSpace(kind+" "+str(issuerRef, "name")))
	}
	s.field("Common Name", str(spec, "commonName"))
	s.field("Duration", str(spec, "duration"))
	s.field("Renew Before", str(spec, "renewBefore"))
	s.field("Is CA", str(spec, "isCA"))
	s.field("Revision Limit", str(spec, "revisionHistoryLimit"))

	subjects := newSection("Subjects")
	subjects.field("DNS Names", joinList(spec["dnsNames"]))
	subjects.field("IP Addresses", joinList(spec["ipAddresses"]))
	subjects.field("URIs", joinList(spec["uris"]))

	key := newSection("Key")
	key.field("Usages", joinList(spec["usages"]))
	key.field("Algorithm", str(privateKey, "algorithm"))
	key.field("Size", str(privateKey, "size"))
	key.field("Rotation Policy", str(privateKey, "rotationPolicy"))
	key.field("Encoding", str(privateKey, "encoding"))

	sections := add([]*Section{s}, subjects)
	return add(sections, key)
}

// issuerView renders a cert-manager.io Issuer / ClusterIssuer, summarizing
// whichever backend is configured.
func issuerView(doc record) []*Section {
	spec := asRecord(doc["spec"])
	if spec == nil {
		return []*Section{newSection("Issuer").note("No issuer spec found.")}
	}

	if acme := asRecord(spec["acme"]); acme != nil {
		return acmeIssuerSections(acme)
	}

	if ca := asRecord(spec["ca"]); ca != nil {
		s := newSection("CA Issuer")
		s.field("Secret Name", str(ca, "secretName"))
		s.field("CRL Distribution", joinList(ca["crlDistributionPoints"]))
		return []*Section{s}
	}

	if asRecord(spec["selfSigned"]) != nil {
		s := newSection("Self-Signed Issuer")
		s.note("Issues self-signed certificates; no further configuration.")
		return []*Section{s}
	}

	if vault := asRecord(spec["vault"]); vault != nil {
		s := newSection("Vault Issuer")
		s.field("Server", str(vault, "server"))
		s.field("Path", str(vault, "path"))
		s.field("Namespace", str(vault, "namespace"))
		return []*Section{s}
	}

	if venafi := asRecord(spec["venafi"]); venafi != nil {
		s := newSection("Venafi Issuer")
		s.field("Zone", str(venafi, "zone"))
		return []*Section{s}
	}

	return []*Section{newSection("Issuer").note("Unrecognized issuer backend.")}
}

func acmeIssuerSections(acme record) []*Section {
	s := newSection("ACME Issuer")
	s.field("Server", str(acme, "server"))
	s.field("Email", str(acme, "email"))
	s.field("Account Secret", str(acme, "privateKeySecretRef", "name"))
	s.field("Skip TLS Verify", str(acme, "skipTLSVerify"))

	solvers := newSection("Solvers")
	var rows [][]string
	for _, sv := range asArray(acme["solvers"]) {
		var solverType, detail string
		if http01 := asRecord(get(sv, "http01")); http01 != nil {
			solverType = "HTTP-01"
			if ingress := asRecord(http01["ingress"]); ingress != nil {
				class := str(ingress, "ingressClassName")
				if class == "" {
					class = str(ingress, "class")
				}
				if class == "" {
					class = "default"
				}
				detail = "ingress class " + class
			} else {
				detail = firstKey(http01)
			}
		} else if dns01 := asRecord(get(sv, "dns01")); dns01 != nil {
			solverType = "DNS-01"
			detail = firstKey(dns01)
		}
		rows = append(rows, []string{solverType, detail, joinList(get(sv, "selector", "dnsZones"))})
	}
	solvers.table([]string{"Type", "Detail", "Selector"}, rows)

	return add([]*Section{s}, solvers)
}
