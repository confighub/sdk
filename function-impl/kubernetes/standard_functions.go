// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"log/slog"

	"github.com/cockroachdb/errors"
	"github.com/yannh/kubeconform/pkg/resource"
	"github.com/yannh/kubeconform/pkg/validator"
	quantity "k8s.io/apimachinery/pkg/api/resource"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/function-impl/generic"
)

func registerStandardFunctions(fh handler.FunctionRegistry, rp *k8skit.K8sResourceProviderType) {
	generic.RegisterStandardFunctions(fh, rp, rp)

	api.InitTypeSchemas()

	// Override where-filter with an extended implementation
	if err := fh.RegisterFunction("where-filter", nil); err != nil { // clear generic function first
		slog.Error("failed to register function", "error", err)
	}
	if err := fh.RegisterFunction("where-filter", &handler.FunctionRegistration{
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
					Description:   "The specified string is an expression for the purpose of evaluating whether the configuration data matches the filter. It supports conjunctions using `AND` of relational expressions of the form *path* *operator* *literal*. The path specifications are dot-separated, for both map fields and array indices, as in `spec.template.spec.containers.0.image = 'ghcr.io/headlamp-k8s/headlamp:latest' AND spec.replicas > 1`. Path expressions support `*` for wildcard array or map segments and `?key=value` syntax for associative matches of array elements containing objects with a `key` attribute. Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `NOT LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `!~`, `~*`, `!~*`, `IN`, `NOT IN`. String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards, `ILIKE` for case-insensitive pattern matching, `NOT LIKE` and `!~~` for negated pattern matching. String regex operators: `~` for regex matching, `~*` for case-insensitive regex, `!~` and `!~*` for regex not matching (case-sensitive and insensitive). Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`. Boolean values support equality and inequality only. The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses, such as `spec.template.spec.containers.0.image#reference IN (':latest', ':arm64-latest')`. The syntax `.|` requires the preceding path to exist; otherwise the relation `!=` will always return true regardless what it is compared with. String literals are quoted with single quotes, such as `'string'`. Integer and boolean literals are also supported for attributes of those types. Kubernetes resource quantities, such as '500m' and '128Mi', may be compared.",
					DataType:      api.DataTypeString,
					Example:       "spec.template.spec.|securityContext.runAsNonRoot != true AND spec.template.spec.containers.*.|securityContext.runAsNonRoot != true",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "matched",
				Description: "True if filter passed for at least one resource, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
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
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}

	if err := fh.RegisterFunction("vet-schemas", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-schemas",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "kubernetes-version",
					Required:      false,
					Description:   "Kubernetes version to validate against, matching a version in https://github.com/yannh/kubernetes-json-schema/",
					DataType:      api.DataTypeString,
					Example:       "1.30.0",
					ValueConstraints: api.ValueConstraints{
						Regexp: `^(master|\d+\.\d+\.\d+)$`,
					},
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if schema passes validation, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
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
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
	// TODO: Deprecated in favor of vet-schemas. Remove this.
	if err := fh.RegisterFunction("validate", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "validate",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "kubernetes-version",
					Required:      false,
					Description:   "Kubernetes version to validate against, matching a version in https://github.com/yannh/kubernetes-json-schema/",
					DataType:      api.DataTypeString,
					Example:       "1.30.0",
					ValueConstraints: api.ValueConstraints{
						Regexp: `^(master|\d+\.\d+\.\d+)$`,
					},
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if schema passes validation, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
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
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

const attributeNameAppLabel = api.AttributeName("app-label")
const attributeNameConfigMapHash = api.AttributeName("configmap-hash")

var resourceTypeToLabelPrefixPaths = map[api.ResourceType][]string{
	api.ResourceType("apps/v1/Deployment"):  {"metadata.labels.", "spec.selector.matchLabels.", "spec.template.metadata.labels."},
	api.ResourceType("apps/v1/ReplicaSet"):  {"metadata.labels.", "spec.selector.matchLabels.", "spec.template.metadata.labels."},
	api.ResourceType("apps/v1/DaemonSet"):   {"metadata.labels.", "spec.selector.matchLabels.", "spec.template.metadata.labels."},
	api.ResourceType("apps/v1/StatefulSet"): {"metadata.labels.", "spec.selector.matchLabels.", "spec.template.metadata.labels."},
	api.ResourceType("v1/Pod"):              {"metadata.labels."},
	// A Service selects pods via spec.selector, which is a plain label map (not matchLabels).
	api.ResourceType("v1/Service"): {"metadata.labels.", "spec.selector."},
	// Do not set labels and selectors for Jobs and CronJobs
}

// TODO: Should we set metadata.labels for all resource types?

func initStandardFunctions(rp *k8skit.K8sResourceProviderType) {
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
			&yamlkit.AttributeRegistrationDetails{SetterInvocation: setterFunctionInvocation},
			false, false,
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
				AttributeName: api.AttributeNameResourceName,
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
			false, false,
		)
	}

	// Register the confighub.com/Hash annotation as a provided value on ConfigMaps
	// and as a needed value on workload podSpec template annotations. This enables
	// propagation of a content hash from mutable ConfigMaps to workload Deployments,
	// which triggers rolling updates when the ConfigMap content changes.
	// TODO: When a workload references multiple ConfigMaps, we need a way to combine
	// their hash values into a single annotation value.
	hashAnnotationKey := yamlkit.EscapeDotsInPathSegment(k8skit.ContextKeyPrefix + constants.HashKeySuffix)
	hashAnnotationPath := api.UnresolvedPath(k8skit.K8sContextPath(constants.HashKeySuffix))
	hashProvidedPathInfos := api.PathToVisitorInfoType{
		hashAnnotationPath: {
			Path:          hashAnnotationPath,
			AttributeName: attributeNameConfigMapHash,
			DataType:      api.DataTypeString,
		},
	}
	yamlkit.RegisterPathsByAttributeName(rp, attributeNameConfigMapHash, api.ResourceType("v1/ConfigMap"), hashProvidedPathInfos, &yamlkit.AttributeRegistrationDetails{
		GetterInvocation: &api.FunctionInvocation{
			FunctionName: "get-annotation",
			Arguments:    []api.FunctionArgument{{ParameterName: "annotation-key", Value: k8skit.ContextKeyPrefix + constants.HashKeySuffix}},
		},
	}, false, true)

	// Register the needed hash annotation on workload podSpec template metadata.
	for resourceType, podSpecPaths := range k8skit.ResourceTypeToPodSpecPaths {
		for _, podSpecPath := range podSpecPaths {
			// Derive the template metadata annotations path from the podSpec path
			// (e.g., "spec.template.spec" -> "spec.template.metadata.annotations.<key>").
			templatePath := strings.TrimSuffix(podSpecPath, ".spec")
			if templatePath == podSpecPath {
				// v1/Pod: podSpec is just "spec", template annotations go on metadata directly
				templatePath = ""
			}
			var neededPath string
			if templatePath == "" {
				neededPath = "metadata.annotations." + hashAnnotationKey
			} else {
				neededPath = templatePath + ".metadata.annotations." + hashAnnotationKey
			}
			neededPathInfos := api.PathToVisitorInfoType{
				api.UnresolvedPath(neededPath): {
					Path:          api.UnresolvedPath(neededPath),
					AttributeName: attributeNameConfigMapHash,
					DataType:      api.DataTypeString,
				},
			}
			yamlkit.RegisterPathsByAttributeName(rp, attributeNameConfigMapHash, resourceType, neededPathInfos, &yamlkit.AttributeRegistrationDetails{
				SetterInvocation: &api.FunctionInvocation{
					FunctionName: "set-annotation",
					Arguments:    []api.FunctionArgument{{ParameterName: "annotation-key", Value: k8skit.ContextKeyPrefix + constants.HashKeySuffix}},
				},
			}, true, false)
		}
	}
}

func addDescriptionToPathInfos(resourceType api.ResourceType, pathInfos api.PathToVisitorInfoType) {
	// Skip resource types without a bundled OpenAPI schema (CRDs, unknown types).
	// LookupPath would otherwise fall back to a network fetch from the CRDs-catalog
	// and log spurious "failed to find schema info" errors at registration time.
	if !resourceTypeHasBundledSchema(string(resourceType)) {
		return
	}
	for k := range pathInfos {
		schemaInfo, err := LookupPath(string(resourceType), string(pathInfos[k].Path))
		if err != nil {
			slog.Error("failed to find schema info for path", "path", string(pathInfos[k].Path), "gvk", string(resourceType), "error", err)
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
		return k8sFnResourceWhereMatch(rp, fArgs.Options, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
	}
}

func k8sFnResourceWhereMatch(rp *k8skit.K8sResourceProviderType, options *api.FunctionOptions, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	// Create custom comparator for Kubernetes resource quantities
	customComparators := []api.CustomStringComparator{
		NewResourceQuantityComparison(),
	}

	// Use the extensible generic function with the Kubernetes-specific resource quantity comparator
	return generic.GenericFnResourceWhereMatchWithComparators(rp, customComparators, options, functionContext, parsedData, args)
}

func makeK8sFnVetSchemas(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnVetSchemas(rp, fArgs.Options, fArgs.ParsedData, fArgs.Arguments)
	}
}

func k8sFnVetSchemas(rp *k8skit.K8sResourceProviderType, options *api.FunctionOptions, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	// See https://github.com/yannh/kubeconform/blob/master/pkg/validator/validator.go
	schemaLocations := []string{
		"https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/{{ .NormalizedKubernetesVersion }}-standalone{{ .StrictSuffix }}/{{ .ResourceKind }}{{ .KindSuffix }}.json",
		"https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json",
	}
	cacheDir := "/tmp/kubeconform-cache"
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		slog.Warn("failed to create kubeconform cache directory", "error", err)
		cacheDir = ""
	}
	opts := validator.Opts{Strict: true, IgnoreMissingSchemas: true, Cache: cacheDir}
	if len(args) > 0 {
		kubeVersion, ok := args[0].Value.(string)
		if ok && kubeVersion != "" {
			opts.KubernetesVersion = kubeVersion
		}
	}
	v, err := validator.New(schemaLocations, opts)
	if err != nil {
		return parsedData, api.ValidationResultFalse, errors.Wrap(err, "failed to initialize kubeconform validator")
	}

	result := api.ValidationResult{Passed: true}
	output, err := yamlkit.VisitResourcesFiltered(parsedData, &result, rp, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		vr := output.(*api.ValidationResult)
		res := resource.Resource{Bytes: doc.BytesWithoutCommentKeys()}
		valResult := v.ValidateResource(res)
		switch valResult.Status {
		case validator.Skipped, validator.Empty:
			// N/A
		case validator.Valid:
			// Passed
		case validator.Invalid:
			vr.Passed = false
			for _, validationError := range valResult.ValidationErrors {
				vr.Details = append(vr.Details, validationError.Msg)
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
							AttributeName: api.AttributeNameNone,
						},
					},
					// The Value is not relevant for schema validation, but users may find it useful.
					// TODO: Add the Value if it's a scalar (string, int, bool). Otherwise empty is fine for now.
					Value: "",
				}
				vr.FailedAttributes = append(vr.FailedAttributes, failedPath)
			}
		case validator.Error:
			vr.Passed = false
			return vr, []error{valResult.Err}
		}
		return vr, nil
	})

	if err != nil {
		// VisitResources collects errors from both GetResourceInfo failures and validator errors
		vr, _ := output.(*api.ValidationResult)
		if vr != nil && !vr.Passed {
			return parsedData, *vr, err
		}
		return parsedData, api.ValidationResultFalse, err
	}

	vr := output.(*api.ValidationResult)
	return parsedData, *vr, nil
}
