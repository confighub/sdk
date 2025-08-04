// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/engine"
	"helm.sh/helm/v3/pkg/strvals"

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
	Long: `Render a Helm chart's templates and install them as ConfigHub units.
This command loads a chart (e.g., <repo>/<chartname>) from configured Helm repositories.
It processes values from files and --set flags.
CRDs are always rendered and splitted if exist.

Examples:
  # Render nginx chart (ensure 'bitnami' repo is added via 'helm repo add')
  # This command would create:
  # 1. my-nginx-ns containing nginx Namespace definition
  # 2. my-nginx-base containing nginx resources
  #

  cub helm install --namespace nginx my-nginx bitnami/nginx --version 15.5.2 --set image.tag=latest

  # Render the cert-manager chart using ConfigHub clone-based deployment
  # This creates 4 units:
  # 1. cert-manager-ns: Namespace definition
  # 2. cert-manager-crds: Custom Resource Definitions  
  # 3. cert-manager-base: Main resources (rendered directly from Helm)
  # 4. cert-manager: Clone of base unit for customizations
  #
  # Why using the clone-based deployment? This preserves manual changes when re-rendering charts.
  # The base unit gets replaced on updates, while the clone retains customizations.
  # To get the new updates to the clone, we will perform an "upgrade".
  # CRDs and namespaces typically don't need clones as they rarely require modification.
  #

  cub helm install \
    --namespace cert-manager \
	  cert-manager \
	  jetstack/cert-manager \
	  --version v1.17.1
`,
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
	clone          bool // Support clone as downstream
	usePlaceholder bool // Use confighubplaceholder placeholder for rendering
	skipCRDs       bool // Skip CRDs from crds/ directory only (mirrors helm install --skip-crds)
}

func init() {
	// Add flags to the install command
	helmInstallCmd.Flags().StringArrayVarP(&helmInstallArgs.valuesFiles, "values", "f", []string{}, "specify values in a YAML file or a URL (can specify multiple)")
	helmInstallCmd.Flags().StringArrayVar(&helmInstallArgs.set, "set", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	helmInstallCmd.Flags().StringVar(&helmInstallArgs.version, "version", "", "specify a version constraint for the chart version to use. This constraint can be a specific tag (e.g. 1.1.1) or range (e.g. ^2.0.0)")
	helmInstallCmd.Flags().StringVar(&helmInstallArgs.repo, "repo", "", "specify the chart repository URL where to locate the requested chart")
	helmInstallCmd.Flags().StringVar(&helmInstallArgs.namespace, "namespace", "default", "namespace to install the release into (only used for metadata if not actually installing)")
	helmInstallCmd.Flags().BoolVar(&helmInstallArgs.clone, "clone", true, "clone as downstream unit")
	helmInstallCmd.Flags().BoolVar(&helmInstallArgs.usePlaceholder, "use-placeholder", true, "use confighubplaceholder placeholder")
	helmInstallCmd.Flags().BoolVar(&helmInstallArgs.skipCRDs, "skip-crds", false, "if set, no CRDs from the chart's crds/ directory will be installed (does not affect templated CRDs). Mirrors 'helm install --skip-crds'")

	// Enable wait flag for this command
	enableWaitFlag(helmInstallCmd)

	// Enable quiet flag for this command
	enableQuietFlagForOperation(helmInstallCmd)

	// Compose command hierarchy
	helmCmd.AddCommand(helmInstallCmd) // helmCmd here refers to the package-level variable
}

// createNamespaceUnit creates a new unit representing a Kubernetes namespace.
func createNamespaceUnit(ctx context.Context, client *goclientnew.ClientWithResponses, spaceIDStr string, namespaceName string, releaseNameForSlug string, unitLabels map[string]string) (*goclientnew.Unit, error) {
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
	}

	// For a new unit without cloning, params can be an empty struct.
	createParams := goclientnew.CreateUnitParams{}

	// API Call to create the unit
	unitRes, err := client.CreateUnitWithResponse(ctx, parsedSpaceID, &createParams, apiUnit)
	if IsAPIError(err, unitRes) { // Use the standard error handling
		return nil, InterpretErrorGeneric(err, unitRes)
	}

	createdUnit := unitRes.JSON200
	if createdUnit == nil {
		// This case should ideally be caught by IsAPIError or InterpretErrorGeneric
		return nil, fmt.Errorf("failed to create unit '%s', API response was not successful. Status: %s. Body: %s", unitSlug, unitRes.Status(), string(unitRes.Body))
	}
	return createdUnit, nil
}

// createCRDsUnit creates a new unit representing the CRDs from a Helm chart.
func createCRDsUnit(ctx context.Context, client *goclientnew.ClientWithResponses, spaceIDStr string, crdYAMLContent string, releaseName string, chartName string, unitLabels map[string]string) (*goclientnew.Unit, error) {
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
	}

	createParams := goclientnew.CreateUnitParams{}

	unitRes, err := client.CreateUnitWithResponse(ctx, parsedSpaceID, &createParams, apiUnit)
	if IsAPIError(err, unitRes) {
		return nil, InterpretErrorGeneric(err, unitRes)
	}

	createdUnit := unitRes.JSON200
	if createdUnit == nil {
		return nil, fmt.Errorf("failed to create CRDs unit '%s', API response was not successful. Status: %s. Body: %s", unitSlug, unitRes.Status(), string(unitRes.Body))
	}
	return createdUnit, nil
}

// createBaseUnit creates a new unit representing the regular resources from a Helm chart.
func createBaseUnit(ctx context.Context, client *goclientnew.ClientWithResponses, spaceIDStr string, resourceYAMLContent string, releaseName string, chartName string, unitLabels map[string]string) (*goclientnew.Unit, error) {
	unitSlug := releaseName + "-base"
	toolchainType := "Kubernetes/YAML"

	parsedSpaceID, err := uuid.Parse(spaceIDStr)
	if err != nil {
		return nil, fmt.Errorf("internal error: selected space ID '%s' is not a valid UUID: %w", spaceIDStr, err)
	}

	// Add abstract label to base unit
	baseLabels := make(map[string]string)
	for k, v := range unitLabels {
		baseLabels[k] = v
	}
	baseLabels[AbstractLabel] = "true"

	apiUnit := goclientnew.Unit{
		SpaceID:       parsedSpaceID,
		Slug:          unitSlug,
		ToolchainType: toolchainType,
		Data:          base64.StdEncoding.EncodeToString([]byte(resourceYAMLContent)),
		Labels:        baseLabels,
	}

	createParams := goclientnew.CreateUnitParams{}

	unitRes, err := client.CreateUnitWithResponse(ctx, parsedSpaceID, &createParams, apiUnit)
	if IsAPIError(err, unitRes) {
		return nil, InterpretErrorGeneric(err, unitRes)
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

	linkRes, linkErr := client.CreateLinkWithResponse(ctx, spaceID, linkToCreate)

	if IsAPIError(linkErr, linkRes) {
		return InterpretErrorGeneric(linkErr, linkRes)
	}

	if linkRes.JSON200 != nil {
		linkDetails := linkRes.JSON200
		displayCreateResults(linkDetails, "link", linkDetails.Slug, linkDetails.LinkID.String(), displayLinkDetails)
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

	// use placeholder to render chart by default
	replaceMeNamespace := "confighubplaceholder"
	// if we don't want to use placeholder, set it to namespace at the render time
	if !helmInstallArgs.usePlaceholder {
		replaceMeNamespace = helmInstallArgs.namespace
	}

	chartName := helmInstallArgs.chartName
	if strings.Contains(chartName, "/") {
		parts := strings.Split(chartName, "/")
		chartName = parts[len(parts)-1]
	}
	unitLabels := map[string]string{
		HelmChartLabel:   chartName,
		HelmReleaseLabel: helmInstallArgs.releaseName,
		// TODO "helmChartVersion": helmInstallArgs.version,
	}

	// TODO: helmInstallArgs.namespace will be used for creating <release>-ns object

	// Initialize Helm SDK objects
	settings := cli.New()
	actionConfig := new(action.Configuration)
	// You might need to provide a logger to actionConfig, e.g., using Genericclioptions.NewConfigFlags
	// For simplicity here, we'll proceed without deep K8s client config if only templating.
	if err := actionConfig.Init(nil /*settings.RESTClientGetter()*/, replaceMeNamespace, os.Getenv("HELM_DRIVER"), func(format string, v ...interface{}) {
		// Simple logger: prints to stdout. Replace with a more sophisticated logger if needed.
		fmt.Printf(format+"\n", v...)
	}); err != nil {
		return fmt.Errorf("failed to initialize Helm action configuration: %w", err)
	}

	// Set up chart path options
	chartPathOptions := action.ChartPathOptions{
		Version: helmInstallArgs.version,
		RepoURL: helmInstallArgs.repo, // Used if chartNameOrPath is not 'repo/chart' and repo needs to be specified.
	}

	// Locate the chart (handles local paths, URLs, and repository charts)
	cp, err := chartPathOptions.LocateChart(helmInstallArgs.chartName, settings)
	if err != nil {
		return fmt.Errorf("failed to locate chart %s (version: %s, repo: %s): %w", helmInstallArgs.chartName, helmInstallArgs.version, helmInstallArgs.repo, err)
	}

	// 1. Load the chart.
	chrt, err := loader.Load(cp) // Use the path returned by LocateChart
	if err != nil {
		return fmt.Errorf("failed to load chart from %s: %w", cp, err)
	}

	// 2. Collect values.
	userSuppliedValues := map[string]interface{}{}

	// From --values files (later files override earlier ones)
	for _, filePath := range helmInstallArgs.valuesFiles {
		currentFileValues := map[string]interface{}{}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("cannot read values file %s: %w", filePath, err)
		}
		if err := yaml.Unmarshal(data, &currentFileValues); err != nil {
			return fmt.Errorf("cannot parse values file %s: %w", filePath, err)
		}
		userSuppliedValues = chartutil.CoalesceTables(userSuppliedValues, currentFileValues)
	}

	// Removed forced installCRDs=true to allow user control over CRD installation
	// Users can now set installCRDs=false or use --skip-crds flag as needed
	// also make sure to set the chart's values to crds.create=true
	// helmInstallArgs.set = append(helmInstallArgs.set, "crds.create=true")
	// some charts may have a different key for CRDs
	// helmInstallArgs.set = append(helmInstallArgs.set, "crds.enabled=true")

	// From --set flags (these override file values)
	for _, val := range helmInstallArgs.set {
		if err := strvals.ParseInto(val, userSuppliedValues); err != nil {
			return fmt.Errorf("failed to parse --set value %q: %w", val, err)
		}
	}

	// 3. Build render-time values.
	releaseOptions := chartutil.ReleaseOptions{
		Name:      helmInstallArgs.releaseName,
		Namespace: replaceMeNamespace,
		Revision:  1,
		IsInstall: true, // Simulates helm template / fresh install scenario
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
	if !helmInstallArgs.skipCRDs {
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
	} else if helmInstallArgs.skipCRDs && chrt.CRDObjects() != nil && len(chrt.CRDObjects()) > 0 {
		tprint("Skipping %d CRDs from %s/crds/ directory due to --skip-crds flag", len(chrt.CRDObjects()), chrt.Name())
	}

	// Create the unit for the namespace if specified
	var nsUnit *goclientnew.Unit
	if helmInstallArgs.namespace != "" && helmInstallArgs.namespace != "default" {
		nsUnit, err = createNamespaceUnit(ctx, cubClientNew, selectedSpaceID, helmInstallArgs.namespace, helmInstallArgs.releaseName, unitLabels)
		if err != nil {
			return fmt.Errorf("failed to create namespace unit: %w", err)
		}
		if wait {
			if err := awaitTriggersRemoval(nsUnit); err != nil {
				return err
			}
		}
		displayCreateResults(nsUnit, "unit", nsUnit.Slug, nsUnit.UnitID.String(), displayUnitDetails)
	}

	// Create a unit for CRDs if any were found
	var crdUnit *goclientnew.Unit
	if len(splitResult.CRDs) > 0 {
		createdCRDsUnit, err := createCRDsUnit(ctx, cubClientNew, selectedSpaceID, splitResult.CRDs, helmInstallArgs.releaseName, helmInstallArgs.chartName, unitLabels)
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
	if len(splitResult.Resources) > 0 {
		createdResourceUnit, err := createBaseUnit(ctx, cubClientNew, selectedSpaceID, splitResult.Resources, helmInstallArgs.releaseName, helmInstallArgs.chartName, unitLabels)
		if err != nil {
			return fmt.Errorf("failed to create resources unit: %w", err)
		}
		if wait {
			if err := awaitTriggersRemoval(createdResourceUnit); err != nil {
				return fmt.Errorf("failed to wait for base unit triggers: %w", err)
			}
		}
		displayCreateResults(createdResourceUnit, "unit", createdResourceUnit.Slug, createdResourceUnit.UnitID.String(), displayUnitDetails)

		// Clone the createdResourceUnit
		if !helmInstallArgs.clone {
			tprint("Skipping cloning: clone flag is not set.")
		} else if createdResourceUnit == nil {
			// This implies helmInstallArgs.clone is true.
			tprint("Skipping cloning: source resource unit is nil (it might not have been created if no regular resources were found or due to an error during its creation).")
		} else if createdResourceUnit.UnitID == uuid.Nil {
			// This implies helmInstallArgs.clone is true AND createdResourceUnit is not nil.
			tprint("Skipping cloning: source resource unit '%s' has a nil/invalid ID (%s).", createdResourceUnit.Slug, createdResourceUnit.UnitID.String())
		} else {
			// All prerequisites met: helmInstallArgs.clone is true, createdResourceUnit is not nil, and createdResourceUnit.UnitID is not nil.
			clonedUnitSlug := helmInstallArgs.releaseName

			spaceID, parseErr := uuid.Parse(selectedSpaceID)
			if parseErr != nil {
				return fmt.Errorf("failed to parse space ID '%s' for cloning: %w", selectedSpaceID, parseErr)
			}
			clonedUnitToCreate := goclientnew.Unit{
				SpaceID:       spaceID, // This is the spaceID parsed for cloning operations
				Slug:          makeSlug(clonedUnitSlug),
				ToolchainType: createdResourceUnit.ToolchainType,
				Labels:        unitLabels,
			}

			upstreamUnitID := createdResourceUnit.UnitID
			upstreamSpaceID := spaceID // Cloning into the same space

			cloningParams := goclientnew.CreateUnitParams{
				UpstreamUnitId:  &upstreamUnitID,
				UpstreamSpaceId: &upstreamSpaceID,
			}

			clonedUnitRes, cloneErr := cubClientNew.CreateUnitWithResponse(ctx, spaceID, &cloningParams, clonedUnitToCreate)

			if IsAPIError(cloneErr, clonedUnitRes) {
				return fmt.Errorf("failed to clone unit '%s': %w", createdResourceUnit.Slug, InterpretErrorGeneric(cloneErr, clonedUnitRes))
			}
			if clonedUnitRes.JSON200 != nil {
				clonedUnitDetails := clonedUnitRes.JSON200
				if wait { // global wait flag
					if err := awaitTriggersRemoval(clonedUnitDetails); err != nil {
						return fmt.Errorf("failed to wait for triggers on cloned unit '%s': %w", clonedUnitDetails.Slug, err)
					}
				}
				displayCreateResults(clonedUnitDetails, "unit", clonedUnitDetails.Slug, clonedUnitDetails.UnitID.String(), displayUnitDetails)

				// Link the cloned unit to the namespace unit
				if err := createUnitLink(ctx, cubClientNew, clonedUnitDetails, nsUnit, linkSuffixNamespace, spaceID); err != nil {
					return err
				}

				// TODO call set-namespace or making sure the value is propagated from the namespace object correctly

				// Link the cloned unit to the CRDs unit
				if err := createUnitLink(ctx, cubClientNew, clonedUnitDetails, crdUnit, linkSuffixCRDs, spaceID); err != nil {
					return err
				}

			} else {
				// Handle unexpected response
				if clonedUnitRes == nil {
					return fmt.Errorf("failed to clone unit '%s': no response received", createdResourceUnit.Slug)
				}
				return fmt.Errorf("failed to clone unit '%s': unexpected response status %s", createdResourceUnit.Slug, clonedUnitRes.Status())
			}
		}
	} else {
		if !quiet {
			tprint("No regular resources found in chart '%s', skipping creation of resources unit.", helmInstallArgs.chartName)
		}
	}

	return nil
}
