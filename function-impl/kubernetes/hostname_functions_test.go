// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
)

// hostnameResources is a multi-resource Unit covering every resource type wired
// into k8skit.ResourceTypeToNeededHostnamePaths whose hostnames live in their own
// scalar (or string-array element). Traefik is intentionally absent — it packs
// multiple hostnames into one match string and is handled separately.
const hostnameResources = `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: web
  namespace: prod
spec:
  tls:
  - hosts:
    - web.example.com
    secretName: web-tls
  rules:
  - host: web.example.com
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: web-cert
  namespace: prod
spec:
  commonName: web.example.com
  dnsNames:
  - web.example.com
  - api.example.com
  - "*.example.com"
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: web-gw
  namespace: prod
spec:
  listeners:
  - name: https
    hostname: gw.example.com
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: web-route
  namespace: prod
spec:
  hostnames:
  - route.example.com
---
apiVersion: externaldns.k8s.io/v1alpha1
kind: DNSEndpoint
metadata:
  name: web-dns
  namespace: prod
spec:
  endpoints:
  - dnsName: dns.example.com
`

// invokeHostnameGetter runs a no-argument hostname getter (get-hostname,
// get-hostname-subdomain, or get-hostname-domain) and returns the value list.
func invokeHostnameGetter(t *testing.T, fn, input string) api.AttributeValueList {
	t.Helper()
	req := &api.FunctionInvocationRequest{
		ConfigData: []byte(input),
		FunctionInvocations: []api.FunctionInvocation{
			{FunctionName: fn},
		},
	}
	resp, err := testFunctionHandler.InvokeCore(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.Success, "%s should succeed; errors: %v", fn, resp.ErrorMessages)
	out, ok := resp.Outputs[api.OutputTypeAttributeValueList]
	require.True(t, ok, "expected AttributeValueList output from %s, got %v", fn, resp.Outputs)
	var list api.AttributeValueList
	require.NoError(t, json.Unmarshal(out, &list))
	return list
}

// hasHostnameValue reports whether the list contains an entry at the given
// resource name and path with the given value.
func hasHostnameValue(list api.AttributeValueList, resourceName, path, value string) bool {
	for _, v := range list {
		if string(v.ResourceName) == resourceName && string(v.Path) == path {
			if s, ok := v.Value.(string); ok && s == value {
				return true
			}
		}
	}
	return false
}

// TestHostname_GetAcrossResources verifies that get-hostname surfaces the
// hostname fields newly registered for cert-manager, Gateway API, Ingress TLS,
// and ExternalDNS — including string-array elements addressed via a `*` wildcard.
func TestHostname_GetAcrossResources(t *testing.T) {
	list := invokeHostnameGetter(t, "get-hostname", hostnameResources)

	expected := []struct{ resourceName, path, value string }{
		{"prod/web", "spec.rules.0.host", "web.example.com"},
		{"prod/web", "spec.tls.0.hosts.0", "web.example.com"},
		{"prod/web-cert", "spec.commonName", "web.example.com"},
		{"prod/web-cert", "spec.dnsNames.0", "web.example.com"},
		{"prod/web-cert", "spec.dnsNames.1", "api.example.com"},
		{"prod/web-cert", "spec.dnsNames.2", "*.example.com"},
		{"prod/web-gw", "spec.listeners.0.hostname", "gw.example.com"},
		{"prod/web-route", "spec.hostnames.0", "route.example.com"},
		{"prod/web-dns", "spec.endpoints.0.dnsName", "dns.example.com"},
	}
	for _, e := range expected {
		assert.True(t, hasHostnameValue(list, e.resourceName, e.path, e.value),
			"expected get-hostname to return %s=%s on %s", e.path, e.value, e.resourceName)
	}
}

// TestHostname_SubdomainAndDomainExtraction verifies the embedded regexp accessor
// splits each scalar hostname into subdomain and domain. Wildcard dnsNames like
// *.example.com do not match the DNS regexp and are simply omitted (no error).
func TestHostname_SubdomainAndDomainExtraction(t *testing.T) {
	subs := invokeHostnameGetter(t, "get-hostname-subdomain", hostnameResources)
	assert.True(t, hasHostnameValue(subs, "prod/web-cert", "spec.dnsNames.0#subdomain", "web"),
		"expected subdomain 'web' extracted from spec.dnsNames.0")
	assert.True(t, hasHostnameValue(subs, "prod/web-cert", "spec.dnsNames.1#subdomain", "api"),
		"expected subdomain 'api' extracted from spec.dnsNames.1")
	assert.True(t, hasHostnameValue(subs, "prod/web-gw", "spec.listeners.0.hostname#subdomain", "gw"),
		"expected subdomain 'gw' extracted from the Gateway listener hostname")
	// The wildcard dnsName must not yield a subdomain.
	for _, v := range subs {
		if string(v.ResourceName) == "prod/web-cert" && string(v.Path) == "spec.dnsNames.2#subdomain" {
			t.Errorf("wildcard dnsName *.example.com should not produce a subdomain, got %v", v.Value)
		}
	}

	domains := invokeHostnameGetter(t, "get-hostname-domain", hostnameResources)
	assert.True(t, hasHostnameValue(domains, "prod/web-cert", "spec.dnsNames.0#domain", "example.com"),
		"expected domain 'example.com' extracted from spec.dnsNames.0")
	assert.True(t, hasHostnameValue(domains, "prod/web-route", "spec.hostnames.0#domain", "example.com"),
		"expected domain 'example.com' extracted from the HTTPRoute hostname")
}

// TestHostname_SetDomain verifies set-hostname-domain rewrites only the domain
// portion of hostnames via the embedded accessor, across the new resource types.
func TestHostname_SetDomain(t *testing.T) {
	const input = `apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: web-cert
  namespace: prod
spec:
  commonName: web.example.com
  dnsNames:
  - web.example.com
  - api.example.com
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: web-gw
  namespace: prod
spec:
  listeners:
  - name: https
    hostname: gw.example.com
`
	req := &api.FunctionInvocationRequest{
		ConfigData: []byte(input),
		FunctionInvocations: []api.FunctionInvocation{
			{
				FunctionName: "set-hostname-domain",
				Arguments:    []api.FunctionArgument{{ParameterName: "domain", Value: "internal.test"}},
			},
		},
	}
	resp, err := testFunctionHandler.InvokeCore(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.Success, "set-hostname-domain should succeed; errors: %v", resp.ErrorMessages)

	out := string(resp.ConfigData)
	// Subdomains are preserved; only the domain changes.
	assert.Contains(t, out, "web.internal.test")
	assert.Contains(t, out, "api.internal.test")
	assert.Contains(t, out, "gw.internal.test")
	assert.False(t, strings.Contains(out, "example.com"),
		"no original domain should remain after set-hostname-domain; got:\n%s", out)
}
