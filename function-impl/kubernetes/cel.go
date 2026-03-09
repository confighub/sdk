// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"github.com/google/cel-go/cel"
	"github.com/swaggest/jsonschema-go"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/function-impl/generic"
	"github.com/confighub/sdk/third_party/gaby"
	k8scel "k8s.io/apiserver/pkg/cel/library"
)

// k8sCELEnvOpts returns the additional CEL environment options that provide
// Kubernetes-specific CEL libraries matching the admission policy environment.
// This includes quantity(), url(), ip(), cidr(), regex(), and format() functions.
func k8sCELEnvOpts() []cel.EnvOption {
	return []cel.EnvOption{
		k8scel.Quantity(),
		k8scel.URLs(),
		k8scel.IP(),
		k8scel.CIDR(),
		k8scel.Regex(),
		k8scel.Lists(),
		k8scel.Format(),
	}
}

func registerK8sCELFunctions(fh handler.FunctionRegistry, rp *k8skit.K8sResourceProviderType) {
	reflector := jsonschema.Reflector{}
	validationSchema, _ := reflector.Reflect(api.ValidationResultList{})
	attributeSchema, _ := reflector.Reflect(api.AttributeValueList{})
	k8sOpts := k8sCELEnvOpts()

	// Override vet-cel with K8s CEL libraries
	fh.RegisterFunction("vet-cel", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-cel",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "expression",
					Required:      true,
					Description:   "CEL expression to validate each Kubernetes resource. The resource is available as 'object' (alias 'r'). Kubernetes CEL libraries are available: quantity(), url(), ip(), cidr(), regex(), format(). Must return a bool or a map with 'passed' (bool), 'details' (list of strings), and optionally 'failed_attributes' (list of maps). Params are accessible via 'params'.",
					DataType:      api.DataTypeCEL,
					Example:       `object.kind != 'Deployment' || object.spec.template.spec.containers.all(c, quantity(c.resources.limits['memory']).isGreaterThan(quantity('64Mi')))`,
				},
				{
					ParameterName: "param",
					Required:      false,
					Description:   "Parameters passed as key=value strings, accessible via the 'params' dict",
					DataType:      api.DataTypeString,
				},
			},
			VarArgs: true,
			OutputInfo: &api.FunctionOutput{
				ResultName:  "validation",
				Description: "Validation result from CEL expression with Kubernetes libraries",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Validates Kubernetes resources using a CEL expression with Kubernetes admission policy libraries (quantity, url, ip, cidr, regex, format). Evaluated once per resource.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return generic.GenericFnVetCEL(rp, fArgs.ParsedData, fArgs.Arguments, k8sOpts...)
		},
	})

	// Override get-cel with K8s CEL libraries
	fh.RegisterFunction("get-cel", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-cel",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "expression",
					Required:      true,
					Description:   "CEL expression that extracts values from a Kubernetes resource. The resource is available as 'object' (alias 'r'). Kubernetes CEL libraries are available. Must return a list of maps with fields: ResourceName, ResourceType, Path, Value, and optionally AttributeName, DataType. Params are accessible via 'params'.",
					DataType:      api.DataTypeCEL,
					Example:       `object.kind == 'Deployment' ? [{"ResourceName": object.metadata.namespace + "/" + object.metadata.name, "ResourceType": object.apiVersion + "/" + object.kind, "Path": "spec.replicas", "Value": object.spec.replicas}] : []`,
				},
				{
					ParameterName: "param",
					Required:      false,
					Description:   "Parameters passed as key=value strings, accessible via the 'params' dict",
					DataType:      api.DataTypeString,
				},
			},
			VarArgs: true,
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attributes",
				Description: "Extracted attribute values from CEL expression with Kubernetes libraries",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &attributeSchema,
			},
			Mutating:              false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Extracts attribute values from Kubernetes resources using a CEL expression with Kubernetes admission policy libraries. Evaluated once per resource.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return generic.GenericFnGetCEL(rp, fArgs.ParsedData, fArgs.Arguments, k8sOpts...)
		},
	})

	// Override set-cel with K8s CEL libraries
	fh.RegisterFunction("set-cel", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-cel",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "expression",
					Required:      true,
					Description:   "CEL expression that returns a partial Kubernetes resource as a map. Only the fields present in the result are modified; all other fields are preserved. The resource is available as 'object' (alias 'r'). Kubernetes CEL libraries are available. Params are accessible via 'params'.",
					DataType:      api.DataTypeCEL,
					Example:       `{"spec": {"replicas": int(params.replicas)}}`,
				},
				{
					ParameterName: "param",
					Required:      false,
					Description:   "Parameters passed as key=value strings, accessible via the 'params' dict",
					DataType:      api.DataTypeString,
				},
			},
			VarArgs:               true,
			Mutating:              true,
			Hermetic:              true,
			Idempotent:            false,
			Description:           "Mutates Kubernetes resources by merging a partial CEL expression result into the original resource. Kubernetes admission policy libraries are available. Comments are preserved.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return generic.GenericFnSetCEL(rp, fArgs.ParsedData, fArgs.Arguments, k8sOpts...)
		},
	})
}
