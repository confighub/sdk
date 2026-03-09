// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	bridgeapi "github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/cubapi"
	api "github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/bridge-impl/helmutils"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/confighub/sdk/workerapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var gitopsImportCmd = &cobra.Command{
	Use:   "import <target-slug> <render-target-slug>",
	Short: "Import GitOps resources from a Kubernetes target",
	Long: getCommandHelp(`Import discovered GitOps resources (ArgoCD Applications, Flux HelmReleases,
Flux Kustomizations) from a Kubernetes target, render them using a render target,
and create the corresponding ConfigHub units and links.

Examples:
`+"```"+`
  cub gitops import --space my-space my-k8s-target my-render-target
  cub gitops import --space my-space my-k8s-target my-render-target --where-resource "metadata.namespace = 'argocd'"
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(2),
	RunE: gitopsImportCmdRun,
}

func init() {
	gitopsImportCmd.Flags().StringVar(&whereResource, "where-resource", "", "Additional resource filter expression")
	addStandardDisplayFlags(gitopsImportCmd)
	enableActionWaitFlag(gitopsImportCmd)
	gitopsCmd.AddCommand(gitopsImportCmd)
}

// discoverResourceInfo holds information about a discovered GitOps resource.
type discoverResourceInfo struct {
	resourceName string
	resourceType string
	resourceBody string
	apiGroup     string
	kind         string
}

// parseDiscoveredResources invokes get-resources on the LiveState and returns parsed resource info.
func parseDiscoveredResources(liveState []byte) ([]discoverResourceInfo, error) {
	resp, err := invokeLocalFunction(liveState, "get-resources", nil, string(workerapi.ToolchainKubernetesYAML))
	if err != nil {
		return nil, fmt.Errorf("failed to invoke get-resources: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("get-resources failed: %v", resp.ErrorMessages)
	}

	outputBytes, ok := resp.Outputs[api.OutputTypeResourceList]
	if !ok {
		return nil, fmt.Errorf("get-resources did not return ResourceList output")
	}

	var resources api.ResourceList
	if err := json.Unmarshal(outputBytes, &resources); err != nil {
		return nil, fmt.Errorf("failed to parse resource list: %w", err)
	}

	var result []discoverResourceInfo
	for _, r := range resources {
		// ResourceType for K8s is "apiVersion/kind"
		rt := string(r.ResourceType)
		apiGroup, kind := parseResourceType(rt)
		result = append(result, discoverResourceInfo{
			resourceName: string(r.ResourceName),
			resourceType: rt,
			resourceBody: r.ResourceBody,
			apiGroup:     apiGroup,
			kind:         kind,
		})
	}
	return result, nil
}

// parseResourceType splits a Kubernetes resource type string (e.g., "argoproj.io/v1alpha1/Application")
// into the API group prefix and kind.
func parseResourceType(resourceType string) (apiGroup string, kind string) {
	parts := strings.Split(resourceType, "/")
	if len(parts) >= 3 {
		// e.g., "argoproj.io/v1alpha1/Application" -> apiGroup="argoproj.io", kind="Application"
		apiGroup = parts[0]
		kind = parts[len(parts)-1]
	} else if len(parts) == 2 {
		// e.g., "v1/ConfigMap" -> apiGroup="", kind="ConfigMap"
		kind = parts[1]
	} else {
		kind = resourceType
	}
	return
}

// providerForResource determines which bridge provider to use based on the API group.
func providerForResource(apiGroup string) bridgeapi.ProviderType {
	if strings.HasSuffix(apiGroup, "argoproj.io") || apiGroup == "argoproj.io" {
		return bridgeapi.ProviderArgoCDRenderer
	}
	if strings.HasSuffix(apiGroup, "fluxcd.io") || strings.Contains(apiGroup, "fluxcd.io") {
		return bridgeapi.ProviderFluxRenderer
	}
	return ""
}

func gitopsImportCmdRun(cmd *cobra.Command, args []string) error {
	targetSlug := args[0]
	renderTargetSlug := args[1]
	spaceID := uuid.MustParse(selectedSpaceID)

	// Parse render target
	renderTarget, err := parseEntityIdentifierSingleAsEntity[goclientnew.Target](
		renderTargetSlug,
		EntityTypeTarget,
		"*",
		apiGetTargetFromSlugInSpaceCore,
		func(t *goclientnew.Target) string { return t.TargetID.String() },
	)
	if err != nil {
		return fmt.Errorf("failed to resolve render target: %w", err)
	}

	// Run discover to find GitOps resources on the target
	liveState, target, err := runDiscover(targetSlug)
	if err != nil {
		return err
	}

	if len(liveState) == 0 {
		tprint("No GitOps resources were discovered for the specified target")
		return nil
	}

	// Parse discovered resources
	resources, err := parseDiscoveredResources(liveState)
	if err != nil {
		return err
	}

	if len(resources) == 0 {
		tprint("No GitOps resources found to import")
		return nil
	}

	tprint("Discovered %d GitOps resources, creating renderer units...", len(resources))

	targetID := target.TargetID
	discoverLabel := "d" + targetID.String()

	// Create renderer (-dry) units for each discovered resource
	var dryUnits []goclientnew.Unit
	for _, r := range resources {
		provider := providerForResource(r.apiGroup)
		if provider == "" {
			tprint("Skipping resource %s: unknown provider for API group %s", r.resourceName, r.apiGroup)
			continue
		}

		// Verify render target supports this provider
		ct := FindConfigType(renderTarget, string(workerapi.ToolchainKubernetesYAML), string(provider))
		if ct == nil {
			return fmt.Errorf("render target %s does not support provider %s with toolchain Kubernetes/YAML", renderTarget.Slug, provider)
		}

		drySuffix := "-dry"
		drySlug := truncateWithHash(makeSlug(r.resourceName+"-"+r.kind), MaxSlugLength-len(drySuffix)) + drySuffix
		renderTargetID := renderTarget.TargetID

		dryUnit := goclientnew.Unit{
			Slug:          drySlug,
			ToolchainType: string(workerapi.ToolchainKubernetesYAML),
			TargetID:      &renderTargetID,
			Labels: map[string]string{
				discoverTargetLabel: discoverLabel,
			},
			SpaceID:      spaceID,
			ProviderType: string(provider),
			Data:         base64.StdEncoding.EncodeToString([]byte(r.resourceBody)),
		}
		dryUnits = append(dryUnits, dryUnit)
	}

	if len(dryUnits) == 0 {
		tprint("No supported GitOps resources found to import")
		return nil
	}

	// Create dry units one by one
	var createdDryUnits []*goclientnew.Unit
	for i := range dryUnits {
		allowExistsStr := "true"
		params := &goclientnew.CreateUnitParams{
			AllowExists: &allowExistsStr,
		}
		createRes, createErr := cubClientNew.CreateUnitWithResponse(ctx, spaceID, params, dryUnits[i])
		if cubapi.IsAPIError(createErr, createRes) {
			return cubapi.InterpretErrorGeneric(createErr, createRes)
		}
		createdDryUnits = append(createdDryUnits, createRes.JSON200)
		tprint("Created renderer unit: %s", createRes.JSON200.Slug)
	}

	// Bulk apply renderer units (only the -dry units, not the discover unit)
	whereLabel := fmt.Sprintf("Labels.%s = '%s' AND Slug LIKE '%%-dry'", discoverTargetLabel, discoverLabel)
	effectiveWhere := addSpaceIDToWhereClause(whereLabel, selectedSpaceID)

	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	applyParams := &goclientnew.BulkApplyUnitsParams{
		Where:   effectiveWhere,
		Include: &include,
	}

	tprint("Rendering discovered resources...")

	resp, err := cubClientNew.BulkApplyUnitsWithResponse(ctx, applyParams)
	if cubapi.IsAPIError(err, resp) {
		return cubapi.InterpretErrorGeneric(err, resp)
	}

	var responses *[]goclientnew.UnitActionResponse
	if resp.JSON200 != nil {
		responses = resp.JSON200
	} else if resp.JSON207 != nil {
		responses = resp.JSON207
	} else {
		return fmt.Errorf("unexpected response from bulk apply API")
	}

	// Await bulk completion
	if responses != nil && len(*responses) > 0 {
		var queuedOps []*goclientnew.QueuedOperation
		for _, result := range *responses {
			if result.Action != nil {
				queuedOps = append(queuedOps, result.Action)
			}
		}
		if len(queuedOps) > 0 {
			savedQuiet := quiet
			quiet = true
			awaitBulkCompletion("apply", queuedOps)
			quiet = savedQuiet
		}
	}

	tprint("Rendering complete. Creating wet units and links...")

	// For each dry unit, inspect LiveState and create corresponding wet/crds units
	targetIDForWet := target.TargetID
	for _, dryUnit := range createdDryUnits {
		// Get the unit's LiveState after apply
		refreshedUnit, refreshErr := apiGetUnitFromSlug(dryUnit.Slug, "*")
		if refreshErr != nil {
			tprint("Warning: could not refresh unit %s: %v", dryUnit.Slug, refreshErr)
			continue
		}

		// Decode LiveState once for CRD check and helm label extraction
		hasCRDs := false
		var liveStateBytes []byte
		if refreshedUnit.LiveState != "" {
			lsBytes, decErr := base64.StdEncoding.DecodeString(refreshedUnit.LiveState)
			if decErr == nil {
				liveStateBytes = lsBytes
				hasCRDs = liveStateHasCRDs(lsBytes)
			}
		}

		// Determine base slug (remove -dry suffix)
		baseSlug := strings.TrimSuffix(dryUnit.Slug, "-dry")

		// Build labels for wet units
		wetLabels := map[string]string{
			discoverTargetLabel: discoverLabel,
		}

		// Add helm labels from rendered resources if applicable
		if liveStateBytes != nil {
			addHelmLabelsFromLiveState(wetLabels, liveStateBytes)
		}

		if hasCRDs {
			// Create -crds unit
			crdsSuffix := "-crds"
			crdsSlug := truncateWithHash(baseSlug, MaxSlugLength-len(crdsSuffix)) + crdsSuffix
			crdsUnit := goclientnew.Unit{
				Slug:          crdsSlug,
				ToolchainType: string(workerapi.ToolchainKubernetesYAML),
				TargetID:      &targetIDForWet,
				Labels:        copyLabels(wetLabels),
				SpaceID:       spaceID,
			}
			allowExistsStr := "true"
			crdsCreateRes, crdsErr := cubClientNew.CreateUnitWithResponse(ctx, spaceID, &goclientnew.CreateUnitParams{AllowExists: &allowExistsStr}, crdsUnit)
			if cubapi.IsAPIError(crdsErr, crdsCreateRes) {
				return cubapi.InterpretErrorGeneric(crdsErr, crdsCreateRes)
			}
			tprint("Created CRDs unit: %s", crdsCreateRes.JSON200.Slug)

			// Create link from -crds to -dry with WhereResource for CRDs
			if err := createGitopsLink(spaceID, crdsCreateRes.JSON200, dryUnit, "kind = 'CustomResourceDefinition'"); err != nil {
				return err
			}

			// Create -wet unit
			wetSuffix := "-wet"
			wetSlug := truncateWithHash(baseSlug, MaxSlugLength-len(wetSuffix)) + wetSuffix
			wetUnit := goclientnew.Unit{
				Slug:          wetSlug,
				ToolchainType: string(workerapi.ToolchainKubernetesYAML),
				TargetID:      &targetIDForWet,
				Labels:        copyLabels(wetLabels),
				SpaceID:       spaceID,
			}
			wetCreateRes, wetErr := cubClientNew.CreateUnitWithResponse(ctx, spaceID, &goclientnew.CreateUnitParams{AllowExists: &allowExistsStr}, wetUnit)
			if cubapi.IsAPIError(wetErr, wetCreateRes) {
				return cubapi.InterpretErrorGeneric(wetErr, wetCreateRes)
			}
			tprint("Created wet unit: %s", wetCreateRes.JSON200.Slug)

			// Create link from -wet to -dry with WhereResource excluding CRDs
			if err := createGitopsLink(spaceID, wetCreateRes.JSON200, dryUnit, "kind != 'CustomResourceDefinition'"); err != nil {
				return err
			}
		} else {
			// No CRDs: just create -wet unit
			wetSuffix := "-wet"
			wetSlug := truncateWithHash(baseSlug, MaxSlugLength-len(wetSuffix)) + wetSuffix
			wetUnit := goclientnew.Unit{
				Slug:          wetSlug,
				ToolchainType: string(workerapi.ToolchainKubernetesYAML),
				TargetID:      &targetIDForWet,
				Labels:        copyLabels(wetLabels),
				SpaceID:       spaceID,
			}
			allowExistsStr := "true"
			wetCreateRes, wetErr := cubClientNew.CreateUnitWithResponse(ctx, spaceID, &goclientnew.CreateUnitParams{AllowExists: &allowExistsStr}, wetUnit)
			if cubapi.IsAPIError(wetErr, wetCreateRes) {
				return cubapi.InterpretErrorGeneric(wetErr, wetCreateRes)
			}
			tprint("Created wet unit: %s", wetCreateRes.JSON200.Slug)

			// Create link from -wet to -dry (no where filter)
			if err := createGitopsLink(spaceID, wetCreateRes.JSON200, dryUnit, ""); err != nil {
				return err
			}
		}
	}

	tprint("GitOps import complete")
	return nil
}

// createGitopsLink creates a link from fromUnit to toUnit with UseLiveState, AutoUpdate, and MergeUnits.
func createGitopsLink(spaceID uuid.UUID, fromUnit *goclientnew.Unit, toUnit *goclientnew.Unit, whereResourceExpr string) error {
	link := goclientnew.Link{
		SpaceID:      spaceID,
		Slug:         "", // autogenerate Slug
		FromUnitID:   fromUnit.UnitID,
		ToUnitID:     toUnit.UnitID,
		ToSpaceID:    spaceID,
		UseLiveState: true,
		AutoUpdate:   true,
		UpdateType:   "MergeUnits",
	}
	if whereResourceExpr != "" {
		link.WhereResource = whereResourceExpr
	}

	allowExistsStr := "true"
	linkRes, err := cubClientNew.CreateLinkWithResponse(ctx, spaceID, &goclientnew.CreateLinkParams{AllowExists: &allowExistsStr}, link)
	if cubapi.IsAPIError(err, linkRes) {
		return cubapi.InterpretErrorGeneric(err, linkRes)
	}
	tprint("Created link: %s", linkRes.JSON200.Slug)
	return nil
}

// liveStateHasCRDs checks if the LiveState contains any CustomResourceDefinition resources.
func liveStateHasCRDs(liveStateBytes []byte) bool {
	resp, err := invokeLocalFunction(liveStateBytes, "get-resources", []string{"body=none"}, string(workerapi.ToolchainKubernetesYAML))
	if err != nil || !resp.Success {
		return false
	}
	outputBytes, ok := resp.Outputs[api.OutputTypeResourceList]
	if !ok {
		return false
	}
	var resources api.ResourceList
	if err := json.Unmarshal(outputBytes, &resources); err != nil {
		return false
	}
	for _, r := range resources {
		_, kind := parseResourceType(string(r.ResourceType))
		if kind == "CustomResourceDefinition" {
			return true
		}
	}
	return false
}

// addHelmLabelsFromLiveState inspects the rendered resources in the LiveState to detect
// Helm-managed resources and extract chart/release metadata from their Kubernetes labels.
//
// ArgoCD adds labels like: chart: <name>-<version>, heritage: Helm, release: <name>
// Flux adds labels like: helm.sh/chart: <name>-<version>, app.kubernetes.io/managed-by: Helm
func addHelmLabelsFromLiveState(unitLabels map[string]string, liveStateBytes []byte) {
	decoder := yaml.NewDecoder(bytes.NewReader(liveStateBytes))
	for {
		var doc map[string]any
		if err := decoder.Decode(&doc); err != nil {
			break
		}
		metadata, ok := doc["metadata"].(map[string]any)
		if !ok {
			continue
		}
		k8sLabels, ok := metadata["labels"].(map[string]any)
		if !ok {
			continue
		}

		// Check for ArgoCD Helm labels: heritage=Helm
		if fmt.Sprint(k8sLabels["heritage"]) == "Helm" {
			parseChartNameVersion(unitLabels, fmt.Sprint(k8sLabels["chart"]))
			if release := fmt.Sprint(k8sLabels["release"]); release != "" && release != "<nil>" {
				unitLabels[helmutils.HelmReleaseLabel] = release
			}
			unitLabels[helmutils.HelmChartAPIVersionLabel] = "v2"
			return
		}

		// Check for Flux Helm labels: app.kubernetes.io/managed-by=Helm
		if fmt.Sprint(k8sLabels["app.kubernetes.io/managed-by"]) == "Helm" {
			parseChartNameVersion(unitLabels, fmt.Sprint(k8sLabels["helm.sh/chart"]))
			if name := fmt.Sprint(k8sLabels["app.kubernetes.io/name"]); name != "" && name != "<nil>" {
				unitLabels[helmutils.HelmReleaseLabel] = name
			}
			unitLabels[helmutils.HelmChartAPIVersionLabel] = "v2"
			return
		}
	}
}

// parseChartNameVersion parses a Helm chart label value like "name-1.2.3" into chart name and version.
func parseChartNameVersion(unitLabels map[string]string, chartLabel string) {
	if chartLabel == "" || chartLabel == "<nil>" {
		return
	}
	// The chart label format is "<chart-name>-<version>", e.g. "podinfo-6.10.1"
	// Find the last hyphen that separates name from version (version starts with a digit)
	for i := len(chartLabel) - 1; i > 0; i-- {
		if chartLabel[i] == '-' && i+1 < len(chartLabel) && chartLabel[i+1] >= '0' && chartLabel[i+1] <= '9' {
			unitLabels[helmutils.HelmChartLabel] = chartLabel[:i]
			unitLabels[helmutils.HelmChartVersionLabel] = chartLabel[i+1:]
			return
		}
	}
	unitLabels[helmutils.HelmChartLabel] = chartLabel
}

// copyLabels creates a copy of the labels map.
func copyLabels(labels map[string]string) map[string]string {
	result := make(map[string]string, len(labels))
	maps.Copy(result, labels)
	return result
}
