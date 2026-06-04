// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package upload

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// AppConfig annotation keys — kept in sync with the installer's render pre-pass
// (installer/internal/upload/appconfig.go) and installer/internal/cli/appconfig.go.
// A rendered ConfigMap carrying these annotations is not uploaded as a plain
// Kubernetes/YAML Unit; instead it is expanded into an AppConfig data Unit, a
// render-configmap Invocation, a placeholder Unit, and an Upsert link.
const (
	annoToolchain = "installer.confighub.com/toolchain"
	annoMode      = "installer.confighub.com/appconfig-mode"
	annoSourceKey = "installer.confighub.com/appconfig-source-key"
	annoMutable   = "installer.confighub.com/appconfig-mutable"

	appConfigModeFile = "file"
	appConfigModeEnv  = "env"

	// AppConfigToolchainEnv is the only AppConfig toolchain where the
	// as-key-value rendering option is meaningful (envFrom injection).
	AppConfigToolchainEnv = "AppConfig/Env"
)

// AppConfigManifest describes one annotated ConfigMap discovered in a rendered
// manifest stream. DetectAppConfigManifest fills it in from a single YAML
// document; callers use it to drive the AppConfig Unit / render-configmap
// Invocation / placeholder Unit creation.
type AppConfigManifest struct {
	// CarrierName is metadata.name of the rendered ConfigMap.
	CarrierName string
	// CarrierNamespace is metadata.namespace (may be empty).
	CarrierNamespace string
	// Toolchain is installer.confighub.com/toolchain (e.g. AppConfig/Properties).
	Toolchain string
	// Mode is appconfig-mode (file|env).
	Mode string
	// SourceKey is the data: key holding the raw file body (file mode only).
	SourceKey string
	// Mutable: when true the render-configmap Invocation uses --immutable=false
	// (stable name, in-place updates); when false (the kustomize default) it
	// uses --immutable=true (hashed names).
	Mutable bool
	// Content is the raw AppConfig file body: file mode reads data[SourceKey]
	// verbatim; env mode emits a `.env`-shaped doc from data: in sorted order.
	Content []byte
}

// DetectAppConfigManifest inspects one parsed YAML document and returns an
// AppConfigManifest only when it is a ConfigMap carrying
// installer.confighub.com/toolchain (with mode + source-key injected by the
// render pre-pass). Returns nil for every other document so callers branch with
// `if appCfg != nil`.
func DetectAppConfigManifest(doc []byte) (*AppConfigManifest, error) {
	var shape struct {
		Kind     string `yaml:"kind"`
		Metadata struct {
			Name        string            `yaml:"name"`
			Namespace   string            `yaml:"namespace"`
			Annotations map[string]string `yaml:"annotations"`
		} `yaml:"metadata"`
		Data map[string]string `yaml:"data"`
	}
	if err := yaml.Unmarshal(doc, &shape); err != nil {
		// Not parseable as a structured ConfigMap — let the normal path handle it.
		return nil, nil
	}
	if shape.Kind != "ConfigMap" {
		return nil, nil
	}
	toolchain := shape.Metadata.Annotations[annoToolchain]
	if toolchain == "" {
		return nil, nil
	}
	if !strings.HasPrefix(toolchain, "AppConfig/") {
		return nil, fmt.Errorf("ConfigMap %q: %s=%q must be an AppConfig/* toolchain",
			shape.Metadata.Name, annoToolchain, toolchain)
	}
	mode := shape.Metadata.Annotations[annoMode]
	if mode == "" {
		return nil, fmt.Errorf("ConfigMap %q: %s missing — re-render so the transformer injects it",
			shape.Metadata.Name, annoMode)
	}
	sourceKey := shape.Metadata.Annotations[annoSourceKey]
	mutable, err := parseMutable(shape.Metadata.Annotations[annoMutable])
	if err != nil {
		return nil, fmt.Errorf("ConfigMap %q: %w", shape.Metadata.Name, err)
	}

	m := &AppConfigManifest{
		CarrierName:      shape.Metadata.Name,
		CarrierNamespace: shape.Metadata.Namespace,
		Toolchain:        toolchain,
		Mode:             mode,
		SourceKey:        sourceKey,
		Mutable:          mutable,
	}
	switch mode {
	case appConfigModeFile:
		if sourceKey == "" {
			return nil, fmt.Errorf("ConfigMap %q: file mode requires %s annotation", shape.Metadata.Name, annoSourceKey)
		}
		body, ok := shape.Data[sourceKey]
		if !ok {
			return nil, fmt.Errorf("ConfigMap %q: %s=%q references missing data: key", shape.Metadata.Name, annoSourceKey, sourceKey)
		}
		m.Content = []byte(body)
	case appConfigModeEnv:
		keys := make([]string, 0, len(shape.Data))
		for k := range shape.Data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&b, "%s=%s\n", k, shape.Data[k])
		}
		m.Content = []byte(b.String())
	default:
		return nil, fmt.Errorf("ConfigMap %q: %s=%q must be %q or %q", shape.Metadata.Name, annoMode, mode, appConfigModeFile, appConfigModeEnv)
	}
	return m, nil
}

// UnitSlug is the slug for the AppConfig data Unit. It equals the carrier
// ConfigMap's name because the rendered ConfigMap uses the Unit slug as its
// metadata.name, so workloads referencing the ConfigMap by name resolve to it.
func (m *AppConfigManifest) UnitSlug() string { return m.CarrierName }

// PlaceholderSlug is the slug for the placeholder Kubernetes/YAML ConfigMap Unit
// the Upsert link populates. The "-rendered" suffix avoids colliding with
// UnitSlug in the same Space.
func (m *AppConfigManifest) PlaceholderSlug() string { return m.CarrierName + "-rendered" }

// InvocationSlug is the slug for the render-configmap Invocation referenced as
// the Upsert link's TransformInvocation.
func (m *AppConfigManifest) InvocationSlug() string { return m.CarrierName + "-render" }

// RenderConfigMapArgs returns the function-argument tokens for the
// render-configmap Invocation: --immutable, and --as-key-value only for the
// env-mode AppConfig/Env toolchain where it is meaningful.
func (m *AppConfigManifest) RenderConfigMapArgs() []string {
	args := []string{fmt.Sprintf("--immutable=%t", !m.Mutable)}
	if m.Mode == appConfigModeEnv && m.Toolchain == AppConfigToolchainEnv {
		args = append(args, "--as-key-value=true")
	}
	return args
}

// parseMutable parses the appconfig-mutable annotation: missing → false
// (immutable, the kustomize default); "true"/"false" → as written; anything
// else is rejected so a typo doesn't silently degrade to immutable.
func parseMutable(v string) (bool, error) {
	switch v {
	case "":
		return false, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%s=%q must be \"true\" or \"false\"", annoMutable, v)
	}
}
