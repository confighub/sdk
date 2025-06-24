// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package helloworld demonstrates how to create a simple custom function for ConfigHub.
// This example shows the basic structure and patterns that third-party developers
// should follow when creating their own functions.
package main

import (
	"fmt"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
)

// HelloWorldFunction is a simple example function that adds a greeting annotation
// to all resources in the configuration data.
//
// This function demonstrates:
// - How to access function parameters
// - How to modify configuration data
// - How to return modified data
// - Error handling patterns
func HelloWorldFunction(
	ctx *api.FunctionContext,
	parsedData gaby.Container,
	args []api.FunctionArgument,
	liveState []byte,
) (gaby.Container, any, error) {
	// The expected number of arguments is validated by the handler.
	// The handler also validates the parameter names and types.
	// Extract the greeting parameter
	greeting := args[0].Value.(string)

	// Confirm that we have any resources to work with
	if len(parsedData) == 0 {
		return parsedData, nil, fmt.Errorf("no resources found in configuration data")
	}

	// Walk all of the resources in the configuration data.
	visitor := func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		// Add a comment to the metadata section
		// This demonstrates how to modify YAML structure using gaby
		// Annotation segments containing dots would need to be escaped using yamlkit.EscapeDotsInPathSegment,
		// but this annotation key doesn't contain dots.
		commentPath := "metadata.annotations.confighub-example/hello-world-greeting"
		_, err := doc.SetP(greeting, commentPath)
		if err != nil {
			return nil, []error{fmt.Errorf("failed to set greeting annotation: %w", err)}
		}
		return output, []error{}
	}
	_, err := yamlkit.VisitResources(parsedData, nil, k8skit.K8sResourceProvider, visitor)

	// Return the modified data
	// The second return value is for function output (like validation results or extracted data)
	// For mutating functions, we return nil for output
	return parsedData, nil, err
}

// GetHelloWorldFunctionSignature returns the function signature that describes
// this function to the ConfigHub system. This is used for registration,
// validation, and documentation.
func GetHelloWorldFunctionSignature() api.FunctionSignature {
	return api.FunctionSignature{
		FunctionName: "hello-world",
		Parameters: []api.FunctionParameter{
			{
				ParameterName: "greeting",
				Description:   "The greeting message to add to the configuration",
				Required:      true,
				DataType:      api.DataTypeString,
				Example:       "Hello from ConfigHub!",
			},
		},
		RequiredParameters: 1,
		VarArgs:            false, // This function doesn't accept variable arguments
		OutputInfo: &api.FunctionOutput{
			ResultName:  "modified-config",
			Description: "Configuration with greeting annotation added",
			OutputType:  api.OutputTypeYAML,
		},
		Mutating:              true,  // This function modifies the configuration
		Validating:            false, // This function doesn't validate (return pass/fail)
		Hermetic:              true,  // This function doesn't call external systems
		Idempotent:            true,  // Running this function multiple times has the same effect
		Description:           "Adds a greeting message as an annotation to the first Kubernetes resource",
		FunctionType:          api.FunctionTypeCustom,
		AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny}, // Works on any resource type
	}
}
