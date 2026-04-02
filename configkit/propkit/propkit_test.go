// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package propkit

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/confighub/sdk/core/function/api"
)

func TestPropToYAML(t *testing.T) {
	tests := []struct {
		name   api.ResourceName
		schema api.ResourceType
		data   string
		want   string
	}{
		{
			name:   "MyApplicationConfig",
			schema: "SimpleApp",
			data: `configHub.configSchema=SimpleApp
configHub.configName=MyApplicationConfig
app.features.0=authentication
app.features.1=logging
app.name=MyApplication
app.version=1.0.0
database.host=localhost
database.port=5432
database.ssl.enabled=true
`,
			want: `configHub:
  configSchema: SimpleApp
  configName: MyApplicationConfig
app:
  features:
  - authentication
  - logging
  name: MyApplication
  version: 1.0.0
database:
  host: localhost
  port: 5432
  ssl:
    enabled: true
`,
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.name), func(t *testing.T) {
			yamlData, err := NewPropertiesResourceProvider().NativeToYAML([]byte(tt.data))
			assert.NoError(t, err)
			if !slices.Equal(yamlData, []byte(tt.want)) {
				t.Errorf("%s: want %s got %s", tt.name, tt.want, string(yamlData))
			}
		})
	}
}

func TestYAMLToProp(t *testing.T) {
	tests := []struct {
		name   api.ResourceName
		schema api.ResourceType
		data   string
		want   string
	}{
		{
			name:   "MyApplicationConfig",
			schema: "SimpleApp",
			data: `app:
  features:
    "0": authentication
    "1": logging
  name: MyApplication
  version: 1.0.0
configHub:
  configName: MyApplicationConfig
  configSchema: SimpleApp
database:
  host: localhost
  port: 5432
  ssl:
    enabled: true
`,
			want: `app.features.0=authentication
app.features.1=logging
app.name=MyApplication
app.version=1.0.0
configHub.configName=MyApplicationConfig
configHub.configSchema=SimpleApp
database.host=localhost
database.port=5432
database.ssl.enabled=true
`,
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.name), func(t *testing.T) {
			propData, err := NewPropertiesResourceProvider().YAMLToNative([]byte(tt.data))
			assert.NoError(t, err)
			if !slices.Equal(propData, []byte(tt.want)) {
				t.Errorf("%s: want %s got %s", tt.name, tt.want, string(propData))
			}
		})
	}
}

func TestPropertiesToYAMLWithComments(t *testing.T) {
	propData := []byte(`# Database settings
database.host=localhost
database.port=5432
# Application name
app.name=MyApp
`)

	yamlData, err := NewPropertiesResourceProvider().NativeToYAML(propData)
	if err != nil {
		t.Fatalf("Failed to convert Properties to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	t.Logf("YAML:\n%s", yamlStr)

	if !strings.Contains(yamlStr, "$comment$head$host") {
		t.Errorf("Expected head comment for host key")
	}
	if !strings.Contains(yamlStr, "Database settings") {
		t.Errorf("Expected 'Database settings' comment text")
	}
	if !strings.Contains(yamlStr, "$comment$head$name") {
		t.Errorf("Expected head comment for name key")
	}
	if !strings.Contains(yamlStr, "Application name") {
		t.Errorf("Expected 'Application name' comment text")
	}
}

func TestYAMLToPropertiesWithComments(t *testing.T) {
	yamlData := []byte(`database:
  $comment$head$host: Database host
  host: localhost
  port: 5432
`)

	propData, err := NewPropertiesResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed to convert YAML to Properties: %v", err)
	}

	propStr := string(propData)
	t.Logf("Properties:\n%s", propStr)

	if !strings.Contains(propStr, "# Database host") {
		t.Errorf("Expected '# Database host' comment")
	}
	if strings.Contains(propStr, "$comment") {
		t.Errorf("Properties output should not contain $comment keys")
	}
	if !strings.Contains(propStr, "database.host=localhost") {
		t.Errorf("Expected 'database.host=localhost' in output")
	}
}

func TestPropertiesInlineComments(t *testing.T) {
	propData := []byte(`database.port=5432 # Two comment
database.host=localhost
`)

	yamlData, err := NewPropertiesResourceProvider().NativeToYAML(propData)
	if err != nil {
		t.Fatalf("Failed to convert Properties to YAML: %v", err)
	}

	yamlStr := string(yamlData)
	t.Logf("YAML:\n%s", yamlStr)

	// Value should NOT contain the inline comment text
	if strings.Contains(yamlStr, "5432 #") {
		t.Errorf("Value should not contain inline comment: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "port: 5432") {
		t.Errorf("Expected 'port: 5432' as clean int value")
	}
	// Inline comment should be extracted as a comment key
	if !strings.Contains(yamlStr, "$comment$line$port") {
		t.Errorf("Expected inline comment key for port")
	}
	if !strings.Contains(yamlStr, "Two comment") {
		t.Errorf("Expected 'Two comment' as inline comment text")
	}

	// Round-trip: inline comment should be re-injected
	propsOut, err := NewPropertiesResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed YAML to Properties: %v", err)
	}
	propsStr := string(propsOut)
	t.Logf("Properties:\n%s", propsStr)

	if !strings.Contains(propsStr, "# Two comment") {
		t.Errorf("Round trip lost inline comment 'Two comment'")
	}
	if strings.Contains(propsStr, "$comment") {
		t.Errorf("Properties output should not contain $comment keys")
	}
}

func TestPropertiesRoundTripWithComments(t *testing.T) {
	originalProps := []byte(`# Server configuration
server.host=localhost
server.port=8080
`)

	yamlData, err := NewPropertiesResourceProvider().NativeToYAML(originalProps)
	if err != nil {
		t.Fatalf("Failed Properties to YAML: %v", err)
	}
	t.Logf("YAML:\n%s", string(yamlData))

	propData, err := NewPropertiesResourceProvider().YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("Failed YAML to Properties: %v", err)
	}
	t.Logf("Properties:\n%s", string(propData))

	propStr := string(propData)
	if !strings.Contains(propStr, "# Server configuration") {
		t.Errorf("Round trip lost 'Server configuration' comment")
	}
}
