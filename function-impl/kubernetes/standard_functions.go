// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/join"
	"github.com/labstack/gommon/log"
	"github.com/swaggest/jsonschema-go"
	"github.com/yannh/kubeconform/pkg/resource"
	"github.com/yannh/kubeconform/pkg/validator"
	quantity "k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/kustomize/kyaml/resid"
	"sigs.k8s.io/yaml"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/function-impl/generic"
	"github.com/confighub/sdk/third_party/gaby"
	kustomizeexcerpts "github.com/confighub/sdk/function-impl/third_party/kustomize"
)

func registerStandardFunctions(fh handler.FunctionRegistry, rp *k8skit.K8sResourceProviderType) {
	generic.RegisterStandardFunctions(fh, rp, rp)

	reflector := jsonschema.Reflector{}
	attributeValueListSchema, err := reflector.Reflect(api.AttributeValueList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.AttributeValueList")
	}
	validationResultListSchema, err := reflector.Reflect(api.ValidationResultList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.ValidationResultList")
	}
	// Override some functions with extended implementations
	fh.RegisterFunction("get-placeholders", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-placeholders",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "path",
				Description: "Resource paths containing placeholder values",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &attributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of attributes containing the placeholder string 'confighubplaceholder' or number 999999999",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: makeK8sFnGetPlaceholders(rp),
	})
	fh.RegisterFunction("no-placeholders", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "no-placeholders",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if no placeholders remain, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns true if no attributes contain the placeholder string 'confighubplaceholder' or number 999999999",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: makeK8sFnNoPlaceholders(rp),
	})
	fh.RegisterFunction("where-filter", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "where-filter",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (API group/version/kind) to match, for example apps/v1/Deployment",
					DataType:      api.DataTypeString,
					Example:       "apps/v1/Deployment",
				},
				{
					ParameterName: "where-expression",
					Required:      true,
					Description:   "The specified string is an expression for the purpose of evaluating whether the configuration data matches the filter. It supports conjunctions using `AND` of relational expressions of the form *path* *operator* *literal*. The path specifications are dot-separated, for both map fields and array indices, as in `spec.template.spec.containers.0.image = 'ghcr.io/headlamp-k8s/headlamp:latest' AND spec.replicas > 1`. Path expressions support `*` for wildcard array or map segments and `?key=value` syntax for associative matches of array elements containing objects with a `key` attribute. Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `!~`, `~*`, `!~*`, `IN`, `NOT IN`. String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards, `ILIKE` for case-insensitive pattern matching, `!~~` for NOT LIKE. String regex operators: `~` for regex matching, `~*` for case-insensitive regex, `!~` and `!~*` for regex not matching (case-sensitive and insensitive). Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`. Boolean values support equality and inequality only. The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses, such as `spec.template.spec.containers.0.image#reference IN (':latest', ':arm64-latest')`. The syntax `.|` requires the preceding path to exist; otherwise the relation `!=` will always return true regardless what it is compared with. String literals are quoted with single quotes, such as `'string'`. Integer and boolean literals are also supported for attributes of those types. Kubernetes resource quantities, such as '500m' and '128Mi', may be compared.",
					DataType:      api.DataTypeString,
					Example:       "spec.template.spec.|securityContext.runAsNonRoot != true AND spec.template.spec.containers.*.|securityContext.runAsNonRoot != true",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "matched",
				Description: "True if filter passed for at least one resource, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           `Returns true if all terms of the conjunction of relational expressions evaluate to true for at least one matching path of a resource of the specified type`,
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: makeK8sFnResourceWhereMatch(rp),
	})

	fh.RegisterFunction("vet-schemas", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-schemas",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if schema passes validation, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns true if schema passes validation",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: makeK8sFnVetSchemas(rp),
	})
	// TODO: Deprecated in favor of vet-schemas. Remove this.
	fh.RegisterFunction("validate", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "validate",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if schema passes validation, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns true if schema passes validation",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: makeK8sFnVetSchemas(rp),
	})
}

var noncoreDefaultGroup = map[string]string{
	"Deployment":              "apps",
	"ReplicaSet":              "apps",
	"StatefulSet":             "apps",
	"DaemonSet":               "apps",
	"Job":                     "batch",
	"CronJob":                 "batch",
	"HorizontalPodAutoscaler": "autoscaling",
	"Role":                    "rbac.authorization.k8s.io",
	"ClusterRole":             "rbac.authorization.k8s.io",
	"RoleBinding":             "rbac.authorization.k8s.io",
	"ClusterRoleBinding":      "rbac.authorization.k8s.io",
	"Ingress":                 "networking.k8s.io",
	"IngressClass":            "networking.k8s.io",
}

func gvkString(gvk resid.Gvk) api.ResourceType {
	g := gvk.Group
	v := gvk.Version
	k := gvk.Kind
	if g == "" {
		g = noncoreDefaultGroup[k]
	}
	if v == "" {
		if k == "HorizontalPodAutoscaler" {
			v = "v2"
		} else {
			v = "v1"
		}
	}
	if k == "" {
		// This shouldn't happen
		k = "NoKind"
	}
	if g == "" {
		return api.ResourceType(v + "/" + k)
	}
	return api.ResourceType(g + "/" + v + "/" + k)
}

func attributeNameForResourceType(resourceType api.ResourceType) api.AttributeName {
	return api.AttributeName(string(api.AttributeNameResourceName) + "/" + string(resourceType))
}

var segmentIsArray = map[string]struct{}{
	"containers":       {},
	"initContainers":   {},
	"volumes":          {},
	"env":              {},
	"envFrom":          {},
	"sources":          {},
	"imagePullSecrets": {},
	"parameters":       {},
	"paths":            {},
	"webhooks":         {},
	"subjects":         {},
	"apiGroups":        {},
	"nonResourceURLs":  {},
	"resources":        {},
	"resourceNames":    {},
	"verbs":            {},
	"rules":            {}, // in both Roles/ClusterRoles and Ingress
}

const attributeNameAppLabel = api.AttributeName("app-label")
const attributeNameDefaultNames = api.AttributeName("default-name")

var resourceTypeToLabelPrefixPaths = map[api.ResourceType][]string{
	api.ResourceType("apps/v1/Deployment"):  {"metadata.labels.", "spec.selector.matchLabels.", "spec.template.metadata.labels."},
	api.ResourceType("apps/v1/ReplicaSet"):  {"metadata.labels.", "spec.selector.matchLabels.", "spec.template.metadata.labels."},
	api.ResourceType("apps/v1/DaemonSet"):   {"metadata.labels.", "spec.selector.matchLabels.", "spec.template.metadata.labels."},
	api.ResourceType("apps/v1/StatefulSet"): {"metadata.labels.", "spec.selector.matchLabels.", "spec.template.metadata.labels."},
	api.ResourceType("v1/Pod"):              {"metadata.labels."},
	// Do not set labels and selectors for Jobs and CronJobs
}

func initStandardFunctions(rp *k8skit.K8sResourceProviderType) {
	namespaceNbrs := kustomizeexcerpts.NbrSlice{}
	err := yaml.Unmarshal([]byte(kustomizeexcerpts.NameReferenceFieldSpecs), &namespaceNbrs)
	if err != nil {
		log.Errorf("couldn't unmarshal NameReferenceFieldSpecs: %v", err)
	} else {
		// Split the backreferences by type and also invert the backreferences to references
		for _, nbr := range namespaceNbrs {
			nbrgvk := gvkString(nbr.Gvk)
			attributeName := attributeNameForResourceType(nbrgvk)
			pathInfos := api.PathToVisitorInfoType{
				api.UnresolvedPath("metadata.name"): {
					Path:          api.UnresolvedPath("metadata.name"),
					AttributeName: api.AttributeNameResourceName,
					DataType:      api.DataTypeString,
				},
			}
			// Function to get the value.
			getterFunctionInvocation := &api.FunctionInvocation{
				FunctionName: "get-resources-of-type",
				Arguments:    []api.FunctionArgument{{ParameterName: "resource-type", Value: nbrgvk}},
			}
			yamlkit.RegisterProvidedPaths(rp, nbrgvk, pathInfos, getterFunctionInvocation)
			for _, field := range nbr.Referrers {
				gvk := gvkString(field.Gvk)
				// This is kind of hacky in lieu of actual schemas. Kustomize always searches arrays.
				pathSegments := strings.Split(field.Path, "/")
				for i, pathSegment := range pathSegments {
					_, ok := segmentIsArray[pathSegment]
					if ok {
						pathSegments[i] = pathSegment + ".*"
					}
				}
				// NOTE: We'd need to insert a path segment above in order to use yamlkit.JoinPathSegments.
				// Kubernetes resources don't have fields with dots in their paths, fortunately.
				path := api.UnresolvedPath(strings.Join(pathSegments, "."))
				pathInfos = api.PathToVisitorInfoType{
					path: {
						Path:          path,
						AttributeName: api.AttributeNameResourceName,
						DataType:      api.DataTypeString,
					},
				}
				// Function to set the value. The parameters are expected to match the corresponding
				// get function's parameters plus its result.
				setterFunctionInvocation := &api.FunctionInvocation{
					FunctionName: "set-references-of-type",
					Arguments:    []api.FunctionArgument{{ParameterName: "resource-type", Value: nbrgvk}},
				}
				yamlkit.RegisterNeededPaths(rp, gvk, pathInfos, setterFunctionInvocation)
				yamlkit.RegisterPathsByAttributeName(
					rp,
					attributeName,
					gvk,
					pathInfos,
					nil,
					setterFunctionInvocation,
					false,
				)
			}
		}
	}

	var defaultNames = api.ResourceTypeToPathToVisitorInfoType{
		api.ResourceTypeAny: {
			// In general we don't recommend changing names of resources since names are used for identifying
			// resources across mutations, but it is necessary for "container" resources, such as Kubernetes Namespaces.
			api.UnresolvedPath(rp.ScopelessResourceNamePath()): {
				Path:          api.UnresolvedPath(rp.ScopelessResourceNamePath()),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
	}
	simpleAppLabel := "app"
	standardAppLabel := yamlkit.EscapeDotsInPathSegment("app.kubernetes.io/name")
	for resourceType, pathPrefxes := range resourceTypeToLabelPrefixPaths {
		defaultNames[resourceType] = api.PathToVisitorInfoType{}
		for _, pathPrefix := range pathPrefxes {
			defaultNames[resourceType][api.UnresolvedPath(pathPrefix+simpleAppLabel)] = &api.PathVisitorInfo{
				Path:          api.UnresolvedPath(pathPrefix + simpleAppLabel),
				AttributeName: attributeNameAppLabel,
				DataType:      api.DataTypeString,
			}
			defaultNames[resourceType][api.UnresolvedPath(pathPrefix+standardAppLabel)] = &api.PathVisitorInfo{
				Path:          api.UnresolvedPath(pathPrefix + standardAppLabel),
				AttributeName: attributeNameAppLabel,
				DataType:      api.DataTypeString,
			}
		}
	}
	setterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "set-default-names",
	}
	for resourceType, pathInfos := range defaultNames {
		yamlkit.RegisterPathsByAttributeName(
			rp,
			api.AttributeNameDefaultName,
			resourceType,
			pathInfos,
			nil,
			setterFunctionInvocation,
			false,
		)
	}

	// NOTE: workload controller paths are registered in container_functions.go
	var detailPaths = api.ResourceTypeToPathToVisitorInfoType{
		api.ResourceType("v1/Service"): {
			api.UnresolvedPath("spec.type"): {
				Path:          api.UnresolvedPath("spec.type"),
				AttributeName: api.AttributeName("service-type"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("spec.ports.*.port"): {
				Path:          api.UnresolvedPath("spec.ports.*.port"),
				AttributeName: api.AttributeName("port"),
				DataType:      api.DataTypeInt,
			},
			api.UnresolvedPath("spec.ports.*.targetPort"): {
				Path:          api.UnresolvedPath("spec.ports.*.targetPort"),
				AttributeName: api.AttributeName("target-port"),
				DataType:      api.DataTypeInt,
			},
		},
		api.ResourceType("networking.k8s.io/v1/Ingress"): {
			api.UnresolvedPath("spec.rules.*.host"): {
				Path:          api.UnresolvedPath("spec.rules.*.host"),
				AttributeName: api.AttributeNameHostname,
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("spec.rules.*.http.paths.*.path"): {
				Path:          api.UnresolvedPath("spec.rules.*.http.paths.*.path"),
				AttributeName: api.AttributeName("uri-path"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("spec.rules.*.http.paths.*.backend.service.name"): {
				Path:          api.UnresolvedPath("spec.rules.*.http.paths.*.backend.service.name"),
				AttributeName: api.AttributeName("backend-service-name"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("spec.rules.*.http.paths.*.backend.service.port.number"): {
				Path:          api.UnresolvedPath("spec.rules.*.http.paths.*.backend.service.port.number"),
				AttributeName: api.AttributeName("backend-service-port"),
				DataType:      api.DataTypeInt,
			},
		},
		api.ResourceType("networking.k8s.io/v1/IngressClass"): {
			api.UnresolvedPath("spec.controller"): {
				Path:          api.UnresolvedPath("spec.controller"),
				AttributeName: api.AttributeName("ingress-controller"),
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType("v1/ServiceAccount"): {
			api.UnresolvedPath("automountServiceAccountToken"): {
				Path:          api.UnresolvedPath("automountServiceAccountToken"),
				AttributeName: api.AttributeName("automount-token"),
				DataType:      api.DataTypeBool,
			},
		},
		api.ResourceType("rbac.authorization.k8s.io/v1/Role"): {
			api.UnresolvedPath("rules.*.apiGroups.*"): {
				Path:          api.UnresolvedPath("rules.*.apiGroups.*"),
				AttributeName: api.AttributeName("api-group"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("rules.*.resources.*"): {
				Path:          api.UnresolvedPath("rules.*.resources.*"),
				AttributeName: api.AttributeName("resource-type"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("rules.*.resourceNames.*"): {
				Path:          api.UnresolvedPath("rules.*.resourceNames.*"),
				AttributeName: api.AttributeName("resource-name"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("rules.*.verbs.*"): {
				Path:          api.UnresolvedPath("rules.*.verbs.*"),
				AttributeName: api.AttributeName("verb"),
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType("rbac.authorization.k8s.io/v1/ClusterRole"): {
			api.UnresolvedPath("rules.*.apiGroups.*"): {
				Path:          api.UnresolvedPath("rules.*.apiGroups.*"),
				AttributeName: api.AttributeName("api-group"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("rules.*.resources.*"): {
				Path:          api.UnresolvedPath("rules.*.resources.*"),
				AttributeName: api.AttributeName("resource-type"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("rules.*.resourceNames.*"): {
				Path:          api.UnresolvedPath("rules.*.resourceNames.*"),
				AttributeName: api.AttributeName("resource-name"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("rules.*.verbs.*"): {
				Path:          api.UnresolvedPath("rules.*.verbs.*"),
				AttributeName: api.AttributeName("verb"),
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType("rbac.authorization.k8s.io/v1/RoleBinding"): {
			api.UnresolvedPath("roleRef.name"): {
				Path:          api.UnresolvedPath("roleRef.name"),
				AttributeName: api.AttributeName("role-name"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("roleRef.kind"): {
				Path:          api.UnresolvedPath("roleRef.kind"),
				AttributeName: api.AttributeName("role-kind"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("subjects.*.name"): {
				Path:          api.UnresolvedPath("subjects.*.name"),
				AttributeName: api.AttributeName("subject-name"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("subjects.*.namespace"): {
				Path:          api.UnresolvedPath("subjects.*.namespace"),
				AttributeName: api.AttributeName("subject-namespace"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("subjects.*.kind"): {
				Path:          api.UnresolvedPath("subjects.*.kind"),
				AttributeName: api.AttributeName("subject-kind"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("subjects.*.apiGroup"): {
				Path:          api.UnresolvedPath("subjects.*.apiGroup"),
				AttributeName: api.AttributeName("subject-api-group"),
				DataType:      api.DataTypeString,
			},
		},
		api.ResourceType("rbac.authorization.k8s.io/v1/ClusterRoleBinding"): {
			api.UnresolvedPath("roleRef.name"): {
				Path:          api.UnresolvedPath("roleRef.name"),
				AttributeName: api.AttributeName("role-name"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("roleRef.kind"): {
				Path:          api.UnresolvedPath("roleRef.kind"),
				AttributeName: api.AttributeName("role-kind"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("subjects.*.name"): {
				Path:          api.UnresolvedPath("subjects.*.name"),
				AttributeName: api.AttributeName("subject-name"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("subjects.*.namespace"): {
				Path:          api.UnresolvedPath("subjects.*.namespace"),
				AttributeName: api.AttributeName("subject-namespace"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("subjects.*.kind"): {
				Path:          api.UnresolvedPath("subjects.*.kind"),
				AttributeName: api.AttributeName("subject-kind"),
				DataType:      api.DataTypeString,
			},
			api.UnresolvedPath("subjects.*.apiGroup"): {
				Path:          api.UnresolvedPath("subjects.*.apiGroup"),
				AttributeName: api.AttributeName("subject-api-group"),
				DataType:      api.DataTypeString,
			},
		},
	}
	for resourceType, pathInfos := range detailPaths {
		addDescriptionToPathInfos(resourceType, pathInfos)
		yamlkit.RegisterPathsByAttributeName(
			rp,
			api.AttributeNameDetail,
			resourceType,
			pathInfos,
			nil,
			nil,
			false,
		)
	}
}

func addDescriptionToPathInfos(resourceType api.ResourceType, pathInfos api.PathToVisitorInfoType) {
	for k := range pathInfos {
		schemaInfo, err := LookupPath(string(resourceType), string(pathInfos[k].Path))
		if err != nil {
			log.Errorf("failed to find schema info for path %s of group/version/kind %s: %v", string(pathInfos[k].Path), string(resourceType), err)
		}
		if err == nil && schemaInfo.Description != "" {
			if pathInfos[k].Details == nil {
				pathInfos[k].Details = &api.AttributeDetails{}
			}
			pathInfos[k].Details.Description = schemaInfo.Description
			// log.Infof("%s: %s: %s", string(resourceType), string(pathInfos[k].Path), schemaInfo.Description)
		}
	}
}

// TODO: Remove these once all originalName annotations are gone

const OriginalNameAnnotation = "confighub.com/OriginalName"

var originalNamePath = "metadata.annotations." + yamlkit.EscapeDotsInPathSegment(OriginalNameAnnotation)

func makeK8sFnGetPlaceholders(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnGetPlaceholders(rp, fArgs.ParsedData)
	}
}

func k8sFnGetPlaceholders(rp *k8skit.K8sResourceProviderType, parsedData gaby.Container) (gaby.Container, any, error) {
	paths := yamlkit.FindYAMLPathsByValue(parsedData, rp, yamlkit.PlaceHolderBlockApplyString)
	// OriginalName annotations can contain confighubplaceholder values for namespaces and/or names.
	// Ignore those. They aren't a problem for apply.
	filteredPaths := make(api.AttributeValueList, 0, len(paths))
	for _, pathValue := range paths {
		// There may be one of these for each resource in the unit. Remove them all.
		if string(pathValue.Path) != originalNamePath {
			filteredPaths = append(filteredPaths, pathValue)
		}
	}
	paths = append(filteredPaths, yamlkit.FindYAMLPathsByValue(parsedData, rp, yamlkit.PlaceHolderBlockApplyInt)...)
	return parsedData, paths, nil
}

func makeK8sFnNoPlaceholders(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnNoPlaceholders(rp, fArgs.ParsedData)
	}
}

func k8sFnNoPlaceholders(rp *k8skit.K8sResourceProviderType, parsedData gaby.Container) (gaby.Container, any, error) {
	paths := yamlkit.FindYAMLPathsByValue(parsedData, rp, yamlkit.PlaceHolderBlockApplyString)
	paths = append(paths, yamlkit.FindYAMLPathsByValue(parsedData, rp, yamlkit.PlaceHolderBlockApplyInt)...)
	// OriginalName annotations can contain confighubplaceholder values for namespaces and/or names.
	// Ignore those. They aren't a problem for apply.
	filteredPaths := make(api.AttributeValueList, 0, len(paths))
	for _, pathValue := range paths {
		// There may be one of these for each resource in the unit. Remove them all.
		if string(pathValue.Path) != originalNamePath {
			filteredPaths = append(filteredPaths, pathValue)
		}
	}
	result := api.ValidationResult{
		Passed: len(filteredPaths) == 0,
	}
	return parsedData, result, nil
}

// Kubernetes-specific resource quantity handling

func evaluateResourceQuantityRelationalExpression(expr *api.RelationalExpression, pathQuantity quantity.Quantity) bool {
	stringLiteral := strings.Trim(expr.Literal, "'")
	exprQuantity, err := quantity.ParseQuantity(stringLiteral)
	if err != nil {
		return false
	}
	switch expr.Operator {
	case "=":
		return pathQuantity.Equal(exprQuantity)
	case "!=":
		return !pathQuantity.Equal(exprQuantity)
	case "<":
		return pathQuantity.Cmp(exprQuantity) < 0
	case "<=":
		return pathQuantity.Cmp(exprQuantity) <= 0
	case ">":
		return pathQuantity.Cmp(exprQuantity) > 0
	case ">=":
		return pathQuantity.Cmp(exprQuantity) >= 0
	}
	return false
}

var resourcesPathRegexpString = "\\.resources\\.(requests|limits)\\.[a-z]+$"
var resourcesPathRegexp = regexp.MustCompile(resourcesPathRegexpString)

// ResourceQuantityComparison implements CustomStringComparator for Kubernetes resource quantities
type ResourceQuantityComparison struct {
	pathRegexp *regexp.Regexp
}

// NewResourceQuantityComparison creates a new ResourceQuantityComparison instance
func NewResourceQuantityComparison() *ResourceQuantityComparison {
	return &ResourceQuantityComparison{
		pathRegexp: resourcesPathRegexp,
	}
}

// MatchesPath implements CustomStringComparator.MatchesPath
func (r *ResourceQuantityComparison) MatchesPath(path string) bool {
	return r.pathRegexp.MatchString(path)
}

// Evaluate implements CustomStringComparator.Evaluate
func (r *ResourceQuantityComparison) Evaluate(expr *api.RelationalExpression, value string) (bool, error) {
	return evaluateResourceQuantityComparison(expr, value)
}

// evaluateResourceQuantityComparison wraps resource quantity parsing and comparison
func evaluateResourceQuantityComparison(expr *api.RelationalExpression, value string) (bool, error) {
	resourceQuantity, err := quantity.ParseQuantity(value)
	if err != nil {
		return false, fmt.Errorf("invalid resource quantity %s: %w", value, err)
	}
	return evaluateResourceQuantityRelationalExpression(expr, resourceQuantity), nil
}

func makeK8sFnResourceWhereMatch(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnResourceWhereMatch(rp, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
	}
}

func k8sFnResourceWhereMatch(rp *k8skit.K8sResourceProviderType, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	// Create custom comparator for Kubernetes resource quantities
	customComparators := []api.CustomStringComparator{
		NewResourceQuantityComparison(),
	}

	// Use the extensible generic function with the Kubernetes-specific resource quantity comparator
	return generic.GenericFnResourceWhereMatchWithComparators(rp, customComparators, functionContext, parsedData, args)
}

func makeK8sFnVetSchemas(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnVetSchemas(rp, fArgs.ParsedData, fArgs.Arguments)
	}
}

func k8sFnVetSchemas(rp *k8skit.K8sResourceProviderType, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	// See https://github.com/yannh/kubeconform/blob/master/pkg/validator/validator.go
	schemaLocations := []string{
		"https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/{{ .NormalizedKubernetesVersion }}-standalone{{ .StrictSuffix }}/{{ .ResourceKind }}{{ .KindSuffix }}.json",
		"https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json",
	}
	// TODO: Cache once an emptyDir is added
	v, err := validator.New(schemaLocations, validator.Opts{Strict: true, IgnoreMissingSchemas: true})
	if err != nil {
		return parsedData, api.ValidationResultFalse, errors.Wrap(err, "failed to initialize kubeconform validator")
	}
	var multiErrs []error
	details := []string{}
	failedPaths := api.AttributeValueList{}
	passed := true
	for _, doc := range parsedData {
		res := resource.Resource{Bytes: doc.Bytes()}
		result := v.ValidateResource(res)
		switch result.Status {
		case validator.Skipped, validator.Empty:
			// N/A
		case validator.Valid:
			// Passed
		case validator.Invalid:
			passed = false
			resourceInfo, err := yamlkit.GetResourceInfo(doc, rp)
			if err != nil {
				resourceInfo = &api.ResourceInfo{}
				details = append(details, err.Error())
			}
			for _, validationError := range result.ValidationErrors {
				details = append(details, validationError.Msg)
				// This path will be the parent of a bogus path. Try to parse the field out of the message.
				path := gaby.JSONPointerToPath(validationError.Path)
				if strings.HasPrefix(validationError.Msg, "additionalProperties '") {
					field, _, found := strings.Cut(strings.TrimPrefix(validationError.Msg, "additionalProperties '"), "'")
					if found {
						path += "." + field
					}
				}
				failedPath := api.AttributeValue{
					AttributeInfo: api.AttributeInfo{
						AttributeIdentifier: api.AttributeIdentifier{
							ResourceInfo: *resourceInfo,
							Path:         api.ResolvedPath(path),
						},
						AttributeMetadata: api.AttributeMetadata{
							AttributeName: api.AttributeNameNone, // TODO: look up possibly known attribute?
						},
					},
					// Is Value relevant? Should the message go in the Value? For now, set the Value so that it's not null.
					Value: "",
				}
				failedPaths = append(failedPaths, failedPath)
			}
		case validator.Error:
			passed = false
			multiErrs = append(multiErrs, result.Err)
		}
	}

	if passed {
		return parsedData, api.ValidationResultTrue, nil
	}

	failureResult := api.ValidationResultFalse
	failureResult.Details = details
	failureResult.FailedAttributes = failedPaths

	return parsedData, failureResult, join.Join(multiErrs...)
}
