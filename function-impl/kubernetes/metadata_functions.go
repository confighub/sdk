// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/function-impl/generic"
)

func registerMetadataFunctions(fh handler.FunctionRegistry, rp *k8skit.K8sResourceProviderType) {
	if err := fh.RegisterFunction("ensure-namespaces", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "ensure-namespaces",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "cluster-scoped-types",
					Required:      false,
					Description:   "Additional Kubernetes resource types to treat as cluster-scoped, as a comma-separated list of apiVersion/Kind (e.g. \"example.com/v1/Foo,example.com/v1/Bar\")",
					DataType:      api.DataTypeStringArray,
					Example:       "example.com/v1/Foo,example.com/v1/Bar",
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Ensure every namespaced resource has a namespace field by adding one with the placeholder value if it is not present",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny}, // technically only namespace-scoped resources
		},
		Function: makeK8sFnEnsureNamespaces(rp),
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
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
	// Register only get-namespace via the generic helper. set-namespace is registered
	// as a custom function below because it does more than a path-registry update:
	// it renames v1/Namespace, upserts subjects[*].namespace on RBAC bindings for
	// ServiceAccount subjects, and rewrites Service DNS names in pod-spec
	// command/args/env values.
	generic.RegisterPathSetterAndGetter(fh, "namespace", namespaceParameters,
		" the namespace attributes in resource", AttributeNameNamespaceNameReference, rp, false, false, false)
	setNamespaceParameters := []api.FunctionParameter{
		{
			ParameterName: "namespace-name",
			Required:      true,
			Description:   "New namespace value to set on namespaced resources and namespace-typed references",
			DataType:      api.DataTypeString,
		},
		{
			ParameterName: "old-namespace",
			Required:      false,
			Description:   "Old namespace to look for in pod-spec command/args/env Service DNS names. If empty, each resource's existing metadata.namespace is used.",
			DataType:      api.DataTypeString,
		},
	}
	setNamespaceResourceTypes := yamlkit.ResourceTypesForAttribute(AttributeNameNamespaceNameReference, rp)
	if err := fh.RegisterFunction("set-namespace", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "set-namespace",
			Parameters:            setNamespaceParameters,
			Mutating:              true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set the namespace on every namespaced resource, the name on v1/Namespace resources, and Service DNS references in pod-spec command/args/env values",
			FunctionType:          api.FunctionTypeCustom,
			AttributeName:         AttributeNameNamespaceNameReference,
			AffectedResourceTypes: setNamespaceResourceTypes,
		},
		Function: makeK8sFnSetNamespace(rp),
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
	nameParameters := []api.FunctionParameter{
		{
			ParameterName: "name",
			Required:      true,
			Description:   "Value of metadata.name in the resources",
			DataType:      api.DataTypeString,
		},
	}
	generic.RegisterPathSetterAndGetter(fh, "name", nameParameters,
		" metadata.name in resources. Use --where-resource to scope to a specific resource type.",
		AttributeNameMetadataName, rp, true, false, false)
	api.InitTypeSchemas()
	if err := fh.RegisterFunction("get-needed-namespaces", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-needed-namespaces",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "namespace-name",
				Description: "Namespace attributes in the resources that need to be set",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &api.AttributeValueListSchema,
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
		Function: makeK8sFnNeededNamespaces(rp),
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
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
		" an annotation", AttributeNameAnnotationValue, rp, true, true, false)

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
		" a label", AttributeNameLabelValue, rp, true, true, false)

	workloadLabelResourceTypes := unionResourceTypes(
		k8skit.ResourceTypeToPodTemplateLabelsPaths,
		k8skit.ResourceTypeToPodLabelSelectorPaths,
	)
	if err := fh.RegisterFunction("get-workload-labels", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-workload-labels",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "workload-labels",
				Description: "Pod-template labels and pod label-selectors on workload controllers and resources that select pods, one AttributeValue per map path as YAML",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &api.AttributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Get pod-template labels and pod label-selectors on workload controllers and resources that select pods. Returns one AttributeValue per labels/selector map path; the value is the YAML serialization of the map.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: workloadLabelResourceTypes,
		},
		Function: makeK8sFnGetWorkloadLabels(rp),
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
	if err := fh.RegisterFunction("set-workload-labels", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-workload-labels",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "label-key-value",
					Required:      true,
					Description:   "key=value to upsert; empty value (key=) removes the entry. Applied to the pod-template labels and any pod label-selector on the resource.",
					DataType:      api.DataTypeString,
					Example:       "app.kubernetes.io/name=myapp",
				},
			},
			VarArgs:               true,
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set pod-template labels and pod label-selectors on workload controllers and resources that select pods, using <key>=<value> syntax",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: workloadLabelResourceTypes,
		},
		Function: makeK8sFnSetWorkloadLabels(rp),
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// unionResourceTypes returns a deduplicated list of resource types appearing in any of the given path maps.
func unionResourceTypes(maps ...map[api.ResourceType][]string) []api.ResourceType {
	seen := map[api.ResourceType]struct{}{}
	result := []api.ResourceType{}
	for _, m := range maps {
		for rt := range m {
			if _, ok := seen[rt]; ok {
				continue
			}
			seen[rt] = struct{}{}
			result = append(result, rt)
		}
	}
	return result
}

const AttributeNameNamespaceNameReference = api.AttributeName("namespace-name-reference")
const AttributeNameAnnotationValue = api.AttributeName("annotation-value")
const AttributeNameLabelValue = api.AttributeName("label-value")
const AttributeNameMetadataName = api.AttributeName("metadata-name")
const AttributeNameWorkloadLabels = api.AttributeName("workload-labels")

func initMetadataFunctions(rp *k8skit.K8sResourceProviderType) {
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
	yamlkit.RegisterPathsByAttributeName(rp, api.AttributeNameResourceName, namespaceResourceType, pathInfos,
		&yamlkit.AttributeRegistrationDetails{
			GetterInvocation: getterFunctionInvocation,
			AttributeNeedsProvidesDetails: api.AttributeNeedsProvidesDetails{
				ProvidedProperties: map[string]string{"ResourceType": string(namespaceResourceType)},
			},
		},
		false, true)

	// Register metadata.name on every Kubernetes resource type so that set-name / get-name
	// can target it. Callers should use --where-resource to scope the operation to a single
	// resource type (otherwise every resource would receive the same name).
	yamlkit.RegisterPathsByAttributeName(rp, AttributeNameMetadataName, api.ResourceType(api.ResourceTypeAny), api.PathToVisitorInfoType{
		api.UnresolvedPath("metadata.name"): {
			Path:          api.UnresolvedPath("metadata.name"),
			AttributeName: AttributeNameMetadataName,
			DataType:      api.DataTypeString,
		},
	}, nil, false, false)

	// These paths are not included in kustomize's namereference list.
	//
	// The `|` prefix on a leaf segment (e.g. `service.|namespace`) gates upsert:
	// the parent chain up to that segment must already exist in the resource for
	// the path to resolve, but the leaf itself will be created if missing. That
	// keeps `set-namespace` from inventing optional sub-objects (e.g. a CRD's
	// spec.conversion.webhook) on resources that don't have them, while still
	// adding `metadata.namespace` to every namespaced resource and
	// `subjects[*].namespace` to every ServiceAccount subject in an RB/CRB.
	var resourceTypeToNamespacePath = api.ResourceTypeToPathToVisitorInfoType{
		api.ResourceType("v1/Namespace"): {
			api.UnresolvedPath("metadata.name"): {
				Path:          api.UnresolvedPath("metadata.name"),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType(api.ResourceTypeAny): {
			api.UnresolvedPath("metadata.namespace"): {
				Path:           api.UnresolvedPath("metadata.namespace"),
				AttributeName:  api.AttributeNameResourceName,
				DataType:       api.DataTypeString,
				TypeExceptions: k8skit.K8sClusterScopedResourceTypes,
			},
		},
		api.ResourceType("rbac.authorization.k8s.io/v1/RoleBinding"): {
			api.UnresolvedPath("subjects.*?kind=ServiceAccount.|namespace"): {
				Path:          api.UnresolvedPath("subjects.*?kind=ServiceAccount.|namespace"),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType("rbac.authorization.k8s.io/v1/ClusterRoleBinding"): {
			api.UnresolvedPath("subjects.*?kind=ServiceAccount.|namespace"): {
				Path:          api.UnresolvedPath("subjects.*?kind=ServiceAccount.|namespace"),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType("apiextensions.k8s.io/v1/CustomResourceDefinition"): {
			api.UnresolvedPath("spec.conversion.webhook.clientConfig.service.|namespace"): {
				Path:          api.UnresolvedPath("spec.conversion.webhook.clientConfig.service.|namespace"),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType("apiregistration.k8s.io/v1/APIService"): {
			api.UnresolvedPath("spec.service.|namespace"): {
				Path:          api.UnresolvedPath("spec.service.|namespace"),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType("admissionregistration.k8s.io/v1/MutatingWebhookConfiguration"): {
			api.UnresolvedPath("webhooks.*.clientConfig.service.|namespace"): {
				Path:          api.UnresolvedPath("webhooks.*.clientConfig.service.|namespace"),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType("admissionregistration.k8s.io/v1/ValidatingWebhookConfiguration"): {
			api.UnresolvedPath("webhooks.*.clientConfig.service.|namespace"): {
				Path:          api.UnresolvedPath("webhooks.*.clientConfig.service.|namespace"),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType("kustomize.toolkit.fluxcd.io/v1/Kustomization"): {
			api.UnresolvedPath("spec.|targetNamespace"): {
				Path:          api.UnresolvedPath("spec.|targetNamespace"),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType("helm.toolkit.fluxcd.io/v2/HelmRelease"): {
			api.UnresolvedPath("spec.chartRef.|namespace"): {
				Path:          api.UnresolvedPath("spec.chartRef.|namespace"),
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
		yamlkit.RegisterPathsByAttributeName(
			rp,
			AttributeNameNamespaceNameReference,
			resourceType,
			pathInfos,
			&yamlkit.AttributeRegistrationDetails{SetterInvocation: setterFunctionInvocation},
			true, false,
		)
		// The Namespace resource provides (rather than needs) its own namespace
		// name; its metadata.name registration as a Provides was handled above via
		// resourceTypeToNamespaceNamePath. Don't re-register it as a Needs here.
		if resourceType == namespaceResourceType {
			continue
		}
		yamlkit.RegisterPathsByAttributeName(
			rp,
			api.AttributeNameResourceName,
			resourceType,
			pathInfos,
			&yamlkit.AttributeRegistrationDetails{
				SetterInvocation: setterFunctionInvocation,
				AttributeNeedsProvidesDetails: api.AttributeNeedsProvidesDetails{
					NeededRequired: map[string]string{"ResourceType": string(namespaceResourceType)},
				},
			},
			true, false,
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
		rp,
		AttributeNameAnnotationValue,
		api.ResourceTypeAny,
		pathInfos,
		&yamlkit.AttributeRegistrationDetails{GetterInvocation: getterFunctionInvocation, SetterInvocation: setterFunctionInvocation},
		false, false,
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
		rp,
		AttributeNameLabelValue,
		api.ResourceTypeAny,
		pathInfos,
		&yamlkit.AttributeRegistrationDetails{GetterInvocation: getterFunctionInvocation, SetterInvocation: setterFunctionInvocation},
		false, false,
	)

	// Register cross-resource reference fields from CRDs.
	for resourceType, refs := range k8skit.CRDReferenceFields {
		for _, ref := range refs {
			path := api.UnresolvedPath(ref.Path)
			refPathInfos := api.PathToVisitorInfoType{
				path: {
					Path:          path,
					AttributeName: api.AttributeNameResourceName,
					DataType:      api.DataTypeString,
				},
			}
			refSetterFunctionInvocation := &api.FunctionInvocation{
				FunctionName: "set-references-of-type",
				Arguments:    []api.FunctionArgument{{ParameterName: "resource-type", Value: string(ref.Target)}},
			}
			attributeName := attributeNameForResourceType(ref.Target)
			yamlkit.RegisterPathsByAttributeName(
				rp,
				attributeName,
				resourceType,
				refPathInfos,
				&yamlkit.AttributeRegistrationDetails{
					SetterInvocation: refSetterFunctionInvocation,
					AttributeNeedsProvidesDetails: api.AttributeNeedsProvidesDetails{
						NeededRequired: map[string]string{"ResourceType": string(ref.Target)},
					},
				},
				true, false,
			)
		}
	}
}

func makeK8sFnEnsureNamespaces(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnEnsureNamespaces(rp, fArgs.Options, fArgs.ParsedData, fArgs.Arguments)
	}
}

func k8sFnEnsureNamespaces(rp *k8skit.K8sResourceProviderType, options *api.FunctionOptions, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	var extraClusterScoped map[api.ResourceType]bool
	for _, arg := range args {
		if arg.ParameterName != "cluster-scoped-types" {
			continue
		}
		types, err := api.ParseStringArrayCSV(arg.Value)
		if err != nil {
			return parsedData, nil, fmt.Errorf("cluster-scoped-types: %w", err)
		}
		if len(types) > 0 {
			extraClusterScoped = make(map[api.ResourceType]bool, len(types))
			for _, t := range types {
				extraClusterScoped[api.ResourceType(t)] = true
			}
		}
	}

	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, rp, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		nameSegments := strings.Split(string(resourceInfo.ResourceName), "/")
		if len(nameSegments) != 2 {
			return output, []error{fmt.Errorf("improperly formatted resource name %s", string(resourceInfo.ResourceName))}
		}
		if nameSegments[0] == "" {
			// No namespace. Check whether it should have one.
			if k8skit.IsResourceTypeClusterScoped(resourceInfo.ResourceType) || extraClusterScoped[resourceInfo.ResourceType] {
				return output, nil
			}
			_, err := doc.SetP(yamlkit.PlaceHolderBlockApplyString, ".metadata.namespace")
			if err != nil {
				return output, []error{err}
			}
		}
		return output, nil
	})
	if err != nil {
		return parsedData, nil, err
	}
	return parsedData, nil, nil
}

func makeK8sFnNeededNamespaces(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnNeededNamespaces(rp, fArgs.ParsedData, fArgs.Options)
	}
}

func k8sFnNeededNamespaces(rp *k8skit.K8sResourceProviderType, parsedData gaby.Container, options *api.FunctionOptions) (gaby.Container, any, error) {
	// No arguments
	resourceTypeToNamespacePath := yamlkit.GetPathRegistryForAttributeName(rp, AttributeNameNamespaceNameReference)
	values, err := yamlkit.GetNeededStringPaths(parsedData, resourceTypeToNamespacePath, []any{}, rp, options)
	return parsedData, values, err
}

func makeK8sFnSetNamespace(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnSetNamespace(rp, fArgs.Options, fArgs.ParsedData, fArgs.Arguments)
	}
}

// dnsLabelPrefixPattern matches one or more DNS labels (the service name, plus
// optionally a leading hostname.subdomain) preceding `.<oldns>.svc`. Captured as
// group 1 so it can be preserved verbatim during the replacement.
const dnsLabelPrefixPattern = `((?:[a-z0-9](?:[-a-z0-9]*[a-z0-9])?\.)+)`

// dnsSvcSuffixPattern matches `.svc` at the end of the host or followed by
// another DNS label / port / path boundary (captured as group 3).
const dnsSvcSuffixPattern = `(\.svc(?:[.:/]|$))`

// k8sFnSetNamespace is the implementation of `set-namespace`. In order:
//  1. Snapshot each resource's existing metadata.namespace (used as the
//     fall-back "old namespace" for the DNS pass below).
//  2. Run UpdateStringPaths over every path registered under
//     AttributeNameNamespaceNameReference — this renames v1/Namespace, upserts
//     metadata.namespace on every namespace-scoped resource, sets
//     subjects[*].namespace on ServiceAccount RBAC subjects, and rewrites the
//     handful of cross-resource Service-namespace references (webhook configs,
//     APIService, FluxCD Kustomization/HelmRelease).
//  3. For each resource that has containers, rewrite Service DNS names of the
//     form `<service>.<oldns>.svc[.cluster.local]` (and the headless
//     `hostname.subdomain.<oldns>.svc[...]` form) embedded in container
//     `command`, `args`, and `env[].value` strings. The "old namespace" comes
//     from the explicit `old-namespace` argument when provided, otherwise from
//     the snapshot taken in step 1.
func k8sFnSetNamespace(rp *k8skit.K8sResourceProviderType, options *api.FunctionOptions, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	var newNamespace, explicitOldNamespace string
	for _, arg := range args {
		switch arg.ParameterName {
		case "namespace-name", "":
			if newNamespace == "" {
				if s, ok := arg.Value.(string); ok {
					newNamespace = s
				}
			}
		case "old-namespace":
			if s, ok := arg.Value.(string); ok {
				explicitOldNamespace = s
			}
		}
	}
	if newNamespace == "" {
		return parsedData, nil, errors.New("set-namespace: namespace-name is required")
	}

	// Snapshot per-resource old namespace before the registry update overwrites it.
	oldNamespaces := map[int]string{}
	_, snapErr := yamlkit.VisitResourcesFiltered(parsedData, nil, rp, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		segments := strings.SplitN(string(resourceInfo.ResourceName), "/", 2)
		if len(segments) == 2 && segments[0] != "" {
			oldNamespaces[index] = segments[0]
		}
		return output, nil
	})
	if snapErr != nil {
		return parsedData, nil, snapErr
	}

	resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(rp, AttributeNameNamespaceNameReference)
	if err := yamlkit.UpdateStringPaths(parsedData, resourceTypeToPaths, []any{}, rp, newNamespace, true, options); err != nil {
		return parsedData, nil, err
	}

	var dnsErrs []error
	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, rp, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		oldNS := explicitOldNamespace
		if oldNS == "" {
			oldNS = oldNamespaces[index]
		}
		if oldNS == "" || oldNS == newNamespace {
			return output, nil
		}
		return output, rewriteContainerDNS(doc, resourceInfo.ResourceType, oldNS, newNamespace)
	})
	if err != nil {
		dnsErrs = append(dnsErrs, err)
	}
	if len(dnsErrs) > 0 {
		return parsedData, nil, errors.WithStack(errors.Join(dnsErrs...))
	}
	return parsedData, nil, nil
}

// rewriteContainerDNS scans container `command`, `args`, and `env[].value`
// strings on the given resource and replaces occurrences of
// `<service>.<oldNS>.svc[...]` with `<service>.<newNS>.svc[...]`. Returns nil
// (no errors) if the resource type has no container paths registered.
func rewriteContainerDNS(doc *gaby.YamlDoc, resourceType api.ResourceType, oldNS, newNS string) []error {
	containerPaths, ok := k8skit.ResourceTypeToContainersPaths[resourceType]
	if !ok {
		return nil
	}
	re := regexp.MustCompile(dnsLabelPrefixPattern + regexp.QuoteMeta(oldNS) + dnsSvcSuffixPattern)
	replacement := "${1}" + newNS + "${2}"
	var errs []error
	rewriteString := func(s string) (string, bool) {
		newS := re.ReplaceAllString(s, replacement)
		return newS, newS != s
	}
	setIfChanged := func(path, s string) {
		newS, changed := rewriteString(s)
		if !changed {
			return
		}
		if _, err := doc.SetP(newS, path); err != nil {
			errs = append(errs, errors.Wrapf(err, "rewriting DNS at %s", path))
		}
	}
	for _, cp := range containerPaths {
		containersNode := doc.Path(cp)
		if containersNode == nil {
			continue
		}
		for ci, container := range containersNode.Children() {
			for _, field := range []string{"command", "args"} {
				arr := container.S(field)
				if arr == nil {
					continue
				}
				for ai, elem := range arr.Children() {
					s, ok := elem.Data().(string)
					if !ok {
						continue
					}
					setIfChanged(fmt.Sprintf("%s.%d.%s.%d", cp, ci, field, ai), s)
				}
			}
			envArr := container.S("env")
			if envArr == nil {
				continue
			}
			for ei, ev := range envArr.Children() {
				valNode := ev.S("value")
				if valNode == nil {
					continue
				}
				s, ok := valNode.Data().(string)
				if !ok {
					continue
				}
				setIfChanged(fmt.Sprintf("%s.%d.env.%d.value", cp, ci, ei), s)
			}
		}
	}
	return errs
}

func makeK8sFnSetWorkloadLabels(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnSetWorkloadLabels(rp, fArgs.Options, fArgs.ParsedData, fArgs.Arguments)
	}
}

// workloadLabelPathRegistry builds the registry of pod-template-labels and pod-label-selector
// map paths for use by get-workload-labels. The same paths drive set-workload-labels.
func workloadLabelPathRegistry() api.ResourceTypeToPathToVisitorInfoType {
	registry := api.ResourceTypeToPathToVisitorInfoType{}
	for rt, paths := range k8skit.ResourceTypeToPodTemplateLabelsPaths {
		if _, ok := registry[rt]; !ok {
			registry[rt] = api.PathToVisitorInfoType{}
		}
		for _, p := range paths {
			path := api.UnresolvedPath(p)
			registry[rt][path] = &api.PathVisitorInfo{
				Path:          path,
				AttributeName: AttributeNameWorkloadLabels,
				DataType:      api.DataTypeYAML,
			}
		}
	}
	for rt, paths := range k8skit.ResourceTypeToPodLabelSelectorPaths {
		if _, ok := registry[rt]; !ok {
			registry[rt] = api.PathToVisitorInfoType{}
		}
		for _, p := range paths {
			path := api.UnresolvedPath(p)
			registry[rt][path] = &api.PathVisitorInfo{
				Path:          path,
				AttributeName: AttributeNameWorkloadLabels,
				DataType:      api.DataTypeYAML,
			}
		}
	}
	return registry
}

func makeK8sFnGetWorkloadLabels(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnGetWorkloadLabels(rp, fArgs.Options, fArgs.ParsedData)
	}
}

// k8sFnGetWorkloadLabels returns the pod-template labels map and the pod label-selector
// map at each path registered for the resource type, one AttributeValue per map. The
// returned Value is the YAML serialization of the map (DataTypeYAML).
func k8sFnGetWorkloadLabels(rp *k8skit.K8sResourceProviderType, options *api.FunctionOptions, parsedData gaby.Container) (gaby.Container, any, error) {
	values, err := yamlkit.GetPathsAnyType(
		parsedData,
		workloadLabelPathRegistry(),
		[]any{},
		rp,
		api.DataTypeYAML,
		false, false,
		options,
	)
	return parsedData, values, err
}

// k8sFnSetWorkloadLabels upserts pod-template labels and pod label-selectors on
// each visited resource. Argument values must be key=value strings; an empty
// value (key=) removes the entry. The same change is applied to every relevant
// labels/selector map registered for the resource type in k8skit. Selectors
// that reference *other* workloads (NetworkPolicy ingress/egress podSelectors,
// ServiceMonitor service selectors) are intentionally excluded — see
// k8skit.ResourceTypeToPodLabelSelectorPaths.
func k8sFnSetWorkloadLabels(rp *k8skit.K8sResourceProviderType, options *api.FunctionOptions, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	multiErrs := []error{}
	pairs := map[string]string{}
	keys := make([]string, 0, len(args))
	for _, arg := range args {
		pairString, ok := arg.Value.(string)
		if !ok {
			multiErrs = append(multiErrs, fmt.Errorf("label-key-value argument must be a string, got %T", arg.Value))
			continue
		}
		kv := strings.SplitN(pairString, "=", 2)
		if len(kv) != 2 {
			multiErrs = append(multiErrs, fmt.Errorf("invalid key-value pair: %s", pairString))
			continue
		}
		if kv[0] == "" {
			multiErrs = append(multiErrs, fmt.Errorf("invalid key: %q", kv[0]))
			continue
		}
		if _, exists := pairs[kv[0]]; !exists {
			keys = append(keys, kv[0])
		}
		pairs[kv[0]] = kv[1]
	}
	if len(pairs) == 0 {
		if len(multiErrs) != 0 {
			return parsedData, nil, errors.WithStack(errors.Join(multiErrs...))
		}
		return parsedData, nil, errors.WithStack(errors.New("no valid key-value pairs"))
	}

	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, rp, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		var visitorErrs []error
		mapPaths := append(
			append([]string{}, k8skit.ResourceTypeToPodTemplateLabelsPaths[resourceInfo.ResourceType]...),
			k8skit.ResourceTypeToPodLabelSelectorPaths[resourceInfo.ResourceType]...,
		)
		if len(mapPaths) == 0 {
			return output, nil
		}
		for _, mapPath := range mapPaths {
			for _, k := range keys {
				v := pairs[k]
				entryPath := mapPath + "." + yamlkit.EscapeDotsInPathSegment(k)
				if v == "" {
					if doc.ExistsP(entryPath) {
						if err := doc.DeleteP(entryPath); err != nil {
							visitorErrs = append(visitorErrs, errors.Wrapf(err, "error deleting %s on %s", entryPath, resourceInfo.ResourceType))
						}
					}
					continue
				}
				if _, err := doc.SetP(v, entryPath); err != nil {
					visitorErrs = append(visitorErrs, errors.Wrapf(err, "error setting %s on %s", entryPath, resourceInfo.ResourceType))
				}
			}
		}
		return output, visitorErrs
	})

	if err != nil {
		multiErrs = append(multiErrs, err)
	}
	if len(multiErrs) != 0 {
		return parsedData, nil, errors.WithStack(errors.Join(multiErrs...))
	}
	return parsedData, nil, nil
}
