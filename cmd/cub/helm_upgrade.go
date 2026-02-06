// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/confighub/sdk/helmutils"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/engine"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

// helmUpgradeCmd upgrades a Helm chart (a convenience wrapper around `helm upgrade`).
var helmUpgradeCmd = &cobra.Command{
	Use:   "upgrade <release-name> <repo>/<chartname>",
	Short: "Render a Helm chart's templates and update ConfigHub units",
	Long: getCommandHelp(`Render a Helm chart's templates and update existing ConfigHub units.
This command loads a chart (e.g., <repo>/<chartname>) from configured Helm repositories.
It processes values from files and --set flags.

The upgrade process:

1. Renders the new chart version
2. Checks if <release-name> unit exists
3. If it exists, updates the unit with the new resources
4. Optionally updates CRDs unit if --update-crds flag is set

Examples:
`+"```"+`
  # Upgrade nginx chart
  cub helm upgrade --namespace nginx my-nginx bitnami/nginx --version 15.6.0 --set image.tag=latest

  # Upgrade cert-manager chart with CRDs update
  cub helm upgrade --namespace cert-manager \
      --update-crds \
      cert-manager \
      jetstack/cert-manager \
      --version v1.17.2
`+"```"+`
`, ""),
	Args:          cobra.MinimumNArgs(2),
	RunE:          helmUpgradeCmdRun,
	SilenceUsage:  true,
	SilenceErrors: true,
}

var helmUpgradeArgs struct {
	valuesFiles    []string
	set            []string
	version        string
	repo           string
	namespace      string
	chartName      string
	releaseName    string
	updateCRDs     bool
	usePlaceholder bool // Use confighubplaceholder placeholder for rendering
	skipCRDs       bool // Skip CRDs from crds/ directory only (mirrors helm upgrade --skip-crds)
}

func init() {
	// Add flags to the upgrade command
	helmUpgradeCmd.Flags().StringArrayVarP(&helmUpgradeArgs.valuesFiles, "values", "f", []string{}, "specify values in a YAML file or a URL (can specify multiple)")
	helmUpgradeCmd.Flags().StringArrayVar(&helmUpgradeArgs.set, "set", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	helmUpgradeCmd.Flags().StringVar(&helmUpgradeArgs.version, "version", "", "specify a version constraint for the chart version to use. This constraint can be a specific tag (e.g. 1.1.1) or range (e.g. ^2.0.0)")
	helmUpgradeCmd.Flags().StringVar(&helmUpgradeArgs.repo, "repo", "", "specify the chart repository URL where to locate the requested chart")
	helmUpgradeCmd.Flags().StringVar(&helmUpgradeArgs.namespace, "namespace", "default", "namespace to install the release into (only used for metadata if not actually installing)")
	helmUpgradeCmd.Flags().BoolVar(&helmUpgradeArgs.updateCRDs, "update-crds", false, "update CRDs unit if it exists")
	helmUpgradeCmd.Flags().BoolVar(&helmUpgradeArgs.usePlaceholder, "use-placeholder", false, "use confighubplaceholder placeholder")
	helmUpgradeCmd.Flags().BoolVar(&helmUpgradeArgs.skipCRDs, "skip-crds", false, "if set, no CRDs from the chart's crds/ directory will be installed (does not affect templated CRDs). Mirrors 'helm upgrade --skip-crds'")

	// Enable wait flag for this command
	enableWaitFlag(helmUpgradeCmd)

	// Enable quiet flag for this command
	enableQuietFlag(helmUpgradeCmd)

	// Compose command hierarchy
	helmCmd.AddCommand(helmUpgradeCmd)
}

func helmUpgradeCmdRun(cmd *cobra.Command, args []string) error {
	helmUpgradeArgs.releaseName = args[0]
	helmUpgradeArgs.chartName = args[1]

	// by default, use the actual namespace for rendering
	replaceMeNamespace := helmUpgradeArgs.namespace
	// if use-placeholder flag is set, use placeholder instead
	if helmUpgradeArgs.usePlaceholder {
		replaceMeNamespace = "confighubplaceholder"
	}

	// Initialize Helm SDK objects
	settings := cli.New()
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(nil, replaceMeNamespace, os.Getenv("HELM_DRIVER"), func(format string, v ...interface{}) {
		fmt.Printf(format+"\n", v...)
	}); err != nil {
		return fmt.Errorf("failed to initialize Helm action configuration: %w", err)
	}

	// Set up chart path options
	chartPathOptions := action.ChartPathOptions{
		Version: helmUpgradeArgs.version,
		RepoURL: helmUpgradeArgs.repo,
	}

	// Locate the chart
	cp, err := chartPathOptions.LocateChart(helmUpgradeArgs.chartName, settings)
	if err != nil {
		return fmt.Errorf("failed to locate chart %s (version: %s, repo: %s): %w", helmUpgradeArgs.chartName, helmUpgradeArgs.version, helmUpgradeArgs.repo, err)
	}

	// 1. Load the chart.
	chrt, err := loader.Load(cp)
	if err != nil {
		return fmt.Errorf("failed to load chart from %s: %w", cp, err)
	}

	// 2. Collect values using the shared function
	userSuppliedValues, err := loadHelmValues(helmUpgradeArgs.valuesFiles, helmUpgradeArgs.set)
	if err != nil {
		return err
	}

	// 2.5. Process dependencies (this filters out disabled subcharts)
	// This must be called BEFORE ToRenderValues to properly handle subchart conditions
	if err := chartutil.ProcessDependencies(chrt, userSuppliedValues); err != nil {
		return fmt.Errorf("failed to process chart dependencies: %w", err)
	}

	// 2.6. Create unit labels with chart metadata
	// All Helm labels are sourced from Chart.yaml metadata (required for Helm upgrade/rollback operations)
	unitLabels := map[string]string{
		helmutils.HelmReleaseLabel: helmUpgradeArgs.releaseName,
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

	// 3. Build render-time values.
	releaseOptions := chartutil.ReleaseOptions{
		Name:      helmUpgradeArgs.releaseName,
		Namespace: replaceMeNamespace,
		Revision:  1,
		IsInstall: false, // This is an upgrade
		IsUpgrade: true,
	}
	valuesToRender, err := chartutil.ToRenderValues(chrt, userSuppliedValues, releaseOptions, chartutil.DefaultCapabilities)
	if err != nil {
		return fmt.Errorf("failed to prepare render values: %w", err)
	}

	// 4. Render using Helm's engine.
	renderingEngine := engine.Engine{}
	renderedResources, err := renderingEngine.Render(chrt, valuesToRender)
	if err != nil {
		return fmt.Errorf("template render failed: %w", err)
	}

	// 4.5. Extract CRDs from the chart's crds/ directory
	// Many charts package CRDs separately in a crds/ directory that aren't processed as templates
	// --skip-crds flag only affects these CRDs, not templated CRDs
	var crdContent strings.Builder
	if !helmUpgradeArgs.skipCRDs {
		crdFiles := chrt.CRDObjects()
		if len(crdFiles) > 0 {
			for _, crdFile := range crdFiles {
				if crdContent.Len() > 0 {
					crdContent.WriteString("---\n")
				}
				crdContent.WriteString(fmt.Sprintf("# Source: %s/crds/%s\n", chrt.Name(), crdFile.Name))
				crdContent.WriteString(string(crdFile.File.Data))
				crdContent.WriteString("\n")
			}
		}
	}

	// 5. Split resources into CRDs and regular resources
	splitResult, err := splitHelmResources(renderedResources, chrt.Name())
	if err != nil {
		return err
	}

	// Combine CRDs from crds/ directory with any CRDs from templates
	if crdContent.Len() > 0 {
		if splitResult.CRDs != "" {
			splitResult.CRDs = crdContent.String() + "---\n" + splitResult.CRDs
		} else {
			splitResult.CRDs = crdContent.String()
		}
	} else if helmUpgradeArgs.skipCRDs && chrt.CRDObjects() != nil && len(chrt.CRDObjects()) > 0 {
		if !quiet {
			tprint("Skipping %d CRDs from %s/crds/ directory due to --skip-crds flag", len(chrt.CRDObjects()), chrt.Name())
		}
	}

	// 6. Check if unit exists
	unitSlug := helmUpgradeArgs.releaseName
	unitToUpdate, err := apiGetUnitFromSlug(unitSlug, "*")
	if err != nil {
		return fmt.Errorf("unit '%s' not found: %w", unitSlug, err)
	}

	// 7. Update the unit with new resources
	if len(splitResult.Resources) > 0 {
		// Prepend namespace YAML if namespace is specified and not default
		var finalYAMLContent string
		if helmUpgradeArgs.namespace != "" && helmUpgradeArgs.namespace != "default" {
			namespaceResource := fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
---
`, helmUpgradeArgs.namespace)
			finalYAMLContent = namespaceResource + splitResult.Resources
		} else {
			finalYAMLContent = splitResult.Resources
		}

		// Encode the resources content
		encodedContent := base64.StdEncoding.EncodeToString([]byte(finalYAMLContent))
		unitToUpdate.Data = encodedContent
		for k, v := range unitLabels {
			unitToUpdate.Labels[k] = v
		}

		// Update the unit
		params := &goclientnew.UpdateUnitParams{}
		updatedUnit, err := updateUnit(unitToUpdate.SpaceID, unitToUpdate, params)
		if err != nil {
			return fmt.Errorf("failed to update unit: %w", err)
		}
		if wait {
			if err := awaitTriggersRemoval(updatedUnit); err != nil {
				return fmt.Errorf("failed to wait for unit triggers: %w", err)
			}
		}
		displayUpdateResults(updatedUnit, "unit", updatedUnit.Slug, updatedUnit.UnitID.String(), displayUnitDetails)
	} else {
		if !quiet {
			tprint("No resources found in chart '%s', skipping update of unit.", helmUpgradeArgs.chartName)
		}
	}

	// 8. Optionally update CRDs unit if flag is set
	if helmUpgradeArgs.updateCRDs && len(splitResult.CRDs) > 0 {
		crdUnitSlug := fmt.Sprintf("%s-crds", helmUpgradeArgs.releaseName)
		crdUnit, err := apiGetUnitFromSlug(crdUnitSlug, "*")
		if err != nil {
			if !quiet {
				tprint("CRDs unit '%s' not found, skipping CRDs update: %v", crdUnitSlug, err)
			}
		} else {
			// Encode the CRDs content
			encodedCRDs := base64.StdEncoding.EncodeToString([]byte(splitResult.CRDs))
			crdUnit.Data = encodedCRDs
			for k, v := range unitLabels {
				crdUnit.Labels[k] = v
			}

			// Update the CRDs unit
			params := &goclientnew.UpdateUnitParams{}
			updatedCRDUnit, err := updateUnit(crdUnit.SpaceID, crdUnit, params)
			if err != nil {
				return fmt.Errorf("failed to update CRDs unit: %w", err)
			}
			if wait {
				if err := awaitTriggersRemoval(updatedCRDUnit); err != nil {
					return fmt.Errorf("failed to wait for CRDs unit triggers: %w", err)
				}
			}
			displayUpdateResults(updatedCRDUnit, "unit", updatedCRDUnit.Slug, updatedCRDUnit.UnitID.String(), displayUnitDetails)
		}
	}

	return nil
}
