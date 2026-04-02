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

func TestTOMLToYAMLWithComments(t *testing.T) {
	tomlData := []byte(`# Application settings
# Version 2.0

[database]
# Connection info
server = "192.168.1.1"  # main server
port = 5432
`)

	yamlData, err := NewTOMLResourceProvider().NativeToYAML(tomlData)
	if err != nil {
		t.Fatalf("Failed to convert TOML to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	t.Logf("YAML output:\n%s", yamlStr)

	// Head comment before [database] section should be preserved
	if !strings.Contains(yamlStr, "$comment$head$database") {
		t.Errorf("Expected YAML to contain head comment for database section")
	}
	if !strings.Contains(yamlStr, "Application settings") {
		t.Errorf("Expected YAML to contain 'Application settings' comment text")
	}
	// Head comment before server key
	if !strings.Contains(yamlStr, "$comment$head$server") {
		t.Errorf("Expected YAML to contain head comment for server key")
	}
	if !strings.Contains(yamlStr, "Connection info") {
		t.Errorf("Expected YAML to contain 'Connection info' comment text")
	}
	// Inline comment on server
	if !strings.Contains(yamlStr, "$comment$line$server") {
		t.Errorf("Expected YAML to contain line comment for server key")
	}
	if !strings.Contains(yamlStr, "main server") {
		t.Errorf("Expected YAML to contain 'main server' comment text")
	}
	// Data should still be present
	if !strings.Contains(yamlStr, "server:") {
		t.Errorf("Expected YAML to contain 'server:' data key")
	}
}

func TestYAMLToTOMLWithComments(t *testing.T) {
	yamlData := []byte(`$comment$head$database: Application settings
database:
  $comment$head$server: Connection info
  $comment$line$server: main server
  server: 192.168.1.1
  port: 5432
`)

	tomlData, err := NewTOMLResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed to convert YAML to TOML: %v", err)
	}

	tomlStr := string(tomlData)
	t.Logf("TOML output:\n%s", tomlStr)

	// Head comment before [database] section
	if !strings.Contains(tomlStr, "# Application settings") {
		t.Errorf("Expected TOML to contain '# Application settings'")
	}
	// Head comment before server key
	if !strings.Contains(tomlStr, "# Connection info") {
		t.Errorf("Expected TOML to contain '# Connection info'")
	}
	// Inline comment on server
	if !strings.Contains(tomlStr, "# main server") {
		t.Errorf("Expected TOML to contain '# main server' inline comment")
	}
	// No comment keys should remain in TOML output
	if strings.Contains(tomlStr, "$comment") {
		t.Errorf("TOML output should not contain $comment keys")
	}
	// Data should still be present
	if !strings.Contains(tomlStr, "[database]") {
		t.Errorf("Expected TOML to contain '[database]'")
	}
}

func TestRoundTripWithComments(t *testing.T) {
	originalTOML := []byte(`# Application configuration
# Manages app settings

[app]
# Application name
name = "MyApp" # short name
version = "1.0.0"
`)

	// TOML -> YAML
	yamlData, err := NewTOMLResourceProvider().NativeToYAML(originalTOML)
	if err != nil {
		t.Fatalf("Failed TOML to YAML conversion: %v", err)
	}
	t.Logf("YAML:\n%s", string(yamlData))

	// YAML -> TOML
	tomlData, err := NewTOMLResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed YAML to TOML conversion: %v", err)
	}
	t.Logf("TOML:\n%s", string(tomlData))

	tomlStr := string(tomlData)
	// Verify comments survived the round trip
	if !strings.Contains(tomlStr, "# Application configuration") {
		t.Errorf("Round trip lost head comment on [app] section")
	}
	if !strings.Contains(tomlStr, "# Manages app settings") {
		t.Errorf("Round trip lost second line of head comment")
	}
	if !strings.Contains(tomlStr, "# Application name") {
		t.Errorf("Round trip lost head comment on name key")
	}
	if !strings.Contains(tomlStr, "# short name") {
		t.Errorf("Round trip lost inline comment on name key")
	}
	// Verify data survived
	if !strings.Contains(tomlStr, "[app]") {
		t.Errorf("Round trip lost [app] section")
	}
	if !strings.Contains(tomlStr, "MyApp") {
		t.Errorf("Round trip lost MyApp value")
	}
}

func TestCommentsWithNestedSections(t *testing.T) {
	tomlData := []byte(`[database]
host = "localhost"

# SSL configuration
[database.ssl]
# Whether SSL is enabled
enabled = true
`)

	yamlData, err := NewTOMLResourceProvider().NativeToYAML(tomlData)
	if err != nil {
		t.Fatalf("Failed to convert TOML to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	t.Logf("YAML:\n%s", yamlStr)

	// Comment before [database.ssl] is stored as head comment on "ssl" inside "database" map
	if !strings.Contains(yamlStr, "$comment$head$ssl") {
		t.Errorf("Expected YAML to contain head comment for ssl key")
	}
	if !strings.Contains(yamlStr, "SSL configuration") {
		t.Errorf("Expected YAML to contain 'SSL configuration' comment text")
	}
	if !strings.Contains(yamlStr, "$comment$head$enabled") {
		t.Errorf("Expected YAML to contain head comment for enabled key")
	}

	// Round-trip back to TOML
	tomlResult, err := NewTOMLResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed YAML to TOML: %v", err)
	}
	tomlStr := string(tomlResult)
	t.Logf("TOML:\n%s", tomlStr)

	if !strings.Contains(tomlStr, "# SSL configuration") {
		t.Errorf("Round trip lost SSL configuration comment")
	}
	if !strings.Contains(tomlStr, "# Whether SSL is enabled") {
		t.Errorf("Round trip lost enabled comment")
	}
}

func TestCommentsWithTableArrays(t *testing.T) {
	tomlData := []byte(`# Server list

# First server
[[servers]]
name = "alpha"

# Second server
[[servers]]
name = "beta"
`)

	yamlData, err := NewTOMLResourceProvider().NativeToYAML(tomlData)
	if err != nil {
		t.Fatalf("Failed to convert TOML to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	t.Logf("YAML:\n%s", yamlStr)

	// Head comment on the servers key at root level
	if !strings.Contains(yamlStr, "$comment$head$servers") {
		t.Errorf("Expected YAML to contain head comment for servers key")
	}
	if !strings.Contains(yamlStr, "Server list") {
		t.Errorf("Expected YAML to contain 'Server list' comment text")
	}
	// Per-element comments inside each array element
	if !strings.Contains(yamlStr, "First server") {
		t.Errorf("Expected YAML to contain 'First server' comment text")
	}
	if !strings.Contains(yamlStr, "Second server") {
		t.Errorf("Expected YAML to contain 'Second server' comment text")
	}
	// Data
	if !strings.Contains(yamlStr, "alpha") {
		t.Errorf("Expected YAML to contain 'alpha'")
	}
	if !strings.Contains(yamlStr, "beta") {
		t.Errorf("Expected YAML to contain 'beta'")
	}
}

func TestNoCommentsUnchanged(t *testing.T) {
	// Existing data without comments should work exactly as before
	tomlData := []byte(`[app]
name = "MyApp"
version = "1.0.0"
`)

	yamlData, err := NewTOMLResourceProvider().NativeToYAML(tomlData)
	if err != nil {
		t.Fatalf("Failed to convert TOML to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	if strings.Contains(yamlStr, "$comment") {
		t.Errorf("Expected no comment keys in output for uncommented TOML")
	}
	if !strings.Contains(yamlStr, "name:") {
		t.Errorf("Expected YAML to contain 'name:'")
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
