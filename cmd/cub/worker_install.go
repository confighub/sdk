// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"strings"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var workerInstallCmd = &cobra.Command{
	Use:           "install [worker-name]",
	Short:         "Install a worker to a Kubernetes cluster",
	Long:          `Install a worker to a Kubernetes cluster.`,
	Args:          cobra.ExactArgs(1),
	RunE:          workerInstallCmdRun,
	SilenceUsage:  true,
	SilenceErrors: true,
}

var workerInstallArgs struct {
	workerType string
	envs       []string
	export     bool
}

func init() {
	workerInstallCmd.Flags().StringVarP(&workerInstallArgs.workerType, "worker-type", "t", "kubernetes", "worker type")
	workerInstallCmd.Flags().StringSliceVarP(&workerInstallArgs.envs, "env", "e", []string{}, "environment variables")
	workerInstallCmd.Flags().BoolVar(&workerInstallArgs.export, "export", false, "export manifest to stdout instead of applying it")

	workerCmd.AddCommand(workerInstallCmd)
}

func workerInstallCmdRun(cmd *cobra.Command, args []string) error {
	workerSlug := args[0]
	worker, err := apiGetBridgeWorkerFromSlug(workerSlug)
	if err != nil {
		return err
	}

	// Generate Kubernetes manifest
	manifest, err := generateKubernetesManifest(worker)
	if err != nil {
		return err
	}

	if workerInstallArgs.export {
		// Print manifest to stdout
		fmt.Println(manifest)
		return nil
	}

	// TODO: Apply manifest to Kubernetes cluster
	// This would use the kubernetes client-go to apply the manifest
	// For now, we'll just print a message
	fmt.Printf("Installing worker %s to Kubernetes cluster...\n", workerSlug)
	fmt.Println("This functionality is not yet implemented. Use --export to get the manifest.")

	return nil
}

func generateKubernetesManifest(worker *goclientnew.BridgeWorker) (string, error) {
	// Define the Kubernetes resources
	namespace := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]interface{}{
			"name": "confighub",
		},
	}

	serviceAccount := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ServiceAccount",
		"metadata": map[string]interface{}{
			"name":      "confighub-worker",
			"namespace": "confighub",
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
				"namespace": "confighub",
			},
		},
	}

	// Create a hashmap of environment variables first to handle overrides
	envMap := map[string]string{
		"CONFIGHUB_WORKER_ID":     worker.BridgeWorkerID.String(),
		"CONFIGHUB_WORKER_SECRET": worker.Secret,
		"CONFIGHUB_URL":           os.Getenv("CONFIGHUB_URL"),
		"CONFIGHUB_WORKER_PORT":   os.Getenv("CONFIGHUB_WORKER_PORT"),
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

	deployment := map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name":      worker.Slug,
			"namespace": "confighub",
		},
		"spec": map[string]interface{}{
			"replicas": 1,
			"selector": map[string]interface{}{
				"matchLabels": map[string]interface{}{
					"app": worker.Slug,
				},
			},
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"app": worker.Slug,
					},
				},
				"spec": map[string]interface{}{
					"serviceAccountName": "confighub-worker",
					"containers": []map[string]interface{}{
						{
							"name":            "worker",
							"image":           "ghcr.io/confighubai/confighub-worker:latest",
							"imagePullPolicy": "Always",
							"args":            []string{workerInstallArgs.workerType},
							"env":             containerEnvs,
						},
					},
				},
			},
		},
	}

	// Convert to YAML
	resources := []map[string]interface{}{namespace, serviceAccount, clusterRoleBinding, deployment}
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
