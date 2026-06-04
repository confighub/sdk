// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	neturl "net/url"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo-contrib/pprof"
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
	"github.com/confighub/sdk/core/function/executor"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/confighub/sdk/core/worker/lib"
	"github.com/confighub/sdk/core/workerapi"
	funcimpl "github.com/confighub/sdk/function-impl"
	k8sfunc "github.com/confighub/sdk/function-impl/kubernetes"
	kyvernoserver "github.com/confighub/sdk/worker-function-impl/kyverno-server"
	opagatekeeper "github.com/confighub/sdk/worker-function-impl/opa-gatekeeper"
)

var rootCmd = &cobra.Command{
	Use:   "cub-worker-run [<provider-types>]",
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
- FluxOCI
- ArgoCDRenderer
- ArgoCDOCI
- ConfigMapRenderer

Here the provider types are case-insensitive and they can be comma-separated, like "kubernetes,configmaprenderer".

They can be passed in the one optional command-line argument (deprecated), or
via the CONFIGHUB_WORKER_PROVIDER_TYPES environment variable.

By default, all provider types are started.

The worker takes its configuration primarily from environment variables.
Flags should only be used for testing. They are not part of the worker invocation contract
used by cub worker install, cub worker run, and cub worker get-envs.

The environment variables it expects are:

- CONFIGHUB_WORKER_ID: The worker ID
- CONFIGHUB_WORKER_SECRET: The worker secret

Optional environment variables:

- CONFIGHUB_WORKER_PROVIDER_TYPES: Comma-separated list of lower-cased provider types
- CONFIGHUB_WORKER_FUNCTIONS: Comma-separated list of additional function names to register (e.g., "vet-kyverno-server", "vet-opa-gatekeeper")

- CONFIGHUB_URL: The URL (scheme and host) to call the ConfigHub API. Defaults to ` + defaultConfighubURL + `
- CONFIGHUB_WORKER_PORT: The port for the worker's HTTP2 connection to ConfigHub. Defaults to ` + defaultWorkerPort + `
- CONFIGHUB_WORKER_TRANSPORT: Wire protocol used to talk to ConfigHub. "http2-stream" (default) keeps the v1 long-lived h2c stream. "long-poll" uses HTTP long-polling on the main API port and does not require a separate worker port.
- CONFIGHUB_WORKER_HTTP_SERVER_PORT: When set, starts a local HTTP server on this port. Exposes /internal/metrics (Prometheus), /internal/pprof, /internal/ok (liveness), and /internal/ready (readiness). When unset, no HTTP server is started.
- CONFIGHUB_WORKER_SERVER_SHUTDOWN_TIMEOUT: The amount of time to allow the HTTP server to shutdown, default is 5 seconds

Run "cub-worker-run docgen env" to print the worker environment variables as a JSON Schema.
Run "cub-worker-run docgen command" to print Cobra YAML documentation for the worker command.
Run "cub-worker-run docgen runtime" to print the worker's runtime spec (ports, paths, probes) as YAML.
Run "cub-worker-run get-env" to print the loaded worker configuration as JSON.
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
	defaultMainPort                  = "443"
	defaultWorkerPort                = "443"
	defaultHTTPServerShutdownTimeout = 5 * time.Second
)

var rootArgs ConfigHubWorkerArgs

// providerToolchainTypes maps provider types to the toolchain types they require.
var providerToolchainTypes = map[string][]workerapi.ToolchainType{
	LowerProviderTypeConfigHub:      {workerapi.ToolchainConfigHubYAML},
	LowerProviderTypeKubernetes:     {workerapi.ToolchainKubernetesYAML},
	LowerProviderTypeFluxRenderer:   {workerapi.ToolchainKubernetesYAML},
	LowerProviderTypeArgoCDRenderer: {workerapi.ToolchainKubernetesYAML},
	LowerProviderTypeArgoCDOCI:      {workerapi.ToolchainKubernetesYAML},
	LowerProviderTypeFluxOCI:        {workerapi.ToolchainKubernetesYAML},
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
	"generate-kubecontext": {
		toolchain: workerapi.ToolchainKubernetesYAML,
		registration: handler.FunctionRegistration{
			FunctionSignature: k8sfunc.GetGenerateKubecontextSignature(),
			Function:          k8sfunc.GenerateKubecontextFunction,
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
	if rootArgs.WorkerFunctions == "" {
		return nil
	}
	var parsed []parsedWorkerFunction
	for _, name := range strings.Split(rootArgs.WorkerFunctions, ",") {
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

// newFunctionExecutor creates a function executor with the required toolchain handlers.
// If CONFIGHUB_STANDARD_FUNCTIONS is set, all standard functions for the provider types
// are included. Worker functions (-f flag) always get their toolchain handlers set up.
// If neither standard functions nor worker functions are requested, an empty executor
// is returned.
func newFunctionExecutor(providerTypes []string, workerFunctions []parsedWorkerFunction) executor.FunctionExecutor {
	var exec *executor.ConcreteFunctionExecutor
	v := rootArgs.StandardFunctions
	registerStandardFunctions := v == "1" || v == "true" || v == "TRUE"

	// Build the list of required toolchain types from worker functions and provider types.
	seen := map[workerapi.ToolchainType]bool{}
	var toolchainTypes []workerapi.ToolchainType
	for _, wf := range workerFunctions {
		if !seen[wf.toolchain] {
			toolchainTypes = append(toolchainTypes, wf.toolchain)
			seen[wf.toolchain] = true
		}
	}
	for _, pt := range providerTypes {
		for _, tt := range providerToolchainTypes[pt] {
			if !seen[tt] {
				toolchainTypes = append(toolchainTypes, tt)
				seen[tt] = true
			}
		}
	}
	exec = funcimpl.NewStandardExecutor(toolchainTypes, registerStandardFunctions)

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
	// Populate rootArgs from environment variables. We don't fail on errors
	// here because (a) the docgen command must work without any env vars set,
	// and (b) flags can supply CONFIGHUB_WORKER_ID / CONFIGHUB_WORKER_SECRET
	// instead of env. Required-field validation happens in rootPreRunE.
	if err := rootArgs.loadEnv(context.Background()); err != nil {
		log.FromContext(context.Background()).Error(err, "Failed to load worker config from environment")
	}
	rootArgs.normalizeURL()

	// Default provider types: every built-in bridge worker, comma-separated.
	// Sort for deterministic flag-default output (relevant to "docgen command").
	if rootArgs.WorkerProviderTypes == "" {
		var providers []string
		for wt := range availableBridgeWorkers {
			providers = append(providers, wt)
		}
		sort.Strings(providers)
		rootArgs.WorkerProviderTypes = strings.Join(providers, ",")
	}

	// Define flags. The current value of each rootArgs field (loaded from env
	// or defaulted above) is used as the flag default, so passing a flag
	// overrides the corresponding environment variable.
	rootCmd.PersistentFlags().StringVarP(&rootArgs.ConfigHubURL, "url", "u", rootArgs.ConfigHubURL, "ConfigHub Server URL (CONFIGHUB_URL)")
	rootCmd.PersistentFlags().StringVar(&rootArgs.MainPort, "main-port", rootArgs.MainPort, "ConfigHub Main Port (extracted from CONFIGHUB_URL by default)")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.WorkerPort, "worker-port", "p", rootArgs.WorkerPort, "ConfigHub Worker Port (CONFIGHUB_WORKER_PORT)")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.WorkerID, "worker-id", "w", rootArgs.WorkerID, "Worker ID (CONFIGHUB_WORKER_ID)")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.WorkerSecret, "worker-secret", "s", rootArgs.WorkerSecret, "Worker Secret (CONFIGHUB_WORKER_SECRET)")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.WorkerProviderTypes, "provider-types", "t", rootArgs.WorkerProviderTypes, "Comma-separated list of provider types (CONFIGHUB_WORKER_PROVIDER_TYPES)")
	rootCmd.PersistentFlags().IntVar(&rootArgs.GracePeriodDelay, "grace-period-delay", rootArgs.GracePeriodDelay, "Delay in seconds after receiving SIGTERM before starting shutdown (GRACE_PERIOD_DELAY)")
}

// These are lowercase to make the provider type matching case insensitive
const (
	LowerProviderTypeConfigHub         = "confighub"
	LowerProviderTypeKubernetes        = "kubernetes"
	LowerProviderTypeFluxOCI           = "fluxoci"
	LowerProviderTypeFluxRenderer      = "fluxrenderer"
	LowerProviderTypeConfigMapRenderer = "configmaprenderer"
	LowerProviderTypeArgoCDRenderer    = "argocdrenderer"
	LowerProviderTypeArgoCDOCI         = "argocdoci"
)

// Note: ConfigHub bridge worker needs to be initialized with a client in rootRunE

var availableBridgeWorkers = map[string]api.BridgeWorker{
	LowerProviderTypeConfigHub:         confighub.NewConfigHubBridgeWorker(),
	LowerProviderTypeKubernetes:        kubernetes.NewKubernetesBridgeWorker(),
	LowerProviderTypeFluxRenderer:      fluxrenderer.NewFluxRendererWorker(),
	LowerProviderTypeConfigMapRenderer: configmap.NewConfigMapBridgeWorker(),
	LowerProviderTypeArgoCDRenderer:    argocdrenderer.NewArgoCDRendererWorker(),
	// ArgoCDOCI worker is initialized in rootRunE with credentials (like ConfigHub worker)
	LowerProviderTypeArgoCDOCI: nil, // placeholder, initialized in rootRunE
}

func rootPreRunE(cmd *cobra.Command, args []string) error {
	var missing []string
	if rootArgs.WorkerID == "" {
		missing = append(missing, "--worker-id (CONFIGHUB_WORKER_ID)")
	}
	if rootArgs.WorkerSecret == "" {
		missing = append(missing, "--worker-secret (CONFIGHUB_WORKER_SECRET)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("required: %s", strings.Join(missing, ", "))
	}
	return nil
}

func newHTTPServer(w *lib.Worker, livenessThreshold time.Duration) *echo.Echo {
	rootRouter := echo.New()
	rootRouter.HideBanner = true
	internalRouter := rootRouter.Group("/internal")
	internalRouter.GET("/metrics", echoprometheus.NewHandler())

	// Default route /debug/pprof/*
	// Example: go tool pprof http://localhost:9092/internal/pprof/heap
	pprof.Register(rootRouter, "/internal/pprof")
	internalRouter.GET("/routes", listRoutes(rootRouter))
	internalRouter.GET("/ok", livenessHandler(w, livenessThreshold))
	internalRouter.GET("/ready", readinessHandler(w, livenessThreshold))

	return rootRouter
}

func listRoutes(router *echo.Echo) echo.HandlerFunc {
	return func(c echo.Context) error {
		routes := router.Routes()
		slices.SortFunc(routes, func(a, b *echo.Route) int {
			const noRoute = "echo_route_not_found"
			if a.Method == noRoute && b.Method != noRoute {
				return 1
			}
			if a.Method != noRoute && b.Method == noRoute {
				return -1
			}
			r := cmp.Compare(a.Path, b.Path)
			if r != 0 {
				return r
			}
			return cmp.Compare(a.Method, b.Method)
		})
		return c.JSON(http.StatusOK, routes)
	}
}

type probeOK struct {
	LastEventHandledAt time.Time `description:"Time at which the worker most recently handled an event from the ConfigHub server's stream."`
}

type probeError struct {
	Error              string    `description:"Human-readable explanation of why the probe failed."`
	LastEventHandledAt time.Time `description:"Time at which the worker most recently handled an event from the ConfigHub server's stream. Zero value if the worker has not yet handled any events."`
}

// eventStaleError returns a probeError describing a stale-event condition if
// the worker has not handled an event within livenessThreshold, or nil if the
// worker is current. The zero LastEventHandledAt (worker never reached the
// stream loop) is treated as stale.
func eventStaleError(w *lib.Worker, livenessThreshold time.Duration) *probeError {
	last := w.LastEventHandledAt()
	if last.IsZero() {
		return &probeError{
			Error: "no event has been handled yet",
		}
	}
	if age := time.Since(last); age > livenessThreshold {
		return &probeError{
			Error:              fmt.Sprintf("last event was handled %s ago at %s, exceeds threshold %s", age.Round(time.Second), last.Format(time.RFC3339), livenessThreshold),
			LastEventHandledAt: last,
		}
	}
	return nil
}

func livenessHandler(w *lib.Worker, livenessThreshold time.Duration) echo.HandlerFunc {
	return func(c echo.Context) error {
		if probeErr := eventStaleError(w, livenessThreshold); probeErr != nil {
			return c.JSON(http.StatusServiceUnavailable, probeErr)
		}
		return c.JSON(http.StatusOK, probeOK{LastEventHandledAt: w.LastEventHandledAt()})
	}
}

func readinessHandler(w *lib.Worker, livenessThreshold time.Duration) echo.HandlerFunc {
	return func(c echo.Context) error {
		if !w.IsServing() {
			return c.JSON(http.StatusServiceUnavailable, probeError{
				Error:              "worker is not serving",
				LastEventHandledAt: w.LastEventHandledAt(),
			})
		}
		if probeErr := eventStaleError(w, livenessThreshold); probeErr != nil {
			return c.JSON(http.StatusServiceUnavailable, probeErr)
		}
		return c.JSON(http.StatusOK, probeOK{LastEventHandledAt: w.LastEventHandledAt()})
	}
}

func rootRunE(cmd *cobra.Command, args []string) error {
	workerProviderTypesStr := strings.ToLower(rootArgs.WorkerProviderTypes)
	if len(args) > 0 {
		// Override provider types
		workerProviderTypesStr = strings.ToLower(args[0])
	}

	// Initialize frontdoor client
	frontdoorClient := lib.NewWorkerFrontdoorClient(rootArgs.ConfigHubURL, rootArgs.MainPort, rootArgs.WorkerID, rootArgs.WorkerSecret)
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
			directBridgeWorker = argocd.NewArgoCDOCIWorker(rootArgs.WorkerID, rootArgs.WorkerSecret)
			ok = true
		} else if lowerProviderType == LowerProviderTypeFluxOCI {
			directBridgeWorker = flux.NewFluxOCIWorker(rootArgs.WorkerID, rootArgs.WorkerSecret)
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

		// Register bridges directly
		for _, toolchainType := range toolchainTypes {
			bridgeDispatcher.RegisterWorker(toolchainType, providerType, directBridgeWorker)
			log.FromContext(context.Background()).Info("Registered bridge worker",
				"toolchainType", toolchainType,
				"providerType", providerType)
		}
	}

	// Create a context that will be cancelled on SIGTERM or SIGINT
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown - only trap SIGTERM
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM)

	// Check if the URL already contains a port
	parsedURL, err := neturl.Parse(rootArgs.ConfigHubURL)
	if err != nil {
		// Handle potential parsing error, though init() should prevent this
		log.FromContext(ctx).Error(err, "Failed to parse configHubURL", "url", rootArgs.ConfigHubURL)
		// Decide on fallback behavior, e.g., use default or return error
		// For now, let's proceed with the potentially malformed URL, assuming init handled basics
	}

	// Two URLs:
	//   - h2c stream transport needs the worker-port URL (separate h2c port)
	//   - long-poll transport needs the main API URL (no port switch)
	streamURL := rootArgs.ConfigHubURL
	mainURL := rootArgs.ConfigHubURL

	if err == nil { // Only proceed if parsing was successful
		hostname := parsedURL.Hostname() // Get hostname without port
		if hostname == "" {
			log.FromContext(ctx).Info("Could not extract hostname from URL, not modifying port", "url", rootArgs.ConfigHubURL)
		} else if parsedURL.Scheme == "" {
			log.FromContext(ctx).Info("URL scheme is missing, cannot reliably reconstruct URL with new port", "url", rootArgs.ConfigHubURL)
		} else {
			streamURL = fmt.Sprintf("%s://%s:%s", parsedURL.Scheme, hostname, rootArgs.WorkerPort)
			mainURL = fmt.Sprintf("%s://%s:%s", parsedURL.Scheme, hostname, rootArgs.MainPort)
		}
	}

	transportType := lib.TransportType(rootArgs.WorkerTransport)
	if transportType == "" {
		transportType = lib.TransportHTTP2Stream
	}
	finalURL := streamURL
	if transportType == lib.TransportLongPoll {
		finalURL = mainURL
	}

	metricsProvider, err := lib.NewPrometheusProvider()
	if err != nil {
		return fmt.Errorf("failed to instantiate prometheus provider: %w", err)
	}

	metricsMeter := metricsProvider.Meter("confighub-worker")

	w := lib.New(finalURL,
		rootArgs.WorkerID,
		rootArgs.WorkerSecret).
		WithBridgeWorker(bridgeDispatcher).
		WithFunctionExecutor(newFunctionExecutor(providerTypes, workerFunctions)).
		WithMetricsMeter(metricsMeter).
		WithTransport(transportType)

	var httpServer *echo.Echo
	if rootArgs.HTTPServerPort != "" {
		livenessThreshold := time.Duration(rootArgs.LivenessEventThresholdSeconds) * time.Second
		httpServer = newHTTPServer(w, livenessThreshold)
	}

	// Start worker in a errgroup with the http server so we can handle signals
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return w.Start(ctx)
	})
	if httpServer != nil {
		eg.Go(func() error {
			if startErr := httpServer.Start(":" + rootArgs.HTTPServerPort); startErr != nil && !errors.Is(startErr, http.ErrServerClosed) {
				return errors.Wrap(startErr, "HTTP server unexpected failure")
			}
			return nil
		})
	}

	sig := <-sigChan
	log.FromContext(ctx).Info("Received signal, initiating graceful shutdown", "signal", sig)

	// Sleep for configured delay to allow new pod to become active
	// This ensures smooth handoff in rolling updates with our standby promotion system
	if rootArgs.GracePeriodDelay > 0 {
		log.FromContext(ctx).Info("Waiting for smooth handoff to new pod...", "delay_seconds", rootArgs.GracePeriodDelay)
		time.Sleep(time.Duration(rootArgs.GracePeriodDelay) * time.Second)
	}

	// Wait for all pending operations to complete first
	// This ensures Apply/Destroy/etc operations fully complete including sending final status
	w.WaitForPendingOperations()

	// Now cancel context to stop accepting new work and close SSE stream
	cancel()

	if httpServer != nil {
		serverShutdownTimeout := defaultHTTPServerShutdownTimeout
		if rootArgs.ServerShutdownTimeoutSeconds > 0 {
			serverShutdownTimeout = time.Duration(rootArgs.ServerShutdownTimeoutSeconds) * time.Second
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
