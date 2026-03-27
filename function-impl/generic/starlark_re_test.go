// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/third_party/gaby"
)

var bt = "`" // backtick helper to keep fixtures and programs readable

var ingressRouteFixture = "apiVersion: traefik.io/v1alpha1\n" +
	"kind: IngressRoute\n" +
	"metadata:\n" +
	"  name: confighub-api-https\n" +
	"  namespace: confighub\n" +
	"spec:\n" +
	"  entryPoints:\n" +
	"  - websecure\n" +
	"  routes:\n" +
	"  - match: (Host(" + bt + "hub.prod.confighub.net" + bt + ") || Host(" + bt + "hub.confighub.com" + bt + ")) && PathRegexp(" + bt + "^/api/bridge_worker/[^/]+/(stream|action_result|me)" + bt + ")\n" +
	"    priority: 100\n" +
	"    kind: Rule\n" +
	"    services:\n" +
	"    - name: confighub-bridgeworker\n" +
	"      port: 9091\n" +
	"      scheme: h2c\n" +
	"  - match: (Host(" + bt + "hub.prod.confighub.net" + bt + ") || Host(" + bt + "hub.confighub.com" + bt + ")) && !PathPrefix(" + bt + "/internal" + bt + ")\n" +
	"    priority: 50\n" +
	"    kind: Rule\n" +
	"    services:\n" +
	"    - name: confighub-api\n" +
	"      port: 9090\n" +
	"  tls:\n" +
	"    secretName: hub-prod-tls\n"

var websiteIngressRouteFixture = "apiVersion: traefik.io/v1alpha1\n" +
	"kind: IngressRoute\n" +
	"metadata:\n" +
	"  name: website-https\n" +
	"  namespace: website\n" +
	"spec:\n" +
	"  entryPoints:\n" +
	"  - websecure\n" +
	"  routes:\n" +
	"  - match: Host(" + bt + "www.confighub.com" + bt + ") || Host(" + bt + "confighub.com" + bt + ")\n" +
	"    kind: Rule\n" +
	"    services:\n" +
	"    - name: website\n" +
	"      port: 8000\n" +
	"  tls:\n" +
	"    secretName: confighub-com-tls\n"

// hostPattern matches one or more Host(`...`) expressions separated by ||.
// Passed as a param so Starlark doesn't need to deal with regex escaping.
var hostPattern = "Host\\(" + bt + "[^" + bt + "]*" + bt + "\\)( *\\|\\| *Host\\(" + bt + "[^" + bt + "]*" + bt + "\\))*"

func TestSetStarlark_ReSubHostExpression(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(ingressRouteFixture))
	require.NoError(t, err)

	// Replace Host expressions with a single new Host using re.sub and params.
	// The regex pattern is passed as a param to avoid Starlark string escaping issues.
	program := `
for route in r["spec"]["routes"]:
    route["match"] = re.sub(params["pattern"], "Host(` + bt + `" + params["hostname"] + "` + bt + `)", route["match"])
`
	args := stringArgsToFunctionArgs([]string{
		program,
		"hostname=hub.staging.confighub.net",
		"pattern=" + hostPattern,
	})

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, nil, docs, args)
	require.NoError(t, err)

	output := newDocs.String()
	// Verify Host expressions were replaced
	assert.Contains(t, output, "Host("+bt+"hub.staging.confighub.net"+bt+")")
	assert.NotContains(t, output, "hub.prod.confighub.net")
	assert.NotContains(t, output, "hub.confighub.com")
	// Verify non-Host parts are preserved
	assert.Contains(t, output, "PathRegexp")
	assert.Contains(t, output, "!PathPrefix")
}

func TestSetStarlark_ReSubWebsiteHost(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(websiteIngressRouteFixture))
	require.NoError(t, err)

	program := `
for route in r["spec"]["routes"]:
    route["match"] = re.sub(params["pattern"], "Host(` + bt + `" + params["hostname"] + "` + bt + `)", route["match"])
`
	args := stringArgsToFunctionArgs([]string{
		program,
		"hostname=staging.confighub.com",
		"pattern=" + hostPattern,
	})

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, nil, docs, args)
	require.NoError(t, err)

	output := newDocs.String()
	assert.Contains(t, output, "Host("+bt+"staging.confighub.com"+bt+")")
	assert.NotContains(t, output, "www.confighub.com")
	assert.NotContains(t, output, "Host("+bt+"confighub.com"+bt+")")
}

func TestReSearch(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	program := `
m = re.search("nginx:(\\S+)", r["spec"]["template"]["spec"]["containers"][0]["image"])
if m:
    r["metadata"]["annotations"] = {"image-tag": m.group(1)}
`
	args := stringArgsToFunctionArgs([]string{program})

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, nil, docs, args)
	require.NoError(t, err)
	assert.Contains(t, newDocs.String(), "image-tag: \"1.21\"")
}

func TestReMatch(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	// re.match anchors at start - should match "apps/v1"
	program := `
m = re.match("(\\w+)/(\\w+)", r["apiVersion"])
if m:
    r["metadata"]["annotations"] = {"api-group": m.group(1), "api-version": m.group(2)}
`
	args := stringArgsToFunctionArgs([]string{program})

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, nil, docs, args)
	require.NoError(t, err)
	output := newDocs.String()
	assert.Contains(t, output, "api-group: apps")
	assert.Contains(t, output, "api-version: v1")
}

func TestReFindall(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(ingressRouteFixture))
	require.NoError(t, err)

	// Extract all hostnames from Host() expressions in the first route.
	// Pattern is passed as a param.
	program := `
hosts = re.findall(params["pattern"], r["spec"]["routes"][0]["match"])
r["metadata"]["annotations"] = {"hosts": ",".join(hosts)}
`
	args := stringArgsToFunctionArgs([]string{
		program,
		"pattern=Host\\(" + bt + "([^" + bt + "]*)" + bt + "\\)",
	})

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, nil, docs, args)
	require.NoError(t, err)
	output := newDocs.String()
	assert.Contains(t, output, "hosts: hub.prod.confighub.net,hub.confighub.com")
}

func TestReSubWithCount(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	// Replace only the first occurrence
	program := `
labels = r["spec"]["template"]["metadata"]["labels"]
for k in list(labels.keys()):
    labels[k] = re.sub("nginx", "myapp", labels[k], count=1)
`
	args := stringArgsToFunctionArgs([]string{program})

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, nil, docs, args)
	require.NoError(t, err)
	assert.Contains(t, newDocs.String(), "app: myapp")
}

func TestReSearchNoMatch(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	program := `
m = re.search("notfound", r["metadata"]["name"])
if m:
    r["metadata"]["annotations"] = {"found": "yes"}
`
	args := stringArgsToFunctionArgs([]string{program})

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, nil, docs, args)
	require.NoError(t, err)
	assert.NotContains(t, newDocs.String(), "found: \"yes\"")
}

func TestReMatchGroups(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	program := `
m = re.search("(\\w+):(\\S+)", r["spec"]["template"]["spec"]["containers"][0]["image"])
if m:
    g = m.groups()
    s = m.span()
    r["metadata"]["annotations"] = {
        "full-match": m.group(0),
        "image-name": g[0],
        "image-tag": g[1],
        "match-start": str(s[0]),
        "match-end": str(s[1]),
    }
`
	args := stringArgsToFunctionArgs([]string{program})

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, nil, docs, args)
	require.NoError(t, err)
	output := newDocs.String()
	assert.Contains(t, output, "full-match: nginx:1.21")
	assert.Contains(t, output, "image-name: nginx")
	assert.Contains(t, output, "image-tag: \"1.21\"")
}

func TestReFindallWithGroups(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	// findall with two groups returns list of tuples
	program := `
results = re.findall("(\\w+)=(\\w+)", "app=nginx,tier=frontend")
annotations = {}
for r_tuple in results:
    annotations[r_tuple[0]] = r_tuple[1]
r["metadata"]["annotations"] = annotations
`
	args := stringArgsToFunctionArgs([]string{program})

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, nil, docs, args)
	require.NoError(t, err)
	output := newDocs.String()
	assert.Contains(t, output, "app: nginx")
	assert.Contains(t, output, "tier: frontend")
}

func TestReInvalidPattern(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	program := `re.search("[invalid", "test")`
	args := stringArgsToFunctionArgs([]string{program})

	_, _, err = genericFnSetStarlark(testResourceProvider, nil, docs, args)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "re.search")
}
