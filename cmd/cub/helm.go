// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/helmutils"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/engine"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/strvals"
)

// helmCmd is the top-level command group for Helm-related operations.
var helmCmd = &cobra.Command{
	Use:               "helm",
	Short:             "Helm commands",
	Long:              getCommandHelp("Interact with Helm charts from the ConfigHub CLI.", ""),
	PersistentPreRunE: spacePreRunE, // Re-use the space selection mechanism used elsewhere
}

// Helm label constants
const (
	HelmChartLabel   = "HelmChart"
	HelmReleaseLabel = "HelmRelease"
)

func init() {
	addSpaceFlags(helmCmd)
	rootCmd.AddCommand(helmCmd) // helmCmd here refers to the package-level variable
}

// splitHelmResources separates rendered Helm resources into CRDs and regular resources
func splitHelmResources(renderedResources map[string]string, chartName string) (*k8skit.SplitResourcesResult, error) {
	return k8skit.SplitResources(renderedResources, chartName)
}

// loadHelmValues loads and merges Helm values from files and --set flags.
// The function processes values in the correct order:
// 1. Values from files are loaded in order (later files override earlier ones)
// 2. Values from --set flags override file values
// This ensures proper precedence: defaults < file1 < file2 < ... < --set flags
func loadHelmValues(valuesFiles []string, setValues []string) (map[string]interface{}, error) {
	mergedValues := map[string]interface{}{}

	// Process values files in order
	// Later files override earlier ones
	for _, filePath := range valuesFiles {
		fileValues := map[string]interface{}{}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("cannot read values file %s: %w", filePath, err)
		}
		if err := yaml.Unmarshal(data, &fileValues); err != nil {
			return nil, fmt.Errorf("cannot parse values file %s: %w", filePath, err)
		}
		// Merge with proper precedence: new values override old ones
		mergedValues = chartutil.CoalesceTables(fileValues, mergedValues)
	}

	// Process --set flags (these have highest precedence)
	for _, val := range setValues {
		if err := strvals.ParseInto(val, mergedValues); err != nil {
			return nil, fmt.Errorf("failed to parse --set value %q: %w", val, err)
		}
	}

	return mergedValues, nil
}

// helmRenderInput holds the inputs for rendering a Helm chart.
type helmRenderInput struct {
	releaseName    string
	chartName      string
	valuesFiles    []string
	set            []string
	version        string
	repo           string
	namespace      string
	usePlaceholder bool
	skipCRDs       bool
	isUpgrade      bool
}

// helmRenderResult holds the output of rendering a Helm chart.
type helmRenderResult struct {
	CRDs       string
	Resources  string
	UnitLabels map[string]string
}

// renderHelmChart performs the full Helm rendering pipeline: locates and loads the chart,
// merges values, renders templates, extracts CRDs, and splits resources.
func renderHelmChart(args helmRenderInput) (*helmRenderResult, error) {
	// Determine the namespace used during rendering
	renderNamespace := args.namespace
	if args.usePlaceholder {
		renderNamespace = "confighubplaceholder"
	}

	// Initialize Helm SDK
	settings := cli.New()
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(nil, renderNamespace, os.Getenv("HELM_DRIVER"), func(format string, v ...interface{}) {
		fmt.Printf(format+"\n", v...)
	}); err != nil {
		return nil, fmt.Errorf("failed to initialize Helm action configuration: %w", err)
	}

	// Initialize OCI registry client for oci:// chart support
	registryClient, err := registry.NewClient()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize OCI registry client: %w", err)
	}
	actionConfig.RegistryClient = registryClient

	// Use action.NewInstall to get a properly wired ChartPathOptions
	// (it copies the registry client from actionConfig into the unexported field)
	installAction := action.NewInstall(actionConfig)
	installAction.ChartPathOptions.Version = args.version
	installAction.ChartPathOptions.RepoURL = args.repo

	// Locate the chart
	cp, err := installAction.ChartPathOptions.LocateChart(args.chartName, settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chart %s (version: %s, repo: %s): %w", args.chartName, args.version, args.repo, err)
	}

	// Load the chart
	chrt, err := loader.Load(cp)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart from %s: %w", cp, err)
	}

	// Collect and merge values
	userSuppliedValues, err := loadHelmValues(args.valuesFiles, args.set)
	if err != nil {
		return nil, err
	}

	// Process dependencies (filters out disabled subcharts)
	if err := chartutil.ProcessDependencies(chrt, userSuppliedValues); err != nil {
		return nil, fmt.Errorf("failed to process chart dependencies: %w", err)
	}

	// Build unit labels from chart metadata
	unitLabels := map[string]string{
		helmutils.HelmReleaseLabel: args.releaseName,
	}
	if chrt.Metadata != nil {
		if chrt.Metadata.Name != "" {
			unitLabels[helmutils.HelmChartLabel] = chrt.Metadata.Name
		}
		if chrt.Metadata.APIVersion != "" {
			unitLabels[helmutils.HelmChartAPIVersionLabel] = chrt.Metadata.APIVersion
		}
		if chrt.Metadata.Version != "" {
			unitLabels[helmutils.HelmChartVersionLabel] = chrt.Metadata.Version
		}
		if chrt.Metadata.AppVersion != "" {
			unitLabels[helmutils.HelmAppVersionLabel] = chrt.Metadata.AppVersion
		}
	}

	// Render templates
	releaseOptions := chartutil.ReleaseOptions{
		Name:      args.releaseName,
		Namespace: renderNamespace,
		Revision:  1,
		IsInstall: !args.isUpgrade,
		IsUpgrade: args.isUpgrade,
	}
	valuesToRender, err := chartutil.ToRenderValues(chrt, userSuppliedValues, releaseOptions, chartutil.DefaultCapabilities)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare render values: %w", err)
	}

	renderingEngine := engine.Engine{}
	renderedResources, err := renderingEngine.Render(chrt, valuesToRender)
	if err != nil {
		return nil, fmt.Errorf("template render failed: %w", err)
	}

	// Extract CRDs from the chart's crds/ directory
	var crdContent strings.Builder
	if !args.skipCRDs {
		crdFiles := chrt.CRDObjects()
		for _, crdFile := range crdFiles {
			if crdContent.Len() > 0 {
				crdContent.WriteString("---\n")
			}
			crdContent.WriteString(fmt.Sprintf("# Source: %s/crds/%s\n", chrt.Name(), crdFile.Name))
			crdContent.WriteString(string(crdFile.File.Data))
			crdContent.WriteString("\n")
		}
	} else if len(chrt.CRDObjects()) > 0 {
		tprint("Skipping %d CRDs from %s/crds/ directory due to --skip-crds flag", len(chrt.CRDObjects()), chrt.Name())
	}

	// Split resources into CRDs and regular resources
	splitResult, err := splitHelmResources(renderedResources, chrt.Name())
	if err != nil {
		return nil, err
	}

	// Combine CRDs from crds/ directory with any CRDs from templates
	if crdContent.Len() > 0 {
		if splitResult.CRDs != "" {
			splitResult.CRDs = crdContent.String() + "---\n" + splitResult.CRDs
		} else {
			splitResult.CRDs = crdContent.String()
		}
	}

	return &helmRenderResult{
		CRDs:       splitResult.CRDs,
		Resources:  splitResult.Resources,
		UnitLabels: unitLabels,
	}, nil
}

// prependNamespaceResource prepends a Kubernetes Namespace resource to YAML content
// if the namespace is specified and not "default".
func prependNamespaceResource(yamlContent string, namespace string) string {
	if namespace != "" && namespace != "default" {
		namespaceResource := fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
---
`, namespace)
		return namespaceResource + yamlContent
	}
	return yamlContent
}
