// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8sdescribe

import "strings"

// externalSecretView renders an external-secrets.io ExternalSecret.
func externalSecretView(doc record) []*Section {
	spec := asRecord(doc["spec"])
	target := asRecord(spec["target"])

	s := newSection("External Secret")
	if storeRef := asRecord(spec["secretStoreRef"]); storeRef != nil {
		kind := str(storeRef, "kind")
		if kind == "" {
			kind = "SecretStore"
		}
		s.field("Secret Store", strings.TrimSpace(kind+" "+str(storeRef, "name")))
	}
	s.field("Refresh Interval", str(spec, "refreshInterval"))
	s.field("Target Secret", str(target, "name"))
	s.field("Creation Policy", str(target, "creationPolicy"))
	s.field("Deletion Policy", str(target, "deletionPolicy"))
	s.field("Template Type", str(target, "template", "type"))

	data := newSection("Data Mappings")
	var dataRows [][]string
	for _, d := range asArray(spec["data"]) {
		remote := get(d, "remoteRef")
		dataRows = append(dataRows, []string{
			str(d, "secretKey"),
			str(remote, "key"),
			str(remote, "property"),
			str(remote, "version"),
		})
	}
	data.table([]string{"Secret Key", "Remote Key", "Property", "Version"}, dataRows)

	dataFrom := newSection("Data From")
	var fromRows [][]string
	for _, d := range asArray(spec["dataFrom"]) {
		if extract := asRecord(get(d, "extract")); extract != nil {
			fromRows = append(fromRows, []string{"extract", str(extract, "key")})
			continue
		}
		if find := asRecord(get(d, "find")); find != nil {
			detail := str(find, "name", "regexp")
			if detail == "" {
				detail = selectorString(asStringMap(find["tags"]))
			}
			fromRows = append(fromRows, []string{"find", detail})
		}
	}
	dataFrom.table([]string{"Source", "Key / Pattern"}, fromRows)

	sections := add([]*Section{s}, data)
	return add(sections, dataFrom)
}

// secretStoreView renders an external-secrets.io SecretStore / ClusterSecretStore.
// Exactly one key is set under spec.provider, naming the backend.
func secretStoreView(doc record) []*Section {
	provider := asRecord(get(doc, "spec", "provider"))
	if provider == nil {
		return []*Section{newSection("Secret Store").note("No provider configured.")}
	}
	providerName := firstKey(provider)
	config := asRecord(provider[providerName])

	// Salient fields per provider; anything unlisted stays in the raw YAML.
	s := newSection("Secret Store")
	s.field("Provider", providerName)
	s.field("Region", str(config, "region"))
	s.field("Service", str(config, "service"))
	s.field("Server", str(config, "server"))
	s.field("URL", str(config, "url"))
	s.field("Path", str(config, "path"))
	s.field("Project ID", str(config, "projectID"))
	s.field("Vault URL", str(config, "vaultUrl"))
	s.field("Tenant ID", str(config, "tenantId"))
	s.field("Auth Type", str(config, "authType"))
	s.field("Refresh Interval", str(doc, "spec", "refreshInterval"))
	return []*Section{s}
}
