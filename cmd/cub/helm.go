// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/chartutil"
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
