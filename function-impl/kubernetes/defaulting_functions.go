// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"log/slog"
	"strings"

	orderedmap "github.com/wk8/go-ordered-map/v2"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

const (
	attributeNamePodSecurityDefaults             = api.AttributeName("pod-security-defaults")
	attributeNameAutomountServiceAccountToken    = api.AttributeName("automount-service-account-token")
	attributeNamePodContainerSecurityCtxDefaults = api.AttributeName("pod-container-security-context-defaults")
	attributeNameContainerResourcesDefaults      = api.AttributeName("container-resources-defaults")
)

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
			Details:       &api.AttributeDetails{DefaultValue: entry.value},
		}
	}
	yamlkit.RegisterPathsByAttributeName(
		rp, attributeNamePodSecurityDefaults, namespaceResourceType,
		namespaceLabelPaths, nil,
		false, false,
	)
}

func registerDefaultingFunctions(fh handler.FunctionRegistry, rp *k8skit.K8sResourceProviderType) {
	// set-pod-security-defaults
	if err := fh.RegisterFunction("set-pod-security-defaults", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "set-pod-security-defaults",
			Mutating:              true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set pod security labels on Namespace resources (baseline enforce, restricted warn). Only labels that are missing or hold a placeholder are set, so it is safe to run again",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         attributeNamePodSecurityDefaults,
			AffectedResourceTypes: yamlkit.ResourceTypesForAttribute(attributeNamePodSecurityDefaults, rp),
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(rp, attributeNamePodSecurityDefaults)
			err := yamlkit.UpdatePathsSetterArgument(fArgs.ParsedData, resourceTypeToPaths, []any{}, rp, true, fArgs.Options)
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
			Description:           "Set automountServiceAccountToken to false on all pod specs that do not set it. A pod that calls the Kubernetes API needs it set to true",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         attributeNameAutomountServiceAccountToken,
			AffectedResourceTypes: yamlkit.ResourceTypesForAttribute(attributeNameAutomountServiceAccountToken, rp),
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(rp, attributeNameAutomountServiceAccountToken)
			err := yamlkit.UpdatePathsSetterArgument(fArgs.ParsedData, resourceTypeToPaths, []any{}, rp, true, fArgs.Options)
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
			Description:           "Set security context defaults for pods (seccomp, runAsNonRoot, runAsUser, runAsGroup, fsGroup) and containers (readOnlyRootFilesystem, allowPrivilegeEscalation, privileged), where they are missing or hold a placeholder. A container that is privileged or adds the SYS_ADMIN capability gets allowPrivilegeEscalation true, since either one grants the escalation anyway (Kubernetes rejects false beside privileged or CAP_SYS_ADMIN). With readOnlyRootFilesystem true, every path a container writes to needs a volume mounted there, such as an emptyDir set with set-container-volume-mount-path",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         attributeNamePodContainerSecurityCtxDefaults,
			AffectedResourceTypes: yamlkit.ResourceTypesForAttribute(attributeNamePodContainerSecurityCtxDefaults, rp),
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(rp, attributeNamePodContainerSecurityCtxDefaults)
			err := yamlkit.UpdatePathsSetterArgument(fArgs.ParsedData, resourceTypeToPaths, []any{}, rp, true, fArgs.Options)
			if err != nil {
				return fArgs.ParsedData, nil, err
			}
			return allowPrivilegeEscalationWhereGranted(rp, fArgs.Options, fArgs.ParsedData)
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
			Description:           "Set minimum resource requests (128m CPU, 128Mi memory) for containers that don't have resources set. Values already set, other than placeholders, are left alone, so it is safe to run again",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         attributeNameContainerResourcesDefaults,
			AffectedResourceTypes: yamlkit.ResourceTypesForAttribute(attributeNameContainerResourcesDefaults, rp),
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(rp, attributeNameContainerResourcesDefaults)
			err := yamlkit.UpdatePathsSetterArgument(fArgs.ParsedData, resourceTypeToPaths, []any{}, rp, true, fArgs.Options)
			return fArgs.ParsedData, nil, err
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}

	// set-container-probe-defaults
	resourceTypes := k8skit.PodSpecResourceTypes()
	if err := fh.RegisterFunction("set-container-probe-defaults", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-container-probe-defaults",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "liveness-path",
					Required:      false,
					Description:   "URL path for the liveness probe HTTP GET. Defaults to /healthz.",
					DataType:      api.DataTypeString,
					Example:       "/internal/ok",
				},
				{
					ParameterName: "readiness-path",
					Required:      false,
					Description:   "URL path for the readiness probe HTTP GET. Defaults to /healthz.",
					DataType:      api.DataTypeString,
					Example:       "/internal/ready",
				},
				{
					ParameterName: "startup-path",
					Required:      false,
					Description:   "URL path for the startup probe HTTP GET. Defaults to /healthz.",
					DataType:      api.DataTypeString,
					Example:       "/internal/ok",
				},
			},
			Mutating:              true,
			Hermetic:              true,
			Idempotent:            true,
			Replayable:            true,
			Description:           "Add liveness, readiness, and startup probes to containers that don't have them, as HTTP GET probes of the first containerPort, at the paths the optional parameters give. A container that does not serve HTTP, such as a database, needs a tcpSocket or exec probe instead",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: resourceTypes,
		},
		Function: makeK8sFnSetContainerProbeDefaults(rp),
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// probePaths holds per-probe-type URL paths.
type probePaths struct {
	liveness  string
	readiness string
	startup   string
}

func makeK8sFnSetContainerProbeDefaults(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		// Look up by ParameterName (not slice index) because optional
		// args may be sparse: when only some named args are supplied,
		// the handler packs only the present ones into the slice. The
		// handler populates ParameterName on positional args too, so
		// this works for either call style.
		paths := probePaths{liveness: "/healthz", readiness: "/healthz", startup: "/healthz"}
		for _, a := range fArgs.Arguments {
			s, ok := a.Value.(string)
			if !ok || s == "" {
				continue
			}
			switch a.ParameterName {
			case "liveness-path":
				paths.liveness = s
			case "readiness-path":
				paths.readiness = s
			case "startup-path":
				paths.startup = s
			}
		}
		return k8sFnSetContainerProbeDefaults(rp, fArgs.Options, fArgs.ParsedData, paths)
	}
}

func k8sFnSetContainerProbeDefaults(rp *k8skit.K8sResourceProviderType, options *api.FunctionOptions, parsedData gaby.Container, paths probePaths) (gaby.Container, any, error) {
	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, rp, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		podSpecPaths := k8skit.PodSpecPaths(resourceInfo.ResourceType)
		ok := len(podSpecPaths) > 0
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
					errs := setContainerProbeDefaults(containerDoc, paths)
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
func setContainerProbeDefaults(containerDoc *gaby.YamlDoc, paths probePaths) []error {
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
		// No existing probes, add all three with appropriate defaults.
		// Each probe gets its own httpGet (liveness/readiness/startup may
		// differ in path, and orderedmap is reference-shared if reused).
		newHTTPGet := func(path string) *orderedmap.OrderedMap[string, any] {
			m := orderedmap.New[string, any]()
			m.Set("path", path)
			m.Set("port", probePort)
			return m
		}

		startupProbe := orderedmap.New[string, any]()
		startupProbe.Set("httpGet", newHTTPGet(paths.startup))
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
		livenessProbe.Set("httpGet", newHTTPGet(paths.liveness))
		livenessProbe.Set("initialDelaySeconds", 30)
		livenessProbe.Set("periodSeconds", 20)
		livenessProbe.Set("timeoutSeconds", 5)
		livenessProbe.Set("failureThreshold", 3)
		livenessProbe.Set("successThreshold", 1)
		_, err = containerDoc.Set(livenessProbe, "livenessProbe")
		if err != nil {
			multiErrs = append(multiErrs, err)
		}

		readinessProbe := orderedmap.New[string, any]()
		readinessProbe.Set("httpGet", newHTTPGet(paths.readiness))
		readinessProbe.Set("initialDelaySeconds", 5)
		readinessProbe.Set("periodSeconds", 10)
		readinessProbe.Set("timeoutSeconds", 2)
		readinessProbe.Set("failureThreshold", 3)
		readinessProbe.Set("successThreshold", 1)
		_, err = containerDoc.Set(readinessProbe, "readinessProbe")
		if err != nil {
			multiErrs = append(multiErrs, err)
		}
	}

	return multiErrs
}

// allowPrivilegeEscalationWhereGranted sets allowPrivilegeEscalation true on every container
// that is privileged or adds SYS_ADMIN, which the security context defaults set false wherever
// it was missing. Either one grants the escalation the field would deny, so false there only
// claims a restriction the container does not have. Kubernetes goes further for privileged and
// for the CAP_SYS_ADMIN spelling, and rejects false beside them.
func allowPrivilegeEscalationWhereGranted(rp *k8skit.K8sResourceProviderType, options *api.FunctionOptions, parsedData gaby.Container) (gaby.Container, any, error) {
	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, rp, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		var multiErrs []error
		for _, containersPath := range k8skit.ContainerArrayPaths(resourceInfo.ResourceType) {
			containers, found, err := yamlkit.YamlSafePathGetDoc(doc, api.ResolvedPath(containersPath), true)
			if err != nil {
				multiErrs = append(multiErrs, err)
				continue
			}
			if !found {
				continue
			}
			for _, container := range containers.Children() {
				if !containerGrantsPrivilegeEscalation(container) {
					continue
				}
				if allowed, ok := container.Path("securityContext.allowPrivilegeEscalation").Data().(bool); ok && allowed {
					continue
				}
				if _, err := container.SetP(true, "securityContext.allowPrivilegeEscalation"); err != nil {
					multiErrs = append(multiErrs, err)
				}
			}
		}
		return output, multiErrs
	})
	return parsedData, nil, err
}

// containerGrantsPrivilegeEscalation reports whether a container is privileged or adds
// SYS_ADMIN. Kubernetes validation checks for the CAP_SYS_ADMIN spelling, and container
// runtimes accept SYS_ADMIN for the same capability, so both are matched.
func containerGrantsPrivilegeEscalation(container *gaby.YamlDoc) bool {
	if privileged, ok := container.Path("securityContext.privileged").Data().(bool); ok && privileged {
		return true
	}
	for _, capability := range container.Path("securityContext.capabilities.add").Children() {
		if name, ok := capability.Data().(string); ok && (name == "CAP_SYS_ADMIN" || name == "SYS_ADMIN") {
			return true
		}
	}
	return false
}
