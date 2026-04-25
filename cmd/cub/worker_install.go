// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

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
- OpenTofu/AWS (experimental)

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
	hostNetwork         bool
	deploymentName      string
	functionsFile       string
	exportSecretOnly    bool
	image               string
	imagePullPolicy     string
	updateStrategy      string
	serviceAccount      string
}

const defaultServiceAcccount = "confighub-worker"

func init() {
	workerInstallCmd.Flags().StringVarP(&workerInstallArgs.workerProviderTypes, "provider-types", "t", "", "Comma-separated list of provider types")
	workerInstallCmd.Flags().StringVarP(&workerInstallArgs.workerFunctions, "worker-functions", "f", "", "Comma-separated list of worker function names (e.g., vet-kyverno-server)")
	workerInstallCmd.Flags().StringSliceVarP(&workerInstallArgs.envs, "env", "e", []string{}, "environment variables")
	workerInstallCmd.Flags().BoolVar(&workerInstallArgs.export, "export", false, "export manifest to stdout instead of applying it")
	workerInstallCmd.Flags().BoolVar(&workerInstallArgs.includeSecret, "include-secret", false, "include Secret resource in manifest")
	workerInstallCmd.Flags().StringVarP(&workerInstallArgs.namespace, "namespace", "n", "confighub", "namespace to install worker in")
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.unitSlug, "unit", "", "create a unit in ConfigHub with the generated manifest")
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.targetSlug, "target", "", "target for the unit")
	workerInstallCmd.Flags().BoolVar(&workerInstallArgs.hostNetwork, "host-network", false, "use host networking for the worker pod")
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.deploymentName, "deployment-name", "", "custom name for the Deployment and labels (defaults to worker slug)")
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.functionsFile, "functions", "", "file containing functions to execute on the created unit")
	workerInstallCmd.Flags().BoolVar(&workerInstallArgs.exportSecretOnly, "export-secret-only", false, "export only the Secret resource to stdout")
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.image, "image", "", "Container image for the worker. Defaults to ghcr.io/confighubai/confighub-worker at the most recent tagged release.")
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.imagePullPolicy, "image-pull-policy", "Always", "Image pull policy (Always, IfNotPresent, Never)")
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.updateStrategy, "update-strategy", "RollingUpdate", "Deployment update strategy (RollingUpdate, Recreate)")
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.serviceAccount, "service-account", defaultServiceAcccount, "Service account name")
	enableWaitFlag(workerInstallCmd)

	workerCmd.AddCommand(workerInstallCmd)
}

func workerInstallCmdRun(cmd *cobra.Command, args []string) error {
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
	manifest, err := generateKubernetesManifest(worker, workerInstallArgs.includeSecret, workerInstallArgs.namespace, workerInstallArgs.hostNetwork, deploymentName, workerInstallArgs.image, workerInstallArgs.imagePullPolicy, workerInstallArgs.updateStrategy)
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
		return nil
	}

	// TODO: Bootstrap the worker in the Kubernetes cluster using the Kubernetes bridge implementation
	// For now, we'll just print a message
	fmt.Println("Use --export to display the configuration or --unit to create a unit.")

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

func getWorkerImage(image string) string {
	if image == "" {
		image = "ghcr.io/confighubai/confighub-worker" + getWorkerReference()
	}
	return image
}

func generateKubernetesManifest(worker *goclientnew.BridgeWorker, includeSecret bool, namespace string, hostNetwork bool, deploymentName string, image string, imagePullPolicy string, updateStrategy string) (string, error) {
	// Define the Kubernetes resources
	namespaceResource := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]interface{}{
			"name": namespace,
		},
	}

	serviceAccount := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ServiceAccount",
		"metadata": map[string]interface{}{
			"name":      workerInstallArgs.serviceAccount,
			"namespace": namespace,
		},
	}

	clusterRoleBinding := map[string]interface{}{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRoleBinding",
		"metadata": map[string]interface{}{
			"name": fmt.Sprintf("%s-%s-admin", namespace, workerInstallArgs.serviceAccount),
		},
		"roleRef": map[string]interface{}{
			"apiGroup": "rbac.authorization.k8s.io",
			"kind":     "ClusterRole",
			"name":     "cluster-admin",
		},
		"subjects": []map[string]interface{}{
			{
				"kind":      "ServiceAccount",
				"name":      workerInstallArgs.serviceAccount,
				"namespace": namespace,
			},
		},
	}

	activeContext := contextManager.ActiveContext()
	serverURL := activeContext.Coordinate.ServerURL

	// Create a hashmap of environment variables first to handle overrides
	envMap := map[string]string{
		"CONFIGHUB_URL":         serverURL,
		"CONFIGHUB_WORKER_PORT": os.Getenv("CONFIGHUB_WORKER_PORT"),
	}

	if workerInstallArgs.workerProviderTypes != "" {
		envMap["CONFIGHUB_WORKER_PROVIDER_TYPES"] = workerInstallArgs.workerProviderTypes
	}
	if workerInstallArgs.workerFunctions != "" {
		envMap["CONFIGHUB_WORKER_FUNCTIONS"] = workerInstallArgs.workerFunctions
	}

	// Add additional environment variables from command line arguments
	// These will override any existing values with the same name
	for _, env := range workerInstallArgs.envs {
		parts := strings.Split(env, "=")
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	// Convert the hashmap to the required format for container env vars
	containerEnvs := []map[string]interface{}{}
	for name, value := range envMap {
		containerEnvs = append(containerEnvs, map[string]interface{}{
			"name":  name,
			"value": value,
		})
	}

	// Pass the Kubernetes Namespace for workers that need it
	containerEnvs = append(containerEnvs, map[string]interface{}{
		"name": "NAMESPACE",
		"valueFrom": map[string]interface{}{
			"fieldRef": map[string]interface{}{
				"fieldPath": "metadata.namespace",
			},
		},
	})

	// Create Secret resource if includeSecret is true
	var secret map[string]interface{}
	if includeSecret {
		secret = createWorkerSecret(worker, namespace)
	}

	// Create pod spec
	podSpec := map[string]interface{}{
		"serviceAccountName":            workerInstallArgs.serviceAccount,
		"terminationGracePeriodSeconds": 60,
		"containers": []map[string]interface{}{
			{
				"name":            "worker",
				"image":           getWorkerImage(image),
				"imagePullPolicy": imagePullPolicy,
				"env":             containerEnvs,
				"envFrom": []map[string]interface{}{
					{
						"secretRef": map[string]interface{}{
							"name": "confighub-worker-secret",
						},
					},
				},
				"volumeMounts": []map[string]interface{}{
					{
						"name":      "tmp",
						"mountPath": "/tmp",
					},
				},
			},
		},
		"volumes": []map[string]interface{}{
			{
				"name":     "tmp",
				"emptyDir": map[string]interface{}{},
			},
		},
	}

	// Add hostNetwork if requested
	if hostNetwork {
		podSpec["hostNetwork"] = true
	}

	// Build strategy based on type
	strategy := map[string]interface{}{
		"type": updateStrategy,
	}

	// When strategy type is Recreate, explicitly set rollingUpdate to null
	// to ensure any existing rollingUpdate fields are removed
	if updateStrategy == "Recreate" {
		strategy["rollingUpdate"] = nil
	} else if updateStrategy == "RollingUpdate" {
		// For RollingUpdate, set maxSurge: 1 and maxUnavailable: 0
		strategy["rollingUpdate"] = map[string]interface{}{
			"maxSurge":       1,
			"maxUnavailable": 0,
		}
	}

	deploymentSpec := map[string]interface{}{
		"replicas":        1,
		"minReadySeconds": 10,
		"selector": map[string]interface{}{
			"matchLabels": map[string]interface{}{
				"app": deploymentName,
			},
		},
		"strategy": strategy,
		"template": map[string]interface{}{
			"metadata": map[string]interface{}{
				"labels": map[string]interface{}{
					"app": deploymentName,
				},
			},
			"spec": podSpec,
		},
	}

	deployment := map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name":      deploymentName,
			"namespace": namespace,
		},
		"spec": deploymentSpec,
	}

	// Convert to YAML
	resources := []map[string]interface{}{namespaceResource, serviceAccount, clusterRoleBinding}
	if includeSecret {
		resources = append(resources, secret)
	}
	resources = append(resources, deployment)
	var manifests []string

	for _, resource := range resources {
		yamlBytes, err := yaml.Marshal(resource)
		if err != nil {
			return "", err
		}
		manifests = append(manifests, string(yamlBytes))
	}

	docList := strings.Join(manifests, "---\n")

	// Set security context
	response, err := invokeLocalFunction([]byte(docList), "set-pod-defaults", []string{"--security-context=true"}, string(workerapi.ToolchainKubernetesYAML))
	if err != nil {
		return docList, err
	}

	return string(response.ConfigData), nil
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
