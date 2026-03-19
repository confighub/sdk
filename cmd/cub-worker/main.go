// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"net/http"
	neturl "net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo/v4"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/confighub/sdk/bridge-impl/argocd"
	argocdrenderer "github.com/confighub/sdk/bridge-impl/argocd-renderer"
	"github.com/confighub/sdk/bridge-impl/confighub"
	"github.com/confighub/sdk/bridge-impl/configmap"
	"github.com/confighub/sdk/bridge-impl/flux"
	fluxrenderer "github.com/confighub/sdk/bridge-impl/flux-renderer"
	"github.com/confighub/sdk/bridge-impl/kubernetes"
	"github.com/confighub/sdk/bridge-impl/opentofu"
	funcimpl "github.com/confighub/sdk/function-impl"
	"github.com/confighub/sdk/function/executor"
	"github.com/confighub/sdk/function/handler"
	kyvernoserver "github.com/confighub/sdk/worker-function-impl/kyverno-server"
	opagatekeeper "github.com/confighub/sdk/worker-function-impl/opa-gatekeeper"
	"github.com/confighub/sdk/worker/api"
	"github.com/confighub/sdk/worker/lib"
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
- FluxRenderer
- ArgoCDRenderer
- OpenTofu/AWS
- ConfigMapRenderer

Here the provider types are case-insensitive and they can be comma-separated, like "kubernetes,configmaprenderer".

They can be passed in the one optional command-line argument (deprecated), or
via the CONFIGHUB_WORKER_PROVIDER_TYPES environment variable.

By default, all provider types are started.

The worker takes its configuration primarily from environment variables.

The other environment variables it expects are:

- CONFIGHUB_URL: The URL (scheme and host) to call the ConfigHub API. Defaults to ` + defaultConfighubURL + `
- CONFIGHUB_WORKER_PORT: The port for the worker's HTTP2 connection to ConfigHub. Defaults to ` + defaultWorkerPort + `
- CONFIGHUB_WORKER_ID: The worker ID
- CONFIGHUB_WORKER_SECRET: The worker secret
- CONFIGHUB_WORKER_PROVIDER_TYPES: Comma-separated list of lower-cased provider types
- CONFIGHUB_WORKER_FUNCTIONS: Comma-separated list of additional function names to register (e.g., "vet-kyverno-server", "vet-opa-gatekeeper")
- CONFIGHUB_WORKER_HTTP_SERVER_ENABLED: When set, enables a HTTP server with a prometheus exporter endpoint
- CONFIGHUB_WORKER_HTTP_SERVER_PORT: The port to listen for a server, currently only exposes metrics
- CONFIGHUB_WORKER_SERVER_SHUTDOWN_TIMEOUT: The amount of time to allow the HTTP server to shutdown, default is 5 seconds
`,
	SilenceErrors:     true,
	SilenceUsage:      true,
	PersistentPreRunE: rootPreRunE,
	RunE:              rootRunE,
}

const (
	defaultConfighubScheme           = "https"
	defaultConfighubHost             = "hub.confighub.com"
	defaultConfighubURL              = defaultConfighubScheme + "://" + defaultConfighubHost
	defaultWorkerPort                = "443"
	defaultServerPort                = "9092"
	defaultHTTPServerShutdownTimeout = 5 * time.Second
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
	gracePeriodDelay       int // Delay in seconds after SIGTERM before starting shutdown
	// autoRefresh  bool
	enableFluxOCI bool
}

// providerToolchainTypes maps provider types to the toolchain types they require.
var providerToolchainTypes = map[string][]workerapi.ToolchainType{
	LowerProviderTypeConfigHub:         {workerapi.ToolchainConfigHubYAML},
	LowerProviderTypeKubernetes:        {workerapi.ToolchainKubernetesYAML},
	LowerProviderTypeFluxRenderer:      {workerapi.ToolchainKubernetesYAML},
	LowerProviderTypeArgoCDRenderer:    {workerapi.ToolchainKubernetesYAML},
	LowerProviderTypeArgoCDOCI:         {workerapi.ToolchainKubernetesYAML},
	LowerProviderTypeFluxOCIWriter:     {workerapi.ToolchainKubernetesYAML},
	LowerProviderTypeFluxOCI:           {workerapi.ToolchainKubernetesYAML},
	LowerProviderTypeOpenTofuAWS:       {workerapi.ToolchainOpenTofuHCL},
	LowerProviderTypeConfigMapRenderer: {
		workerapi.ToolchainAppConfigProperties,
		workerapi.ToolchainAppConfigYAML,
		workerapi.ToolchainAppConfigTOML,
		workerapi.ToolchainAppConfigINI,
		workerapi.ToolchainAppConfigJSON,
		workerapi.ToolchainAppConfigEnv,
	},
}

// availableWorkerFunctions maps function names to their registration details.
var availableWorkerFunctions = map[string]struct {
	toolchain    workerapi.ToolchainType
	registration handler.FunctionRegistration
}{
	"vet-kyverno-server": {
		toolchain: workerapi.ToolchainKubernetesYAML,
		registration: handler.FunctionRegistration{
			FunctionSignature: kyvernoserver.GetVetKyvernoServerSignature(),
			Function:          kyvernoserver.VetKyvernoServerFunction,
			FunctionInit:      kyvernoserver.InitKyvernoServer,
		},
	},
	"vet-opa-gatekeeper": {
		toolchain: workerapi.ToolchainKubernetesYAML,
		registration: handler.FunctionRegistration{
			FunctionSignature: opagatekeeper.GetVetOPAGatekeeperSignature(),
			Function:          opagatekeeper.VetOPAGatekeeperFunction,
			FunctionInit:      opagatekeeper.InitOPAGatekeeper,
		},
	},
}

// parsedWorkerFunction holds a validated worker function entry for deferred registration.
type parsedWorkerFunction struct {
	name         string
	toolchain    workerapi.ToolchainType
	registration handler.FunctionRegistration
}

// parseWorkerFunctions parses and validates the CONFIGHUB_WORKER_FUNCTIONS env var.
func parseWorkerFunctions() []parsedWorkerFunction {
	workerFunctions := os.Getenv("CONFIGHUB_WORKER_FUNCTIONS")
	if workerFunctions == "" {
		return nil
	}
	var parsed []parsedWorkerFunction
	for _, name := range strings.Split(workerFunctions, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		wf, ok := availableWorkerFunctions[name]
		if !ok {
			log.FromContext(context.Background()).Error(fmt.Errorf("unknown function %q", name), "Skipping unknown worker function")
			continue
		}
		parsed = append(parsed, parsedWorkerFunction{
			name:         name,
			toolchain:    wf.toolchain,
			registration: wf.registration,
		})
	}
	return parsed
}

// newFunctionExecutor returns a standard executor if CONFIGHUB_STANDARD_FUNCTIONS
// is set to "1" or "true", otherwise an empty executor with no functions registered.
// When using the standard executor, only toolchain types needed by the given
// providerTypes and workerFunctions are registered.
// It then registers additional worker functions on the executor.
func newFunctionExecutor(providerTypes []string, workerFunctions []parsedWorkerFunction) executor.FunctionExecutor {
	var exec *executor.ConcreteFunctionExecutor
	if v := os.Getenv("CONFIGHUB_STANDARD_FUNCTIONS"); v == "1" || v == "true" {
		// Build the list of required toolchain types from provider types and worker functions
		seen := map[workerapi.ToolchainType]bool{}
		var toolchainTypes []workerapi.ToolchainType
		for _, pt := range providerTypes {
			for _, tt := range providerToolchainTypes[pt] {
				if !seen[tt] {
					toolchainTypes = append(toolchainTypes, tt)
					seen[tt] = true
				}
			}
		}
		for _, wf := range workerFunctions {
			if !seen[wf.toolchain] {
				toolchainTypes = append(toolchainTypes, wf.toolchain)
				seen[wf.toolchain] = true
			}
		}
		exec = funcimpl.NewStandardExecutor(toolchainTypes)
	} else {
		exec = executor.NewEmptyExecutor()
	}

	for _, wf := range workerFunctions {
		if err := exec.RegisterFunction(wf.toolchain, wf.registration); err != nil {
			log.FromContext(context.Background()).Error(err, "Failed to register worker function", "function", wf.name)
		} else {
			log.FromContext(context.Background()).Info("Registered worker function", "function", wf.name)
		}
	}

	return exec
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
	}

	workerProviderTypesStr := ""
	for wt := range availableBridgeWorkers {
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

	rootCmd.PersistentFlags().IntVar(&rootArgs.gracePeriodDelay, "grace-period-delay", gracePeriodDelay, "Delay in seconds after receiving SIGTERM before starting shutdown (GRACE_PERIOD_DELAY)")
}

// These are lowercase to make the provider type matching case insensitive
const (
	LowerProviderTypeConfigHub         = "confighub"
	LowerProviderTypeKubernetes        = "kubernetes"
	LowerProviderTypeFluxOCIWriter     = "fluxociwriter"
	LowerProviderTypeFluxOCI           = "fluxoci"
	LowerProviderTypeFluxRenderer      = "fluxrenderer"
	LowerProviderTypeOpenTofuAWS       = "opentofu/aws"
	LowerProviderTypeConfigMapRenderer = "configmaprenderer"
	LowerProviderTypeArgoCDRenderer    = "argocdrenderer"
	LowerProviderTypeArgoCDOCI         = "argocdoci"
)

// NOTE: The FluxOCIWriter provider type is disabled by default for now and may be deprecated in the future.

// Note: ConfigHub bridge worker needs to be initialized with a client in rootRunE

var availableBridgeWorkers = map[string]api.BridgeWorker{
	LowerProviderTypeConfigHub:    confighub.NewConfigHubBridgeWorker(),
	LowerProviderTypeKubernetes:   kubernetes.NewKubernetesBridgeWorker(),
	LowerProviderTypeFluxRenderer: fluxrenderer.NewFluxRendererWorker(),
	// LowerProviderTypeFluxOCIWriter:       fluxOCIWorker,
	LowerProviderTypeOpenTofuAWS:       opentofu.NewOpenTofuAWSWorker(),
	LowerProviderTypeConfigMapRenderer: configmap.NewConfigMapBridgeWorker(),
	LowerProviderTypeArgoCDRenderer:    argocdrenderer.NewArgoCDRendererWorker(),
	// ArgoCDOCI worker is initialized in rootRunE with credentials (like ConfigHub worker)
	LowerProviderTypeArgoCDOCI: nil, // placeholder, initialized in rootRunE
}
var fluxOCIWorker = flux.NewFluxOCIWriterWorker()

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

func newHTTPServer() *echo.Echo {
	rootRouter := echo.New()
	rootRouter.HideBanner = true
	internalRouter := rootRouter.Group("/internal")
	internalRouter.GET("/metrics", echoprometheus.NewHandler())

	return rootRouter
}

func rootRunE(cmd *cobra.Command, args []string) error {
	if rootArgs.enableFluxOCI {
		availableBridgeWorkers[LowerProviderTypeFluxOCIWriter] = fluxOCIWorker
	}

	workerProviderTypesStr := strings.ToLower(rootArgs.workerProviderTypesStr)
	if len(args) > 0 {
		// Override provider types
		workerProviderTypesStr = strings.ToLower(args[0])
	}

	// Initialize frontdoor client
	frontdoorClient := lib.NewWorkerFrontdoorClient(rootArgs.configHubURL, rootArgs.mainPort, rootArgs.workerID, rootArgs.workerSecret)
	if frontdoorClient == nil {
		return errors.New("frontdoor client initialization failed")
	}

	// Split the provider types string by comma
	providerTypes := strings.Split(workerProviderTypesStr, ",")

	// Parse and validate worker functions before creating the executor
	workerFunctions := parseWorkerFunctions()

	// Initialize appropriate workers based on the input
	bridgeDispatcher := lib.NewBridgeDispatcher()

	// For multiple provider types or explicitly using generic worker, use dispatchers
	log.FromContext(context.Background()).Info("Using dispatcher pattern for multi-provider/multi-toolchain support with unit-level serialization")

	// Process each provider type and register with dispatchers
	for _, lowerProviderType := range providerTypes {
		// Register bridge worker based on provider type
		var directBridgeWorker api.BridgeWorker
		var ok bool

		// Handle workers that need credentials at construction time
		if lowerProviderType == LowerProviderTypeArgoCDOCI {
			directBridgeWorker = argocd.NewArgoCDOCIWorker(rootArgs.workerID, rootArgs.workerSecret)
			ok = true
		} else if lowerProviderType == LowerProviderTypeFluxOCI {
			directBridgeWorker = flux.NewFluxOCIWorker(rootArgs.workerID, rootArgs.workerSecret)
			ok = true
		} else {
			directBridgeWorker, ok = availableBridgeWorkers[lowerProviderType]
			if !ok {
				return fmt.Errorf("unknown bridge provider type %s", lowerProviderType)
			}
		}

		// Get toolchain types and provider type from the bridge worker itself
		bridgeID := directBridgeWorker.ID()
		toolchainTypes := bridgeID.ToolchainTypes
		providerType := bridgeID.ProviderType
		if len(toolchainTypes) == 0 || providerType == "" {
			return fmt.Errorf("could not determine toolchain/provider for provider type %s", providerType)
		}

		// Handle ConfigHub worker specially - it needs authentication
		if lowerProviderType == LowerProviderTypeConfigHub {
			configHubWorker, _ := directBridgeWorker.(*confighub.ConfigHubBridgeWorker)
			if configHubWorker == nil {
				ok = false
			} else {
				err := configHubWorker.Init(frontdoorClient)
				if err != nil {
					return err
				}
			}
		}

		// Currently disabled by default
		// Special case for FluxOCIWriter - initialize it
		if rootArgs.enableFluxOCI && lowerProviderType == LowerProviderTypeFluxOCIWriter {
			if fluxWorker, ok := directBridgeWorker.(*flux.FluxOCIWriterWorker); ok {
				err := flux.NewFluxOCIWorkerConfig(fluxWorker,
					rootArgs.inCluster,
					rootArgs.authMethod,
					rootArgs.kubernetesSecretPath,
				)
				if err != nil {
					return fmt.Errorf("failed to initialize FluxOCIWorker: %w", err)
				}
				// Use fresh instance for dispatcher registration
				for _, toolchainType := range toolchainTypes {
					bridgeDispatcher.RegisterWorker(toolchainType, providerType, fluxWorker)
					log.FromContext(context.Background()).Info("Registered bridge worker",
						"toolchainType", toolchainType,
						"providerType", providerType)
				}
			}

		} else {
			// Register other bridges directly
			for _, toolchainType := range toolchainTypes {
				bridgeDispatcher.RegisterWorker(toolchainType, providerType, directBridgeWorker)
				log.FromContext(context.Background()).Info("Registered bridge worker",
					"toolchainType", toolchainType,
					"providerType", providerType)
			}
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

	metricsProvider, err := lib.NewPrometheusProvider()
	if err != nil {
		return fmt.Errorf("failed to instantiate prometheus provider: %w", err)
	}

	metricsMeter := metricsProvider.Meter("confighub-worker")

	var httpServer *echo.Echo
	if httpServerEnabled := os.Getenv("CONFIGHUB_WORKER_HTTP_SERVER_ENABLED"); httpServerEnabled != "" && httpServerEnabled != "0" {
		httpServer = newHTTPServer()
	}

	w := lib.New(finalURL, // Use the potentially modified URL
		rootArgs.workerID,
		rootArgs.workerSecret).
		WithBridgeWorker(bridgeDispatcher).
		WithFunctionExecutor(newFunctionExecutor(providerTypes, workerFunctions)).
		WithMetricsMeter(metricsMeter)

	// Start worker in a errgroup with the http server so we can handle signals
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return w.Start(ctx)
	})
	if httpServer != nil {
		eg.Go(func() error {
			port := os.Getenv("CONFIGHUB_WORKER_HTTP_SERVER_PORT")
			if port == "" {
				port = defaultServerPort
			}
			if startErr := httpServer.Start(":" + port); startErr != nil && !errors.Is(startErr, http.ErrServerClosed) {
				return errors.Wrap(startErr, "HTTP server unexpected failure")
			}
			return nil
		})
	}

	sig := <-sigChan
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

	if httpServer != nil {
		serverShutdownTimeout := defaultHTTPServerShutdownTimeout
		if shutdownOverride := os.Getenv("CONFIGHUB_WORKER_SERVER_SHUTDOWN_TIMEOUT"); shutdownOverride != "" {
			override, err := strconv.Atoi(shutdownOverride)
			if err != nil {
				log.FromContext(context.Background()).Info("Failed to parse HTTP server shutdown override, using default", "error", err)
			} else {
				serverShutdownTimeout = time.Duration(override) * time.Second
			}
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.FromContext(context.Background()).Info(
				"Worker HTTP server stopped with an error during shutdown",
				"error", err.Error(),
			)
		}
		cancel()
	}

	// Wait for worker goroutine to exit
	if err := eg.Wait(); err != nil {
		// Just log the error message without stack trace
		log.FromContext(context.Background()).Info("Worker stopped with error during shutdown", "error", err.Error())
	}

	log.FromContext(context.Background()).Info("Worker shutdown completed gracefully")
	return nil
}

func main() {
	logr := zap.New(zap.UseDevMode(true))
	log.SetLogger(logr)
	if err := rootCmd.Execute(); err != nil {
		log.FromContext(context.Background()).Error(err, "failed to execute command")
	}
}
