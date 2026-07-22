// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package helmutils

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
)

func testChart() *chart.Chart {
	return &chart.Chart{
		Metadata: &chart.Metadata{
			Name:       "testchart",
			APIVersion: "v2",
			Version:    "1.2.3",
			AppVersion: "0.9.0",
		},
		Templates: []*chart.File{
			{Name: "templates/deployment.yaml", Data: []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Release.Name }}-app
  namespace: {{ .Release.Namespace }}
spec:
  replicas: {{ .Values.replicas | default 1 }}
`)},
			{Name: "templates/rbac/role.yaml", Data: []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: app-role
`)},
			{Name: "templates/hook-job.yaml", Data: []byte(`apiVersion: batch/v1
kind: Job
metadata:
  name: setup-hook
  annotations:
    helm.sh/hook: pre-install
`)},
			{Name: "templates/_helpers.tpl", Data: []byte(`{{- define "noop" -}}{{- end -}}`)},
			{Name: "templates/NOTES.txt", Data: []byte(`installed!`)},
		},
		Files: []*chart.File{
			{Name: "crds/widgets.yaml", Data: []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
`)},
		},
	}
}

func testSource() *HelmSource {
	return &HelmSource{
		APIVersion: HelmSourceAPIVersion,
		Kind:       HelmSourceKind,
		Metadata:   HelmSourceMetadata{Name: "myrelease"},
		Spec: HelmSourceSpec{
			Chart:   HelmSourceChart{Ref: "testchart", Version: "1.2.3"},
			Release: HelmSourceRelease{Name: "myrelease"},
		},
	}
}

func unitBySlug(t *testing.T, result *GenerateResult, slug string) GeneratedUnit {
	t.Helper()
	for _, u := range result.Units {
		if u.Slug == slug {
			return u
		}
	}
	t.Fatalf("no unit with slug %q; have %v", slug, unitSlugs(result))
	return GeneratedUnit{}
}

func unitSlugs(result *GenerateResult) []string {
	slugs := make([]string, len(result.Units))
	for i, u := range result.Units {
		slugs[i] = u.Slug
	}
	return slugs
}

func TestGeneratePerFileUnits(t *testing.T) {
	result, err := Generate(testChart(), testSource(), "mycomponent")
	require.NoError(t, err)

	assert.Equal(t, []string{"crds-widgets", "deployment", "rbac-role"}, unitSlugs(result))

	dep := unitBySlug(t, result, "deployment")
	assert.Contains(t, dep.Content, "# Source: testchart/templates/deployment.yaml")
	assert.Contains(t, dep.Content, "name: myrelease-app")
	assert.Contains(t, dep.Content, "namespace: "+PlaceholderNamespace)
	assert.Equal(t, "testchart/templates/deployment.yaml", dep.Source)

	crd := unitBySlug(t, result, "crds-widgets")
	assert.Contains(t, crd.Content, "widgets.example.com")

	assert.Equal(t, "1.2.3", result.ResolvedVersion)
	assert.Equal(t, "0.9.0", result.AppVersion)
	assert.Equal(t, "myrelease", result.UnitLabels[HelmReleaseLabel])
	assert.Equal(t, "testchart", result.UnitLabels[HelmChartLabel])
}

func TestGenerateDropsHooksByDefault(t *testing.T) {
	result, err := Generate(testChart(), testSource(), "mycomponent")
	require.NoError(t, err)

	assert.NotContains(t, unitSlugs(result), "hook-job")
	require.Len(t, result.DroppedHooks, 1)
	assert.Contains(t, result.DroppedHooks[0], "Job setup-hook")
	assert.Contains(t, result.DroppedHooks[0], "pre-install")
}

func TestGenerateIncludeHooks(t *testing.T) {
	src := testSource()
	src.Spec.IncludeHooks = true
	result, err := Generate(testChart(), src, "mycomponent")
	require.NoError(t, err)

	assert.Contains(t, unitSlugs(result), "hook-job")
	assert.Empty(t, result.DroppedHooks)
}

func TestGenerateSkipCRDs(t *testing.T) {
	src := testSource()
	src.Spec.SkipCRDs = true
	result, err := Generate(testChart(), src, "mycomponent")
	require.NoError(t, err)

	assert.NotContains(t, unitSlugs(result), "crds-widgets")
	assert.Equal(t, []string{"testchart/crds/widgets.yaml"}, result.SkippedCRDFiles)
}

func TestGenerateUnitPrefix(t *testing.T) {
	src := testSource()
	src.Spec.UnitPrefix = "pg"
	result, err := Generate(testChart(), src, "mycomponent")
	require.NoError(t, err)

	assert.Equal(t, []string{"pg-crds-widgets", "pg-deployment", "pg-rbac-role"}, unitSlugs(result))
}

func TestGenerateValues(t *testing.T) {
	src := testSource()
	src.Spec.Values = map[string]any{"replicas": 3}
	result, err := Generate(testChart(), src, "mycomponent")
	require.NoError(t, err)

	dep := unitBySlug(t, result, "deployment")
	assert.Contains(t, dep.Content, "replicas: 3")
}

func TestGenerateCreateNamespace(t *testing.T) {
	t.Run("placeholder namespace uses component slug", func(t *testing.T) {
		src := testSource()
		src.Spec.CreateNamespace = true
		result, err := Generate(testChart(), src, "mycomponent")
		require.NoError(t, err)

		ns := unitBySlug(t, result, "mycomponent-ns")
		assert.Contains(t, ns.Content, "kind: Namespace")
		assert.Contains(t, ns.Content, "name: "+PlaceholderNamespace)
	})

	t.Run("explicit namespace uses its name", func(t *testing.T) {
		src := testSource()
		src.Spec.CreateNamespace = true
		src.Spec.Release.Namespace = "myapp"
		result, err := Generate(testChart(), src, "mycomponent")
		require.NoError(t, err)

		ns := unitBySlug(t, result, "myapp-ns")
		assert.Contains(t, ns.Content, "name: myapp")
	})

	t.Run("suppressed when chart renders its own Namespace", func(t *testing.T) {
		chrt := testChart()
		chrt.Templates = append(chrt.Templates, &chart.File{
			Name: "templates/namespace.yaml",
			Data: []byte(`apiVersion: v1
kind: Namespace
metadata:
  name: {{ .Release.Namespace }}
`)})
		src := testSource()
		src.Spec.CreateNamespace = true
		result, err := Generate(chrt, src, "mycomponent")
		require.NoError(t, err)

		assert.Contains(t, unitSlugs(result), "namespace")
		assert.NotContains(t, unitSlugs(result), "mycomponent-ns")
	})

	t.Run("not created by default", func(t *testing.T) {
		result, err := Generate(testChart(), testSource(), "mycomponent")
		require.NoError(t, err)
		assert.NotContains(t, unitSlugs(result), "mycomponent-ns")
	})
}

func TestGenerateSubchart(t *testing.T) {
	sub := &chart.Chart{
		Metadata: &chart.Metadata{Name: "postgres", APIVersion: "v2", Version: "0.1.0"},
		Templates: []*chart.File{
			{Name: "templates/statefulset.yaml", Data: []byte(`apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: pg
`)},
		},
		Files: []*chart.File{
			{Name: "crds/dbs.yaml", Data: []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: dbs.example.com
`)},
		},
	}
	chrt := testChart()
	chrt.AddDependency(sub)

	result, err := Generate(chrt, testSource(), "mycomponent")
	require.NoError(t, err)

	assert.Contains(t, unitSlugs(result), "postgres-statefulset")
	assert.Contains(t, unitSlugs(result), "postgres-crds-dbs")
}

func TestGenerateSlugCollision(t *testing.T) {
	chrt := testChart()
	// templates/rbac-role.yaml collides with templates/rbac/role.yaml.
	chrt.Templates = append(chrt.Templates, &chart.File{
		Name: "templates/rbac-role.yaml",
		Data: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
`)})
	_, err := Generate(chrt, testSource(), "mycomponent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collides")
}

func TestGenerateEmptyRenderedFileSkipped(t *testing.T) {
	chrt := testChart()
	chrt.Templates = append(chrt.Templates, &chart.File{
		Name: "templates/empty.yaml",
		Data: []byte(`{{- if .Values.enabled }}
apiVersion: v1
kind: ConfigMap
metadata:
  name: optional
{{- end }}
`)})
	result, err := Generate(chrt, testSource(), "mycomponent")
	require.NoError(t, err)
	assert.NotContains(t, unitSlugs(result), "empty")
}

func TestUnitSlugForPath(t *testing.T) {
	cases := []struct {
		path   string
		prefix string
		want   string
	}{
		{"testchart/templates/deployment.yaml", "", "deployment"},
		{"testchart/templates/rbac/role.yaml", "", "rbac-role"},
		{"testchart/crds/foo.yaml", "", "crds-foo"},
		{"testchart/charts/postgres/templates/ss.yaml", "", "postgres-ss"},
		{"testchart/charts/postgres/crds/x.yaml", "", "postgres-crds-x"},
		{"testchart/templates/deployment.yaml", "pg", "pg-deployment"},
		{"testchart/templates/My_File.YAML", "", "My_File"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, UnitSlugForPath(c.path, c.prefix), "path %s prefix %q", c.path, c.prefix)
	}
}

func TestParseHelmSourceRoundTrip(t *testing.T) {
	src := testSource()
	src.Spec.Values = map[string]any{"replicas": 2, "image": map[string]any{"tag": "v1"}}
	src.Status.ResolvedVersion = "1.2.3"

	data, err := src.Marshal()
	require.NoError(t, err)

	parsed, err := ParseHelmSource(data)
	require.NoError(t, err)
	// YAML numbers decode as float64, so compare the serialized forms.
	reData, err := parsed.Marshal()
	require.NoError(t, err)
	assert.Equal(t, string(data), string(reData))
	assert.Equal(t, "1.2.3", parsed.Status.ResolvedVersion)
}

func TestParseHelmSourceValidation(t *testing.T) {
	_, err := ParseHelmSource([]byte("apiVersion: v1\nkind: ConfigMap\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a HelmSource")

	_, err = ParseHelmSource([]byte(strings.ReplaceAll(`apiVersion: confighub.com/v1alpha1
kind: HelmSource
metadata:
  name: x
spec:
  chart:
    ref: oci://example/chart
`, "\t", "  ")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec.release.name")
}
