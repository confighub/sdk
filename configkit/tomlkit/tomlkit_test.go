// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package tomlkit

import (
	"strings"
	"testing"
)

func TestTOMLToYAML(t *testing.T) {
	tomlData := []byte(`
[database]
server = "192.168.1.1"
port = 5432
enabled = true

[servers]

  [servers.alpha]
  ip = "10.0.0.1"
  dc = "eqdc10"

  [servers.beta]
  ip = "10.0.0.2"
  dc = "eqdc10"
`)

	yamlData, err := NewTOMLResourceProvider().NativeToYAML(tomlData)
	if err != nil {
		t.Fatalf("Failed to convert TOML to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	if !strings.Contains(yamlStr, "database:") {
		t.Errorf("Expected YAML to contain 'database:', got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "servers:") {
		t.Errorf("Expected YAML to contain 'servers:', got: %s", yamlStr)
	}
}

func TestYAMLToTOML(t *testing.T) {
	yamlData := []byte(`
database:
  server: 192.168.1.1
  port: 5432
  enabled: true
servers:
  alpha:
    ip: 10.0.0.1
    dc: eqdc10
  beta:
    ip: 10.0.0.2
    dc: eqdc10
`)

	tomlData, err := NewTOMLResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed to convert YAML to TOML: %v", err)
	}

	tomlStr := string(tomlData)
	if !strings.Contains(tomlStr, "[database]") {
		t.Errorf("Expected TOML to contain '[database]', got: %s", tomlStr)
	}
	if !strings.Contains(tomlStr, "[servers") {
		t.Errorf("Expected TOML to contain '[servers', got: %s", tomlStr)
	}
}

func TestEmptyTOML(t *testing.T) {
	yamlData, err := NewTOMLResourceProvider().NativeToYAML([]byte{})
	if err != nil {
		t.Fatalf("Failed on empty input: %v", err)
	}
	if len(yamlData) != 0 {
		t.Errorf("Expected empty output for empty input, got: %v", yamlData)
	}
}

func TestEmptyYAML(t *testing.T) {
	tomlData, err := NewTOMLResourceProvider().YAMLToNative([]byte{})
	if err != nil {
		t.Fatalf("Failed on empty input: %v", err)
	}
	if len(tomlData) != 0 {
		t.Errorf("Expected empty output for empty input, got: %v", tomlData)
	}
}

func TestRoundTrip(t *testing.T) {
	originalTOML := []byte(`
[app]
name = "MyApp"
version = "1.0.0"
`)

	// TOML -> YAML -> TOML
	yamlData, err := NewTOMLResourceProvider().NativeToYAML(originalTOML)
	if err != nil {
		t.Fatalf("Failed TOML to YAML conversion: %v", err)
	}

	tomlData, err := NewTOMLResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed YAML to TOML conversion: %v", err)
	}

	tomlStr := string(tomlData)
	if !strings.Contains(tomlStr, "[app]") {
		t.Errorf("Round trip lost [app] section")
	}
	if !strings.Contains(tomlStr, "name") {
		t.Errorf("Round trip lost name field")
	}
	if !strings.Contains(tomlStr, "MyApp") {
		t.Errorf("Round trip lost MyApp value")
	}
}

func TestTOMLWithArrays(t *testing.T) {
	tomlData := []byte(`
[app]
features = ["authentication", "logging"]
name = "MyApplication"
version = "1.0.0"
`)

	yamlData, err := NewTOMLResourceProvider().NativeToYAML(tomlData)
	if err != nil {
		t.Fatalf("Failed to convert TOML with arrays to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	if !strings.Contains(yamlStr, "features:") {
		t.Errorf("Expected YAML to contain 'features:', got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "authentication") {
		t.Errorf("Expected YAML to contain 'authentication', got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "logging") {
		t.Errorf("Expected YAML to contain 'logging', got: %s", yamlStr)
	}
}

func TestTOMLWithNestedSections(t *testing.T) {
	tomlData := []byte(`
[configHub]
configName = "MyApplicationConfig"
configSchema = "SimpleApp"

[configHub.kubernetes]
namespace = "confighubplaceholder"

[database]
host = "localhost"
port = 5432

[database.ssl]
enabled = true
`)

	yamlData, err := NewTOMLResourceProvider().NativeToYAML(tomlData)
	if err != nil {
		t.Fatalf("Failed to convert nested TOML to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	if !strings.Contains(yamlStr, "configHub:") {
		t.Errorf("Expected YAML to contain 'configHub:', got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "kubernetes:") {
		t.Errorf("Expected YAML to contain 'kubernetes:', got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "namespace:") {
		t.Errorf("Expected YAML to contain 'namespace:', got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "database:") {
		t.Errorf("Expected YAML to contain 'database:', got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "ssl:") {
		t.Errorf("Expected YAML to contain 'ssl:', got: %s", yamlStr)
	}
}

func TestYAMLWithArraysToTOML(t *testing.T) {
	yamlData := []byte(`
app:
  features:
    - authentication
    - logging
  name: MyApplication
  version: 1.0.0
`)

	tomlData, err := NewTOMLResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed to convert YAML with arrays to TOML: %v", err)
	}

	tomlStr := string(tomlData)
	if !strings.Contains(tomlStr, "[app]") {
		t.Errorf("Expected TOML to contain '[app]', got: %s", tomlStr)
	}
	if !strings.Contains(tomlStr, "features") {
		t.Errorf("Expected TOML to contain 'features', got: %s", tomlStr)
	}
	if !strings.Contains(tomlStr, "authentication") {
		t.Errorf("Expected TOML to contain 'authentication', got: %s", tomlStr)
	}
}

func TestTOMLWithMixedTypes(t *testing.T) {
	tomlData := []byte(`
[database]
host = "localhost"
port = 5432
ssl_enabled = true
max_connections = 100
description = "Primary database"
`)

	yamlData, err := NewTOMLResourceProvider().NativeToYAML(tomlData)
	if err != nil {
		t.Fatalf("Failed to convert TOML with mixed types to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	// Verify the structure and types are preserved
	if !strings.Contains(yamlStr, "database:") {
		t.Errorf("Expected YAML to contain 'database:', got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "host:") {
		t.Errorf("Expected YAML to contain 'host:', got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "port: 5432") {
		t.Errorf("Expected YAML to contain 'port: 5432' as int, got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "ssl_enabled: true") {
		t.Errorf("Expected YAML to contain 'ssl_enabled: true' as bool, got: %s", yamlStr)
	}
}
