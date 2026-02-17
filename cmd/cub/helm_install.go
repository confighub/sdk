// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

const (
	// Link suffix for namespace links
	linkSuffixNamespace = "ns"
	// Link suffix for CRD links
	linkSuffixCRDs = "crds"
)

// helmInstallCmd installs a Helm chart (a convenience wrapper around `helm install`).
var helmInstallCmd = &cobra.Command{
	Use:   "install <release-name> <repo>/<chartname>",
	Short: "Render a Helm chart's templates and install to ConfigHub",
	Long: getCommandHelp(`Render a Helm chart's templates and install them as ConfigHub units.
This command loads a chart (e.g., <repo>/<chartname>) from configured Helm repositories.
It processes values from files and --set flags.
CRDs are always rendered and splitted if exist.

Examples:
`+"```"+`
  # Render nginx chart (ensure 'bitnami' repo is added via 'helm repo add')
  # This command would create:
  # 1. my-nginx containing nginx Namespace definition + nginx resources
  # 2. my-nginx-crds containing CRDs (if any)
  #

  cub helm install --namespace nginx my-nginx bitnami/nginx --version 15.5.2 --set image.tag=latest

  # Render the cert-manager chart
  # This creates 2 units:
  # 1. cert-manager-crds: Custom Resource Definitions
  # 2. cert-manager: Namespace definition + Main resources (rendered directly from Helm)
  #

  cub helm install \
    --namespace cert-manager \
	  cert-manager \
	  jetstack/cert-manager \
	  --version v1.17.1
`+"```"+`
`, ""),
	Args:          cobra.MinimumNArgs(2),
	RunE:          helmInstallCmdRun,
	SilenceUsage:  true,
	SilenceErrors: true,
}

var helmInstallArgs struct {
	valuesFiles    []string
	set            []string
	version        string
	repo           string
	namespace      string // This will be used for k8s namespace object for the release
	chartName      string
	releaseName    string
	usePlaceholder bool // Use confighubplaceholder placeholder for rendering
	skipCRDs       bool // Skip CRDs from crds/ directory only (mirrors helm install --skip-crds)
	targetSlug     string
}

func init() {
	// Add flags to the install command
	helmInstallCmd.Flags().StringArrayVarP(&helmInstallArgs.valuesFiles, "values", "f", []string{}, "specify values in a YAML file or a URL (can specify multiple)")
	helmInstallCmd.Flags().StringArrayVar(&helmInstallArgs.set, "set", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	helmInstallCmd.Flags().StringVar(&helmInstallArgs.version, "version", "", "specify a version constraint for the chart version to use. This constraint can be a specific tag (e.g. 1.1.1) or range (e.g. ^2.0.0)")
	helmInstallCmd.Flags().StringVar(&helmInstallArgs.repo, "repo", "", "specify the chart repository URL where to locate the requested chart")
	helmInstallCmd.Flags().StringVar(&helmInstallArgs.namespace, "namespace", "default", "namespace to install the release into (only used for metadata if not actually installing)")

	helmInstallCmd.Flags().BoolVar(&helmInstallArgs.usePlaceholder, "use-placeholder", false, "use confighubplaceholder placeholder")
	helmInstallCmd.Flags().BoolVar(&helmInstallArgs.skipCRDs, "skip-crds", false, "if set, no CRDs from the chart's crds/ directory will be installed (does not affect templated CRDs). Mirrors 'helm install --skip-crds'")
	helmInstallCmd.Flags().StringVar(&helmInstallArgs.targetSlug, "target", "", "target for the units")

	// Enable wait flag for this command
	enableWaitFlag(helmInstallCmd)

	// Enable quiet flag for this command
	enableQuietFlag(helmInstallCmd)

	// Compose command hierarchy
	helmCmd.AddCommand(helmInstallCmd) // helmCmd here refers to the package-level variable
}

// createNamespaceUnit creates a new unit representing a Kubernetes namespace.
func createNamespaceUnit(ctx context.Context, client *goclientnew.ClientWithResponses, spaceIDStr string, namespaceName string, releaseNameForSlug string, unitLabels map[string]string, targetID *uuid.UUID) (*goclientnew.Unit, error) {
	namespaceResource := fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, namespaceName)

	unitSlug := releaseNameForSlug + "-ns"
	toolchainType := "Kubernetes/YAML"

	parsedSpaceID, err := uuid.Parse(spaceIDStr)
	if err != nil {
		return nil, fmt.Errorf("internal error: selected space ID '%s' is not a valid UUID: %w", spaceIDStr, err)
	}

	apiUnit := goclientnew.Unit{
		SpaceID:       parsedSpaceID,
		Slug:          unitSlug,
		ToolchainType: toolchainType,
		Data:          base64.StdEncoding.EncodeToString([]byte(namespaceResource)),
		Labels:        unitLabels,
		TargetID:      targetID,
	}

	// For a new unit without cloning, params can be an empty struct.
	createParams := goclientnew.CreateUnitParams{}

	// API Call to create the unit
	unitRes, err := client.CreateUnitWithResponse(ctx, parsedSpaceID, &createParams, apiUnit)
	if cubapi.IsAPIError(err, unitRes) { // Use the standard error handling
		return nil, cubapi.InterpretErrorGeneric(err, unitRes)
	}

	createdUnit := unitRes.JSON200
	if createdUnit == nil {
		// This case should ideally be caught by IsAPIError or cubapi.InterpretErrorGeneric
		return nil, fmt.Errorf("failed to create unit '%s', API response was not successful. Status: %s. Body: %s", unitSlug, unitRes.Status(), string(unitRes.Body))
	}
	return createdUnit, nil
}

// createCRDsUnit creates a new unit representing the CRDs from a Helm chart.
func createCRDsUnit(ctx context.Context, client *goclientnew.ClientWithResponses, spaceIDStr string, crdYAMLContent string, releaseName string, unitLabels map[string]string, targetID *uuid.UUID) (*goclientnew.Unit, error) {
	unitSlug := releaseName + "-crds"
	toolchainType := "Kubernetes/YAML"

	parsedSpaceID, err := uuid.Parse(spaceIDStr)
	if err != nil {
		return nil, fmt.Errorf("internal error: selected space ID '%s' is not a valid UUID: %w", spaceIDStr, err)
	}

	apiUnit := goclientnew.Unit{
		SpaceID:       parsedSpaceID,
		Slug:          unitSlug,
		ToolchainType: toolchainType,
		Data:          base64.StdEncoding.EncodeToString([]byte(crdYAMLContent)),
		Labels:        unitLabels,
		TargetID:      targetID,
	}

	createParams := goclientnew.CreateUnitParams{}

	unitRes, err := client.CreateUnitWithResponse(ctx, parsedSpaceID, &createParams, apiUnit)
	if cubapi.IsAPIError(err, unitRes) {
		return nil, cubapi.InterpretErrorGeneric(err, unitRes)
	}

	createdUnit := unitRes.JSON200
	if createdUnit == nil {
		return nil, fmt.Errorf("failed to create CRDs unit '%s', API response was not successful. Status: %s. Body: %s", unitSlug, unitRes.Status(), string(unitRes.Body))
	}
	return createdUnit, nil
}

// createResourceUnit creates a new unit representing the regular resources from a Helm chart.
func createResourceUnit(ctx context.Context, client *goclientnew.ClientWithResponses, spaceIDStr string, resourceYAMLContent string, releaseName string, unitLabels map[string]string, namespace string, targetID *uuid.UUID) (*goclientnew.Unit, error) {
	unitSlug := releaseName
	toolchainType := "Kubernetes/YAML"

	parsedSpaceID, err := uuid.Parse(spaceIDStr)
	if err != nil {
		return nil, fmt.Errorf("internal error: selected space ID '%s' is not a valid UUID: %w", spaceIDStr, err)
	}

	finalYAMLContent := prependNamespaceResource(resourceYAMLContent, namespace)

	apiUnit := goclientnew.Unit{
		SpaceID:       parsedSpaceID,
		Slug:          unitSlug,
		ToolchainType: toolchainType,
		Data:          base64.StdEncoding.EncodeToString([]byte(finalYAMLContent)),
		Labels:        unitLabels,
		TargetID:      targetID,
	}

	createParams := goclientnew.CreateUnitParams{}

	unitRes, err := client.CreateUnitWithResponse(ctx, parsedSpaceID, &createParams, apiUnit)
	if cubapi.IsAPIError(err, unitRes) {
		return nil, cubapi.InterpretErrorGeneric(err, unitRes)
	}

	createdUnit := unitRes.JSON200
	if createdUnit == nil {
		return nil, fmt.Errorf("failed to create resources unit '%s', API response was not successful. Status: %s. Body: %s", unitSlug, unitRes.Status(), string(unitRes.Body))
	}
	return createdUnit, nil
}

// createUnitLink creates a link between two units with proper error handling
func createUnitLink(ctx context.Context, client *goclientnew.ClientWithResponses, fromUnit, toUnit *goclientnew.Unit, linkSuffix string, spaceID uuid.UUID) error {
	if toUnit == nil || toUnit.UnitID == uuid.Nil {
		// Silently skip if target unit is not available
		return nil
	}

	linkSlug := fmt.Sprintf("%s-to-%s", fromUnit.Slug, linkSuffix)

	linkToCreate := goclientnew.Link{
		SpaceID:    spaceID,
		Slug:       makeSlug(linkSlug),
		FromUnitID: fromUnit.UnitID,
		ToUnitID:   toUnit.UnitID,
		ToSpaceID:  toUnit.SpaceID,
	}

	linkRes, linkErr := client.CreateLinkWithResponse(ctx, spaceID, nil, linkToCreate)

	if cubapi.IsAPIError(linkErr, linkRes) {
		return cubapi.InterpretErrorGeneric(linkErr, linkRes)
	}

	if linkRes.JSON200 != nil {
		linkDetails := linkRes.JSON200
		displayCreateResults(linkDetails, "link", linkDetails.Slug, linkDetails.LinkID.String(), displayLinkDetails)
		if wait { // global wait flag
			// Since we most likely re-use the create result, the awaiting triggers gate should be set and will cause the await loop
			// to wait and re-fetch the unit to check the apply gate again.
			if err := awaitTriggersRemoval(fromUnit); err != nil {
				return fmt.Errorf("failed to wait for triggers on unit '%s': %w", fromUnit.Slug, err)
			}
		}
		return nil
	}

	// Handle unexpected response
	if linkErr != nil {
		return fmt.Errorf("client error during link creation: %w", linkErr)
	}
	if linkRes != nil {
		return fmt.Errorf("unexpected response status during link creation: %s", linkRes.Status())
	}
	return fmt.Errorf("unknown error during link creation")
}

func helmInstallCmdRun(cmd *cobra.Command, args []string) error {
	helmInstallArgs.releaseName = args[0]
	helmInstallArgs.chartName = args[1]

	// Render the Helm chart using the shared rendering pipeline
	renderResult, err := renderHelmChart(helmRenderInput{
		releaseName:    helmInstallArgs.releaseName,
		chartName:      helmInstallArgs.chartName,
		valuesFiles:    helmInstallArgs.valuesFiles,
		set:            helmInstallArgs.set,
		version:        helmInstallArgs.version,
		repo:           helmInstallArgs.repo,
		namespace:      helmInstallArgs.namespace,
		usePlaceholder: helmInstallArgs.usePlaceholder,
		skipCRDs:       helmInstallArgs.skipCRDs,
	})
	if err != nil {
		return err
	}

	unitLabels := renderResult.UnitLabels

	// Resolve target if specified
	var targetID *uuid.UUID
	if helmInstallArgs.targetSlug != "" {
		id, err := parseEntityIdentifierSingle[goclientnew.Target](
			helmInstallArgs.targetSlug,
			EntityTypeTarget,
			apiGetTargetFromSlugInSpaceCore,
			func(t *goclientnew.Target) string { return t.TargetID.String() },
		)
		if err != nil {
			return err
		}
		targetID = &id
	}

	// Create a unit for CRDs if any were found
	var crdUnit *goclientnew.Unit
	if len(renderResult.CRDs) > 0 {
		createdCRDsUnit, err := createCRDsUnit(ctx, cubClientNew, selectedSpaceID, renderResult.CRDs, helmInstallArgs.releaseName, unitLabels, targetID)
		if err != nil {
			return fmt.Errorf("failed to create CRDs unit: %w", err)
		}
		crdUnit = createdCRDsUnit
		if wait {
			if err := awaitTriggersRemoval(crdUnit); err != nil {
				return err
			}
		}
		displayCreateResults(crdUnit, "unit", crdUnit.Slug, crdUnit.UnitID.String(), displayUnitDetails)
	} else {
		if !quiet {
			tprint("No CRDs found in chart '%s', skipping creation of CRDs unit.", helmInstallArgs.chartName)
		}
	}

	// Create a unit for regular resources if any were found
	if len(renderResult.Resources) > 0 {
		createdResourceUnit, err := createResourceUnit(ctx, cubClientNew, selectedSpaceID, renderResult.Resources, helmInstallArgs.releaseName, unitLabels, helmInstallArgs.namespace, targetID)
		if err != nil {
			return fmt.Errorf("failed to create resources unit: %w", err)
		}
		if wait {
			if err := awaitTriggersRemoval(createdResourceUnit); err != nil {
				return fmt.Errorf("failed to wait for base unit triggers: %w", err)
			}
		}
		displayCreateResults(createdResourceUnit, "unit", createdResourceUnit.Slug, createdResourceUnit.UnitID.String(), displayUnitDetails)

		// Link the main unit to the CRDs unit
		spaceID, parseErr := uuid.Parse(selectedSpaceID)
		if parseErr != nil {
			return fmt.Errorf("failed to parse space ID '%s' for linking: %w", selectedSpaceID, parseErr)
		}
		if err := createUnitLink(ctx, cubClientNew, createdResourceUnit, crdUnit, linkSuffixCRDs, spaceID); err != nil {
			return err
		}
	} else {
		if !quiet {
			tprint("No regular resources found in chart '%s', skipping creation of resources unit.", helmInstallArgs.chartName)
		}
	}

	return nil
}
