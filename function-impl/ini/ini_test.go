// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package ini

import (
	"encoding/json"
	"testing"

	"github.com/confighub/sdk/configkit/inikit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function-impl/generic"
	"github.com/confighub/sdk/third_party/gaby"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fakeContext = api.FunctionContext{
	UnitSlug: "MyINIUnit",
	NotLive:  true,
}

// SimpleAppSchema defines the JSONSchema for the SimpleApp resource type
const simpleAppSchema = `{
  "SimpleApp": {
    "$schema": "http://json-schema.org/draft-07/schema#",
    "type": "object",
    "required": ["app", "configHub"],
    "properties": {
      "app": {
        "type": "object",
        "required": ["name", "version"],
        "properties": {
          "features": {
            "type": "object",
            "patternProperties": {
              "^[0-9]+$": {
                "type": "string"
              }
            }
          },
          "name": {
            "type": "string",
            "minLength": 1
          },
          "version": {
            "type": "string",
            "pattern": "^[0-9]+\\.[0-9]+\\.[0-9]+$"
          }
        }
      },
      "configHub": {
        "type": "object",
        "required": ["configSchema"],
        "properties": {
          "configName": {
            "type": "string"
          },
          "configSchema": {
            "type": "string",
            "const": "SimpleApp"
          },
          "kubernetes": {
            "type": "object",
            "properties": {
              "namespace": {
                "type": "string"
              }
            }
          }
        }
      },
      "database": {
        "type": "object",
        "properties": {
          "host": {
            "type": "string"
          },
          "port": {
            "type": "integer",
            "minimum": 1,
            "maximum": 65535
          },
          "ssl": {
            "type": "object",
            "properties": {
              "enabled": {
                "type": "boolean"
              }
            }
          }
        }
      }
    }
  }
}`

// validINI is a valid INI configuration that should pass validation
const validINI = `[app]
features.0 = authentication
features.1 = logging
name = MyApplication
version = 1.0.0

[configHub]
configName = MyApplicationConfig
configSchema = SimpleApp

[configHub.kubernetes]
namespace = production

[database]
host = localhost
port = 5432

[database.ssl]
enabled = true
`

// invalidINI_MissingRequired is an INI configuration missing required fields
const invalidINI_MissingRequired = `[app]
features.0 = authentication
features.1 = logging
name = MyApplication

[configHub]
configName = MyApplicationConfig
configSchema = SimpleApp

[configHub.kubernetes]
namespace = production
`

// invalidINI_InvalidVersion is an INI configuration with an invalid version format
const invalidINI_InvalidVersion = `[app]
features.0 = authentication
features.1 = logging
name = MyApplication
version = invalid-version

[configHub]
configName = MyApplicationConfig
configSchema = SimpleApp

[configHub.kubernetes]
namespace = production

[database]
host = localhost
port = 5432
`

// invalidINI_WrongSchema is an INI configuration with an invalid configSchema value
// The schema enforces that configSchema must be exactly "SimpleApp"
const invalidINI_WrongSchema = `[app]
features.0 = authentication
features.1 = logging
name = MyApplication
version = 1.0.0

[configHub]
configName = MyApplicationConfig
configSchema = SimpleApp

[configHub.kubernetes]
namespace = production

[database]
host = localhost
port = invalid-port
`

func TestVetJSONSchema_ValidINI(t *testing.T) {
	// Parse the valid INI by converting to YAML first
	yamlBytes, err := inikit.NewINIResourceProvider().NativeToYAML([]byte(validINI))
	require.NoError(t, err)

	parsedData, err := gaby.ParseAll(yamlBytes)
	require.NoError(t, err)
	require.NotNil(t, parsedData)

	// Create arguments with the schema map
	args := []api.FunctionArgument{
		{
			ParameterName: "schema-map",
			Value:         simpleAppSchema,
		},
	}

	// Execute the function
	resultData, result, err := generic.GenericFnVetJSONSchema(inikit.NewINIResourceProvider(), &fakeContext, parsedData, args)
	require.NoError(t, err)
	assert.NotNil(t, resultData)

	// Check that validation passed
	validationResult, ok := result.(api.ValidationResult)
	require.True(t, ok, "result should be a ValidationResult")
	assert.True(t, validationResult.Passed, "validation should pass for valid INI")
	assert.Empty(t, validationResult.Details, "should have no validation details for valid INI")
	assert.Empty(t, validationResult.FailedAttributes, "should have no failed attributes for valid INI")
}

func TestVetJSONSchema_InvalidINI_MissingRequired(t *testing.T) {
	// Parse the invalid INI (missing required version field)
	yamlBytes, err := inikit.NewINIResourceProvider().NativeToYAML([]byte(invalidINI_MissingRequired))
	require.NoError(t, err)

	parsedData, err := gaby.ParseAll(yamlBytes)
	require.NoError(t, err)
	require.NotNil(t, parsedData)

	// Create arguments with the schema map
	args := []api.FunctionArgument{
		{
			ParameterName: "schema-map",
			Value:         simpleAppSchema,
		},
	}

	// Execute the function
	resultData, result, err := generic.GenericFnVetJSONSchema(inikit.NewINIResourceProvider(), &fakeContext, parsedData, args)
	assert.NotNil(t, resultData)

	// Check that validation failed
	validationResult, ok := result.(api.ValidationResult)
	require.True(t, ok, "result should be a ValidationResult")
	assert.False(t, validationResult.Passed, "validation should fail for invalid INI")
	assert.NotEmpty(t, validationResult.Details, "should have validation details for invalid INI")
	assert.NotEmpty(t, validationResult.FailedAttributes, "should have failed attributes for invalid INI")

	// Check that the error mentions the missing required field
	detailsJSON, _ := json.Marshal(validationResult.Details)
	detailsStr := string(detailsJSON)
	assert.Contains(t, detailsStr, "version", "error should mention missing 'version' field")
}

func TestVetJSONSchema_InvalidINI_InvalidVersion(t *testing.T) {
	// Parse the invalid INI (invalid version format)
	yamlBytes, err := inikit.NewINIResourceProvider().NativeToYAML([]byte(invalidINI_InvalidVersion))
	require.NoError(t, err)

	parsedData, err := gaby.ParseAll(yamlBytes)
	require.NoError(t, err)
	require.NotNil(t, parsedData)

	// Create arguments with the schema map
	args := []api.FunctionArgument{
		{
			ParameterName: "schema-map",
			Value:         simpleAppSchema,
		},
	}

	// Execute the function
	resultData, result, err := generic.GenericFnVetJSONSchema(inikit.NewINIResourceProvider(), &fakeContext, parsedData, args)
	assert.NotNil(t, resultData)

	// Check that validation failed
	validationResult, ok := result.(api.ValidationResult)
	require.True(t, ok, "result should be a ValidationResult")
	assert.False(t, validationResult.Passed, "validation should fail for invalid version format")
	assert.NotEmpty(t, validationResult.Details, "should have validation details for invalid version")
	assert.NotEmpty(t, validationResult.FailedAttributes, "should have failed attributes for invalid version")

	// Check that the error mentions the version field or pattern
	detailsJSON, _ := json.Marshal(validationResult.Details)
	detailsStr := string(detailsJSON)
	assert.Contains(t, detailsStr, "version", "error should mention 'version' field")
}

func TestVetJSONSchema_InvalidINI_InvalidPort(t *testing.T) {
	// Parse the invalid INI (invalid port - not an integer)
	yamlBytes, err := inikit.NewINIResourceProvider().NativeToYAML([]byte(invalidINI_WrongSchema))
	require.NoError(t, err)

	parsedData, err := gaby.ParseAll(yamlBytes)
	require.NoError(t, err)
	require.NotNil(t, parsedData)

	// Create arguments with the schema map
	args := []api.FunctionArgument{
		{
			ParameterName: "schema-map",
			Value:         simpleAppSchema,
		},
	}

	// Execute the function
	resultData, result, err := generic.GenericFnVetJSONSchema(inikit.NewINIResourceProvider(), &fakeContext, parsedData, args)
	assert.NotNil(t, resultData)

	// Check that validation failed
	validationResult, ok := result.(api.ValidationResult)
	require.True(t, ok, "result should be a ValidationResult")
	assert.False(t, validationResult.Passed, "validation should fail for invalid port")
	assert.NotEmpty(t, validationResult.Details, "should have validation details for invalid port")
	assert.NotEmpty(t, validationResult.FailedAttributes, "should have failed attributes for invalid port")

	// Check that the error mentions the port field
	detailsJSON, _ := json.Marshal(validationResult.Details)
	detailsStr := string(detailsJSON)
	assert.Contains(t, detailsStr, "port", "error should mention 'port' field")
}

func TestVetJSONSchema_NoSchemaForResourceType(t *testing.T) {
	// Parse a valid INI
	yamlBytes, err := inikit.NewINIResourceProvider().NativeToYAML([]byte(validINI))
	require.NoError(t, err)

	parsedData, err := gaby.ParseAll(yamlBytes)
	require.NoError(t, err)
	require.NotNil(t, parsedData)

	// Create arguments with an empty schema map (no schemas for any resource types)
	emptySchemaMap := "{}"
	args := []api.FunctionArgument{
		{
			ParameterName: "schema-map",
			Value:         emptySchemaMap,
		},
	}

	// Execute the function
	resultData, result, err := generic.GenericFnVetJSONSchema(inikit.NewINIResourceProvider(), &fakeContext, parsedData, args)
	require.NoError(t, err)
	assert.NotNil(t, resultData)

	// Check that validation passed (no schemas means no validation)
	validationResult, ok := result.(api.ValidationResult)
	require.True(t, ok, "result should be a ValidationResult")
	assert.True(t, validationResult.Passed, "validation should pass when no schema is provided for resource type")
	assert.Empty(t, validationResult.Details, "should have no validation details when no schema is provided")
	assert.Empty(t, validationResult.FailedAttributes, "should have no failed attributes when no schema is provided")
}

func TestVetJSONSchema_InvalidSchemaMap(t *testing.T) {
	// Parse a valid INI
	yamlBytes, err := inikit.NewINIResourceProvider().NativeToYAML([]byte(validINI))
	require.NoError(t, err)

	parsedData, err := gaby.ParseAll(yamlBytes)
	require.NoError(t, err)
	require.NotNil(t, parsedData)

	// Create arguments with an invalid JSON string
	args := []api.FunctionArgument{
		{
			ParameterName: "schema-map",
			Value:         "invalid json",
		},
	}

	// Execute the function
	resultData, result, err := generic.GenericFnVetJSONSchema(inikit.NewINIResourceProvider(), &fakeContext, parsedData, args)
	assert.NotNil(t, resultData)

	// Check that validation failed due to invalid schema map
	validationResult, ok := result.(api.ValidationResult)
	require.True(t, ok, "result should be a ValidationResult")
	assert.False(t, validationResult.Passed, "validation should fail for invalid schema map")

	// Should have an error about parsing the schema map
	assert.Error(t, err, "should return an error for invalid schema map JSON")
}
