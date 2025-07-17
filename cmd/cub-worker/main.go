// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	neturl "net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/bridge-worker/impl"
	"github.com/confighub/sdk/bridge-worker/lib"
	"github.com/confighub/sdk/workerapi"
)

var rootCmd = &cobra.Command{
	Use:   "cub-worker-run <worker-types>",
	Args:  cobra.ExactArgs(1),
	Short: "Start a worker process",
	Long: `Start a worker process
The available worker types are:
- kubernetes
- flux-oci-writer
- opentofu-aws
- properties-configmap

They can be comma separated like "kubernetes,properties-configmap"
`,
	SilenceErrors:     true,
	SilenceUsage:      true,
	PersistentPreRunE: rootPreRunE,
	RunE:              rootRunE,
}

const (
	defaultConfighubScheme = "https"
	defaultConfighubHost   = "hub.confighub.com"
	defaultConfighubURL    = defaultConfighubScheme + "://" + defaultConfighubHost
)

var rootArgs struct {
	configHubURL         string
	workerPort           string
	workerID             string
	workerSecret         string
	inCluster            bool
	authMethod           string // "kubernetes", "cloud", "docker-config", "keychain"
	kubernetesSecretPath string
	enableMultiplexer    bool   // Enable new multiplexer mode with prefixes
	// autoRefresh  bool
}

func init() {
	url := defaultConfighubURL
	if envUrl := os.Getenv("CONFIGHUB_URL"); envUrl != "" {
		parsedURL, err := neturl.Parse(envUrl)
		if err != nil {
			log.FromContext(context.Background()).Error(err, "Bad CONFIGHUB_URL")
			url = defaultConfighubURL
		} else {
			if parsedURL.Scheme == "" {
				parsedURL.Scheme = defaultConfighubScheme
			}
			if parsedURL.Host == "" {
				parsedURL.Host = defaultConfighubHost
			}
			// Drop any ports, paths, query params, etc.
			url = parsedURL.Scheme + "://" + parsedURL.Hostname()
		}
	}

	workerPort := "443"
	if p := os.Getenv("CONFIGHUB_WORKER_PORT"); p != "" {
		workerPort = p
	}

	authMethod := "keychain"
	if am := os.Getenv("AUTH_METHOD"); am != "" {
		authMethod = am
	}

	kubernetesSecretPath := os.Getenv("KUBERNETES_SECRET_PATH")

	inCluster := false
	if os.Getenv("IN_CLUSTER") == "true" {
		inCluster = true
	}

	enableMultiplexer := false
	if os.Getenv("ENABLE_MULTIPLEXER") == "true" {
		enableMultiplexer = true
	}

	rootCmd.PersistentFlags().StringVarP(&rootArgs.configHubURL, "url", "u", url, "ConfigHub Server URL (CONFIGHUB_URL)")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.workerPort, "worker-port", "p", workerPort, "ConfigHub Worker Port (CONFIGHUB_WORKER_PORT)")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.workerID, "worker-id", "w", os.Getenv("CONFIGHUB_WORKER_ID"), "Worker ID (CONFIGHUB_WORKER_ID)")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.workerSecret, "worker-secret", "s", os.Getenv("CONFIGHUB_WORKER_SECRET"), "Worker Secret (CONFIGHUB_WORKER_SECRET)")

	// TODO not implemented yet
	// rootCmd.Flags().BoolVarP(&rootArgs.autoRefresh, "auto-refresh", "r", false, "Enable auto-refresh")
	rootCmd.PersistentFlags().BoolVar(&rootArgs.inCluster, "in-cluster", inCluster, "Enable in-cluster deployment for FluxOCIWorker (use Kubernetes secrets or cloud provider credentials) (IN_CLUSTER)")
	rootCmd.PersistentFlags().StringVar(&rootArgs.authMethod, "auth-method", authMethod, "Authentication method for FluxOCIWorker (kubernetes, cloud, docker-config, keychain) (AUTH_METHOD)")
	rootCmd.PersistentFlags().StringVar(&rootArgs.kubernetesSecretPath, "kubernetes-secret-path", kubernetesSecretPath, "Path to the Kubernetes secret mounted as a volume. For use with k8s auth-method and FluxOCIWorker (KUBERNETES_SECRET_PATH)")
	rootCmd.PersistentFlags().BoolVar(&rootArgs.enableMultiplexer, "enable-multiplexer", enableMultiplexer, "Enable multiplexer mode with prefixes and multi-worker support (ENABLE_MULTIPLEXER)")
}

const (
	WorkerTypeKubernetes          = "kubernetes"
	WorkerTypeFluxOCIWriter       = "flux-oci-writer"
	WorkerTypeOpenTofuAWS         = "opentofu-aws"
	WorkerTypePropertiesConfigMap = "properties-configmap"
	// TODO: remove "properties" from the worker type once we can support multiple function workers
	// TODO: add configmap-flux type.
)

// TODO: worker types should map to combinations of ToolchainType and ProviderType
var availableBridgeWorkers = map[string]api.BridgeWorker{
	WorkerTypeKubernetes:          &impl.KubernetesBridgeWorker{},
	WorkerTypeFluxOCIWriter:       impl.NewFluxOCIWorker(),
	WorkerTypeOpenTofuAWS:         &impl.OpenTofuAWSWorker{},
	WorkerTypePropertiesConfigMap: &impl.ConfigMapBridgeWorker{},
}

// Initialize individual function workers first
var k8sFunctionWorker = impl.NewKubernetesFunctionWorker()
var propertiesFunctionWorker = impl.NewPropertiesFunctionWorker()
var opentofuFunctionWorker = impl.NewOpentofuFunctionWorker()

// Map of available function workers by worker type
var availableFunctionWorkers = map[string]api.FunctionWorker{
	WorkerTypeKubernetes:          k8sFunctionWorker,
	WorkerTypeFluxOCIWriter:       k8sFunctionWorker,
	WorkerTypeOpenTofuAWS:         opentofuFunctionWorker,
	WorkerTypePropertiesConfigMap: propertiesFunctionWorker,
}

func rootPreRunE(cmd *cobra.Command, args []string) error {
	// ignore required flag marking for version command
	if cmd != versionCmd {
		if os.Getenv("CONFIGHUB_WORKER_ID") == "" {
			_ = cmd.MarkPersistentFlagRequired("worker-id")
		}

		if os.Getenv("CONFIGHUB_WORKER_SECRET") == "" {
			_ = cmd.MarkPersistentFlagRequired("worker-secret")
		}
	}
	return nil
}

// Convert worker type to toolchain type and provider type
func workerTypeToToolchainAndProvider(workerType string) (workerapi.ToolchainType, api.ProviderType) {
	switch workerType {
	case WorkerTypeKubernetes:
		return workerapi.ToolchainKubernetesYAML, api.ProviderKubernetes
	case WorkerTypeFluxOCIWriter:
		return workerapi.ToolchainKubernetesYAML, api.ProviderFluxOCIWriter
	case WorkerTypeOpenTofuAWS:
		return workerapi.ToolchainOpenTofuHCL, api.ProviderAWS
	case WorkerTypePropertiesConfigMap:
		return workerapi.ToolchainAppConfigProperties, api.ProviderConfigMap
	default:
		return "", ""
	}
}

func rootRunE(cmd *cobra.Command, args []string) error {
	// Check if multiplexer mode is enabled
	if !rootArgs.enableMultiplexer {
		log.FromContext(context.Background()).Info("Running in legacy mode (multiplexer disabled by default)")
		
		// In legacy mode, only support single worker type
		if strings.Contains(args[0], ",") {
			return fmt.Errorf("multiple worker types not supported in legacy mode. Enable multiplexer with --enable-multiplexer or ENABLE_MULTIPLEXER=true")
		}
		
		// Use the old behavior - direct worker without dispatcher
		bridgeWorker, ok := availableBridgeWorkers[args[0]]
		if !ok {
			return fmt.Errorf("unknown bridge worker %s", args[0])
		}
		
		if args[0] == WorkerTypeFluxOCIWriter {
			// Additional initialization for FluxOCIWorker
			if fluxWorker, ok := bridgeWorker.(*impl.FluxOCIWorker); ok {
				err := impl.NewFluxOCIWorkerConfig(fluxWorker,
					rootArgs.inCluster,
					rootArgs.authMethod,
					rootArgs.kubernetesSecretPath,
				)
				if err != nil {
					return fmt.Errorf("failed to initialize FluxOCIWorker: %w", err)
				}
			}
		}
		
		functionWorker, ok := availableFunctionWorkers[args[0]]
		if !ok {
			return fmt.Errorf("unknown function worker %s", args[0])
		}
		
		// Use legacy mode without dispatcher
		return runWorkerLegacy(bridgeWorker, functionWorker)
	}

	// New multiplexer mode
	// workerType is a comma separated string like "kubernetes,flux-oci-writer"
	// Get the input worker types string from command-line arguments
	workerTypesStr := args[0]

	// Split the worker types string by comma
	workerTypes := strings.Split(workerTypesStr, ",")

	// Initialize appropriate workers based on the input
	bridgeDispatcher := impl.NewBridgeDispatcher()
	functionDispatcher := impl.NewFunctionDispatcher()

	// Only enable prefixes if multiplexer mode is explicitly enabled
	if !rootArgs.enableMultiplexer {
		bridgeDispatcher.SetDisablePrefixes(true)
	}

	// For multiple worker types or explicitly using generic worker, use dispatchers
	log.FromContext(context.Background()).Info("Using dispatcher pattern for multi-worker support with unit-level serialization")

	// Process each worker type and register with dispatchers
	for _, workerType := range workerTypes {
		// Convert worker type to toolchain type and provider type
		toolchainType, providerType := workerTypeToToolchainAndProvider(workerType)
		if toolchainType == "" || providerType == "" {
			return fmt.Errorf("could not determine toolchain/provider for worker type %s", workerType)
		}

		// Register bridge worker based on worker type
		if directBridgeWorker, ok := availableBridgeWorkers[workerType]; ok {
			// Special case for FluxOCIWriter - initialize it
			if workerType == WorkerTypeFluxOCIWriter {
				fluxWorker := impl.NewFluxOCIWorker()
				err := impl.NewFluxOCIWorkerConfig(fluxWorker,
					rootArgs.inCluster,
					rootArgs.authMethod,
					rootArgs.kubernetesSecretPath,
				)
				if err != nil {
					return fmt.Errorf("failed to initialize FluxOCIWorker: %w", err)
				}
				// Use fresh instance for dispatcher registration
				bridgeDispatcher.RegisterWorker(toolchainType, providerType, fluxWorker)
			} else {
				// Register other workers directly
				bridgeDispatcher.RegisterWorker(toolchainType, providerType, directBridgeWorker)
			}

			log.FromContext(context.Background()).Info("Registered bridge worker",
				"workerType", workerType,
				"toolchainType", toolchainType,
				"providerType", providerType)
		} else {
			return fmt.Errorf("unknown bridge worker type %s", workerType)
		}

		// Register function worker based on worker type
		if directFunctionWorker, ok := availableFunctionWorkers[workerType]; ok {
			// Register with function dispatcher if not already registered
			functionDispatcher.RegisterWorker(toolchainType, directFunctionWorker)

			log.FromContext(context.Background()).Info("Registered function worker",
				"workerType", workerType,
				"toolchainType", toolchainType)
		} else {
			return fmt.Errorf("unknown function worker type %s", workerType)
		}
	}

	// Check if the URL already contains a port
	parsedURL, err := neturl.Parse(rootArgs.configHubURL)
	if err != nil {
		// Handle potential parsing error, though init() should prevent this
		log.FromContext(context.Background()).Error(err, "Failed to parse configHubURL", "url", rootArgs.configHubURL)
		// Decide on fallback behavior, e.g., use default or return error
		// For now, let's proceed with the potentially malformed URL, assuming init handled basics
	}

	finalURL := rootArgs.configHubURL // Default to original URL

	if err == nil { // Only proceed if parsing was successful
		hostname := parsedURL.Hostname() // Get hostname without port
		if hostname == "" {
			log.FromContext(context.Background()).Info("Could not extract hostname from URL, not modifying port", "url", rootArgs.configHubURL)
		} else if parsedURL.Scheme == "" {
			// Handle case where scheme is missing (though init tries to add https)
			log.FromContext(context.Background()).Info("URL scheme is missing, cannot reliably reconstruct URL with new port", "url", rootArgs.configHubURL)
		} else {
			// Always use the workerPort, replacing existing or appending
			// Reconstruct the URL: scheme://hostname:workerPort
			finalURL = fmt.Sprintf("%s://%s:%s", parsedURL.Scheme, hostname, rootArgs.workerPort)
		}
	} // Note: If err != nil, finalURL remains rootArgs.configHubURL

	w := lib.New(finalURL, // Use the potentially modified URL
		rootArgs.workerID,
		rootArgs.workerSecret).
		WithBridgeWorker(bridgeDispatcher).
		WithFunctionWorker(functionDispatcher)
	if err := w.Start(context.Background()); err != nil {
		log.FromContext(context.Background()).Error(err, "failed to start worker")
		return err
	}
	return nil
}

func runWorkerLegacy(bridgeWorker api.BridgeWorker, functionWorker api.FunctionWorker) error {
	// Check if the URL already contains a port
	parsedURL, err := neturl.Parse(rootArgs.configHubURL)
	if err != nil {
		// Handle potential parsing error, though init() should prevent this
		log.FromContext(context.Background()).Error(err, "Failed to parse configHubURL", "url", rootArgs.configHubURL)
		// Decide on fallback behavior, e.g., use default or return error
		// For now, let's proceed with the potentially malformed URL, assuming init handled basics
	}

	finalURL := rootArgs.configHubURL // Default to original URL

	if err == nil { // Only proceed if parsing was successful
		hostname := parsedURL.Hostname() // Get hostname without port
		if hostname == "" {
			log.FromContext(context.Background()).Info("Could not extract hostname from URL, not modifying port", "url", rootArgs.configHubURL)
		} else if parsedURL.Scheme == "" {
			// Handle case where scheme is missing (though init tries to add https)
			log.FromContext(context.Background()).Info("URL scheme is missing, cannot reliably reconstruct URL with new port", "url", rootArgs.configHubURL)
		} else {
			// Always use the workerPort, replacing existing or appending
			// Reconstruct the URL: scheme://hostname:workerPort
			finalURL = fmt.Sprintf("%s://%s:%s", parsedURL.Scheme, hostname, rootArgs.workerPort)
		}
	} // Note: If err != nil, finalURL remains rootArgs.configHubURL

	w := lib.New(finalURL, // Use the potentially modified URL
		rootArgs.workerID,
		rootArgs.workerSecret).
		WithBridgeWorker(bridgeWorker).
		WithFunctionWorker(functionWorker)
	if err := w.Start(context.Background()); err != nil {
		log.FromContext(context.Background()).Error(err, "failed to start worker")
		return err
	}
	return nil
}

func main() {
	logr := zap.New(zap.UseDevMode(true))
	log.SetLogger(logr)
	if err := rootCmd.Execute(); err != nil {
		log.FromContext(context.Background()).Error(err, "failed to execute command")
	}
}
