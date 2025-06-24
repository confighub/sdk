// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/join"
	"github.com/labstack/gommon/log"
	"sigs.k8s.io/kustomize/kyaml/resid"
	"sigs.k8s.io/yaml"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/internal/handlers/generic"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/third_party/gaby"
	kustomizeexcerpts "github.com/confighub/sdk/third_party/kustomize"
	"github.com/yannh/kubeconform/pkg/resource"
	"github.com/yannh/kubeconform/pkg/validator"
	quantity "k8s.io/apimachinery/pkg/api/resource"
)

func registerStandardFunctions(fh handler.FunctionRegistry) {
	generic.RegisterStandardFunctions(fh, k8skit.K8sResourceProvider, k8skit.K8sResourceProvider)

	// Override some functions with extended implementations
	fh.RegisterFunction("get-placeholders", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-placeholders",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "path",
				Description: "Resource paths containing placeholder values",
				OutputType:  api.OutputTypeAttributeValueList,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of attributes containing the placeholder string 'replaceme' or number 999999999",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: k8sFnGetPlaceholders,
	})
	fh.RegisterFunction("no-placeholders", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "no-placeholders",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if no placeholders remain, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns true if no attributes contain the placeholder string 'replaceme' or number 999999999",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: k8sFnNoPlaceholders,
	})
	fh.RegisterFunction("where-filter", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "where-filter",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (g/v/k) to match, for example apps/v1/Deployment",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "where-expression",
					Required:      true,
					Description:   "Where filter: The specified string is an expression for the purpose of evaluating whether the configuration data matches the filter. The expression syntax was inspired by SQL. It supports conjunctions using `AND` of relational expressions of the form *path* *operator* *literal*. The path specifications are dot-separated, for both map fields and array indices, as in `spec.template.spec.containers.0.image = 'ghcr.io/headlamp-k8s/headlamp:latest' AND spec.replicas > 1`. Strings and integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`. Boolean values support equality and inequality only. String literals are quoted with single quotes, such as `'string'`. Integer and boolean literals are also supported for attributes of those types.",
					DataType:      api.DataTypeString,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "matched",
				Description: "True if filter passed for at least one resource, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           `Returns true if all terms of the conjunction of relational expressions evaluate to true for at least one matching path of a resource of the specified type`,
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: k8sFnResourceWhereMatch,
	})

	// validate is custom for each toolchain
	fh.RegisterFunction("validate", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "validate",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if schema passes validation, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns true if schema passes validation",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: k8sFnValidate,
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

func initStandardFunctions() {
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
			yamlkit.RegisterProvidedPaths(k8skit.K8sResourceProvider, nbrgvk, pathInfos, getterFunctionInvocation)
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
				yamlkit.RegisterNeededPaths(k8skit.K8sResourceProvider, gvk, pathInfos, setterFunctionInvocation)
				yamlkit.RegisterPathsByAttributeName(
					k8skit.K8sResourceProvider,
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

	basicNameTemplate := generic.StandardNameTemplate(k8skit.K8sResourceProvider.NameSeparator())
	var defaultNames = api.ResourceTypeToPathToVisitorInfoType{
		api.ResourceTypeAny: {
			// In general we don't recommend changing names of resources since names are used for identifying
			// resources across mutations, but it can be useful for stamping out resources that represent
			// resource containers, such as Kubernetes Namespaces.
			api.UnresolvedPath(k8skit.K8sResourceProvider.ScopelessResourceNamePath()): {
				Path:          api.UnresolvedPath(k8skit.K8sResourceProvider.ScopelessResourceNamePath()),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
				Info:          &api.AttributeDetails{GenerationTemplate: basicNameTemplate},
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
				Info:          &api.AttributeDetails{GenerationTemplate: basicNameTemplate},
			}
			defaultNames[resourceType][api.UnresolvedPath(pathPrefix+standardAppLabel)] = &api.PathVisitorInfo{
				Path:          api.UnresolvedPath(pathPrefix + standardAppLabel),
				AttributeName: attributeNameAppLabel,
				DataType:      api.DataTypeString,
				Info:          &api.AttributeDetails{GenerationTemplate: basicNameTemplate},
			}
		}
	}
	setterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "set-default-names",
	}
	for resourceType, pathInfos := range defaultNames {
		yamlkit.RegisterPathsByAttributeName(
			k8skit.K8sResourceProvider,
			api.AttributeNameDefaultName,
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

	var attributePaths = api.ResourceTypeToPathToVisitorInfoType{
		api.ResourceType("v1/Service"): {
			api.UnresolvedPath("spec.type"): {
				Path:          api.UnresolvedPath("spec.type"),
				AttributeName: api.AttributeName("service-type"),
				DataType:      api.DataTypeString,
			},
			// TODO: more service fields
		},
	}
	for resourceType, pathInfos := range attributePaths {
		yamlkit.RegisterPathsByAttributeName(
			k8skit.K8sResourceProvider,
			api.AttributeNameGeneral,
			resourceType,
			pathInfos,
			nil,
			nil,
			true,
		)
	}

	// NOTE: workload controller paths are registered in container_functions.go
	var detailPaths = api.ResourceTypeToPathToVisitorInfoType{
		api.ResourceType("v1/Service"): {
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
			k8skit.K8sResourceProvider,
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
			if pathInfos[k].Info == nil {
				pathInfos[k].Info = &api.AttributeDetails{}
			}
			pathInfos[k].Info.Description = schemaInfo.Description
			// log.Infof("%s: %s: %s", string(resourceType), string(pathInfos[k].Path), schemaInfo.Description)
		}
	}
}

// TODO: Remove these once all originalName annotations are gone

const OriginalNameAnnotation = "confighub.com/OriginalName"

var originalNamePath = "metadata.annotations." + yamlkit.EscapeDotsInPathSegment(OriginalNameAnnotation)

func k8sFnGetPlaceholders(_ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	paths := yamlkit.FindYAMLPathsByValue(parsedData, k8skit.K8sResourceProvider, yamlkit.PlaceHolderBlockApplyString)
	// OriginalName annotations can contain replaceme values for namespaces and/or names.
	// Ignore those. They aren't a problem for apply.
	filteredPaths := make(api.AttributeValueList, 0, len(paths))
	for _, pathValue := range paths {
		// There may be one of these for each resource in the unit. Remove them all.
		if string(pathValue.Path) != originalNamePath {
			filteredPaths = append(filteredPaths, pathValue)
		}
	}
	paths = append(filteredPaths, yamlkit.FindYAMLPathsByValue(parsedData, k8skit.K8sResourceProvider, yamlkit.PlaceHolderBlockApplyInt)...)
	return parsedData, paths, nil
}

func k8sFnNoPlaceholders(_ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	paths := yamlkit.FindYAMLPathsByValue(parsedData, k8skit.K8sResourceProvider, yamlkit.PlaceHolderBlockApplyString)
	paths = append(paths, yamlkit.FindYAMLPathsByValue(parsedData, k8skit.K8sResourceProvider, yamlkit.PlaceHolderBlockApplyInt)...)
	// OriginalName annotations can contain replaceme values for namespaces and/or names.
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

// There's an extended implementation of where-filter to support resource Quantity

// Path expressions support embedded accessors and escaped dots.
// They also support wildcards and associative matches.
// Kubernetes annotations and labels permit slashes
var parameterNameRegexpString = "(?:[A-Za-z][A-Za-z0-9_\\-]{0,127})"
var pathMapSegmentRegexpString = "(?:[A-Za-z](?:[A-Za-z0-9/_\\-]|(?:\\~[12])){0,127})"
var pathMapSegmentBoundtoParameterRegexpString = "(?:@" + pathMapSegmentRegexpString + "\\:" + parameterNameRegexpString + ")"
var pathIndexSegmentRegexpString = "(?:[0-9][0-9]{0,9})"
var pathWildcardSegmentRegexpString = "\\*(?:(?:\\?" + pathMapSegmentRegexpString + "(?:\\:" + parameterNameRegexpString + ")?)|(?:@\\:" + parameterNameRegexpString + "))?"
var pathAssociativeMatchRegexpString = "\\?" + pathMapSegmentRegexpString + "(?:\\:" + parameterNameRegexpString + ")?=[^.][^.]*"
var pathSegmentRegexpString = "(?:" + pathMapSegmentRegexpString + "|" + pathMapSegmentBoundtoParameterRegexpString + "|" + pathIndexSegmentRegexpString + "|" + pathWildcardSegmentRegexpString + "|" + pathAssociativeMatchRegexpString + ")"
// Path segment without patterns (for right side of split)
var pathSegmentWithoutPatternsRegexpString = "(?:" + pathMapSegmentRegexpString + "|" + pathMapSegmentBoundtoParameterRegexpString + "|" + pathIndexSegmentRegexpString + ")"
var pathRegexpString = "^" + pathSegmentRegexpString + "(?:\\." + pathSegmentRegexpString + ")*(?:\\|" + pathSegmentWithoutPatternsRegexpString + "(?:\\." + pathSegmentWithoutPatternsRegexpString + ")*)?(?:#" + pathMapSegmentRegexpString + ")?"
var pathNameRegexp = regexp.MustCompile(pathRegexpString)
var whitespaceRegexpString = "^[ \t][ \t]*"
var whitespaceRegexp = regexp.MustCompile(whitespaceRegexpString)
var relationalOperatorRegexpString = "^(<=|>=|<|>|=|\\!=)"
var relationalOperatorRegexp = regexp.MustCompile(relationalOperatorRegexpString)
var logicalOperatorRegexpString = "^AND"
var logicalOperatorRegexp = regexp.MustCompile(logicalOperatorRegexpString)
var booleanLiteralRegexpString = "^(true|false)"
var booleanLiteralRegexp = regexp.MustCompile(booleanLiteralRegexpString)
var integerLiteralRegexpString = "^[0-9][0-9]{0,9}"
var integerLiteralRegexp = regexp.MustCompile(integerLiteralRegexpString)
var stringLiteralRegexpString = `^'[^'"\\]{0,255}'`
var stringLiteralRegexp = regexp.MustCompile(stringLiteralRegexpString)

const andOperator = "AND"

func parseLiteral(decodedQueryString string) (string, string, api.DataType, error) {
	pos := integerLiteralRegexp.FindStringIndex(decodedQueryString)
	if pos != nil {
		literal := decodedQueryString[pos[0]:pos[1]]
		decodedQueryString = decodedQueryString[pos[1]:]
		return decodedQueryString, literal, api.DataTypeInt, nil
	}
	pos = booleanLiteralRegexp.FindStringIndex(decodedQueryString)
	if pos != nil {
		literal := decodedQueryString[pos[0]:pos[1]]
		decodedQueryString = decodedQueryString[pos[1]:]
		return decodedQueryString, literal, api.DataTypeBool, nil
	}
	pos = stringLiteralRegexp.FindStringIndex(decodedQueryString)
	if pos != nil {
		literal := decodedQueryString[pos[0]:pos[1]]
		decodedQueryString = decodedQueryString[pos[1]:]
		return decodedQueryString, literal, api.DataTypeString, nil
	}

	return decodedQueryString, "", api.DataTypeNone, fmt.Errorf("no operand found at `%s`", decodedQueryString)
}

type relationalExpression struct {
	Path     string
	Operator string
	Literal  string
	DataType api.DataType
	// New fields for split path feature
	VisitorPath string // Left side of | for visitor
	SubPath     string // Right side of | for property check
	IsSplitPath bool   // Whether this uses the | syntax
}

func parseAndValidateBinaryExpression(decodedQueryString string) (string, *relationalExpression, error) {
	var expression relationalExpression

	// Whitespace should have been skipped already
	// For now, first operand is always a path name
	pos := pathNameRegexp.FindStringIndex(decodedQueryString)
	if pos == nil {
		return decodedQueryString, &expression, fmt.Errorf("invalid path at `%s`", decodedQueryString)
	}
	path := decodedQueryString[pos[0]:pos[1]]
	decodedQueryString = skipWhitespace(decodedQueryString[pos[1]:])

	// Check for split path syntax using | separator
	if strings.Contains(path, "|") {
		parts := strings.SplitN(path, "|", 2)
		if len(parts) != 2 {
			return decodedQueryString, &expression, fmt.Errorf("invalid split path syntax at `%s`", path)
		}
		expression.VisitorPath = parts[0]
		expression.SubPath = parts[1]
		expression.IsSplitPath = true
		expression.Path = path // Keep original path for compatibility
	} else {
		expression.Path = path
		expression.IsSplitPath = false
	}

	// Get the operator
	pos = relationalOperatorRegexp.FindStringIndex(decodedQueryString)
	if pos == nil {
		return decodedQueryString, &expression, fmt.Errorf("invalid operator at `%s`", decodedQueryString)
	}
	// Operator should be a valid SQL operator
	operator := decodedQueryString[pos[0]:pos[1]]
	decodedQueryString = skipWhitespace(decodedQueryString[pos[1]:])

	// Second operand must be a literal
	var literal string
	var dataType api.DataType
	var err error
	decodedQueryString, literal, dataType, err = parseLiteral(decodedQueryString)
	if err != nil {
		return decodedQueryString, &expression, err
	}
	if dataType == api.DataTypeBool && (operator != "=" && operator != "!=") {
		return decodedQueryString, &expression, fmt.Errorf("invalid boolean operator `%s`", operator)
	}

	expression.Path = path
	expression.Operator = operator
	expression.Literal = literal
	expression.DataType = dataType
	return decodedQueryString, &expression, nil
}

func skipWhitespace(decodedQueryString string) string {
	pos := whitespaceRegexp.FindStringIndex(decodedQueryString)
	if pos != nil {
		return decodedQueryString[pos[1]:]
	}
	return decodedQueryString
}

func getLogicalOperator(decodedQueryString string) (string, string) {
	pos := logicalOperatorRegexp.FindStringIndex(decodedQueryString)
	if pos != nil {
		return decodedQueryString[pos[1]:], decodedQueryString[pos[0]:pos[1]]
	}
	return decodedQueryString, ""
}

func parseAndValidateWhereFilter(queryString string) ([]*relationalExpression, error) {
	expressions := []*relationalExpression{}

	decodedQueryString := skipWhitespace(queryString)
	for decodedQueryString != "" {
		var expression *relationalExpression
		var err error
		decodedQueryString, expression, err = parseAndValidateBinaryExpression(decodedQueryString)
		if err != nil {
			return expressions, err
		}
		expressions = append(expressions, expression)
		decodedQueryString = skipWhitespace(decodedQueryString)
		var operator string
		decodedQueryString, operator = getLogicalOperator(decodedQueryString)
		if operator == andOperator {
			decodedQueryString = skipWhitespace(decodedQueryString)
		}
	}

	return expressions, nil
}

func evaluateResourceQuantityRelationalExpression(expr *relationalExpression, pathQuantity quantity.Quantity) bool {
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

func evaluateStringRelationalExpression(expr *relationalExpression, pathValue string) bool {
	stringLiteral := strings.Trim(expr.Literal, "'")
	switch expr.Operator {
	case "=":
		return pathValue == stringLiteral
	case "!=":
		return pathValue != stringLiteral
	case "<":
		return pathValue < stringLiteral
	case "<=":
		return pathValue <= stringLiteral
	case ">":
		return pathValue > stringLiteral
	case ">=":
		return pathValue >= stringLiteral
	}
	return false
}

func evaluateIntRelationalExpression(expr *relationalExpression, pathValue int) bool {
	intLiteral, err := strconv.Atoi(expr.Literal)
	if err != nil {
		return false
	}
	switch expr.Operator {
	case "=":
		return pathValue == intLiteral
	case "!=":
		return pathValue != intLiteral
	case "<":
		return pathValue < intLiteral
	case "<=":
		return pathValue <= intLiteral
	case ">":
		return pathValue > intLiteral
	case ">=":
		return pathValue >= intLiteral
	}
	return false
}

func evaluateBoolRelationalExpression(expr *relationalExpression, pathValue bool) bool {
	boolLiteral := expr.Literal == "true"
	switch expr.Operator {
	case "=":
		return pathValue == boolLiteral
	case "!=":
		return pathValue != boolLiteral
	}
	return false
}

// evaluateSplitPathExpression handles the split path syntax with | separator for Kubernetes
func evaluateSplitPathExpression(expression *relationalExpression, gvk string, parsedData gaby.Container) (map[string]bool, error) {
	matchingResources := map[string]bool{}
	
	// Use VisitPathsDoc to get to the subobjects using the visitor path (left side of |)
	resourceTypeToPaths := generic.GetVisitorMapForPath(k8skit.K8sResourceProvider, api.ResourceType(gvk), api.UnresolvedPath(expression.VisitorPath))
	
	// Custom visitor function that checks the subpath
	visitor := func(doc *gaby.YamlDoc, output any, context yamlkit.VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		// Try to get the value at the subpath within this subobject
		value, found, err := yamlkit.YamlSafePathGetValueAnyType(currentDoc, api.ResolvedPath(expression.SubPath), true)
		
		var matches bool
		if err != nil {
			return output, err
		}
		
		if !found {
			// Property not present - handle special case for != operator
			if expression.Operator == "!=" {
				matches = true // != always evaluates to true for missing properties
			} else {
				matches = false // Other operators evaluate to false for missing properties
			}
		} else {
			// Property is present - evaluate normally
			switch expression.DataType {
			case api.DataTypeString:
				if stringValue, ok := value.(string); ok {
					if resourcesPathRegexp.MatchString(expression.SubPath) {
						resourceQuantity, err := quantity.ParseQuantity(stringValue)
						if err != nil {
							return output, fmt.Errorf("invalid resource quantity %s", stringValue)
						}
						matches = evaluateResourceQuantityRelationalExpression(expression, resourceQuantity)
					} else {
						matches = evaluateStringRelationalExpression(expression, stringValue)
					}
				}
			case api.DataTypeInt:
				if intValue, ok := value.(int); ok {
					matches = evaluateIntRelationalExpression(expression, intValue)
				} else if floatValue, ok := value.(float64); ok {
					// Handle JSON numbers that parse as float64
					matches = evaluateIntRelationalExpression(expression, int(floatValue))
				}
			case api.DataTypeBool:
				if boolValue, ok := value.(bool); ok {
					matches = evaluateBoolRelationalExpression(expression, boolValue)
				}
			}
		}
		
		if matches {
			if existingOutput, ok := output.(map[string]bool); ok {
				existingOutput[string(context.ResourceName)] = true
			}
		}
		
		return output, nil
	}
	
	_, err := yamlkit.VisitPathsDoc(parsedData, resourceTypeToPaths, []any{}, matchingResources, k8skit.K8sResourceProvider, visitor)
	if err != nil {
		return nil, err
	}
	
	return matchingResources, nil
}

var resourcesPathRegexpString = "\\.resources\\.(requests|limits)\\.[a-z]+$"
var resourcesPathRegexp = regexp.MustCompile(resourcesPathRegexpString)

func k8sFnResourceWhereMatch(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
	gvk := args[0].Value.(string)
	whereExpr := args[1].Value.(string)

	// Allow blank whereExpr: filter by gvk only
	if strings.TrimSpace(whereExpr) == "" {
		_, categoryTypeMap, err := k8skit.K8sResourceProvider.ResourceAndCategoryTypeMaps(parsedData)
		if err != nil {
			return parsedData, api.ValidationResultFalse, err
		}
		for categoryType, names := range categoryTypeMap {
			// Ignore the category for now.
			if categoryType.ResourceType == api.ResourceType(gvk) && len(names) > 0 {
				return parsedData, api.ValidationResultTrue, nil
			}
		}
		return parsedData, api.ValidationResultFalse, nil
	}

	expressions, err := parseAndValidateWhereFilter(whereExpr)
	if err != nil {
		return parsedData, api.ValidationResultFalse, err
	}
	// Visit and evaluate.
	// If we allow wildcards, then theoretically the evaluation could be combinatoric to compare
	// every combination of matching paths. Luckily because we support only conjunctions, which
	// are commutative, we don't need to compare every combination. We can compare them independently
	// in any order. If any expression evaluates to false for a path that exists, then the resource
	// is not a match. However, if any resource does match, then the config Unit should match.
	// We could provide another function that accepts multiple expressions and applies a top-level
	// disjunction to them to allow for selection (e.g., based on resource type) and validation.
	// With exactly 2 expressions we could pass validation if !match_expr || validate_expr.
	var multiErrs []error
	var output any
	matchingResources := map[string]bool{}
	for i, expression := range expressions {
		// The visitor functions visit all resources of the specified type.
		// We need to keep track of which resources have matched.
		// If no paths are found for a resource, that's not a match.
		// If there are errors finding any paths, that's not a match.

		if expression.IsSplitPath {
			// Handle split path syntax with | separator
			matchingResourcesForExpression, err := evaluateSplitPathExpression(expression, gvk, parsedData)
			if err != nil {
				multiErrs = append(multiErrs, err)
				matchingResources = nil
				break
			}
			if i == 0 {
				matchingResources = matchingResourcesForExpression
			} else {
				for resourceName, _ := range matchingResources {
					_, matched := matchingResourcesForExpression[resourceName]
					if !matched {
						delete(matchingResources, resourceName)
					}
				}
			}
		} else {
			// Handle original path syntax
			getterArgs := make([]api.FunctionArgument, 2)
			getterArgs[0].Value = gvk
			getterArgs[1].Value = expression.Path
			switch expression.DataType {
			case api.DataTypeString:
				_, output, err = generic.GenericFnGetStringPath(k8skit.K8sResourceProvider, functionContext, parsedData, getterArgs, liveState)
			case api.DataTypeInt:
				_, output, err = generic.GenericFnGetIntPath(k8skit.K8sResourceProvider, functionContext, parsedData, getterArgs, liveState)
			case api.DataTypeBool:
				_, output, err = generic.GenericFnGetBoolPath(k8skit.K8sResourceProvider, functionContext, parsedData, getterArgs, liveState)
			default:
				err = fmt.Errorf("unsupported data type %s", expression.DataType)
			}
			if err != nil {
				multiErrs = append(multiErrs, err)
				matchingResources = nil
				break
			}

			matchingResourcesForExpression := map[string]bool{}
			attribValues, ok := output.(api.AttributeValueList)
			if !ok {
				log.Errorf("couldn't convert output to api.AttributeValueList")
				multiErrs = append(multiErrs, fmt.Errorf("internal error"))
				continue
			}
			for _, attribValue := range attribValues {
				//fmt.Printf("path: %s\n", attribValue.Path)
				var found bool
				switch expression.DataType {
				case api.DataTypeString:
					stringValue, ok := attribValue.Value.(string)
					if !ok {
						multiErrs = append(multiErrs, fmt.Errorf("internal error"))
					} else {
						if resourcesPathRegexp.MatchString(expression.Path) {
							resourceQuantity, err := quantity.ParseQuantity(stringValue)
							if err != nil {
								multiErrs = append(multiErrs, fmt.Errorf("invalid resource quantity %s", stringValue))
								continue
							}
							found = evaluateResourceQuantityRelationalExpression(expression, resourceQuantity)
						} else {
							found = evaluateStringRelationalExpression(expression, stringValue)
						}
					}
				case api.DataTypeInt:
					intValue, ok := attribValue.Value.(int)
					if !ok {
						multiErrs = append(multiErrs, fmt.Errorf("internal error"))
					} else {
						found = evaluateIntRelationalExpression(expression, intValue)
					}
				case api.DataTypeBool:
					boolValue, ok := attribValue.Value.(bool)
					if !ok {
						multiErrs = append(multiErrs, fmt.Errorf("internal error"))
					} else {
						found = evaluateBoolRelationalExpression(expression, boolValue)
					}
				}
				if found {
					matchingResourcesForExpression[string(attribValue.ResourceName)] = true
				}
			}
			if i == 0 {
				matchingResources = matchingResourcesForExpression
			} else {
				for resourceName, _ := range matchingResources {
					_, matched := matchingResourcesForExpression[resourceName]
					if !matched {
						delete(matchingResources, resourceName)
					}
				}
			}
		}
	}
	if len(multiErrs) != 0 {
		err = errors.Join(multiErrs...)
		return parsedData, api.ValidationResultFalse, err
	}
	if len(matchingResources) > 0 {
		return parsedData, api.ValidationResultTrue, nil
	}
	return parsedData, api.ValidationResultFalse, nil
}

func k8sFnValidate(_ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// TODO: Get CRD schemas
	v, err := validator.New(nil, validator.Opts{Strict: true, IgnoreMissingSchemas: true})
	if err != nil {
		return parsedData, api.ValidationResultFalse, errors.Wrap(err, "failed to initialize kubeconform validator")
	}
	var multiErrs []error
	details := []string{}
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
			for _, validationError := range result.ValidationErrors {
				details = append(details, validationError.Msg)
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

	return parsedData, failureResult, join.Join(multiErrs...)
}
