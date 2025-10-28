// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/confighub/sdk/workerapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var workerInstallCmd = &cobra.Command{
	Use:           "install [worker-name]",
	Short:         "Install a worker to a Kubernetes cluster",
	Long:          getCommandHelp(`Install a worker to a Kubernetes cluster.`, ""),
	Args:          cobra.ExactArgs(1),
	RunE:          workerInstallCmdRun,
	SilenceUsage:  true,
	SilenceErrors: true,
}

var workerInstallArgs struct {
	workerType       string
	envs             []string
	export           bool
	includeSecret    bool
	namespace        string
	unitSlug         string
	targetSlug       string
	hostNetwork      bool
	deploymentName   string
	functionsFile    string
	exportSecretOnly bool
	image            string
	imagePullPolicy  string
	updateStrategy   string
}

func init() {
	workerInstallCmd.Flags().StringVarP(&workerInstallArgs.workerType, "worker-type", "t", "kubernetes", "worker type")
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
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.image, "image", "ghcr.io/confighubai/confighub-worker:latest", "Docker image for the worker")
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.imagePullPolicy, "image-pull-policy", "Always", "Image pull policy (Always, IfNotPresent, Never)")
	workerInstallCmd.Flags().StringVar(&workerInstallArgs.updateStrategy, "update-strategy", "Recreate", "Deployment update strategy (RollingUpdate, Recreate)")
	enableWaitFlag(workerInstallCmd)

	workerCmd.AddCommand(workerInstallCmd)
}

func workerInstallCmdRun(cmd *cobra.Command, args []string) error {
	workerSlug := args[0]
	worker, err := apiGetBridgeWorkerFromSlug(workerSlug, "*") // get all fields for now
	if err != nil {
		return err
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
		unitDetails, err := createUnitWithManifest(workerInstallArgs.unitSlug, workerInstallArgs.targetSlug, manifest)
		if err != nil {
			return err
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
			_, err = executeFunctionsFromFile(workerInstallArgs.functionsFile, whereClause, []string{})
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

	// TODO: Apply manifest to Kubernetes cluster
	// This would use the kubernetes client-go to apply the manifest
	// For now, we'll just print a message
	fmt.Printf("Installing worker %s to Kubernetes cluster...\n", workerSlug)
	fmt.Println("This functionality is not yet implemented. Use --export to get the manifest.")

	return nil
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
			"name":      "confighub-worker",
			"namespace": namespace,
		},
	}

	clusterRoleBinding := map[string]interface{}{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRoleBinding",
		"metadata": map[string]interface{}{
			"name": "confighub-worker-admin",
		},
		"roleRef": map[string]interface{}{
			"apiGroup": "rbac.authorization.k8s.io",
			"kind":     "ClusterRole",
			"name":     "cluster-admin",
		},
		"subjects": []map[string]interface{}{
			{
				"kind":      "ServiceAccount",
				"name":      "confighub-worker",
				"namespace": namespace,
			},
		},
	}

	activeContext := contextManager.ActiveContext()
	serverURL := activeContext.Coordinate.ServerURL

	// Create a hashmap of environment variables first to handle overrides
	envMap := map[string]string{
		"CONFIGHUB_WORKER_ID":   worker.BridgeWorkerID.String(),
		"CONFIGHUB_URL":         serverURL,
		"CONFIGHUB_WORKER_PORT": os.Getenv("CONFIGHUB_WORKER_PORT"),
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

	// Create Secret resource if includeSecret is true
	var secret map[string]interface{}
	if includeSecret {
		secret = createWorkerSecret(worker, namespace)
	}

	// Create pod spec
	podSpec := map[string]interface{}{
		"serviceAccountName":            "confighub-worker",
		"terminationGracePeriodSeconds": 60,
		"containers": []map[string]interface{}{
			{
				"name":            "worker",
				"image":           image,
				"imagePullPolicy": imagePullPolicy,
				"args":            []string{workerInstallArgs.workerType},
				"env":             containerEnvs,
				"envFrom": []map[string]interface{}{
					{
						"secretRef": map[string]interface{}{
							"name": "confighub-worker-env",
						},
					},
				},
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

	return strings.Join(manifests, "---\n"), nil
}

func createUnitWithManifest(unitSlug, targetSlug, manifest string) (*goclientnew.Unit, error) {
	spaceID := uuid.MustParse(selectedSpaceID)

	// Create new unit
	newUnit := &goclientnew.Unit{
		SpaceID:       spaceID,
		Slug:          makeSlug(unitSlug),
		DisplayName:   unitSlug,
		ToolchainType: string(workerapi.ToolchainKubernetesYAML),
		Data:          base64.StdEncoding.EncodeToString([]byte(manifest)),
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
			"name":      "confighub-worker-env",
			"namespace": namespace,
		},
		"type": "Opaque",
		"stringData": map[string]interface{}{
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
