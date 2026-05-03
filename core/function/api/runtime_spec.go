// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

// RuntimeSpec describes how a binary behaves at runtime: which ports it
// listens on, which filesystem paths it reads or writes, and which HTTP
// endpoints expose probes. It is intended to give an AI agent (or a human
// operator) enough information to construct a valid Kubernetes Deployment
// for the binary, in combination with the binary's environment-variable
// schema.
//
// YAML field names are PascalCase to make the spec read like a domain
// document rather than a serialization-of-a-Go-struct.
type RuntimeSpec struct {
	Ports  []Port  `yaml:"Ports,omitempty"`
	Paths  []Path  `yaml:"Paths,omitempty"`
	Probes []Probe `yaml:"Probes,omitempty"`
}

// ValueSource describes one location from which a parameterized value can
// be read at startup. A field that lists multiple Sources evaluates them
// in the listed order; the first source that yields a value wins, and
// the field's Default is used only when no source resolves. This is
// analogous to Kubernetes' env.valueFrom but extended to flags and
// config-file keys, with explicit priority.
type ValueSource struct {
	// Type is the kind of source. One of "Env", "Flag", "ConfigFile".
	Type string `yaml:"Type"`
	// EnvVar is the environment variable name when Type == "Env".
	EnvVar string `yaml:"EnvVar,omitempty"`
	// Flag is the long flag name (without leading dashes) when Type == "Flag".
	Flag string `yaml:"Flag,omitempty"`
	// ConfigPath is the path to the config file when Type == "ConfigFile".
	ConfigPath string `yaml:"ConfigPath,omitempty"`
	// ConfigKey is the key within the config file when Type == "ConfigFile".
	// Use dotted notation for nested keys (e.g. "server.port").
	ConfigKey string `yaml:"ConfigKey,omitempty"`
}

// Port describes a network port the binary listens on.
type Port struct {
	// Name is a stable identifier for this port. Probes reference it by
	// name. It is also a good candidate for containers[].ports[].name.
	Name string `yaml:"Name"`
	// Description is a human-readable summary of what is served on this port.
	Description string `yaml:"Description,omitempty"`
	// Default is the default port number used when no Source resolves.
	// Zero (or unset) means there is no default and the port is bound
	// only when one of Sources resolves.
	Default int `yaml:"Default,omitempty"`
	// Protocol is "TCP" (default) or "UDP".
	Protocol string `yaml:"Protocol,omitempty"`
	// Sources, if non-empty, lists locations from which the port number
	// may be overridden. Evaluated in priority order; the first hit wins.
	Sources []ValueSource `yaml:"Sources,omitempty"`
}

// Path describes a filesystem path the binary reads from or writes to.
// AI agents should translate writable paths into volumeMounts (typically
// emptyDir for ephemeral, PersistentVolumeClaim for persistent).
type Path struct {
	// Name is a stable identifier suitable for use as a volume name.
	Name string `yaml:"Name"`
	// Description explains what is stored at the path.
	Description string `yaml:"Description,omitempty"`
	// Path is the absolute filesystem path inside the container.
	Path string `yaml:"Path"`
	// Access is "Read", "Write", or "ReadWrite".
	Access string `yaml:"Access"`
	// Persistence is "Ephemeral" (lifetime of the pod, emptyDir is fine)
	// or "Persistent" (must survive pod restart, requires a PVC). Defaults
	// to "Ephemeral" when omitted.
	Persistence string `yaml:"Persistence,omitempty"`
	// Sources, if non-empty, lists locations from which the path may be
	// overridden. Evaluated in priority order; the first hit wins.
	Sources []ValueSource `yaml:"Sources,omitempty"`
}

// Probe describes a health-check endpoint exposed by the binary.
type Probe struct {
	// Name is a stable identifier for this probe.
	Name string `yaml:"Name"`
	// Type is "Liveness", "Readiness", or "Startup".
	Type string `yaml:"Type"`
	// HTTPGet describes the HTTP endpoint to probe.
	HTTPGet *HTTPGetAction `yaml:"HTTPGet,omitempty"`
}

// HTTPGetAction describes an HTTP GET probe. It maps directly to
// Kubernetes' httpGet probe action.
type HTTPGetAction struct {
	// Path is the URL path on the server (e.g. "/internal/ok").
	Path string `yaml:"Path"`
	// PortName is the Name of the Port this probe targets.
	PortName string `yaml:"PortName"`
	// Scheme is "HTTP" (default) or "HTTPS".
	Scheme string `yaml:"Scheme,omitempty"`
}
