// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// workerInstallManifest holds the four base resources (Namespace, ServiceAccount,
// ClusterRoleBinding, Deployment) with placeholder values that generateKubernetesManifest
// then mutates with ConfigHub functions to inject the per-install configuration.
//
//go:embed worker_install_manifest.yaml
var workerInstallManifest []byte

var workerInstallCmd = &cobra.Command{
	Use:   "install [worker-name]",
	Short: "Generate a worker configuration for a Kubernetes cluster",
	Long: getCommandHelp(`Generate a worker configuration to serve one or more provider types for a Kubernetes cluster.

Each ProviderType corresponds to one or more ToolchainTypes.
For example, the "Kubernetes" provider type corresponds to the Kubernetes/YAML ToolchainType

Some ToolchainTypes are supported by multiple ProviderTypes, and some ProviderTypes support
multiple ToolchainTypes.

The available ProviderTypes are:

- ConfigHub
- Kubernetes
- FluxRenderer
- FluxOCI
- ArgoCDRenderer
- ArgoCDOCI
- ConfigMapRenderer

Here the provider types are case-insensitive and they can be comma-separated, like "kubernetes,configmap".

Use --export to display the configuration. Use --unit to create a unit in ConfigHub for the configuration.

The Secret resource is redacted by default. Use --include-secret to include it with the rest
of the configuration or --export-secret-only to display only the Secret resource.

See the worker guide (https://docs.confighub.com/guide/workers/) for more details.
	`, ""),
	Args:          cobra.ExactArgs(1),
	RunE:          workerInstallCmdRun,
	SilenceUsage:  true,
	SilenceErrors: true,
}

var workerInstallArgs struct {
	workerProviderTypes string
	workerFunctions     string
	envs                []string
	export              bool
	includeSecret       bool
	namespace           string
	unitSlug            string
	targetSlug          string
	deploymentName      string
	functionsFile       string
	exportSecretOnly    bool
	image               string
	imagePullPolicy     string
	httpPort            int
}

const defaultServiceAcccount = "confighub-worker"

// defaultStandardWorkerHTTPPort is the port the standard worker image
// (cub-worker-run) exposes for /internal/metrics + probes when --http-port
// is not explicitly set. Matches what test/scripts and historical worker
// deployments have used.
const defaultStandardWorkerHTTPPort = 9092

func init() {
	workerInstallCmd.Flags().StringVarP(&workerInstallArgs.workerProviderTypes, "provider-types", "t", "", "Comma-separated list of provider types")
	workerInstallCmd.Flags().StringVarP(&workerInstallArgs.workerFunctions, "worker-functions", "f", "", "Comma-separated list of worker function names (e.g., vet-kyverno-server)")
	workerInstallCmd.Flags().StringSliceVarP(&workerInstallArgs.envs, "env", "e", []string{}, "environment variables")
	workerInstallCmd.Flags().BoolVar(&workerInstallArgs.export, "export", false, "export manifest to stdout instead of applying it")
	workerInstallCmd.Flags().BoolVar(&workerInstallArgs.includeSecret, "include-secret", false, "include Secret resource in manifest")
	workerInstallCmd.Flags().StringVarP(&workerInstallArgs.namespace, "namespace", "n", "confighub", "namespace to install worker in")
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.unitSlug, "unit", "", "create a unit in ConfigHub with the generated manifest")
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.targetSlug, "target", "", "target for the unit")
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.deploymentName, "deployment-name", "", "custom name for the Deployment and labels (defaults to worker slug)")
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.functionsFile, "functions", "", "file containing functions to execute on the created unit")
	workerInstallCmd.Flags().BoolVar(&workerInstallArgs.exportSecretOnly, "export-secret-only", false, "export only the Secret resource to stdout")
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.image, "image", "", "Container image for the worker. Defaults to ghcr.io/confighubai/confighub-worker at the most recent tagged release.")
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.imagePullPolicy, "image-pull-policy", "Always", "Image pull policy (Always, IfNotPresent, Never)")
	workerInstallCmd.Flags().IntVar(&workerInstallArgs.httpPort, "http-port", 0, "Container HTTP port to expose, with liveness/readiness probes targeting it. The standard worker image always exposes a port (defaulting to 9092); custom worker images expose a port only when this flag is set.")
	enableWaitFlag(workerInstallCmd)

	workerCmd.AddCommand(workerInstallCmd)
}

func workerInstallCmdRun(cmd *cobra.Command, args []string) error {
	if !workerInstallArgs.export && !workerInstallArgs.exportSecretOnly && workerInstallArgs.unitSlug == "" {
		return fmt.Errorf("one of --export, --export-secret-only, or --unit must be specified")
	}
	switch workerInstallArgs.imagePullPolicy {
	case "Always", "IfNotPresent", "Never":
	default:
		return fmt.Errorf("invalid --image-pull-policy %q: must be Always, IfNotPresent, or Never", workerInstallArgs.imagePullPolicy)
	}

	workerSlug := args[0]
	spaceID := uuid.MustParse(selectedSpaceID)
	worker, err := apiGetBridgeWorkerFromSlug(workerSlug, "*") // get all fields for now
	if err != nil {
		// Worker not found, create it on the fly
		worker, err = apiCreateWorker(&goclientnew.BridgeWorker{
			SpaceID: spaceID,
			Slug:    workerSlug,
		}, spaceID)
		if err != nil {
			return err
		}
	}

	// Handle export-secret-only flag first
	if workerInstallArgs.exportSecretOnly {
		secretManifest, err := generateSecretManifest(worker, workerInstallArgs.namespace)
		if err != nil {
			return err
		}
		fmt.Println(secretManifest)
		return nil
	}

	// Determine deployment name - use custom name if provided, otherwise use worker slug
	deploymentName := workerInstallArgs.deploymentName
	if deploymentName == "" {
		deploymentName = worker.Slug
	}

	// Generate Kubernetes manifest
	manifest, err := generateKubernetesManifest(worker, workerInstallArgs.includeSecret, workerInstallArgs.namespace, deploymentName, workerInstallArgs.image, workerInstallArgs.imagePullPolicy)
	if err != nil {
		return err
	}

	if workerInstallArgs.export {
		// Print manifest to stdout
		fmt.Println(manifest)
		return nil
	}

	// Create unit in ConfigHub if --unit flag is provided
	if workerInstallArgs.unitSlug != "" {
		unitDetails, err := createUnitWithManifest(worker, workerInstallArgs.unitSlug, workerInstallArgs.targetSlug, manifest)
		if err != nil {
			return err
		}

		workerPatchJSON, err := json.Marshal(map[string]any{
			"Annotations": map[string]string{
				"UnitID": unitDetails.UnitID.String(),
			},
		})
		if err != nil {
			return err
		}
		workerPatchRes, err := cubClientNew.PatchBridgeWorkerWithBodyWithResponse(ctx, spaceID, worker.BridgeWorkerID, "application/merge-patch+json", bytes.NewReader(workerPatchJSON))
		if cubapi.IsAPIError(err, workerPatchRes) {
			return cubapi.InterpretErrorGeneric(err, workerPatchRes)
		}

		// Wait for triggers after unit creation
		if wait {
			err = awaitTriggersRemoval(unitDetails)
			if err != nil {
				return err
			}
		}

		// Execute functions if functions file is specified
		if workerInstallArgs.functionsFile != "" {
			whereClause := "Slug='" + workerInstallArgs.unitSlug + "'"
			_, _, err = executeFunctionsFromFile(workerInstallArgs.functionsFile, whereClause, []string{})
			if err != nil {
				return err
			}

			// Wait for triggers after function execution
			if wait {
				// Get updated unit details after function execution
				unitDetails, err = apiGetUnitInSpace(unitDetails.UnitID.String(), unitDetails.SpaceID.String(), "*") // get all fields for now
				if err != nil {
					return err
				}
				err = awaitTriggersRemoval(unitDetails)
				if err != nil {
					return err
				}
			}
		}

		// Display results after all operations are complete
		displayCreateResults(unitDetails, "unit", workerInstallArgs.unitSlug, unitDetails.UnitID.String(), displayUnitDetails)
	}

	return nil
}

// getWorkerReference returns a container image tag (e.g. ":v0.1.12") derived
// from the server's API info. It prefers the Version field when it looks like a
// semver tag and Build is a commit hash; otherwise it falls back to "latest".
func getWorkerReference() string {
	apiInfo := GetApiInfo()
	// Use the version tag if it matches a semver pattern and the build looks like a commit hash
	if regexp.MustCompile(`^v\d+\.\d+\.\d+`).MatchString(apiInfo.Version) &&
		regexp.MustCompile(`^[0-9a-f]{7,}$`).MatchString(apiInfo.Build) {
		return ":" + apiInfo.Version
	}
	return ":latest"
}

const standardWorkerImage = "ghcr.io/confighubai/confighub-worker"

func getWorkerImage(image string) string {
	if image == "" {
		image = standardWorkerImage + getWorkerReference()
	}
	return image
}

// isStandardWorkerImage reports whether image refers to the cub-worker-run
// image we ship (any tag/digest of standardWorkerImage). An empty image
// means "use the default", which is also the standard image.
func isStandardWorkerImage(image string) bool {
	return image == "" ||
		image == standardWorkerImage ||
		strings.HasPrefix(image, standardWorkerImage+":") ||
		strings.HasPrefix(image, standardWorkerImage+"@")
}

// effectiveHTTPPort returns the container HTTP port to expose, or 0 if no
// port should be exposed. The standard worker image always exposes a port
// (defaulting to 9092); custom images expose one only when --http-port is
// explicitly given.
func effectiveHTTPPort(image string, httpPort int) int {
	if httpPort > 0 {
		return httpPort
	}
	if isStandardWorkerImage(image) {
		return defaultStandardWorkerHTTPPort
	}
	return 0
}

// httpPortInvocations returns the function invocations that expose port,
// add HTTP probes targeting it, and force liveness/readiness periodSeconds
// to 60. Standard worker probes target /internal/ok and /internal/ready;
// custom workers fall back to set-container-probe-defaults' defaults
// (/healthz). The 60s period reduces probe traffic to the worker, whose
// /internal/ok freshness threshold is 45s by default and tolerates the
// slower cadence.
func httpPortInvocations(port int, standardWorker bool) []localFunctionInvocation {
	portStr := strconv.Itoa(port)
	probeArgs := []string(nil)
	if standardWorker {
		probeArgs = []string{
			"--liveness-path=/internal/ok",
			"--readiness-path=/internal/ready",
			"--startup-path=/internal/ok",
		}
	}
	deploymentRT := "apps/v1/Deployment"
	containerSel := "spec.template.spec.containers.?name=worker"
	return []localFunctionInvocation{
		{FunctionName: "set-container-port", Arguments: []string{"worker", "http", portStr}},
		{FunctionName: "set-container-probe-defaults", Arguments: probeArgs},
		{FunctionName: "set-int-path", Arguments: []string{deploymentRT, containerSel + ".livenessProbe.periodSeconds", "60"}},
		{FunctionName: "set-int-path", Arguments: []string{deploymentRT, containerSel + ".readinessProbe.periodSeconds", "60"}},
	}
}

func generateKubernetesManifest(worker *goclientnew.BridgeWorker, includeSecret bool, namespace string, deploymentName string, image string, imagePullPolicy string) (string, error) {
	toolchain := string(workerapi.ToolchainKubernetesYAML)

	// Each set-name call needs a different metadata.name value, so they go in
	// their own invocation requests scoped via WhereResource.
	manifest, err := invokeLocalFunctions(workerInstallManifest, []localFunctionInvocation{
		{FunctionName: "set-name", Arguments: []string{namespace}},
	}, "ConfigHub.ResourceType = 'v1/Namespace'", toolchain)
	if err != nil {
		return string(workerInstallManifest), err
	}
	manifest, err = invokeLocalFunctions(manifest.ConfigData, []localFunctionInvocation{
		{FunctionName: "set-name", Arguments: []string{fmt.Sprintf("%s-%s-admin", namespace, defaultServiceAcccount)}},
	}, "ConfigHub.ResourceType = 'rbac.authorization.k8s.io/v1/ClusterRoleBinding'", toolchain)
	if err != nil {
		return string(manifest.ConfigData), err
	}

	httpPort := effectiveHTTPPort(image, workerInstallArgs.httpPort)

	// Build the dynamic env vars for the worker container.
	envMap := map[string]string{
		"CONFIGHUB_URL":         contextManager.ActiveContext().Coordinate.ServerURL,
		"CONFIGHUB_WORKER_PORT": os.Getenv("CONFIGHUB_WORKER_PORT"),
	}
	if workerInstallArgs.workerProviderTypes != "" {
		envMap["CONFIGHUB_WORKER_PROVIDER_TYPES"] = workerInstallArgs.workerProviderTypes
	}
	if workerInstallArgs.workerFunctions != "" {
		envMap["CONFIGHUB_WORKER_FUNCTIONS"] = workerInstallArgs.workerFunctions
	}
	// The standard worker only starts its HTTP server when this env var is
	// set; custom worker images use whatever env var their own code reads,
	// which the user can supply via --env.
	if httpPort > 0 && isStandardWorkerImage(image) {
		envMap["CONFIGHUB_WORKER_HTTP_SERVER_PORT"] = strconv.Itoa(httpPort)
	}
	for _, env := range workerInstallArgs.envs {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	setEnvArgs := []string{"worker"}
	for name, value := range envMap {
		if value == "" {
			continue
		}
		setEnvArgs = append(setEnvArgs, name+"="+value)
	}

	invocations := []localFunctionInvocation{
		{FunctionName: "set-namespace", Arguments: []string{namespace}},
		{FunctionName: "set-default-names", Arguments: []string{deploymentName}},
		{FunctionName: "set-container-image", Arguments: []string{"worker", getWorkerImage(image)}},
		{FunctionName: "set-string-path", Arguments: []string{"apps/v1/Deployment", "spec.template.spec.containers.?name=worker.imagePullPolicy", imagePullPolicy}},
		{FunctionName: "set-env", Arguments: setEnvArgs},
		{FunctionName: "set-pod-container-security-context-defaults"},
		// Resources can be set after generation
		// {FunctionName: "set-container-resources-defaults"},
		{FunctionName: "set-container-volume-mount-path", Arguments: []string{"worker", "tmp", "/tmp", "emptyDir"}},
		{FunctionName: "set-annotation", Arguments: []string{k8skit.ContextKeyPrefix + "BridgeWorkerID", worker.BridgeWorkerID.String()}},
	}
	if httpPort > 0 {
		invocations = append(invocations, httpPortInvocations(httpPort, isStandardWorkerImage(image))...)
	}
	manifest, err = invokeLocalFunctions(manifest.ConfigData, invocations, "", toolchain)
	if err != nil {
		return string(manifest.ConfigData), err
	}

	out := string(manifest.ConfigData)
	if includeSecret {
		secretYAML, err := yaml.Marshal(createWorkerSecret(worker, namespace))
		if err != nil {
			return out, err
		}
		out = strings.TrimRight(out, "\n") + "\n---\n" + string(secretYAML)
	}
	return out, nil
}

func createUnitWithManifest(worker *goclientnew.BridgeWorker, unitSlug, targetSlug, manifest string) (*goclientnew.Unit, error) {
	spaceID := uuid.MustParse(selectedSpaceID)

	// Create new unit
	newUnit := &goclientnew.Unit{
		SpaceID:       spaceID,
		Slug:          makeSlug(unitSlug),
		DisplayName:   unitSlug,
		ToolchainType: string(workerapi.ToolchainKubernetesYAML),
		Data:          base64.StdEncoding.EncodeToString([]byte(manifest)),
		Annotations: map[string]string{
			"BridgeWorkerID": worker.BridgeWorkerID.String(),
		},
	}

	// Set target if specified
	if targetSlug != "" {
		target, err := apiGetTargetFromSlug(targetSlug, selectedSpaceID, "*") // get all fields for now
		if err != nil {
			return nil, err
		}
		newUnit.TargetID = &target.Target.TargetID
	}

	// Create the unit
	newParams := &goclientnew.CreateUnitParams{}
	unitRes, err := cubClientNew.CreateUnitWithResponse(ctx, spaceID, newParams, *newUnit)
	if cubapi.IsAPIError(err, unitRes) {
		return nil, cubapi.InterpretErrorGeneric(err, unitRes)
	}

	return unitRes.JSON200, nil
}

func createWorkerSecret(worker *goclientnew.BridgeWorker, namespace string) map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]interface{}{
			"name":      "confighub-worker-secret",
			"namespace": namespace,
		},
		"type": "Opaque",
		"stringData": map[string]interface{}{
			"CONFIGHUB_WORKER_ID":     worker.BridgeWorkerID.String(),
			"CONFIGHUB_WORKER_SECRET": worker.Secret,
		},
	}
}

func generateSecretManifest(worker *goclientnew.BridgeWorker, namespace string) (string, error) {
	secret := createWorkerSecret(worker, namespace)
	yamlBytes, err := yaml.Marshal(secret)
	if err != nil {
		return "", err
	}
	return string(yamlBytes), nil
}
