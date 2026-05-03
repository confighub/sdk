// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/confighub/sdk/core/function/api"
)

// cubWorkerRuntimeSpec is the runtime spec for cub-worker-run.
//
// The worker has one optional HTTP listener (metrics + probes), enabled
// when CONFIGHUB_WORKER_HTTP_SERVER_PORT is set. When enabled,
// /internal/ok and /internal/ready serve the liveness and readiness
// probes.
//
// The worker writes scratch files under /tmp. When the container runs
// with a read-only root filesystem, /tmp must be backed by a writable
// volume (emptyDir is sufficient).
var cubWorkerRuntimeSpec = api.RuntimeSpec{
	Ports: []api.Port{
		{
			Name:        "http",
			Description: "Local HTTP server. Bound only when CONFIGHUB_WORKER_HTTP_SERVER_PORT is set. Serves /internal/metrics (Prometheus), /internal/pprof/* (pprof), /internal/routes (route listing), /internal/ok (liveness), and /internal/ready (readiness).",
			Protocol:    "TCP",
			Sources: []api.ValueSource{
				{Type: "Env", EnvVar: "CONFIGHUB_WORKER_HTTP_SERVER_PORT"},
			},
		},
	},
	Paths: []api.Path{
		{
			Name:        "tmp",
			Description: "Scratch space for schema caching and transient work. Required to be writable; safe to back with emptyDir.",
			Path:        "/tmp",
			Access:      "ReadWrite",
			Persistence: "Ephemeral",
		},
	},
	Probes: []api.Probe{
		{
			Name: "liveness",
			Type: "Liveness",
			HTTPGet: &api.HTTPGetAction{
				Path:     "/internal/ok",
				PortName: "http",
			},
		},
		{
			Name: "readiness",
			Type: "Readiness",
			HTTPGet: &api.HTTPGetAction{
				Path:     "/internal/ready",
				PortName: "http",
			},
		},
	},
}

// printRuntimeSpec writes the runtime spec to w as YAML.
func printRuntimeSpec(w io.Writer, spec api.RuntimeSpec) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(spec); err != nil {
		return fmt.Errorf("encode runtime spec: %w", err)
	}
	return enc.Close()
}
