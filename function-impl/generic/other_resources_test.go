// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// selects reads a selector as the API server reads a LabelSelector: matchLabels and
// matchExpressions ANDed, an empty selector selecting everything and a null one nothing.
func TestLabelSelectorMatches(t *testing.T) {
	labels := map[string]any{"app": "web", "tier": "front"}
	for name, test := range map[string]struct {
		selector any
		labels   any
		want     bool
	}{
		"empty selects everything": {selector: map[string]any{}, labels: labels, want: true},
		"null selects nothing":     {selector: nil, labels: labels, want: false},
		"matchLabels":              {selector: map[string]any{"matchLabels": map[string]any{"app": "web"}}, labels: labels, want: true},
		"matchLabels differ":       {selector: map[string]any{"matchLabels": map[string]any{"app": "db"}}, labels: labels, want: false},
		"In": {selector: map[string]any{"matchExpressions": []any{
			map[string]any{"key": "tier", "operator": "In", "values": []any{"front", "edge"}}}}, labels: labels, want: true},
		"DoesNotExist": {selector: map[string]any{"matchExpressions": []any{
			map[string]any{"key": "tier", "operator": "DoesNotExist"}}}, labels: labels, want: false},
		"both ANDed": {selector: map[string]any{"matchLabels": map[string]any{"app": "web"}, "matchExpressions": []any{
			map[string]any{"key": "tier", "operator": "NotIn", "values": []any{"front"}}}}, labels: labels, want: false},
		"no labels": {selector: map[string]any{"matchLabels": map[string]any{"app": "web"}}, labels: nil, want: false},
	} {
		got, err := LabelSelectorMatches(test.selector, test.labels)
		require.NoError(t, err, name)
		assert.Equal(t, test.want, got, name)
	}
	_, err := LabelSelectorMatches(map[string]any{"matchExpressions": []any{
		map[string]any{"key": "tier", "operator": "Sometimes"}}}, labels)
	assert.Error(t, err)
}

const coveredDeployments = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: shop
spec:
  template:
    metadata:
      labels:
        app: web
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: db
  namespace: shop
spec:
  template:
    metadata:
      labels:
        app: db
`

const selectedPolicies = `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: web-ingress
  namespace: shop
spec:
  podSelector:
    matchLabels:
      app: web
  policyTypes: [Ingress]
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: other-namespace
  namespace: billing
spec:
  podSelector: {}
  policyTypes: [Ingress]
`

func selectedOtherResources(t *testing.T) OtherResources {
	t.Helper()
	policies, err := gaby.ParseAll([]byte(selectedPolicies))
	require.NoError(t, err)
	unitID := uuid.New()
	origin := api.OtherDataOrigin{UnitID: unitID, UnitSlug: "netpols", SpaceSlug: "platform", RevisionNum: 3}
	other, err := NewOtherResources(map[api.OtherDataSource]gaby.Container{"policies": policies},
		map[api.OtherDataSource][]api.OtherDataOrigin{"policies": {origin, origin}})
	require.NoError(t, err)
	return other
}

// vet-cel reads the selected resources as other, and their origins as otherOrigins: here, a
// workload is covered when a policy in its namespace selects its pods.
func TestVetCELReadsOtherData(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(coveredDeployments))
	require.NoError(t, err)
	other := selectedOtherResources(t)
	args := stringArgsToFunctionArgs([]string{
		`other.policies.exists(p, p.metadata.namespace == r.metadata.namespace && selects(p.spec.podSelector, r.spec.template.metadata.labels))`,
	})
	_, output, err := GenericFnVetCELWithOtherData(testResourceProvider, nil, nil, docs, other, args)
	require.NoError(t, err)
	vr := output.(api.ValidationResult)
	assert.False(t, vr.Passed)
	require.Len(t, vr.Details, 1)
	assert.Contains(t, vr.Details[0], "shop/db")

	args = stringArgsToFunctionArgs([]string{`otherOrigins.policies.all(o, o.UnitSlug == 'netpols' && o.RevisionNum == 3)`})
	_, output, err = GenericFnVetCELWithOtherData(testResourceProvider, nil, nil, docs, other, args)
	require.NoError(t, err)
	assert.True(t, output.(api.ValidationResult).Passed)

	// Without OtherData, other is empty, and a rule can say what that means.
	args = stringArgsToFunctionArgs([]string{`!('policies' in other)`})
	_, output, err = GenericFnVetCEL(testResourceProvider, nil, nil, docs, args)
	require.NoError(t, err)
	assert.True(t, output.(api.ValidationResult).Passed)
}

// vet-starlark reads the same, as other and otherOrigins, with selects as a builtin.
func TestVetStarlarkReadsOtherData(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(coveredDeployments))
	require.NoError(t, err)
	program := `def validate(r):
  labels = r["spec"]["template"]["metadata"]["labels"]
  for i, p in enumerate(other["policies"]):
    if p["metadata"]["namespace"] == r["metadata"]["namespace"] and selects(p["spec"]["podSelector"], labels):
      return {"passed": True}
  return {"passed": False, "details": ["no policy of " + otherOrigins["policies"][0]["UnitSlug"] + " selects " + r["metadata"]["name"]]}
`
	_, output, err := genericFnVetStarlark(testResourceProvider, nil, nil, docs, selectedOtherResources(t),
		stringArgsToFunctionArgs([]string{program}))
	require.NoError(t, err)
	vr := output.(api.ValidationResult)
	assert.False(t, vr.Passed)
	assert.Equal(t, []string{"no policy of netpols selects db"}, vr.Details)
}

// podLabels reads the labels of a resource's pods wherever its kind keeps them, and is null for a
// resource that runs no pods; namespaceLabels gives a Namespace the name label the API server does.
func TestPodLabelsAndNamespaceLabels(t *testing.T) {
	cronJob := map[string]any{"apiVersion": "batch/v1", "kind": "CronJob", "spec": map[string]any{"jobTemplate": map[string]any{
		"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"labels": map[string]any{"app": "report"}}}}}}}
	assert.Equal(t, map[string]any{"app": "report"}, PodLabels(cronJob))
	assert.Equal(t, map[string]any{}, PodLabels(map[string]any{"apiVersion": "apps/v1", "kind": "Deployment"}))
	assert.Nil(t, PodLabels(map[string]any{"apiVersion": "v1", "kind": "Service"}))

	namespaces := []any{map[string]any{"kind": "Namespace", "metadata": map[string]any{"name": "shop", "labels": map[string]any{"team": "a"}}}}
	assert.Equal(t, map[string]any{"team": "a", "kubernetes.io/metadata.name": "shop"}, NamespaceLabels(namespaces, "shop"))
	assert.Equal(t, map[string]any{"kubernetes.io/metadata.name": "billing"}, NamespaceLabels(namespaces, "billing"))
}

const labeledNamespaces = `apiVersion: v1
kind: Namespace
metadata:
  name: shop
  labels:
    team: storefront
`

// A rule can match a policy's selectors against the workload's pods, and a namespaceSelector
// against its Namespace, without spelling out where either keeps its labels.
func TestVetCELAndStarlarkPodAndNamespaceLabels(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(coveredDeployments))
	require.NoError(t, err)
	policies, err := gaby.ParseAll([]byte(selectedPolicies))
	require.NoError(t, err)
	namespaces, err := gaby.ParseAll([]byte(labeledNamespaces))
	require.NoError(t, err)
	other, err := NewOtherResources(map[api.OtherDataSource]gaby.Container{"policies": policies, "namespaces": namespaces}, nil)
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`other.policies.exists(p, p.metadata.namespace == r.metadata.namespace && selects(p.spec.podSelector, podLabels(r))) && ` +
			`selects({'matchLabels': {'team': 'storefront', 'kubernetes.io/metadata.name': r.metadata.namespace}}, namespaceLabels(other.namespaces, r.metadata.namespace))`,
	})
	_, output, err := GenericFnVetCELWithOtherData(testResourceProvider, nil, nil, docs, other, args)
	require.NoError(t, err)
	vr := output.(api.ValidationResult)
	require.Len(t, vr.Details, 1, vr)
	assert.Contains(t, vr.Details[0], "shop/db")

	program := `def validate(r):
  if podLabels({"apiVersion": "v1", "kind": "Service"}) != None:
    return {"passed": False, "details": ["a Service runs no pods"]}
  if namespaceLabels(other["namespaces"], "shop").get("team") != "storefront":
    return {"passed": False, "details": ["shop is labeled"]}
  for p in other["policies"]:
    if p["metadata"]["namespace"] == r["metadata"]["namespace"] and selects(p["spec"]["podSelector"], podLabels(r)):
      return {"passed": True}
  return {"passed": False, "details": ["nothing selects " + r["metadata"]["name"]]}
`
	_, output, err = genericFnVetStarlark(testResourceProvider, nil, nil, docs, other, stringArgsToFunctionArgs([]string{program}))
	require.NoError(t, err)
	vr = output.(api.ValidationResult)
	assert.Equal(t, []string{"nothing selects db"}, vr.Details)
}
