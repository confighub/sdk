// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/confighub/sdk/cubapi"
	api "github.com/confighub/sdk/function/api"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/confighub/sdk/workerapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var gitopsDiscoverCmd = &cobra.Command{
	Use:   "discover <target-slug>",
	Short: "Discover GitOps resources on a Kubernetes target",
	Long: getCommandHelp(`Discover ArgoCD Applications, Flux HelmReleases, and Flux Kustomizations
on a Kubernetes target.

Examples:
`+"```"+`
  cub gitops discover --space my-space my-k8s-target
  cub gitops discover --space my-space my-k8s-target --where-resource "metadata.namespace = 'argocd'"
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: gitopsDiscoverCmdRun,
}

func init() {
	gitopsDiscoverCmd.Flags().StringVar(&whereResource, "where-resource", "", "Additional resource filter expression")
	addStandardDisplayFlags(gitopsDiscoverCmd)
	gitopsCmd.AddCommand(gitopsDiscoverCmd)
}

// runDiscover performs the discover operation and returns the LiveState bytes.
// It creates (or reuses) a discover unit, runs a dry-run import, and returns the LiveState.
func runDiscover(targetSlug string) ([]byte, *goclientnew.Target, error) {
	// Parse target
	target, err := parseEntityIdentifierSingleAsEntity[goclientnew.Target](
		targetSlug,
		EntityTypeTarget,
		"*",
		apiGetTargetFromSlugInSpaceCore,
		func(t *goclientnew.Target) string { return t.TargetID.String() },
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve target: %w", err)
	}

	// Verify target supports Kubernetes/YAML
	ct := FindConfigType(target, string(workerapi.ToolchainKubernetesYAML), "Kubernetes")
	if ct == nil {
		return nil, nil, fmt.Errorf("target %s does not support Kubernetes/YAML with provider Kubernetes", target.Slug)
	}

	spaceID := uuid.MustParse(selectedSpaceID)
	targetID := target.TargetID

	// Generate discover unit slug
	slug := discoverUnitSlug(target.Slug, selectedSpaceID)

	// Try to get existing discover unit
	existingUnit, err := apiGetUnitFromSlug(slug, "*")
	if err != nil {
		// Unit doesn't exist, create it
		newUnit := goclientnew.Unit{
			Slug:          slug,
			ToolchainType: string(workerapi.ToolchainKubernetesYAML),
			TargetID:      &targetID,
			Labels: map[string]string{
				discoverTargetLabel: "d" + targetID.String(),
			},
			SpaceID: spaceID,
		}
		params := &goclientnew.CreateUnitParams{}
		createRes, createErr := cubClientNew.CreateUnitWithResponse(ctx, spaceID, params, newUnit)
		if cubapi.IsAPIError(createErr, createRes) {
			return nil, nil, cubapi.InterpretErrorGeneric(createErr, createRes)
		}
		existingUnit = createRes.JSON200
	}

	// Build the where clause for import
	whereClause := "import.include_custom = true AND kind IN ('Application','HelmRelease','Kustomization')"
	if whereResource != "" {
		whereClause += " AND " + whereResource
	}

	// Run dry-run import
	importRequest := goclientnew.ImportRequest{
		Where: whereClause,
	}
	dryRun := true
	importParams := &goclientnew.ImportUnitParams{
		DryRun: &dryRun,
	}

	importRes, err := cubClientNew.ImportUnitWithResponse(ctx, spaceID, existingUnit.UnitID, importParams, importRequest)
	if cubapi.IsAPIError(err, importRes) {
		return nil, nil, cubapi.InterpretErrorGeneric(err, importRes)
	}

	queuedOp := importRes.JSON200
	if queuedOp == nil {
		return nil, nil, fmt.Errorf("import returned no operation")
	}

	// Save and restore quiet to suppress awaitCompletion output
	savedQuiet := quiet
	quiet = true
	err = awaitCompletion("import", queuedOp)
	quiet = savedQuiet
	if err != nil {
		return nil, nil, fmt.Errorf("discover import failed: %w", err)
	}

	// Get the UnitAction to retrieve the imported data.
	// For a dry-run import, the discovered resources are in the Data field.
	unitAction, err := apiGetUnitAction(spaceID, existingUnit.UnitID, queuedOp.QueuedOperationID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get unit action: %w", err)
	}

	// Try Data first (populated for dry-run imports), then fall back to LiveState
	dataField := unitAction.Data
	if dataField == "" {
		dataField = unitAction.LiveState
	}
	if dataField == "" {
		return nil, target, nil
	}

	dataBytes, err := base64.StdEncoding.DecodeString(dataField)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode discovered data: %w", err)
	}

	return dataBytes, target, nil
}

func gitopsDiscoverCmdRun(cmd *cobra.Command, args []string) error {
	liveState, _, err := runDiscover(args[0])
	if err != nil {
		return err
	}

	if len(liveState) == 0 {
		tprint("No GitOps resources were discovered for the specified target")
		return nil
	}

	// Invoke get-resources local function on the LiveState
	resp, err := invokeLocalFunction(liveState, "get-resources", nil, string(workerapi.ToolchainKubernetesYAML))
	if err != nil {
		return fmt.Errorf("failed to invoke get-resources: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("get-resources failed: %v", resp.ErrorMessages)
	}

	// Extract ResourceList from the output
	outputBytes, ok := resp.Outputs[api.OutputTypeResourceList]
	if !ok {
		return fmt.Errorf("get-resources did not return ResourceList output")
	}

	var resources api.ResourceList
	if err := json.Unmarshal(outputBytes, &resources); err != nil {
		return fmt.Errorf("failed to parse resource list: %w", err)
	}

	if hasAlternativeOutput() {
		if jsonOutput {
			displayJSON(resources)
		}
		if jq != "" {
			displayJQ(resources)
		}
		if yamlOutput {
			displayYAML(resources)
		}
		if yq != "" {
			displayYQ(resources)
		}
		return nil
	}

	if len(resources) == 0 {
		tprint("No GitOps resources were discovered for the specified target")
		return nil
	}

	// Print discovered resources
	view := tableView()
	view.SetHeader([]string{"Resource Type", "Resource Name"})
	for _, r := range resources {
		view.Append([]string{string(r.ResourceType), string(r.ResourceName)})
	}
	view.Render()

	return nil
}
