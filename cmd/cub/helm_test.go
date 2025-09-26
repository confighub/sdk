// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadHelmValues(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := t.TempDir()

	// Test case 1: Multiple values files with override behavior
	t.Run("MultipleValuesFilesOverride", func(t *testing.T) {
		// Create first values file
		values1 := `
app:
  name: app1
  port: 8080
  replicas: 1
database:
  host: localhost
  port: 5432
`
		file1 := filepath.Join(tmpDir, "values1.yaml")
		if err := os.WriteFile(file1, []byte(values1), 0644); err != nil {
			t.Fatal(err)
		}

		// Create second values file that overrides some values
		values2 := `
app:
  port: 9090
  replicas: 3
database:
  host: prod.example.com
`
		file2 := filepath.Join(tmpDir, "values2.yaml")
		if err := os.WriteFile(file2, []byte(values2), 0644); err != nil {
			t.Fatal(err)
		}

		// Load values with both files
		result, err := loadHelmValues([]string{file1, file2}, []string{})
		if err != nil {
			t.Fatal(err)
		}

		// Check that second file overrides first file
		app := result["app"].(map[string]interface{})
		if app["name"] != "app1" {
			t.Errorf("Expected app.name to be 'app1' (from file1), got %v", app["name"])
		}
		if app["port"].(int) != 9090 {
			t.Errorf("Expected app.port to be 9090 (from file2), got %v", app["port"])
		}
		if app["replicas"].(int) != 3 {
			t.Errorf("Expected app.replicas to be 3 (from file2), got %v", app["replicas"])
		}

		database := result["database"].(map[string]interface{})
		if database["host"] != "prod.example.com" {
			t.Errorf("Expected database.host to be 'prod.example.com' (from file2), got %v", database["host"])
		}
		if database["port"].(int) != 5432 {
			t.Errorf("Expected database.port to be 5432 (from file1), got %v", database["port"])
		}
	})

	// Test case 2: --set flags override file values
	t.Run("SetFlagsOverrideFileValues", func(t *testing.T) {
		// Create values file
		values := `
app:
  name: myapp
  port: 8080
  debug: false
`
		file := filepath.Join(tmpDir, "values3.yaml")
		if err := os.WriteFile(file, []byte(values), 0644); err != nil {
			t.Fatal(err)
		}

		// Load values with file and --set flags
		setFlags := []string{
			"app.port=3000",
			"app.debug=true",
			"app.newKey=newValue",
		}
		result, err := loadHelmValues([]string{file}, setFlags)
		if err != nil {
			t.Fatal(err)
		}

		// Check that --set flags override file values
		app := result["app"].(map[string]interface{})
		if app["name"] != "myapp" {
			t.Errorf("Expected app.name to be 'myapp', got %v", app["name"])
		}
		// strvals.ParseInto parses numeric strings as int64
		port, ok := app["port"].(int64)
		if !ok {
			t.Errorf("Expected app.port to be int64, got type %T with value %v", app["port"], app["port"])
		} else if port != 3000 {
			t.Errorf("Expected app.port to be 3000 (from --set), got %v", port)
		}
		// strvals.ParseInto parses "true"/"false" as booleans
		if app["debug"] != true {
			t.Errorf("Expected app.debug to be true (from --set), got %v", app["debug"])
		}
		if app["newKey"] != "newValue" {
			t.Errorf("Expected app.newKey to be 'newValue' (from --set), got %v", app["newKey"])
		}
	})

	// Test case 3: Complex nested values
	t.Run("ComplexNestedValues", func(t *testing.T) {
		// Create values file with nested structure
		values := `
global:
  imageRegistry: docker.io
  storageClass: standard
ingress:
  enabled: true
  hosts:
    - host: example.com
      paths:
        - /
`
		file := filepath.Join(tmpDir, "values4.yaml")
		if err := os.WriteFile(file, []byte(values), 0644); err != nil {
			t.Fatal(err)
		}

		// Load values with complex --set flags
		setFlags := []string{
			"global.imageRegistry=gcr.io",
			"ingress.hosts[0].host=newdomain.com",
			"ingress.annotations.kubernetes\\.io/ingress\\.class=nginx",
		}
		result, err := loadHelmValues([]string{file}, setFlags)
		if err != nil {
			t.Fatal(err)
		}

		// Check nested values
		global := result["global"].(map[string]interface{})
		if global["imageRegistry"] != "gcr.io" {
			t.Errorf("Expected global.imageRegistry to be 'gcr.io', got %v", global["imageRegistry"])
		}
		if global["storageClass"] != "standard" {
			t.Errorf("Expected global.storageClass to be 'standard', got %v", global["storageClass"])
		}

		ingress := result["ingress"].(map[string]interface{})
		hosts := ingress["hosts"].([]interface{})
		firstHost := hosts[0].(map[string]interface{})
		if firstHost["host"] != "newdomain.com" {
			t.Errorf("Expected first host to be 'newdomain.com', got %v", firstHost["host"])
		}

		if annotations, ok := ingress["annotations"]; ok {
			annotationsMap := annotations.(map[string]interface{})
			if annotationsMap["kubernetes.io/ingress.class"] != "nginx" {
				t.Errorf("Expected ingress class annotation to be 'nginx', got %v", annotationsMap["kubernetes.io/ingress.class"])
			}
		} else {
			t.Error("Expected ingress.annotations to be set")
		}
	})

	// Test case 4: Empty inputs
	t.Run("EmptyInputs", func(t *testing.T) {
		result, err := loadHelmValues([]string{}, []string{})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result, map[string]interface{}{}) {
			t.Errorf("Expected empty map for empty inputs, got %v", result)
		}
	})

	// Test case 5: File not found error
	t.Run("FileNotFoundError", func(t *testing.T) {
		_, err := loadHelmValues([]string{"/nonexistent/file.yaml"}, []string{})
		if err == nil {
			t.Error("Expected error for nonexistent file")
		}
	})

	// Test case 6: Invalid YAML error
	t.Run("InvalidYAMLError", func(t *testing.T) {
		invalidYAML := `
invalid: yaml: content
  - this is not valid
    yaml syntax: [
`
		file := filepath.Join(tmpDir, "invalid.yaml")
		if err := os.WriteFile(file, []byte(invalidYAML), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := loadHelmValues([]string{file}, []string{})
		if err == nil {
			t.Error("Expected error for invalid YAML")
		}
	})

	// Test case 7: Invalid --set flag error
	t.Run("InvalidSetFlagError", func(t *testing.T) {
		_, err := loadHelmValues([]string{}, []string{"invalid[set=value"})
		if err == nil {
			t.Error("Expected error for invalid --set flag")
		}
	})

	// Test case 8: Order of precedence (default < file1 < file2 < --set)
	t.Run("OrderOfPrecedence", func(t *testing.T) {
		// Create first values file
		values1 := `
level: file1
override1: file1
override2: file1
override3: file1
`
		file1 := filepath.Join(tmpDir, "precedence1.yaml")
		if err := os.WriteFile(file1, []byte(values1), 0644); err != nil {
			t.Fatal(err)
		}

		// Create second values file
		values2 := `
level: file2
override2: file2
override3: file2
`
		file2 := filepath.Join(tmpDir, "precedence2.yaml")
		if err := os.WriteFile(file2, []byte(values2), 0644); err != nil {
			t.Fatal(err)
		}

		// Load with both files and --set
		setFlags := []string{"override3=setflag"}
		result, err := loadHelmValues([]string{file1, file2}, setFlags)
		if err != nil {
			t.Fatal(err)
		}

		// Check precedence
		if result["level"] != "file2" {
			t.Errorf("Expected level to be 'file2', got %v", result["level"])
		}
		if result["override1"] != "file1" {
			t.Errorf("Expected override1 to be 'file1', got %v", result["override1"])
		}
		if result["override2"] != "file2" {
			t.Errorf("Expected override2 to be 'file2', got %v", result["override2"])
		}
		if result["override3"] != "setflag" {
			t.Errorf("Expected override3 to be 'setflag', got %v", result["override3"])
		}
	})
}
