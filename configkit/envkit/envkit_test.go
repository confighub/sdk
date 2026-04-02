// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package envkit

import (
	"strings"
	"testing"
)

func TestEnvToYAMLWithComments(t *testing.T) {
	envData := []byte(`# Database connection
DB_HOST=localhost
DB_PORT=5432
# Application secret
APP_SECRET=mysecret
`)

	yamlData, err := NewEnvResourceProvider().NativeToYAML(envData)
	if err != nil {
		t.Fatalf("Failed to convert env to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	t.Logf("YAML:\n%s", yamlStr)

	if !strings.Contains(yamlStr, "$comment$head$DB_HOST") {
		t.Errorf("Expected head comment for DB_HOST key")
	}
	if !strings.Contains(yamlStr, "Database connection") {
		t.Errorf("Expected 'Database connection' comment text")
	}
	if !strings.Contains(yamlStr, "$comment$head$APP_SECRET") {
		t.Errorf("Expected head comment for APP_SECRET key")
	}
	if !strings.Contains(yamlStr, "Application secret") {
		t.Errorf("Expected 'Application secret' comment text")
	}
}

func TestYAMLToEnvWithComments(t *testing.T) {
	yamlData := []byte(`$comment$head$DB_HOST: Database host
DB_HOST: localhost
DB_PORT: 5432
`)

	envData, err := NewEnvResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed to convert YAML to env: %v", err)
	}

	envStr := string(envData)
	t.Logf("Env:\n%s", envStr)

	if !strings.Contains(envStr, "# Database host") {
		t.Errorf("Expected '# Database host' comment")
	}
	if strings.Contains(envStr, "$comment") {
		t.Errorf("Env output should not contain $comment keys")
	}
	if !strings.Contains(envStr, "DB_HOST=localhost") {
		t.Errorf("Expected 'DB_HOST=localhost' in output")
	}
}

func TestEnvRoundTripWithComments(t *testing.T) {
	originalEnv := []byte(`# Server port
PORT=8080
# Debug mode
DEBUG=true
`)

	yamlData, err := NewEnvResourceProvider().NativeToYAML(originalEnv)
	if err != nil {
		t.Fatalf("Failed env to YAML: %v", err)
	}
	t.Logf("YAML:\n%s", string(yamlData))

	envData, err := NewEnvResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed YAML to env: %v", err)
	}
	t.Logf("Env:\n%s", string(envData))

	envStr := string(envData)
	if !strings.Contains(envStr, "# Server port") {
		t.Errorf("Round trip lost 'Server port' comment")
	}
	if !strings.Contains(envStr, "# Debug mode") {
		t.Errorf("Round trip lost 'Debug mode' comment")
	}
}

func TestEnvInlineComments(t *testing.T) {
	envData := []byte(`DATABASE_PORT=5432 # Two comment
DATABASE_HOST=localhost
`)

	yamlData, err := NewEnvResourceProvider().NativeToYAML(envData)
	if err != nil {
		t.Fatalf("Failed to convert env to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	t.Logf("YAML:\n%s", yamlStr)

	// Value should NOT contain the inline comment text
	if strings.Contains(yamlStr, "5432 #") {
		t.Errorf("Value should not contain inline comment: %s", yamlStr)
	}
	// Value should be a clean string
	if !strings.Contains(yamlStr, "DATABASE_PORT: \"5432\"") {
		t.Errorf("Expected 'DATABASE_PORT: \"5432\"' as clean string value, got: %s", yamlStr)
	}
	// Inline comment should be extracted as a comment key
	if !strings.Contains(yamlStr, "$comment$line$DATABASE_PORT") {
		t.Errorf("Expected inline comment key for DATABASE_PORT")
	}
	if !strings.Contains(yamlStr, "Two comment") {
		t.Errorf("Expected 'Two comment' as inline comment text")
	}

	// Round-trip: inline comment should be re-injected
	envOut, err := NewEnvResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed YAML to env: %v", err)
	}
	envStr := string(envOut)
	t.Logf("Env:\n%s", envStr)

	if !strings.Contains(envStr, "# Two comment") {
		t.Errorf("Round trip lost inline comment 'Two comment'")
	}
	if strings.Contains(envStr, "$comment") {
		t.Errorf("Env output should not contain $comment keys")
	}
}

func TestEnvValuesAreStrings(t *testing.T) {
	envData := []byte(`PORT=8080
DEBUG=true
NAME=hello
`)

	yamlData, err := NewEnvResourceProvider().NativeToYAML(envData)
	if err != nil {
		t.Fatalf("Failed to convert env to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	t.Logf("YAML:\n%s", yamlStr)

	// All values should be strings (quoted in YAML)
	if !strings.Contains(yamlStr, "PORT: \"8080\"") {
		t.Errorf("Expected PORT to be string '8080', got: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "DEBUG: \"true\"") {
		t.Errorf("Expected DEBUG to be string 'true', got: %s", yamlStr)
	}
}

func TestEnvNoCommentsUnchanged(t *testing.T) {
	envData := []byte(`PORT=8080
HOST=localhost
`)

	yamlData, err := NewEnvResourceProvider().NativeToYAML(envData)
	if err != nil {
		t.Fatalf("Failed to convert env to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	if strings.Contains(yamlStr, "$comment") {
		t.Errorf("Expected no comment keys in output for uncommented env data")
	}
}
