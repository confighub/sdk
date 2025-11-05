// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	neturl "net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/bridge-worker/impl"
	"github.com/confighub/sdk/bridge-worker/lib"
	"github.com/confighub/sdk/workerapi"
)

var rootCmd = &cobra.Command{
	Use:   "cub-worker-run <provider-types>",
	Args:  cobra.MaximumNArgs(1),
	Short: "Start a worker process",
	Long: `Start a worker process to serve one or more supported provider types.

Each ProviderType corresponds to one or more ToolchainTypes.
For example, the "Kubernetes" provider type corresponds to the Kubernetes/YAML ToolchainType

Some ToolchainTypes are supported by multiple ProviderTypes, and some ProviderTypes support
multiple ToolchainTypes.

The available ProviderTypes are:

- ConfigHub
- Kubernetes
- OpenTofu/AWS
- ConfigMap

Here the provider types are case-insensitive and they can be comma-separated, like "kubernetes,configmap".

They can be passed in the one optional command-line argument (deprecated), or
via the CONFIGHUB_WORKER_PROVIDER_TYPES environment variable.

By default, all provider types are started.

The worker takes its configuration primarily from environment variables.

The other environment variables it expects are:

- CONFIGHUB_URL: The URL (scheme and host) to call the ConfigHub API. Defaults to ` + defaultConfighubURL + `
- CONFIGHUB_WORKER_PORT: The port for the worker's HTTP2 connection to ConfigHub. Defaults to ` + defaultWorkerPort + `
- CONFIGHUB_WORKER_ID: The worker ID
- CONFIGHUB_WORKER_SECRET: The worker secret
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
	defaultWorkerPort      = "443"
)

var rootArgs struct {
	configHubURL           string
	mainPort               string
	workerPort             string
	workerID               string
	workerSecret           string
	workerProviderTypesStr string
	inCluster              bool
	authMethod             string // "kubernetes", "cloud", "docker-config", "keychain"
	kubernetesSecretPath   string
	enableMultiplexer      bool // Enable new multiplexer mode with prefixes
	gracePeriodDelay       int  // Delay in seconds after SIGTERM before starting shutdown
	// autoRefresh  bool
	enableFluxOCI bool
}

func init() {
	url := defaultConfighubURL
	mainPort := "443"
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
			port := parsedURL.Port()
			if parsedURL.Port() != "" {
				mainPort = port
			}
			// Drop any ports, paths, query params, etc.
			url = parsedURL.Scheme + "://" + parsedURL.Hostname()
		}
	}

	workerPort := defaultWorkerPort
	if p := os.Getenv("CONFIGHUB_WORKER_PORT"); p != "" {
		workerPort = p
	}

	// FIXME: We should not be using any env vars that are not prefixed with CONFIGHUB_

	authMethod := "keychain"
	if am := os.Getenv("AUTH_METHOD"); am != "" {
		authMethod = am
	}

	kubernetesSecretPath := os.Getenv("KUBERNETES_SECRET_PATH")

	inCluster := false
	if os.Getenv("IN_CLUSTER") == "true" {
		inCluster = true
	}

	enableMultiplexer := true
	if os.Getenv("ENABLE_MULTIPLEXER") == "false" {
		enableMultiplexer = false
	}

	gracePeriodDelay := 10 // default 10 seconds
	if gpd := os.Getenv("GRACE_PERIOD_DELAY"); gpd != "" {
		if delay, err := strconv.Atoi(gpd); err == nil && delay >= 0 {
			gracePeriodDelay = delay
		}
	}

	// FluxOCI is currently disabled by default
	if os.Getenv("CONFIGHUB_ENABLE_FLUXOCI") != "" {
		rootArgs.enableFluxOCI = true
	}
	if rootArgs.enableFluxOCI {
		availableBridgeWorkers[LowerProviderTypeFluxOCIWriter] = fluxOCIWorker
		availableFunctionWorkers[LowerProviderTypeFluxOCIWriter] = []api.FunctionWorker{k8sFunctionWorker}
	}

	workerProviderTypesStr := ""
	for wt := range availableFunctionWorkers {
		workerProviderTypesStr += "," + wt
	}
	workerProviderTypesStr = strings.TrimPrefix(workerProviderTypesStr, ",")
	if wt := os.Getenv("CONFIGHUB_WORKER_PROVIDER_TYPES"); wt != "" {
		workerProviderTypesStr = wt
	}

	// Flags should only be used for testing. They are not part of the worker invocation contract.

	rootCmd.PersistentFlags().StringVarP(&rootArgs.configHubURL, "url", "u", url, "ConfigHub Server URL (CONFIGHUB_URL)")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.mainPort, "main-port", "", mainPort, "ConfigHub Main Port (extracted from CONFIGHUB_URL by default)")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.workerPort, "worker-port", "p", workerPort, "ConfigHub Worker Port (CONFIGHUB_WORKER_PORT)")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.workerID, "worker-id", "w", os.Getenv("CONFIGHUB_WORKER_ID"), "Worker ID (CONFIGHUB_WORKER_ID)")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.workerSecret, "worker-secret", "s", os.Getenv("CONFIGHUB_WORKER_SECRET"), "Worker Secret (CONFIGHUB_WORKER_SECRET)")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.workerProviderTypesStr, "provider-types", "t", workerProviderTypesStr, "Comma-separated list of provider types (CONFIGHUB_WORKER_PROVIDER_TYPES)")

	// TODO not implemented yet
	// rootCmd.Flags().BoolVarP(&rootArgs.autoRefresh, "auto-refresh", "r", false, "Enable auto-refresh")

	if rootArgs.enableFluxOCI {
		rootCmd.PersistentFlags().BoolVar(&rootArgs.inCluster, "in-cluster", inCluster, "Enable in-cluster deployment for FluxOCIWorker (use Kubernetes secrets or cloud provider credentials) (IN_CLUSTER)")
		rootCmd.PersistentFlags().StringVar(&rootArgs.authMethod, "auth-method", authMethod, "Authentication method for FluxOCIWorker (kubernetes, cloud, docker-config, keychain) (AUTH_METHOD)")
		rootCmd.PersistentFlags().StringVar(&rootArgs.kubernetesSecretPath, "kubernetes-secret-path", kubernetesSecretPath, "Path to the Kubernetes secret mounted as a volume. For use with k8s auth-method and FluxOCIWorker (KUBERNETES_SECRET_PATH)")
	}

	rootCmd.PersistentFlags().BoolVar(&rootArgs.enableMultiplexer, "enable-multiplexer", enableMultiplexer, "Enable multiplexer mode with prefixes and multi-worker support (default: true, ENABLE_MULTIPLEXER)")
	rootCmd.PersistentFlags().IntVar(&rootArgs.gracePeriodDelay, "grace-period-delay", gracePeriodDelay, "Delay in seconds after receiving SIGTERM before starting shutdown (GRACE_PERIOD_DELAY)")
}

// These are lowercase to make the provider type matching case insensitive
const (
	LowerProviderTypeConfigHub     = "confighub"
	LowerProviderTypeKubernetes    = "kubernetes"
	LowerProviderTypeFluxOCIWriter = "fluxociwriter"
	LowerProviderTypeOpenTofuAWS   = "opentofu/aws"
	LowerProviderTypeConfigMap     = "configmap"
)

// NOTE: The FluxOCIWriter provider type is disabled by default for now and may be deprecated in the future.

// Note: ConfigHub bridge worker needs to be initialized with a client in rootRunE

var availableBridgeWorkers = map[string]api.BridgeWorker{
	// ConfigHub worker is special - it will be initialized in rootRunE with a client
	LowerProviderTypeKubernetes: impl.NewKubernetesBridgeWorker(),
	// LowerProviderTypeFluxOCIWriter:       fluxOCIWorker,
	LowerProviderTypeOpenTofuAWS: &impl.OpenTofuAWSWorker{},
	LowerProviderTypeConfigMap:   &impl.ConfigMapBridgeWorker{},
}
var fluxOCIWorker = impl.NewFluxOCIWorker()

// Initialize individual function workers first
var confighubFunctionWorker = impl.NewConfigHubFunctionWorker()
var k8sFunctionWorker = impl.NewKubernetesFunctionWorker()
var propertiesFunctionWorker = impl.NewPropertiesFunctionWorker()
var appyamlFunctionWorker = impl.NewAppConfigYAMLFunctionWorker()
var tomlFunctionWorker = impl.NewTOMLFunctionWorker()
var iniFunctionWorker = impl.NewINIFunctionWorker()
var opentofuFunctionWorker = impl.NewOpentofuFunctionWorker()

// Map of available function workers by provider type
var availableFunctionWorkers = map[string][]api.FunctionWorker{
	LowerProviderTypeConfigHub:  []api.FunctionWorker{confighubFunctionWorker},
	LowerProviderTypeKubernetes: []api.FunctionWorker{k8sFunctionWorker},
	// LowerProviderTypeFluxOCIWriter: []api.FunctionWorker{k8sFunctionWorker},
	LowerProviderTypeOpenTofuAWS: []api.FunctionWorker{opentofuFunctionWorker},
	LowerProviderTypeConfigMap:   []api.FunctionWorker{propertiesFunctionWorker, appyamlFunctionWorker, tomlFunctionWorker, iniFunctionWorker},
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

// Convert lowercase provider type to toolchain type and provider type
func providerTypeToToolchainsAndProvider(lowerProviderType string) ([]workerapi.ToolchainType, api.ProviderType) {
	switch lowerProviderType {
	case LowerProviderTypeConfigHub:
		return []workerapi.ToolchainType{workerapi.ToolchainConfigHubYAML}, api.ProviderConfigHub
	case LowerProviderTypeKubernetes:
		return []workerapi.ToolchainType{workerapi.ToolchainKubernetesYAML}, api.ProviderKubernetes
	case LowerProviderTypeFluxOCIWriter:
		return []workerapi.ToolchainType{workerapi.ToolchainKubernetesYAML}, api.ProviderFluxOCIWriter
	case LowerProviderTypeOpenTofuAWS:
		return []workerapi.ToolchainType{workerapi.ToolchainOpenTofuHCL}, api.ProviderOpenTofuAWS
	case LowerProviderTypeConfigMap:
		return []workerapi.ToolchainType{workerapi.ToolchainAppConfigProperties, workerapi.ToolchainAppConfigYAML, workerapi.ToolchainAppConfigTOML, workerapi.ToolchainAppConfigINI}, api.ProviderConfigMap
	default:
		return []workerapi.ToolchainType{}, ""
	}
}

func rootRunE(cmd *cobra.Command, args []string) error {
	workerProviderTypesStr := strings.ToLower(rootArgs.workerProviderTypesStr)
	if len(args) > 0 {
		// Override provider types
		workerProviderTypesStr = strings.ToLower(args[0])
	}

	// Check if multiplexer mode is enabled
	if !rootArgs.enableMultiplexer {

		log.FromContext(context.Background()).Info("Running in legacy mode (multiplexer explicitly disabled)")

		// In legacy mode, only support single provider type
		if strings.Contains(workerProviderTypesStr, ",") {
			return fmt.Errorf("multiple provider types not supported in legacy mode. Remove --enable-multiplexer=false or set ENABLE_MULTIPLEXER=true")
		}

		// Handle ConfigHub worker specially - it needs authentication
		var bridgeWorker api.BridgeWorker
		var ok bool
		if workerProviderTypesStr == LowerProviderTypeConfigHub {
			// Create ConfigHub bridge worker with authentication
			bridgeWorker = impl.NewConfigHubBridgeWorker(rootArgs.configHubURL, rootArgs.mainPort, rootArgs.workerID, rootArgs.workerSecret)
			ok = true
		} else {
			// Use the old behavior - direct worker without dispatcher
			bridgeWorker, ok = availableBridgeWorkers[workerProviderTypesStr]
			if !ok {
				return fmt.Errorf("unknown bridge worker %s", workerProviderTypesStr)
			}
		}

		// Currently disabled by default
		if rootArgs.enableFluxOCI && workerProviderTypesStr == LowerProviderTypeFluxOCIWriter {
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

		functionWorkers, ok := availableFunctionWorkers[workerProviderTypesStr]
		if !ok {
			return fmt.Errorf("provider type %s has no function executors", workerProviderTypesStr)
		}

		if len(functionWorkers) != 1 {
			fmt.Errorf("provider type %s requires multiplexer mode. Remove --enable-multiplexer=false or set ENABLE_MULTIPLEXER=true.", workerProviderTypesStr)
		}

		// Use legacy mode without dispatcher
		return runWorkerLegacy(bridgeWorker, functionWorkers[0])
	}

	// New multiplexer mode
	// workerProviderTypesStr is a comma separated string like "kubernetes,configmap"

	// Split the provider types string by comma
	providerTypes := strings.Split(workerProviderTypesStr, ",")

	// Initialize appropriate workers based on the input
	bridgeDispatcher := impl.NewBridgeDispatcher()
	functionDispatcher := impl.NewFunctionDispatcher()

	// For multiple provider types or explicitly using generic worker, use dispatchers
	log.FromContext(context.Background()).Info("Using dispatcher pattern for multi-provider/multi-toolchain support with unit-level serialization")

	// Process each provider type and register with dispatchers
	for _, lowerProviderType := range providerTypes {
		// Convert lowercase provider type to toolchain type and provider type
		toolchainTypes, providerType := providerTypeToToolchainsAndProvider(lowerProviderType)
		if len(toolchainTypes) == 0 || providerType == "" {
			return fmt.Errorf("could not determine toolchain/provider for provider type %s", providerType)
		}

		// Register bridge worker based on provider type
		var directBridgeWorker api.BridgeWorker
		var ok bool

		// Handle ConfigHub worker specially - it needs authentication
		if lowerProviderType == LowerProviderTypeConfigHub {
			// Create ConfigHub bridge worker with authentication
			directBridgeWorker = impl.NewConfigHubBridgeWorker(rootArgs.configHubURL, rootArgs.mainPort, rootArgs.workerID, rootArgs.workerSecret)
			ok = true
		} else {
			directBridgeWorker, ok = availableBridgeWorkers[lowerProviderType]
		}

		if ok {
			// Currently disabled by default
			// Special case for FluxOCIWriter - initialize it
			if rootArgs.enableFluxOCI && lowerProviderType == LowerProviderTypeFluxOCIWriter {
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
				bridgeDispatcher.RegisterWorker(workerapi.ToolchainKubernetesYAML, providerType, fluxWorker)
				log.FromContext(context.Background()).Info("Registered bridge worker",
					"toolchainType", workerapi.ToolchainKubernetesYAML,
					"providerType", providerType)

			} else {
				// Register other bridges directly
				for _, toolchainType := range toolchainTypes {
					bridgeDispatcher.RegisterWorker(toolchainType, providerType, directBridgeWorker)
					log.FromContext(context.Background()).Info("Registered bridge worker",
						"toolchainType", toolchainType,
						"providerType", providerType)
				}
			}

		} else {
			return fmt.Errorf("unknown bridge provider type %s", providerType)
		}

		// Register function executor based on provider type
		if directFunctionWorkers, ok := availableFunctionWorkers[lowerProviderType]; ok {
			// Register with function dispatcher if not already registered
			for _, directFunctionWorker := range directFunctionWorkers {
				toolchainTypes := directFunctionWorker.Info().ToolchainTypes
				for _, toolchainType := range toolchainTypes {
					functionDispatcher.RegisterWorker(toolchainType, directFunctionWorker)
					log.FromContext(context.Background()).Info("Registered function executor",
						"toolchainType", toolchainType)
				}
			}
		} else {
			return fmt.Errorf("no function executors for provider type %s", lowerProviderType)
		}
	}

	// Create a context that will be cancelled on SIGTERM or SIGINT
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown - only trap SIGTERM
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM)

	// Check if the URL already contains a port
	parsedURL, err := neturl.Parse(rootArgs.configHubURL)
	if err != nil {
		// Handle potential parsing error, though init() should prevent this
		log.FromContext(ctx).Error(err, "Failed to parse configHubURL", "url", rootArgs.configHubURL)
		// Decide on fallback behavior, e.g., use default or return error
		// For now, let's proceed with the potentially malformed URL, assuming init handled basics
	}

	finalURL := rootArgs.configHubURL // Default to original URL

	if err == nil { // Only proceed if parsing was successful
		hostname := parsedURL.Hostname() // Get hostname without port
		if hostname == "" {
			log.FromContext(ctx).Info("Could not extract hostname from URL, not modifying port", "url", rootArgs.configHubURL)
		} else if parsedURL.Scheme == "" {
			// Handle case where scheme is missing (though init tries to add https)
			log.FromContext(ctx).Info("URL scheme is missing, cannot reliably reconstruct URL with new port", "url", rootArgs.configHubURL)
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

	// Start worker in a goroutine so we can handle signals
	errChan := make(chan error, 1)
	go func() {
		if err := w.Start(ctx); err != nil {
			errChan <- err
		}
		close(errChan)
	}()

	// Wait for either signal or worker error
	select {
	case sig := <-sigChan:
		log.FromContext(ctx).Info("Received signal, initiating graceful shutdown", "signal", sig)

		// Sleep for configured delay to allow new pod to become active
		// This ensures smooth handoff in rolling updates with our standby promotion system
		if rootArgs.gracePeriodDelay > 0 {
			log.FromContext(ctx).Info("Waiting for smooth handoff to new pod...", "delay_seconds", rootArgs.gracePeriodDelay)
			time.Sleep(time.Duration(rootArgs.gracePeriodDelay) * time.Second)
		}

		// Wait for all pending operations to complete first
		// This ensures Apply/Destroy/etc operations fully complete including sending final status
		w.WaitForPendingOperations()

		// Now cancel context to stop accepting new work and close SSE stream
		cancel()

		// Wait for worker goroutine to exit
		if err := <-errChan; err != nil {
			// Just log the error message without stack trace
			log.FromContext(context.Background()).Info("Worker stopped with error during shutdown", "error", err.Error())
		}

		log.FromContext(context.Background()).Info("Worker shutdown completed gracefully")
	case err := <-errChan:
		if err != nil {
			log.FromContext(context.Background()).Info("Failed to start worker", "error", err.Error())
			return err
		}
	}
	return nil
}

func runWorkerLegacy(bridgeWorker api.BridgeWorker, functionWorker api.FunctionWorker) error {
	// Create a context that will be cancelled on SIGTERM or SIGINT
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown - only trap SIGTERM
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM)

	// Check if the URL already contains a port
	parsedURL, err := neturl.Parse(rootArgs.configHubURL)
	if err != nil {
		// Handle potential parsing error, though init() should prevent this
		log.FromContext(ctx).Error(err, "Failed to parse configHubURL", "url", rootArgs.configHubURL)
		// Decide on fallback behavior, e.g., use default or return error
		// For now, let's proceed with the potentially malformed URL, assuming init handled basics
	}

	finalURL := rootArgs.configHubURL // Default to original URL

	if err == nil { // Only proceed if parsing was successful
		hostname := parsedURL.Hostname() // Get hostname without port
		if hostname == "" {
			log.FromContext(ctx).Info("Could not extract hostname from URL, not modifying port", "url", rootArgs.configHubURL)
		} else if parsedURL.Scheme == "" {
			// Handle case where scheme is missing (though init tries to add https)
			log.FromContext(ctx).Info("URL scheme is missing, cannot reliably reconstruct URL with new port", "url", rootArgs.configHubURL)
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

	// Start worker in a goroutine so we can handle signals
	errChan := make(chan error, 1)
	go func() {
		if err := w.Start(ctx); err != nil {
			errChan <- err
		}
		close(errChan)
	}()

	// Wait for either signal or worker error
	select {
	case sig := <-sigChan:
		log.FromContext(ctx).Info("Received signal, initiating graceful shutdown", "signal", sig)

		// Sleep for configured delay to allow new pod to become active
		// This ensures smooth handoff in rolling updates with our standby promotion system
		if rootArgs.gracePeriodDelay > 0 {
			log.FromContext(ctx).Info("Waiting for smooth handoff to new pod...", "delay_seconds", rootArgs.gracePeriodDelay)
			time.Sleep(time.Duration(rootArgs.gracePeriodDelay) * time.Second)
		}

		// Wait for all pending operations to complete first
		// This ensures Apply/Destroy/etc operations fully complete including sending final status
		w.WaitForPendingOperations()

		// Now cancel context to stop accepting new work and close SSE stream
		cancel()

		// Wait for worker goroutine to exit
		if err := <-errChan; err != nil {
			// Just log the error message without stack trace
			log.FromContext(context.Background()).Info("Worker stopped with error during shutdown", "error", err.Error())
		}

		log.FromContext(context.Background()).Info("Worker shutdown completed gracefully")
	case err := <-errChan:
		if err != nil {
			log.FromContext(context.Background()).Info("Failed to start worker", "error", err.Error())
			return err
		}
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
