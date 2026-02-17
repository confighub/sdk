// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"

	"github.com/spf13/cobra"

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

	// Render the Helm chart using the shared rendering pipeline
	renderResult, err := renderHelmChart(helmRenderInput{
		releaseName:    helmUpgradeArgs.releaseName,
		chartName:      helmUpgradeArgs.chartName,
		valuesFiles:    helmUpgradeArgs.valuesFiles,
		set:            helmUpgradeArgs.set,
		version:        helmUpgradeArgs.version,
		repo:           helmUpgradeArgs.repo,
		namespace:      helmUpgradeArgs.namespace,
		usePlaceholder: helmUpgradeArgs.usePlaceholder,
		skipCRDs:       helmUpgradeArgs.skipCRDs,
		isUpgrade:      true,
	})
	if err != nil {
		return err
	}

	unitLabels := renderResult.UnitLabels

	// Check if unit exists
	unitSlug := helmUpgradeArgs.releaseName
	unitToUpdate, err := apiGetUnitFromSlug(unitSlug, "*")
	if err != nil {
		return fmt.Errorf("unit '%s' not found: %w", unitSlug, err)
	}

	// Update the unit with new resources
	if len(renderResult.Resources) > 0 {
		finalYAMLContent := prependNamespaceResource(renderResult.Resources, helmUpgradeArgs.namespace)

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
	if helmUpgradeArgs.updateCRDs && len(renderResult.CRDs) > 0 {
		crdUnitSlug := fmt.Sprintf("%s-crds", helmUpgradeArgs.releaseName)
		crdUnit, err := apiGetUnitFromSlug(crdUnitSlug, "*")
		if err != nil {
			if !quiet {
				tprint("CRDs unit '%s' not found, skipping CRDs update: %v", crdUnitSlug, err)
			}
		} else {
			// Encode the CRDs content
			encodedCRDs := base64.StdEncoding.EncodeToString([]byte(renderResult.CRDs))
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
