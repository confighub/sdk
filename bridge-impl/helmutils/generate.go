// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package helmutils

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/third_party/gaby"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/engine"
	"helm.sh/helm/v3/pkg/registry"
)

const helmHookAnnotation = "helm.sh/hook"

// GeneratedUnit is one unit of rendered configuration destined for the base
// variant space.
type GeneratedUnit struct {
	// Slug is the unit slug, derived from the chart-relative source path and
	// the HelmSource's unit prefix.
	Slug string
	// Content is the unit's YAML data.
	Content string
	// Source is the chart-relative path the unit was generated from, for
	// reporting. Empty for the synthesized Namespace unit.
	Source string
}

// GenerateResult is the rendered output of one HelmSource.
type GenerateResult struct {
	Units []GeneratedUnit
	// UnitLabels are the Helm metadata labels to stamp on every generated unit.
	UnitLabels map[string]string
	// ResolvedVersion is the concrete chart version that was rendered.
	ResolvedVersion string
	AppVersion      string
	// DroppedHooks describes hook manifests omitted from the output, for
	// reporting to the user.
	DroppedHooks []string
	// SkippedCRDFiles lists crds/ files omitted due to spec.skipCRDs, for
	// reporting to the user.
	SkippedCRDFiles []string
}

// LoadChart locates and loads the chart a HelmSource refers to: an oci://
// reference, a local path, or a chart name resolved against spec.chart.repo.
// The version constraint from spec.chart.version is applied during resolution.
func LoadChart(src *HelmSource) (*chart.Chart, error) {
	settings := cli.New()
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(nil, "", os.Getenv("HELM_DRIVER"), func(format string, v ...any) {}); err != nil {
		return nil, fmt.Errorf("failed to initialize Helm action configuration: %w", err)
	}

	registryClient, err := registry.NewClient()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize OCI registry client: %w", err)
	}
	actionConfig.RegistryClient = registryClient

	// action.NewInstall wires the registry client into ChartPathOptions.
	installAction := action.NewInstall(actionConfig)
	installAction.ChartPathOptions.Version = src.Spec.Chart.Version
	installAction.ChartPathOptions.RepoURL = src.Spec.Chart.Repo

	cp, err := installAction.ChartPathOptions.LocateChart(src.Spec.Chart.Ref, settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chart %s (version: %s, repo: %s): %w",
			src.Spec.Chart.Ref, src.Spec.Chart.Version, src.Spec.Chart.Repo, err)
	}

	chrt, err := loader.Load(cp)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart from %s: %w", cp, err)
	}
	return chrt, nil
}

// Generate renders the chart per the HelmSource and groups the output into
// units by source file. Rendering is entirely client-side: hooks are dropped
// unless spec.includeHooks is set, lookup returns nothing, and capabilities
// are Helm's defaults, not a cluster's. componentSlug names the synthesized
// Namespace unit when the release namespace is the placeholder.
func Generate(chrt *chart.Chart, src *HelmSource, componentSlug string) (*GenerateResult, error) {
	renderNamespace := src.RenderNamespace()

	values := src.Spec.Values
	if values == nil {
		values = map[string]any{}
	}
	if err := chartutil.ProcessDependencies(chrt, values); err != nil {
		return nil, fmt.Errorf("failed to process chart dependencies: %w", err)
	}

	releaseOptions := chartutil.ReleaseOptions{
		Name:      src.Spec.Release.Name,
		Namespace: renderNamespace,
		Revision:  1,
		IsInstall: true,
	}
	valuesToRender, err := chartutil.ToRenderValues(chrt, values, releaseOptions, chartutil.DefaultCapabilities)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare render values: %w", err)
	}

	renderedFiles, err := engine.Engine{}.Render(chrt, valuesToRender)
	if err != nil {
		return nil, fmt.Errorf("template render failed: %w", err)
	}

	result := &GenerateResult{
		UnitLabels:      buildUnitLabels(chrt, src.Spec.Release.Name),
		ResolvedVersion: chrt.Metadata.Version,
		AppVersion:      chrt.Metadata.AppVersion,
	}

	// slugSources tracks which source path claimed each slug, to diagnose collisions.
	slugSources := map[string]string{}
	addUnit := func(slug, content, source string) error {
		if prev, ok := slugSources[slug]; ok {
			return fmt.Errorf("unit slug %q derived from %s collides with %s; rename one of the chart files", slug, source, prev)
		}
		slugSources[slug] = source
		result.Units = append(result.Units, GeneratedUnit{Slug: slug, Content: content, Source: source})
		return nil
	}

	// namespaceRendered records whether the chart itself emits a Namespace
	// resource named renderNamespace, which suppresses synthesis.
	namespaceRendered := false

	// Group rendered template output into one unit per source file. Iterate in
	// sorted order so generation is deterministic.
	paths := make([]string, 0, len(renderedFiles))
	for p := range renderedFiles {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, path := range paths {
		content := renderedFiles[path]
		base := path[strings.LastIndex(path, "/")+1:]
		if strings.TrimSpace(content) == "" || strings.HasPrefix(base, "_") || base == "NOTES.txt" {
			continue
		}

		docs, err := gaby.ParseAll([]byte(content))
		if err != nil {
			return nil, fmt.Errorf("failed to parse rendered YAML from %s: %w\n%s", path, err, content)
		}

		var unitContent strings.Builder
		for _, doc := range docs {
			if doc.IsEmptyDoc() {
				continue
			}
			if hook := docHookAnnotation(doc); hook != "" && !src.Spec.IncludeHooks {
				result.DroppedHooks = append(result.DroppedHooks,
					fmt.Sprintf("%s (%s: %s) from %s", docDisplayName(doc), helmHookAnnotation, hook, path))
				continue
			}
			if isNamespaceResource(doc, renderNamespace) {
				namespaceRendered = true
			}
			if unitContent.Len() > 0 {
				unitContent.WriteString("---\n")
			}
			fmt.Fprintf(&unitContent, "# Source: %s\n", path)
			unitContent.WriteString(strings.TrimSpace(doc.String()) + "\n")
		}
		if unitContent.Len() == 0 {
			continue
		}
		if err := addUnit(UnitSlugForPath(path, src.Spec.UnitPrefix), unitContent.String(), path); err != nil {
			return nil, err
		}
	}

	// CRDs from crds/ directories (including dependency charts). These are not
	// templated; the file content is used as-is.
	for _, crdFile := range chrt.CRDObjects() {
		path := crdFile.Filename
		if src.Spec.SkipCRDs {
			result.SkippedCRDFiles = append(result.SkippedCRDFiles, path)
			continue
		}
		content := fmt.Sprintf("# Source: %s\n%s", path, strings.TrimSpace(string(crdFile.File.Data))+"\n")
		if err := addUnit(UnitSlugForPath(path, src.Spec.UnitPrefix), content, path); err != nil {
			return nil, err
		}
	}

	if src.Spec.CreateNamespace && !namespaceRendered {
		slug := NamespaceUnitSlug(renderNamespace, componentSlug)
		content := fmt.Sprintf("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n", renderNamespace)
		if err := addUnit(slug, content, ""); err != nil {
			return nil, err
		}
	}

	sort.Slice(result.Units, func(i, j int) bool { return result.Units[i].Slug < result.Units[j].Slug })
	return result, nil
}

// UnitSlugForPath derives a unit slug from a chart-relative file path: the
// chart-name segment and any "templates" or "charts" segments are dropped, the
// remaining segments are joined with "-", and the file extension is stripped.
// For example templates/rbac/role.yaml -> rbac-role, crds/foo.yaml -> crds-foo,
// charts/postgres/templates/ss.yaml -> postgres-ss. The prefix, when non-empty,
// is prepended with a "-".
func UnitSlugForPath(path, prefix string) string {
	segments := strings.Split(path, "/")
	if len(segments) > 1 {
		segments = segments[1:] // drop the chart-name segment
	}
	kept := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg == "templates" || seg == "charts" || seg == "" {
			continue
		}
		kept = append(kept, seg)
	}
	if len(kept) > 0 {
		last := kept[len(kept)-1]
		if i := strings.LastIndex(last, "."); i > 0 {
			kept[len(kept)-1] = last[:i]
		}
	}
	name := strings.Join(kept, "-")
	if prefix != "" {
		name = prefix + "-" + name
	}
	return cubkit.CubNormalizeName(name)
}

// NamespaceUnitSlug names the synthesized Namespace unit: <namespace>-ns, or
// <component>-ns when the namespace is the placeholder.
func NamespaceUnitSlug(namespace, componentSlug string) string {
	if namespace == PlaceholderNamespace {
		return cubkit.CubNormalizeName(componentSlug + "-ns")
	}
	return cubkit.CubNormalizeName(namespace + "-ns")
}

// buildUnitLabels builds the Helm metadata labels stamped on generated units.
func buildUnitLabels(chrt *chart.Chart, releaseName string) map[string]string {
	labels := map[string]string{
		HelmReleaseLabel: releaseName,
	}
	if chrt.Metadata != nil {
		if chrt.Metadata.Name != "" {
			labels[HelmChartLabel] = chrt.Metadata.Name
		}
		if chrt.Metadata.APIVersion != "" {
			labels[HelmChartAPIVersionLabel] = chrt.Metadata.APIVersion
		}
		if chrt.Metadata.Version != "" {
			labels[HelmChartVersionLabel] = chrt.Metadata.Version
		}
		if chrt.Metadata.AppVersion != "" {
			labels[HelmAppVersionLabel] = chrt.Metadata.AppVersion
		}
	}
	return labels
}

// docHookAnnotation returns the helm.sh/hook annotation value, or "" when the
// document is not a hook manifest.
func docHookAnnotation(doc *gaby.YamlDoc) string {
	hook := doc.Search("metadata", "annotations", helmHookAnnotation)
	if hook == nil {
		return ""
	}
	if s, ok := hook.Data().(string); ok {
		return s
	}
	return ""
}

// isNamespaceResource reports whether the document is a v1 Namespace named name.
func isNamespaceResource(doc *gaby.YamlDoc, name string) bool {
	apiVersion, _ := doc.Search("apiVersion").Data().(string)
	kind, _ := doc.Search("kind").Data().(string)
	resName, _ := doc.Search("metadata", "name").Data().(string)
	return apiVersion == "v1" && kind == "Namespace" && resName == name
}

// docDisplayName describes a document as "<kind> <name>" for messages.
func docDisplayName(doc *gaby.YamlDoc) string {
	kind, _ := doc.Search("kind").Data().(string)
	name, _ := doc.Search("metadata", "name").Data().(string)
	if kind == "" {
		kind = "resource"
	}
	return strings.TrimSpace(kind + " " + name)
}
