// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"log/slog"

	"github.com/google/cel-go/cel"
	"github.com/swaggest/jsonschema-go"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/function-impl/generic"
	k8scel "k8s.io/apiserver/pkg/cel/library"
)

// K8sCELEnvOpts returns the additional CEL environment options that provide
// Kubernetes-specific CEL libraries matching the admission policy environment.
// This includes quantity(), url(), ip(), cidr(), regex(), and format() functions.
//
// It is exported so that callers evaluating CEL against Kubernetes configuration
// outside the function executor -- such as a server-side View column read from
// stored JSON -- get the same environment the get-cel function provides.
func K8sCELEnvOpts() []cel.EnvOption {
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
	k8sOpts := K8sCELEnvOpts()

	// Override vet-cel with K8s CEL libraries
	err := fh.RegisterFunction("vet-cel", nil)
	if err != nil {
		slog.Error("failed to unregister function", "error", err)
	}
	if err = fh.RegisterFunction("vet-cel", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-cel",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "expression",
					Required:      true,
					Description:   "CEL expression to validate each Kubernetes resource. The resource is available as 'object' (alias 'r'). Kubernetes CEL libraries are available: quantity(), url(), ip(), cidr(), regex(), format(). Must return a bool or a map with 'passed' (bool), 'details' (list of strings), and optionally 'issues' (list of maps with Identifier and Message, naming the check each finding fails) and 'failed_attributes' (list of maps with ResourceName, ResourceType, Path, Value, and optionally Score and Issues). Params are accessible via 'params'. ConfigHub metadata is accessible via 'functionContext' (e.g. functionContext.TargetID). Every resource the function was given is available as 'resources', whatever WhereResource selects: the rest of a Unit's resources, or, run by a Release trigger, everything the Release bundles. Other Units passed as OtherData, such as those a Trigger's OtherUnitSelections select, are available as 'other', a map from each key to the list of resources passed under it, and where each came from as 'otherOrigins', at the same positions (UnitSlug, SpaceSlug, UnitID, SpaceID, RevisionNum, UnitLabels). selects(selector, labels) says whether a Kubernetes label selector selects the labels, podLabels(resource) gives the labels of the pods a resource runs, wherever its kind keeps them (null for one that runs none), and namespaceLabels(namespaces, name) the labels of a Namespace in a list as a namespaceSelector matches them, e.g. other.policies.exists(p, p.metadata.namespace == r.metadata.namespace && selects(p.spec.podSelector, podLabels(r))).",
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
			Description:           "Fails each resource for which a CEL expression, evaluated once per resource with the Kubernetes admission policy libraries (quantity, url, ip, cidr, regex, format), returns false or {passed: false}. The expression can read other Units passed as OtherData, as other, and match label selectors with selects(selector, labels), podLabels(resource) and namespaceLabels(namespaces, name). Fix: change the resource so the expression holds; its details, or the Trigger's Description, say what it requires.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
			OtherDataAnyKey:       true,
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			other, err := generic.OtherResourcesOf(fArgs)
			if err != nil {
				return fArgs.ParsedData, api.ValidationResultFalse, err
			}
			return generic.GenericFnVetCELWithOtherData(rp, fArgs.Options, fArgs.FunctionContext, fArgs.ParsedData, other, fArgs.Arguments, k8sOpts...)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}

	// Override get-cel with K8s CEL libraries
	err = fh.RegisterFunction("get-cel", nil)
	if err != nil {
		slog.Error("failed to unregister function", "error", err)
	}
	if err := fh.RegisterFunction("get-cel", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-cel",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "expression",
					Required:      true,
					Description:   "CEL expression that extracts values from a Kubernetes resource. The resource is available as 'object' (alias 'r'). Kubernetes CEL libraries are available. Must return a list of maps with fields: ResourceName, ResourceType, Path, Value, and optionally AttributeName, DataType. Params are accessible via 'params'. ConfigHub metadata is accessible via 'functionContext' (e.g. functionContext.TargetID).",
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
			return generic.GenericFnGetCEL(rp, fArgs.Options, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, k8sOpts...)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}

	// Override set-cel with K8s CEL libraries
	err = fh.RegisterFunction("set-cel", nil)
	if err != nil {
		slog.Error("failed to unregister function", "error", err)
	}
	if err := fh.RegisterFunction("set-cel", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-cel",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "expression",
					Required:      true,
					Description:   "CEL expression that returns a partial Kubernetes resource as a map. Only the fields present in the result are modified; all other fields are preserved. The resource is available as 'object' (alias 'r'). Kubernetes CEL libraries are available. Params are accessible via 'params'. ConfigHub metadata is accessible via 'functionContext' (e.g. functionContext.TargetID).",
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
			VarArgs:  true,
			Mutating: true,
			Hermetic: true,
			// Idempotent and Replayable both depend on the expression, not on the function,
			// and the answer is the same one set-yq gives. Whether re-running produces the
			// same result is up to what the caller wrote; claiming false asserts something
			// this function cannot know. Replay needs the invocation to locate what it writes
			// by identity rather than by recorded position, and an expression evaluated per
			// resource under WhereResource does exactly that.
			Idempotent:            true,
			Replayable:            true,
			Description:           "Mutates Kubernetes resources by merging a partial CEL expression result into the original resource. Kubernetes admission policy libraries are available. Comments are preserved.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return generic.GenericFnSetCEL(rp, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options, k8sOpts...)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}
