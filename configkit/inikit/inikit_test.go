// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package inikit

import (
	"strings"
	"testing"
)

func TestINIToYAML(t *testing.T) {
	iniData := []byte(`
[database]
server = 192.168.1.1
port = 5432
enabled = true

[server]
ip = 10.0.0.1
dc = eqdc10
`)

	yamlData, err := NewINIResourceProvider().NativeToYAML(iniData)
	if err != nil {
		t.Fatalf("Failed to convert INI to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	if !strings.Contains(yamlStr, "database:") {
		t.Errorf("Expected YAML to contain 'database:', got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "server:") {
		t.Errorf("Expected YAML to contain 'server:', got: %s", yamlStr)
	}
}

func TestYAMLToINI(t *testing.T) {
	yamlData := []byte(`
database:
  server: 192.168.1.1
  port: 5432
  enabled: true
server:
  ip: 10.0.0.1
  dc: eqdc10
`)

	iniData, err := NewINIResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed to convert YAML to INI: %v", err)
	}

	iniStr := string(iniData)
	if !strings.Contains(iniStr, "[database]") {
		t.Errorf("Expected INI to contain '[database]', got: %s", iniStr)
	}
	if !strings.Contains(iniStr, "[server]") {
		t.Errorf("Expected INI to contain '[server]', got: %s", iniStr)
	}
}

func TestEmptyINI(t *testing.T) {
	yamlData, err := NewINIResourceProvider().NativeToYAML([]byte{})
	if err != nil {
		t.Fatalf("Failed on empty input: %v", err)
	}
	if len(yamlData) != 0 {
		t.Errorf("Expected empty output for empty input, got: %v", yamlData)
	}
}

func TestEmptyYAML(t *testing.T) {
	iniData, err := NewINIResourceProvider().YAMLToNative([]byte{})
	if err != nil {
		t.Fatalf("Failed on empty input: %v", err)
	}
	if len(iniData) != 0 {
		t.Errorf("Expected empty output for empty input, got: %v", iniData)
	}
}

func TestRoundTrip(t *testing.T) {
	originalINI := []byte(`
[app]
name = MyApp
version = 1.0.0
`)

	// INI -> YAML -> INI
	yamlData, err := NewINIResourceProvider().NativeToYAML(originalINI)
	if err != nil {
		t.Fatalf("Failed INI to YAML conversion: %v", err)
	}

	iniData, err := NewINIResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed YAML to INI conversion: %v", err)
	}

	iniStr := string(iniData)
	if !strings.Contains(iniStr, "[app]") {
		t.Errorf("Round trip lost [app] section")
	}
	if !strings.Contains(iniStr, "name") {
		t.Errorf("Round trip lost name field")
	}
	if !strings.Contains(iniStr, "MyApp") {
		t.Errorf("Round trip lost MyApp value")
	}
}

func TestINIWithNestedSections(t *testing.T) {
	iniData := []byte(`
[app]
name = MyApplication
version = 1.0.0

[configHub]
configName = MyApplicationConfig
configSchema = SimpleApp

[configHub.kubernetes]
namespace = confighubplaceholder

[database]
host = localhost
port = 5432

[database.ssl]
enabled = true
`)

	yamlData, err := NewINIResourceProvider().NativeToYAML(iniData)
	if err != nil {
		t.Fatalf("Failed to convert INI with nested sections to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	if !strings.Contains(yamlStr, "configHub:") {
		t.Errorf("Expected YAML to contain 'configHub:', got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "database:") {
		t.Errorf("Expected YAML to contain 'database:', got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "ssl:") {
		t.Errorf("Expected YAML to contain 'ssl:', got: %s", yamlStr)
	}
}

func TestYAMLWithNestedToINI(t *testing.T) {
	yamlData := []byte(`
app:
  name: MyApplication
  version: 1.0.0
configHub:
  configName: MyApplicationConfig
  configSchema: SimpleApp
  kubernetes:
    namespace: confighubplaceholder
database:
  host: localhost
  port: 5432
  ssl:
    enabled: true
`)

	iniData, err := NewINIResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed to convert nested YAML to INI: %v", err)
	}

	iniStr := string(iniData)
	if !strings.Contains(iniStr, "[app]") {
		t.Errorf("Expected INI to contain '[app]', got: %s", iniStr)
	}
	if !strings.Contains(iniStr, "[configHub]") {
		t.Errorf("Expected INI to contain '[configHub]', got: %s", iniStr)
	}
	if !strings.Contains(iniStr, "[database]") {
		t.Errorf("Expected INI to contain '[database]', got: %s", iniStr)
	}
}

func TestINIWithMixedTypes(t *testing.T) {
	iniData := []byte(`
[database]
host = localhost
port = 5432
ssl_enabled = true
max_connections = 100
description = A test database
`)

	yamlData, err := NewINIResourceProvider().NativeToYAML(iniData)
	if err != nil {
		t.Fatalf("Failed to convert INI with mixed types to YAML: %v", err)
	}

	yamlStr := string(yamlData)
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
	if !strings.Contains(yamlStr, "description: A test database") {
		t.Errorf("Expected YAML to contain 'description: A test database' as string, got: %s", yamlStr)
	}
}

func TestINIWithArrayNotation(t *testing.T) {
	// INI format doesn't have native array support, but we can use indexed keys
	iniData := []byte(`
[app]
features.0 = authentication
features.1 = logging
name = MyApplication
`)

	yamlData, err := NewINIResourceProvider().NativeToYAML(iniData)
	if err != nil {
		t.Fatalf("Failed to convert INI with array notation to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	if !strings.Contains(yamlStr, "app:") {
		t.Errorf("Expected YAML to contain 'app:', got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "authentication") {
		t.Errorf("Expected YAML to contain 'authentication', got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "logging") {
		t.Errorf("Expected YAML to contain 'logging', got: %s", yamlStr)
	}
}

func TestINIWithBooleanValues(t *testing.T) {
	iniData := []byte(`
[settings]
debug = true
verbose = false
enabled = true
`)

	yamlData, err := NewINIResourceProvider().NativeToYAML(iniData)
	if err != nil {
		t.Fatalf("Failed to convert INI with boolean values to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	if !strings.Contains(yamlStr, "settings:") {
		t.Errorf("Expected YAML to contain 'settings:', got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "debug:") {
		t.Errorf("Expected YAML to contain 'debug:', got: %s", yamlStr)
	}
}

func TestYAMLToINIPreservesStructure(t *testing.T) {
	yamlData := []byte(`
server:
  host: localhost
  port: 8080
database:
  name: mydb
  user: admin
`)

	iniData, err := NewINIResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed to convert YAML to INI: %v", err)
	}

	iniStr := string(iniData)
	// Check that sections are created
	if !strings.Contains(iniStr, "[server]") {
		t.Errorf("Expected INI to contain '[server]' section, got: %s", iniStr)
	}
	if !strings.Contains(iniStr, "[database]") {
		t.Errorf("Expected INI to contain '[database]' section, got: %s", iniStr)
	}
	// Check that keys are present
	if !strings.Contains(iniStr, "host") {
		t.Errorf("Expected INI to contain 'host' key, got: %s", iniStr)
	}
	if !strings.Contains(iniStr, "port") {
		t.Errorf("Expected INI to contain 'port' key, got: %s", iniStr)
	}
}

func TestYAMLToININestedBooleanValues(t *testing.T) {
	yamlData := []byte(`
app:
  name: MyApplication
  version: 1.0.0
configHub:
  configName: MyApplicationConfig
  configSchema: SimpleApp
  kubernetes:
    namespace: confighubplaceholder
database:
  host: postgres.example.com
  port: 5433
  ssl:
    enabled: false
`)

	iniData, err := NewINIResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed to convert YAML with nested boolean to INI: %v", err)
	}

	iniStr := string(iniData)
	t.Logf("Generated INI:\n%s", iniStr)

	// Check that the nested section is created
	if !strings.Contains(iniStr, "[database.ssl]") {
		t.Errorf("Expected INI to contain '[database.ssl]' section, got: %s", iniStr)
	}

	// Check that the boolean value is present
	if !strings.Contains(iniStr, "enabled = false") {
		t.Errorf("Expected INI to contain 'enabled = false', got: %s", iniStr)
	}

	// Round trip: convert back to YAML and verify the boolean is still false
	yamlDataBack, err := NewINIResourceProvider().NativeToYAML(iniData)
	if err != nil {
		t.Fatalf("Failed to convert INI back to YAML: %v", err)
	}

	yamlStr := string(yamlDataBack)
	t.Logf("Round-tripped YAML:\n%s", yamlStr)

	// Check that the boolean value is preserved as false (not "false" string)
	if !strings.Contains(yamlStr, "enabled: false") {
		t.Errorf("Expected YAML to contain 'enabled: false' as boolean, got: %s", yamlStr)
	}
}

func TestINIToYAMLWithComments(t *testing.T) {
	iniData := []byte(`; Global settings
# Application config

[database]
# Primary connection
server = 192.168.1.1
port = 5432
`)

	yamlData, err := NewINIResourceProvider().NativeToYAML(iniData)
	if err != nil {
		t.Fatalf("Failed to convert INI to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	t.Logf("YAML:\n%s", yamlStr)

	if !strings.Contains(yamlStr, "$comment$head$database") {
		t.Errorf("Expected head comment for database section")
	}
	if !strings.Contains(yamlStr, "Global settings") {
		t.Errorf("Expected 'Global settings' comment text")
	}
	if !strings.Contains(yamlStr, "$comment$head$server") {
		t.Errorf("Expected head comment for server key")
	}
	if !strings.Contains(yamlStr, "Primary connection") {
		t.Errorf("Expected 'Primary connection' comment text")
	}
}

func TestYAMLToINIWithComments(t *testing.T) {
	yamlData := []byte(`$comment$head$database: Database settings
database:
  $comment$head$server: Primary host
  server: 192.168.1.1
  port: 5432
`)

	iniData, err := NewINIResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed to convert YAML to INI: %v", err)
	}

	iniStr := string(iniData)
	t.Logf("INI:\n%s", iniStr)

	if !strings.Contains(iniStr, "# Database settings") {
		t.Errorf("Expected '# Database settings' comment")
	}
	if !strings.Contains(iniStr, "# Primary host") {
		t.Errorf("Expected '# Primary host' comment")
	}
	if strings.Contains(iniStr, "$comment") {
		t.Errorf("INI output should not contain $comment keys")
	}
}

func TestINIRoundTripWithComments(t *testing.T) {
	originalINI := []byte(`; Configuration file
[app]
# Application name
name = MyApp
version = 1.0.0
`)

	yamlData, err := NewINIResourceProvider().NativeToYAML(originalINI)
	if err != nil {
		t.Fatalf("Failed INI to YAML: %v", err)
	}
	t.Logf("YAML:\n%s", string(yamlData))

	iniData, err := NewINIResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed YAML to INI: %v", err)
	}
	t.Logf("INI:\n%s", string(iniData))

	iniStr := string(iniData)
	if !strings.Contains(iniStr, "# Configuration file") {
		t.Errorf("Round trip lost 'Configuration file' comment")
	}
	if !strings.Contains(iniStr, "# Application name") {
		t.Errorf("Round trip lost 'Application name' comment")
	}
	if !strings.Contains(iniStr, "[app]") {
		t.Errorf("Round trip lost [app] section")
	}
}
