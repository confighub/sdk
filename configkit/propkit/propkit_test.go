// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package propkit

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/confighub/sdk/function/api"
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
			want: `app:
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
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.name), func(t *testing.T) {
			yamlData, err := PropertiesResourceProvider.NativeToYAML([]byte(tt.data))
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
			propData, err := PropertiesResourceProvider.YAMLToNative([]byte(tt.data))
			assert.NoError(t, err)
			if !slices.Equal(propData, []byte(tt.want)) {
				t.Errorf("%s: want %s got %s", tt.name, tt.want, string(propData))
			}
		})
	}
}
