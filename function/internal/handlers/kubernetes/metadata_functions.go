// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/function/internal/handlers/generic"
	"github.com/confighub/sdk/third_party/gaby"
)

func registerMetadataFunctions(fh handler.FunctionRegistry) {
	fh.RegisterFunction("ensure-namespaces", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "ensure-namespaces",
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Ensure every namespaced resource has a namespace field by adding one with the placeholder value if it is not present",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny}, // technically only namespace-scoped resources
		},
		Function: k8sFnEnsureNamespaces,
	})
	// NOTE: set-namespace does not set the name of Namespace resources
	// TODO: use OpenAPI schemas to determine namespaced vs cluster scope
	// TODO: skip local config units
	// It sets the metadata.namespace field in all resources that have one except known cluster-scoped resource types.
	// Other references also need to be updated.
	// RoleBinding and ClusterRoleBinding are example resources that can have such a reference.
	// kpt's set-namespace:
	// https://github.com/GoogleContainerTools/kpt-functions-catalog/blob/master/functions/go/set-namespace/transformer/namespace.go
	// kustomize's:
	// https://github.com/kubernetes-sigs/kustomize/blob/master/api/filters/namespace/namespace.go
	// A rigorous check of whether a resource type was cluster-scoped or not would require something like:
	// https://github.com/kubernetes-sigs/kustomize/blob/master/kyaml/resid/gvk.go
	namespaceParameters := []api.FunctionParameter{
		{
			ParameterName: "namespace-name",
			Required:      true,
			Description:   "Value of namespace fields in the resources",
			DataType:      api.DataTypeString,
		},
	}
	generic.RegisterPathSetterAndGetter(fh, "namespace", namespaceParameters,
		" the namespace attributes in resource", AttributeNameNamespaceNameReference, k8skit.K8sResourceProvider, true)
	fh.RegisterFunction("get-needed-namespaces", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-needed-namespaces",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "namespace-name",
				Description: "Namespace attributes in the resources that need to be set",
				OutputType:  api.OutputTypeAttributeValueList,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Get needed namespace attributes in each resource",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         AttributeNameNamespaceNameReference,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny}, // technically only namespace-scoped resources
		},
		Function: k8sFnNeededNamespaces,
	})
	annotationParameters := []api.FunctionParameter{
		{
			ParameterName: "annotation-key",
			Required:      true,
			Description:   "Key of annotation to ", // verb will be appended
			DataType:      api.DataTypeString,
		},
		{
			ParameterName: "annotation-value",
			Required:      true,
			Description:   "Value of the specified annotation",
			DataType:      api.DataTypeString,
		},
	}
	generic.RegisterPathSetterAndGetter(fh, "annotation", annotationParameters,
		" an annotation", AttributeNameAnnotationValue, k8skit.K8sResourceProvider, true)

	labelParameters := []api.FunctionParameter{
		{
			ParameterName: "label-key",
			Required:      true,
			Description:   "Key of label to ", // verb will be appended
			DataType:      api.DataTypeString,
		},
		{
			ParameterName: "label-value",
			Required:      true,
			Description:   "Value of the specified label",
			DataType:      api.DataTypeString,
		},
	}
	generic.RegisterPathSetterAndGetter(fh, "label", labelParameters,
		" a label", AttributeNameLabelValue, k8skit.K8sResourceProvider, true)
}

const AttributeNameNamespaceNameReference = api.AttributeName("namespace-name-reference")
const AttributeNameAnnotationValue = api.AttributeName("annotation-value")
const AttributeNameLabelValue = api.AttributeName("label-value")

func initMetadataFunctions() {
	namespaceResourceType := api.ResourceType("v1/Namespace")

	var resourceTypeToNamespaceNamePath = api.ResourceTypeToPathToVisitorInfoType{
		namespaceResourceType: {
			api.UnresolvedPath("metadata.name"): {
				Path:          api.UnresolvedPath("metadata.name"),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
	}
	pathInfos := resourceTypeToNamespaceNamePath[namespaceResourceType]
	getterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "get-resources-of-type",
		Arguments:    []api.FunctionArgument{{ParameterName: "resource-type", Value: string(namespaceResourceType)}},
	}
	yamlkit.RegisterProvidedPaths(k8skit.K8sResourceProvider, namespaceResourceType, pathInfos, getterFunctionInvocation)

	// These paths are not included in kustomize's namereference list.
	var resourceTypeToNamespacePath = api.ResourceTypeToPathToVisitorInfoType{
		api.ResourceType(api.ResourceTypeAny): {
			api.UnresolvedPath("metadata.namespace"): {
				Path:           api.UnresolvedPath("metadata.namespace"),
				AttributeName:  api.AttributeNameResourceName,
				DataType:       api.DataTypeString,
				TypeExceptions: k8skit.K8sClusterScopedResourceTypes,
			},
		},
		api.ResourceType("rbac.authorization.k8s.io/v1/RoleBinding"): {
			api.UnresolvedPath("subjects.*.namespace"): {
				Path:          api.UnresolvedPath("subjects.*.namespace"),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType("rbac.authorization.k8s.io/v1/ClusterRoleBinding"): {
			api.UnresolvedPath("subjects.*.namespace"): {
				Path:          api.UnresolvedPath("subjects.*.namespace"),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType("apiextensions.k8s.io/v1/CustomResourceDefinition"): {
			api.UnresolvedPath("spec.conversion.webhook.clientConfig.service.namespace"): {
				Path:          api.UnresolvedPath("spec.conversion.webhook.clientConfig.service.namespace"),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType("apiregistration.k8s.io/v1/APIService"): {
			api.UnresolvedPath("spec.service.namespace"): {
				Path:          api.UnresolvedPath("spec.service.namespace"),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType("kustomize.toolkit.fluxcd.io/v1/Kustomization"): {
			api.UnresolvedPath("spec.targetNamespace"): {
				Path:          api.UnresolvedPath("spec.targetNamespace"),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType("helm.toolkit.fluxcd.io/v2/HelmRelease"): {
			api.UnresolvedPath("spec.chartRef.namespace"): {
				Path:          api.UnresolvedPath("spec.chartRef.namespace"),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
	}
	setterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "set-references-of-type",
		Arguments:    []api.FunctionArgument{{ParameterName: "resource-type", Value: string(namespaceResourceType)}},
	}
	for resourceType, pathInfos := range resourceTypeToNamespacePath {
		yamlkit.RegisterNeededPaths(k8skit.K8sResourceProvider, resourceType, pathInfos, setterFunctionInvocation)
		yamlkit.RegisterPathsByAttributeName(
			k8skit.K8sResourceProvider,
			AttributeNameNamespaceNameReference,
			resourceType,
			pathInfos,
			nil,
			setterFunctionInvocation,
			false,
		)
		yamlkit.RegisterPathsByAttributeName(
			k8skit.K8sResourceProvider,
			api.AttributeNameResourceName,
			resourceType,
			pathInfos,
			nil,
			setterFunctionInvocation,
			false,
		)
		yamlkit.RegisterPathsByAttributeName(
			k8skit.K8sResourceProvider,
			api.AttributeNameGeneral,
			resourceType,
			pathInfos,
			nil,
			setterFunctionInvocation,
			true,
		)
	}

	attributePath := api.UnresolvedPath("metadata.annotations.@%s:annotation-key")
	var resourceTypeToAnnotationPath = api.ResourceTypeToPathToVisitorInfoType{
		api.ResourceTypeAny: {
			attributePath: {
				Path:          attributePath,
				AttributeName: AttributeNameAnnotationValue,
				DataType:      api.DataTypeString,
			},
		},
	}
	pathInfos = resourceTypeToAnnotationPath[api.ResourceTypeAny]
	setterFunctionInvocation = &api.FunctionInvocation{
		FunctionName: "set-annotation",
		// arguments will be filled in during traversal
	}
	getterFunctionInvocation = &api.FunctionInvocation{
		FunctionName: "get-annotation",
		// arguments will be filled in during traversal
	}
	yamlkit.RegisterPathsByAttributeName(
		k8skit.K8sResourceProvider,
		AttributeNameAnnotationValue,
		api.ResourceTypeAny,
		pathInfos,
		getterFunctionInvocation,
		setterFunctionInvocation,
		false,
	)

	attributePath = api.UnresolvedPath("metadata.labels.@%s:label-key")
	var resourceTypeToLabelPath = api.ResourceTypeToPathToVisitorInfoType{
		api.ResourceTypeAny: {
			attributePath: {
				Path:          attributePath,
				AttributeName: AttributeNameLabelValue,
				DataType:      api.DataTypeString,
			},
		},
	}
	pathInfos = resourceTypeToLabelPath[api.ResourceTypeAny]
	setterFunctionInvocation = &api.FunctionInvocation{
		FunctionName: "set-label",
		// arguments will be filled in during traversal
	}
	getterFunctionInvocation = &api.FunctionInvocation{
		FunctionName: "get-label",
		// arguments will be filled in during traversal
	}
	yamlkit.RegisterPathsByAttributeName(
		k8skit.K8sResourceProvider,
		AttributeNameLabelValue,
		api.ResourceTypeAny,
		pathInfos,
		getterFunctionInvocation,
		setterFunctionInvocation,
		false,
	)
}

func k8sFnEnsureNamespaces(_ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// TODO: verbose logging
	for _, doc := range parsedData {
		resourceName, err := k8skit.K8sResourceProvider.ResourceNameGetter(doc)
		if err != nil {
			// log.Errorf("%v", err)
			return parsedData, nil, err
		}
		nameSegments := strings.Split(string(resourceName), "/")
		if len(nameSegments) != 2 {
			err = fmt.Errorf("improperly formatted resource name %s", string(resourceName))
			// log.Errorf("%v", err)
			return parsedData, nil, err
		}
		// log.Infof("checking namespace in resource %s", string(resourceName))
		if nameSegments[0] == "" {
			// No namespace. Check whether it should have one.
			resourceType, err := k8skit.K8sResourceProvider.ResourceTypeGetter(doc)
			if err != nil {
				// Malformed resource.
				// log.Errorf("%v", err)
				return parsedData, nil, err
			}
			// TODO: Handle CRDs.
			_, isClusterScoped := k8skit.K8sClusterScopedResourceTypes[resourceType]
			if !isClusterScoped {
				// log.Infof("setting namespace in resource %s of type %s", string(resourceName), string(resourceType))
				_, err = doc.SetP(yamlkit.PlaceHolderBlockApplyString, ".metadata.namespace")
				if err != nil {
					// log.Errorf("%v", err)
					return parsedData, nil, err
				}
			}
		}
	}
	return parsedData, nil, nil
}

func k8sFnNeededNamespaces(_ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// No arguments
	resourceTypeToNamespacePath := yamlkit.GetPathRegistryForAttributeName(k8skit.K8sResourceProvider, AttributeNameNamespaceNameReference)
	values, err := yamlkit.GetNeededStringPaths(parsedData, resourceTypeToNamespacePath, []any{}, k8skit.K8sResourceProvider)
	return parsedData, values, err
}
