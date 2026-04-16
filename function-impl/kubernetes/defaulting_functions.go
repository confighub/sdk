// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"log/slog"
	"sort"
	"strings"

	orderedmap "github.com/wk8/go-ordered-map/v2"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// defaultField describes a field path with its default value and data type.
type defaultField struct {
	path     string
	value    any
	dataType api.DataType
}

const (
	attributeNamePodSecurityDefaults             = api.AttributeName("pod-security-defaults")
	attributeNameAutomountServiceAccountToken    = api.AttributeName("automount-service-account-token")
	attributeNamePodContainerSecurityCtxDefaults = api.AttributeName("pod-container-security-context-defaults")
	attributeNameContainerResourcesDefaults      = api.AttributeName("container-resources-defaults")
)

// visitorSetter creates a $visitor FunctionInvocation with the given value.
func visitorSetter(value any) *api.FunctionInvocation {
	return &api.FunctionInvocation{
		FunctionName: yamlkit.VisitorSetterInvocationFunctionName,
		Arguments: []api.FunctionArgument{
			{Value: value},
		},
	}
}

// sortedResourceTypes returns the keys of a ResourceTypeToPathToVisitorInfoType in sorted order.
func sortedResourceTypes(m api.ResourceTypeToPathToVisitorInfoType) []api.ResourceType {
	keys := make([]api.ResourceType, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// registerDefaultPaths registers paths from a ResourceTypeToPathToVisitorInfoType in deterministic order.
func registerDefaultPaths(rp *k8skit.K8sResourceProviderType, attributeName api.AttributeName, paths api.ResourceTypeToPathToVisitorInfoType) {
	for _, resourceType := range sortedResourceTypes(paths) {
		yamlkit.RegisterPathsByAttributeName(
			rp, attributeName, resourceType,
			paths[resourceType], nil,
			false, false,
		)
	}
}

// makePodSpecDefaultPaths creates a ResourceTypeToPathToVisitorInfoType for a single field
// relative to pod spec paths, using the given attribute name and setter invocation.
func makePodSpecDefaultPaths(
	attributeName api.AttributeName,
	relativePath string,
	dataType api.DataType,
	setter *api.FunctionInvocation,
) api.ResourceTypeToPathToVisitorInfoType {
	result := api.ResourceTypeToPathToVisitorInfoType{}
	for resourceType, podSpecPaths := range k8skit.ResourceTypeToPodSpecPaths {
		pathMap := api.PathToVisitorInfoType{}
		for _, podSpecPath := range podSpecPaths {
			fullPath := api.UnresolvedPath(podSpecPath + "." + relativePath)
			pathInfo := &api.PathVisitorInfo{
				Path:          fullPath,
				AttributeName: attributeName,
				DataType:      dataType,
			}
			if setter != nil {
				pathInfo.Details = &api.AttributeDetails{
					SetterInvocations: []api.FunctionInvocation{*setter},
				}
			}
			pathMap[fullPath] = pathInfo
		}
		result[resourceType] = pathMap
	}
	return result
}

// makeContainerDefaultPaths creates a ResourceTypeToPathToVisitorInfoType for a field
// relative to each container path (containers, initContainers, ephemeralContainers).
func makeContainerDefaultPaths(
	attributeName api.AttributeName,
	relativePath string,
	dataType api.DataType,
	setter *api.FunctionInvocation,
) api.ResourceTypeToPathToVisitorInfoType {
	result := api.ResourceTypeToPathToVisitorInfoType{}
	for resourceType, containerPaths := range k8skit.ResourceTypeToContainersPaths {
		pathMap := api.PathToVisitorInfoType{}
		for _, containerPath := range containerPaths {
			fullPath := api.UnresolvedPath(containerPath + ".*." + relativePath)
			pathInfo := &api.PathVisitorInfo{
				Path:          fullPath,
				AttributeName: attributeName,
				DataType:      dataType,
			}
			if setter != nil {
				pathInfo.Details = &api.AttributeDetails{
					SetterInvocations: []api.FunctionInvocation{*setter},
				}
			}
			pathMap[fullPath] = pathInfo
		}
		result[resourceType] = pathMap
	}
	return result
}

func initDefaultingFunctions(rp *k8skit.K8sResourceProviderType) {
	// Pod security defaults: labels on Namespace resources
	namespaceResourceType := api.ResourceType("v1/Namespace")
	namespaceLabelPaths := api.PathToVisitorInfoType{}
	podSecurityLabels := []struct {
		label string
		value string
	}{
		{"pod-security.kubernetes.io/enforce", "baseline"},
		{"pod-security.kubernetes.io/enforce-version", "latest"},
		{"pod-security.kubernetes.io/warn", "restricted"},
		{"pod-security.kubernetes.io/warn-version", "latest"},
	}
	for _, entry := range podSecurityLabels {
		escapedLabel := yamlkit.EscapeDotsInPathSegment(entry.label)
		path := api.UnresolvedPath("metadata.labels." + escapedLabel)
		namespaceLabelPaths[path] = &api.PathVisitorInfo{
			Path:          path,
			AttributeName: attributeNamePodSecurityDefaults,
			DataType:      api.DataTypeString,
			Details: &api.AttributeDetails{
				SetterInvocations: []api.FunctionInvocation{*visitorSetter(entry.value)},
			},
		}
	}
	yamlkit.RegisterPathsByAttributeName(
		rp, attributeNamePodSecurityDefaults, namespaceResourceType,
		namespaceLabelPaths, nil,
		false, false,
	)

	// Automount service account token
	automountPaths := makePodSpecDefaultPaths(
		attributeNameAutomountServiceAccountToken,
		"automountServiceAccountToken",
		api.DataTypeBool,
		visitorSetter(false),
	)
	registerDefaultPaths(rp, attributeNameAutomountServiceAccountToken, automountPaths)

	// Pod-level security context defaults
	podSecCtxFields := []defaultField{
		{"securityContext.seccompProfile.type", "RuntimeDefault", api.DataTypeString},
		{"securityContext.runAsNonRoot", true, api.DataTypeBool},
		{"securityContext.runAsUser", 1000, api.DataTypeInt},
		{"securityContext.runAsGroup", 3000, api.DataTypeInt},
		{"securityContext.fsGroup", 2000, api.DataTypeInt},
	}
	for _, field := range podSecCtxFields {
		paths := makePodSpecDefaultPaths(
			attributeNamePodContainerSecurityCtxDefaults, field.path, field.dataType,
			visitorSetter(field.value),
		)
		registerDefaultPaths(rp, attributeNamePodContainerSecurityCtxDefaults, paths)
	}

	// Container-level security context defaults
	containerSecCtxFields := []defaultField{
		{"securityContext.allowPrivilegeEscalation", false, api.DataTypeBool},
		{"securityContext.privileged", false, api.DataTypeBool},
		{"securityContext.readOnlyRootFilesystem", true, api.DataTypeBool},
	}
	for _, field := range containerSecCtxFields {
		paths := makeContainerDefaultPaths(
			attributeNamePodContainerSecurityCtxDefaults, field.path, field.dataType,
			visitorSetter(field.value),
		)
		registerDefaultPaths(rp, attributeNamePodContainerSecurityCtxDefaults, paths)
	}

	// Drop all capabilities by default at the container level
	capDropPaths := makeContainerDefaultPaths(
		attributeNamePodContainerSecurityCtxDefaults,
		"securityContext.capabilities.drop",
		api.DataTypeStringArray,
		visitorSetter([]any{"ALL"}),
	)
	registerDefaultPaths(rp, attributeNamePodContainerSecurityCtxDefaults, capDropPaths)

	// Container resource defaults
	containerResourceFields := []defaultField{
		{"resources.requests.cpu", "128m", api.DataTypeString},
		{"resources.requests.memory", "128Mi", api.DataTypeString},
	}
	for _, field := range containerResourceFields {
		paths := makeContainerDefaultPaths(
			attributeNameContainerResourcesDefaults, field.path, field.dataType,
			visitorSetter(field.value),
		)
		registerDefaultPaths(rp, attributeNameContainerResourcesDefaults, paths)
	}
}

func registerDefaultingFunctions(fh handler.FunctionRegistry, rp *k8skit.K8sResourceProviderType) {
	// set-pod-security-defaults
	if err := fh.RegisterFunction("set-pod-security-defaults", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "set-pod-security-defaults",
			Mutating:              true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set pod security labels on Namespace resources (baseline enforce, restricted warn)",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         attributeNamePodSecurityDefaults,
			AffectedResourceTypes: yamlkit.ResourceTypesForAttribute(attributeNamePodSecurityDefaults, rp),
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(rp, attributeNamePodSecurityDefaults)
			err := yamlkit.UpdatePathsSetterArgument(fArgs.ParsedData, resourceTypeToPaths, []any{}, rp, true, whereFromOpts(fArgs.Options))
			return fArgs.ParsedData, nil, err
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}

	// set-automount-service-account-token-false
	if err := fh.RegisterFunction("set-automount-service-account-token-false", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "set-automount-service-account-token-false",
			Mutating:              true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set automountServiceAccountToken to false on all pod specs",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         attributeNameAutomountServiceAccountToken,
			AffectedResourceTypes: yamlkit.ResourceTypesForAttribute(attributeNameAutomountServiceAccountToken, rp),
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(rp, attributeNameAutomountServiceAccountToken)
			err := yamlkit.UpdatePathsSetterArgument(fArgs.ParsedData, resourceTypeToPaths, []any{}, rp, true, whereFromOpts(fArgs.Options))
			return fArgs.ParsedData, nil, err
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}

	// set-pod-container-security-context-defaults
	if err := fh.RegisterFunction("set-pod-container-security-context-defaults", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "set-pod-container-security-context-defaults",
			Mutating:              true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set security context defaults for pods (seccomp, runAsNonRoot, runAsUser, runAsGroup, fsGroup) and containers (readOnlyRootFilesystem, allowPrivilegeEscalation, privileged)",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         attributeNamePodContainerSecurityCtxDefaults,
			AffectedResourceTypes: yamlkit.ResourceTypesForAttribute(attributeNamePodContainerSecurityCtxDefaults, rp),
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(rp, attributeNamePodContainerSecurityCtxDefaults)
			err := yamlkit.UpdatePathsSetterArgument(fArgs.ParsedData, resourceTypeToPaths, []any{}, rp, true, whereFromOpts(fArgs.Options))
			return fArgs.ParsedData, nil, err
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}

	// set-container-resources-defaults
	if err := fh.RegisterFunction("set-container-resources-defaults", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "set-container-resources-defaults",
			Mutating:              true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set minimum resource requests (128m CPU, 128Mi memory) for containers that don't have resources set",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         attributeNameContainerResourcesDefaults,
			AffectedResourceTypes: yamlkit.ResourceTypesForAttribute(attributeNameContainerResourcesDefaults, rp),
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(rp, attributeNameContainerResourcesDefaults)
			err := yamlkit.UpdatePathsSetterArgument(fArgs.ParsedData, resourceTypeToPaths, []any{}, rp, true, whereFromOpts(fArgs.Options))
			return fArgs.ParsedData, nil, err
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}

	// set-container-probe-defaults
	resourceTypes := yamlkit.ResourceTypesForPathMap(k8skit.ResourceTypeToPodSpecPaths)
	if err := fh.RegisterFunction("set-container-probe-defaults", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "set-container-probe-defaults",
			Mutating:              true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Add liveness, readiness, and startup probes to containers that don't have them, using the first containerPort for HTTP GET probes",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: resourceTypes,
		},
		Function: makeK8sFnSetContainerProbeDefaults(rp),
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func makeK8sFnSetContainerProbeDefaults(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnSetContainerProbeDefaults(rp, fArgs.Options, fArgs.ParsedData)
	}
}

func k8sFnSetContainerProbeDefaults(rp *k8skit.K8sResourceProviderType, options *api.FunctionOptions, parsedData gaby.Container) (gaby.Container, any, error) {
	whereExpressions := api.GetWhereResourceExpressions(options)
	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, rp, whereExpressions, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		podSpecPaths, ok := k8skit.ResourceTypeToPodSpecPaths[resourceInfo.ResourceType]
		if !ok {
			return output, nil
		}

		var multiErrs []error
		for _, podSpecPath := range podSpecPaths {
			podSpecDoc, hasPodSpec, err := yamlkit.YamlSafePathGetDoc(doc, api.ResolvedPath(podSpecPath), true)
			if err != nil {
				multiErrs = append(multiErrs, err)
				continue
			}
			if !hasPodSpec {
				continue
			}

			for _, containerPath := range k8skit.ContainersPaths {
				// Skip initContainers and ephemeralContainers
				if strings.HasSuffix(containerPath, "initContainers") || strings.HasSuffix(containerPath, "ephemeralContainers") {
					continue
				}
				containersDoc, hasContainers, err := yamlkit.YamlSafePathGetDoc(podSpecDoc, api.ResolvedPath(containerPath), true)
				if err != nil {
					multiErrs = append(multiErrs, err)
					continue
				}
				if !hasContainers {
					continue
				}
				for _, containerDoc := range containersDoc.Children() {
					errs := setContainerProbeDefaults(containerDoc)
					multiErrs = append(multiErrs, errs...)
				}
			}
		}
		return output, multiErrs
	})
	if err != nil {
		return parsedData, nil, err
	}
	return parsedData, nil, nil
}

// setContainerProbeDefaults adds probes to a single container doc.
// Factored out from k8sFnSetPodDefaults for reuse.
func setContainerProbeDefaults(containerDoc *gaby.YamlDoc) []error {
	multiErrs := []error{}

	// Determine the port to use for probes
	probePort := 8080 // default port
	if containerDoc.Exists("ports") {
		portsDoc, _, err := yamlkit.YamlSafePathGetDoc(containerDoc, api.ResolvedPath("ports"), false)
		if err == nil && portsDoc != nil && len(portsDoc.Children()) > 0 {
			firstPort := portsDoc.Children()[0]
			if firstPort.Exists("containerPort") {
				if portValue, ok := firstPort.Path("containerPort").Data().(int); ok {
					probePort = portValue
				}
			}
		}
	}

	hasReadinessProbe := containerDoc.Exists("readinessProbe")
	hasLivenessProbe := containerDoc.Exists("livenessProbe")
	hasStartupProbe := containerDoc.Exists("startupProbe")

	// If readiness probe exists, don't add any probes
	if hasReadinessProbe {
		return nil
	}

	var err error

	// If liveness probe exists, copy its values for readiness probe
	if hasLivenessProbe {
		livenessProbeDoc, _, err := yamlkit.YamlSafePathGetDoc(containerDoc, api.ResolvedPath("livenessProbe"), false)
		if err == nil && livenessProbeDoc != nil {
			readinessProbe := orderedmap.New[string, any]()
			for key, childDoc := range livenessProbeDoc.ChildrenMap() {
				readinessProbe.Set(key, childDoc.Data())
			}
			readinessProbe.Set("initialDelaySeconds", 5)
			readinessProbe.Set("periodSeconds", 10)
			_, err = containerDoc.Set(readinessProbe, "readinessProbe")
			if err != nil {
				multiErrs = append(multiErrs, err)
			}
		}
	} else if hasStartupProbe {
		startupProbeDoc, _, err := yamlkit.YamlSafePathGetDoc(containerDoc, api.ResolvedPath("startupProbe"), false)
		if err == nil && startupProbeDoc != nil {
			readinessProbe := orderedmap.New[string, any]()
			for key, childDoc := range startupProbeDoc.ChildrenMap() {
				readinessProbe.Set(key, childDoc.Data())
			}
			readinessProbe.Set("initialDelaySeconds", 5)
			readinessProbe.Set("periodSeconds", 10)
			_, err = containerDoc.Set(readinessProbe, "readinessProbe")
			if err != nil {
				multiErrs = append(multiErrs, err)
			}
		}
	} else {
		// No existing probes, add all three with appropriate defaults
		httpGet := orderedmap.New[string, any]()
		httpGet.Set("path", yamlkit.PlaceHolderBlockApplyString)
		httpGet.Set("port", probePort)

		startupProbe := orderedmap.New[string, any]()
		startupProbe.Set("httpGet", httpGet)
		startupProbe.Set("initialDelaySeconds", 10)
		startupProbe.Set("periodSeconds", 10)
		startupProbe.Set("timeoutSeconds", 1)
		startupProbe.Set("failureThreshold", 30)
		startupProbe.Set("successThreshold", 1)
		_, err = containerDoc.Set(startupProbe, "startupProbe")
		if err != nil {
			multiErrs = append(multiErrs, err)
		}

		livenessProbe := orderedmap.New[string, any]()
		livenessProbe.Set("httpGet", httpGet)
		livenessProbe.Set("initialDelaySeconds", 30)
		livenessProbe.Set("periodSeconds", 10)
		livenessProbe.Set("timeoutSeconds", 1)
		livenessProbe.Set("failureThreshold", 3)
		livenessProbe.Set("successThreshold", 1)
		_, err = containerDoc.Set(livenessProbe, "livenessProbe")
		if err != nil {
			multiErrs = append(multiErrs, err)
		}

		readinessProbe := orderedmap.New[string, any]()
		readinessProbe.Set("httpGet", httpGet)
		readinessProbe.Set("initialDelaySeconds", 5)
		readinessProbe.Set("periodSeconds", 10)
		readinessProbe.Set("timeoutSeconds", 1)
		readinessProbe.Set("failureThreshold", 3)
		readinessProbe.Set("successThreshold", 1)
		_, err = containerDoc.Set(readinessProbe, "readinessProbe")
		if err != nil {
			multiErrs = append(multiErrs, err)
		}
	}

	return multiErrs
}
