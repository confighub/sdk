// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	neturl "net/url"

	"github.com/sethvargo/go-envconfig"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"github.com/swaggest/jsonschema-go"
)

// ConfigHubWorkerArgs is the configuration for the worker process.
//
// The worker is configured primarily via environment variables. Command-line
// flags exist for testing and, when supplied, override the corresponding
// environment variable. The flags are not part of the worker invocation
// contract used by `cub worker install`, `cub worker run`, and
// `cub worker get-envs`.
//
// Field tags:
//   - `env`     populated by github.com/sethvargo/go-envconfig
//   - `json`    used as the property name in the env-var JSON schema
//   - `required`/`description` consumed by github.com/swaggest/jsonschema-go
type ConfigHubWorkerArgs struct {
	WorkerID     string `env:"CONFIGHUB_WORKER_ID" json:"CONFIGHUB_WORKER_ID" required:"true" description:"Worker identifier."`
	WorkerSecret string `env:"CONFIGHUB_WORKER_SECRET" json:"CONFIGHUB_WORKER_SECRET" required:"true" description:"Worker shared secret."`

	WorkerProviderTypes string `env:"CONFIGHUB_WORKER_PROVIDER_TYPES" json:"CONFIGHUB_WORKER_PROVIDER_TYPES,omitempty" description:"Comma-separated, case-insensitive list of provider types to serve (e.g. \"kubernetes,configmaprenderer\"). Defaults to all built-in provider types."`
	WorkerFunctions     string `env:"CONFIGHUB_WORKER_FUNCTIONS" json:"CONFIGHUB_WORKER_FUNCTIONS,omitempty" description:"Comma-separated list of additional worker function names to register (e.g. \"vet-kyverno-server,vet-opa-gatekeeper\")."`

	ConfigHubURL string `env:"CONFIGHUB_URL, default=https://hub.confighub.com" json:"CONFIGHUB_URL" description:"Base URL (scheme and host) of the ConfigHub API."`
	WorkerPort   string `env:"CONFIGHUB_WORKER_PORT, default=443" json:"CONFIGHUB_WORKER_PORT" description:"Port for the worker's HTTP/2 connection to ConfigHub."`

	HTTPServerEnabled             string `env:"CONFIGHUB_WORKER_HTTP_SERVER_ENABLED" json:"CONFIGHUB_WORKER_HTTP_SERVER_ENABLED,omitempty" description:"When set to a non-empty value other than \"0\", enables an HTTP server with a Prometheus metrics endpoint at /internal/metrics."`
	HTTPServerPort                string `env:"CONFIGHUB_WORKER_HTTP_SERVER_PORT, default=9092" json:"CONFIGHUB_WORKER_HTTP_SERVER_PORT" description:"Port for the worker's local HTTP server (currently only exposes Prometheus metrics)."`
	ServerShutdownTimeoutSeconds  int    `env:"CONFIGHUB_WORKER_SERVER_SHUTDOWN_TIMEOUT, default=5" json:"CONFIGHUB_WORKER_SERVER_SHUTDOWN_TIMEOUT" description:"Number of seconds the local HTTP server is given to shut down gracefully."`
	LivenessEventThresholdSeconds int    `env:"CONFIGHUB_WORKER_LIVENESS_EVENT_THRESHOLD, default=45" json:"CONFIGHUB_WORKER_LIVENESS_EVENT_THRESHOLD" description:"Maximum age in seconds of the most recent handled event before /internal/ok and /internal/ready report unhealthy. The backend sends heartbeats every 15s and keepalives every 30s, so the default of 45s tolerates one missed tick."`

	StandardFunctions string `env:"CONFIGHUB_STANDARD_FUNCTIONS" json:"CONFIGHUB_STANDARD_FUNCTIONS,omitempty" description:"When set to \"1\", \"true\", or \"TRUE\", registers all standard functions for the active provider types' toolchains."`

	GracePeriodDelay int `env:"GRACE_PERIOD_DELAY, default=10" json:"GRACE_PERIOD_DELAY" description:"Seconds to wait after receiving SIGTERM before starting shutdown, to allow a replacement pod to take over."`

	// Derived from ConfigHubURL during normalization, exposed as a flag for testing.
	MainPort string `json:"-"`
}

// loadEnv populates args from environment variables using go-envconfig.
// Required-ness of CONFIGHUB_WORKER_ID and CONFIGHUB_WORKER_SECRET is enforced
// later, after flag parsing, so that flags can supply them and so that the
// docgen command works without any env vars set.
func (a *ConfigHubWorkerArgs) loadEnv(ctx context.Context) error {
	return envconfig.Process(ctx, a)
}

// normalizeURL parses ConfigHubURL, splits any port out into MainPort, and
// rewrites ConfigHubURL to scheme+host only. Falls back to the default URL on
// parse failure. MainPort defaults to "443" if unset.
func (a *ConfigHubWorkerArgs) normalizeURL() {
	if a.MainPort == "" {
		a.MainPort = defaultMainPort
	}
	if a.ConfigHubURL == "" {
		a.ConfigHubURL = defaultConfighubURL
		return
	}
	parsed, err := neturl.Parse(a.ConfigHubURL)
	if err != nil {
		a.ConfigHubURL = defaultConfighubURL
		return
	}
	if parsed.Scheme == "" {
		parsed.Scheme = defaultConfighubScheme
	}
	if parsed.Host == "" {
		parsed.Host = defaultConfighubHost
	}
	if port := parsed.Port(); port != "" {
		a.MainPort = port
	}
	a.ConfigHubURL = parsed.Scheme + "://" + parsed.Hostname()
}

// printEnvSchema writes a JSON Schema describing the worker's environment
// variables to w.
func printEnvSchema(w io.Writer) error {
	reflector := jsonschema.Reflector{}
	schema, err := reflector.Reflect(ConfigHubWorkerArgs{})
	if err != nil {
		return fmt.Errorf("reflect ConfigHubWorkerArgs schema: %w", err)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(schema)
}

// printCommandDocs writes Cobra YAML documentation for cmd to w.
func printCommandDocs(cmd *cobra.Command, w io.Writer) error {
	return doc.GenYaml(cmd, w)
}
