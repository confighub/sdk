// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"log/slog"

	"github.com/cockroachdb/errors"
	"github.com/swaggest/jsonschema-go"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/function-impl/generic"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	quantity "k8s.io/apimachinery/pkg/api/resource"
)

var setImageHandler, setImageUriHandler, setImageReferenceHandler, setImageReferenceByUriHandler, setContainerFlagHandler handler.FunctionImplementation
var setImageRegistryByRegistryHandler handler.FunctionImplementation

// See:
// https://github.com/kubernetes/apimachinery/blob/master/pkg/util/validation/validation.go
// https://github.com/kubernetes/kubernetes/blob/master/pkg/apis/core/validation/validation.go

const dns1123LabelRegexpString = "[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?"
const containerNameRegexpString = "\\*|[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?"
const envVarRegexpString = "[-._a-zA-Z][-._a-zA-Z0-9]*"
const pflagNameRegexpString = "[a-zA-Z0-9][-a-zA-Z0-9.]*"

var pflagRegexpString = fmt.Sprintf("^--(?P<flag>%s)=(?P<value>.+)$", pflagNameRegexpString)

func convertToFullRegexp(regexp string) string {
	return "^" + regexp + "$"
}

func registerContainerFunctions(fh handler.FunctionRegistry, rp *k8skit.K8sResourceProviderType) {
	// Even though there's no setter, this parameter is used for the output description.
	containerNameParameters := []api.FunctionParameter{
		{
			ParameterName:    "container-name",
			Required:         true,
			Description:      "Name of the container to ", // verb will be appended
			DataType:         api.DataTypeString,
			Example:          "cert-manager-controller",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(containerNameRegexpString)},
		},
	}
	generic.RegisterPathSetterAndGetter(fh, "container-name", containerNameParameters,
		" the container name", api.AttributeNameContainerName, rp, false, false, false)

	imageParameters := []api.FunctionParameter{
		{
			ParameterName:    "container-name",
			Required:         true,
			Description:      "Name of the container whose image to ", // verb will be appended
			DataType:         api.DataTypeString,
			Example:          "cert-manager-controller",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(containerNameRegexpString)},
		},
		{
			ParameterName:    "container-image",
			Required:         true,
			Description:      "Full container image (repository URI and image reference)",
			DataType:         api.DataTypeString,
			Example:          "quay.io/jetstack/cert-manager-controller:v1.17.2",
			ValueConstraints: api.ValueConstraints{Regexp: imageURIReferenceRegexpString}, // already full
		},
	}
	generic.RegisterPathSetterAndGetter(fh, "container-image", imageParameters,
		" the image for a container", api.AttributeNameContainerImage, rp, true, false, false)
	setImageHandler = fh.GetHandlerImplementation("set-container-image") // for testing
	// Deprecated aliases
	generic.RegisterPathSetterAndGetter(fh, "image", imageParameters,
		" the image for a container. [Deprecated: use get-container-image/set-container-image]", api.AttributeNameContainerImage, rp, true, false, false)
	imageURIParameters := []api.FunctionParameter{
		{
			ParameterName:    "container-name",
			Required:         true,
			Description:      "Name of the container whose URI to ", // verb will be appended
			DataType:         api.DataTypeString,
			Example:          "cert-manager-controller",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(containerNameRegexpString)},
		},
		{
			ParameterName:    "repository-uri",
			Required:         true,
			Description:      "Repository URI (including host and repo)",
			DataType:         api.DataTypeString,
			Example:          "quay.io/jetstack/cert-manager-controller",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(imageURIRegexpString)},
		},
	}
	generic.RegisterPathSetterAndGetter(fh, "container-repository-uri", imageURIParameters,
		" the image repository URI for a container", api.AttributeNameContainerRepositoryURI, rp, true, false, false)
	setImageUriHandler = fh.GetHandlerImplementation("set-container-repository-uri") // for testing
	// Deprecated aliases
	generic.RegisterPathSetterAndGetter(fh, "image-uri", imageURIParameters,
		" the image repository URI for a container. [Deprecated: use get-container-repository-uri/set-container-repository-uri]", api.AttributeNameContainerRepositoryURI, rp, true, false, false)
	imageReferenceParameters := []api.FunctionParameter{
		{
			ParameterName:    "container-name",
			Required:         true,
			Description:      "Name of the container whose reference to ", // verb will be appended
			DataType:         api.DataTypeString,
			Example:          "cert-manager-controller",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(containerNameRegexpString)},
		},
		{
			ParameterName:    "image-reference",
			Required:         true,
			Description:      "Image tag or digest (including separator : or @)",
			DataType:         api.DataTypeString,
			Example:          ":v1.17.2",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(imageReferenceRegexpString)},
		},
	}
	generic.RegisterPathSetterAndGetter(fh, "container-image-reference", imageReferenceParameters,
		" the image reference for a container", api.AttributeNameContainerImageReference, rp, true, false, false)
	setImageReferenceHandler = fh.GetHandlerImplementation("set-container-image-reference") // for testing
	// Deprecated aliases
	generic.RegisterPathSetterAndGetter(fh, "image-reference", imageReferenceParameters,
		" the image reference for a container. [Deprecated: use get-container-image-reference/set-container-image-reference]", api.AttributeNameContainerImageReference, rp, true, false, false)

	pflagParameters := []api.FunctionParameter{
		{
			ParameterName:    "container-name",
			Required:         true,
			Description:      "Name of the container whose flag to ", // verb will be appended
			DataType:         api.DataTypeString,
			Example:          "my-app",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(containerNameRegexpString)},
		},
		{
			ParameterName:    "container-flag",
			Required:         true,
			Description:      "Name of the POSIX-style flag (without -- prefix) whose value to ",
			DataType:         api.DataTypeString,
			Example:          "port",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(pflagNameRegexpString)},
		},
		{
			ParameterName: "flag-value",
			Required:      true,
			Description:   "Value of the flag",
			DataType:      api.DataTypeString,
			Example:       "8080",
		},
	}
	generic.RegisterPathSetterAndGetter(fh, "container-flag", pflagParameters,
		" the value of a POSIX-style flag (--flag=value) in container args", attributeNameContainerFlag, rp, true, false, false)
	setContainerFlagHandler = fh.GetHandlerImplementation("set-container-flag") // for testing

	resourceTypes := yamlkit.ResourceTypesForAttribute(api.AttributeNameContainerImage, rp)
	if err := fh.RegisterFunction("set-image-reference-by-uri", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-image-reference-by-uri",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "repository-uri",
					Required:         true,
					Description:      "Image repository URI whose reference should be set",
					DataType:         api.DataTypeString,
					Example:          "quay.io/jetstack/cert-manager-controller",
					ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(imageURIRegexpString)},
				},
				{
					ParameterName:    "image-reference",
					Required:         true,
					Description:      "Tag or digest (including separator : or @)",
					DataType:         api.DataTypeString,
					Example:          ":v1.17.2",
					ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(imageReferenceRegexpString)},
				},
			},
			Mutating:   true,
			Validating: false,
			Hermetic:   true,
			Idempotent: true,
			// Replayable: the image URI selects what to change, so this travels to a
			// variant regardless of how its containers are named or ordered, and matches
			// however many of them use that image.
			Replayable:            true,
			Description:           "Set the reference for a specified image URI",
			FunctionType:          api.FunctionTypeCustom,
			AttributeName:         api.AttributeNameContainerImage,
			AffectedResourceTypes: resourceTypes,
		},
		Function: makeK8sFnSetImageReferenceByURI(rp),
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
	setImageReferenceByUriHandler = fh.GetHandlerImplementation("set-image-reference-by-uri") // for testing
	if err := fh.RegisterFunction("set-image-registry-by-registry", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-image-registry-by-registry",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "registry",
					Required:         true,
					Description:      "Existing image registry prefix to replace; an empty value matches images that have no registry and prepends new-registry to them",
					DataType:         api.DataTypeString,
					Example:          "quay.io",
					ValueConstraints: api.ValueConstraints{Regexp: "^(?:" + imageURIRegexpString + ")?$"},
				},
				{
					ParameterName:    "new-registry",
					Required:         true,
					Description:      "New image repository URI registry prefix to set",
					DataType:         api.DataTypeString,
					Example:          "mirror.registry.example.com",
					ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(imageURIRegexpString)},
				},
				{
					ParameterName:    "container-name",
					Required:         false,
					Description:      "Optional container name to restrict the change to; '*' (the default) matches all containers",
					DataType:         api.DataTypeString,
					Example:          "server",
					ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(containerNameRegexpString)},
				},
			},
			Mutating:   true,
			Validating: false,
			Hermetic:   true,
			Idempotent: true,
			// Replayable for the same reason as set-image-reference-by-uri: the registry
			// prefix names what to change rather than where it lives.
			Replayable:            true,
			Description:           "Replace the specified image registry prefix with a new registry prefix; an empty registry prepends the new registry to images that have none. Optionally restrict to a single container by name",
			FunctionType:          api.FunctionTypeCustom,
			AttributeName:         api.AttributeNameContainerImage,
			AffectedResourceTypes: resourceTypes,
		},
		Function: makeK8sFnSetImageRegistryByRegistry(rp),
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
	setImageRegistryByRegistryHandler = fh.GetHandlerImplementation("set-image-registry-by-registry") // for testing
	minValue := 0
	replicasParameters := []api.FunctionParameter{
		{
			ParameterName:    "replicas",
			Required:         true,
			Description:      "Number of replicas of workload controllers",
			DataType:         api.DataTypeInt,
			Example:          "3",
			ValueConstraints: api.ValueConstraints{Min: &minValue},
		},
	}
	generic.RegisterPathSetterAndGetter(fh, "replicas", replicasParameters,
		" the replicas for workload controllers", attributeNameReplicas, rp, true, true, false)
	resourceTypes = k8skit.ContainerResourceTypes()
	if err := fh.RegisterFunction("set-env", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-env",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "container-name",
					Required:         true,
					Description:      "Name of the container whose env vars to update",
					DataType:         api.DataTypeString,
					Example:          "main",
					ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(containerNameRegexpString)},
				},
				{
					ParameterName:    "env-key-value",
					Required:         true,
					Description:      "key=value format to upsert; no value implies removal",
					DataType:         api.DataTypeString,
					Example:          "DATABASE_URL=postgres://postgres:postgres@localhost:5432/main",
					ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(envVarRegexpString + "=.*")},
				},
			},
			VarArgs:               true,
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Replayable:            true,
			Description:           "Set environment variables for a container using <key>=<value> syntax",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: resourceTypes,
		},
		Function: makeK8sFnSetEnv(rp),
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
	envVarParameters := []api.FunctionParameter{
		{
			ParameterName:    "container-name",
			Required:         true,
			Description:      "Name of the container whose env var to ", // verb will be appended
			DataType:         api.DataTypeString,
			Example:          "main",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(containerNameRegexpString)},
		}, {
			ParameterName:    "env-var",
			Required:         true,
			Description:      "Name of the env var to ",
			DataType:         api.DataTypeString,
			Example:          "DATABASE_URL",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(envVarRegexpString)},
		}, {
			ParameterName: "env-value",
			Required:      true,
			Description:   "Env value",
			DataType:      api.DataTypeString,
			Example:       "postgres://postgres:postgres@localhost:5432/main",
			// no constraints
		},
	}
	generic.RegisterPathSetterAndGetter(fh, "env-var", envVarParameters,
		" an environment variable for a container", attributeNameEnvValue, rp, true, false, false)
	resourceTypes = yamlkit.ResourceTypesForAttribute(attributeNameContainerResources, rp)
	minFactor := 0
	maxFactor := 10
	// See https://github.com/kubernetes/apimachinery/blob/master/pkg/api/resource/quantity.go
	// Optional sign
	// Integer or decimal
	// Binary SI (base 1024) or Decimal SI (base 1000) or Exponent or no suffix
	resourceQuantityRegexpString := "^[+-]?([0-9]*(\\.[0-9]+)?)(([KMGTPE]i?)|[mk]|([eE][-+]?[0-9]+))?$"
	if err := fh.RegisterFunction("set-container-resources", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-container-resources",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "container-name",
					Required:         true,
					Description:      "Name of the container whose resources to set",
					DataType:         api.DataTypeString,
					Example:          "main",
					ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(containerNameRegexpString)},
				}, {
					ParameterName:    "operation",
					Required:         true,
					Description:      "If \"all\" then requests and limits will be set unconditionally; if \"cap\", then the values will be set if they exceed the values; if \"floor\", then the values will be set if they are less than the values",
					DataType:         api.DataTypeEnum,
					Example:          "all",
					ValueConstraints: api.ValueConstraints{EnumValues: []string{containerResourceOperationAll, containerResourceOperationCap, containerResourceOperationFloor}},
				}, {
					ParameterName:    "cpu",
					Required:         true,
					Description:      "Request cpu represented as a Kubernetes resource quantity, such as 500m; ignored if empty",
					DataType:         api.DataTypeString,
					Example:          "500m",
					ValueConstraints: api.ValueConstraints{Regexp: resourceQuantityRegexpString},
				}, {
					ParameterName:    "memory",
					Required:         true,
					Description:      "Request memory represented as a Kubernetes resource quantity, such as 256Mi; ignored if empty",
					DataType:         api.DataTypeString,
					Example:          "256Mi",
					ValueConstraints: api.ValueConstraints{Regexp: resourceQuantityRegexpString},
				}, {
					ParameterName:    "limit-factor",
					Required:         true,
					Description:      "Integer factor to multiply requests to compute limits. A factor of 0 implies no limits.",
					DataType:         api.DataTypeInt,
					Example:          "2",
					ValueConstraints: api.ValueConstraints{Min: &minFactor, Max: &maxFactor},
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Replayable:            true,
			Description:           "Set resource requests and limits for a container",
			FunctionType:          api.FunctionTypeCustom,
			AttributeName:         attributeNameContainerResources,
			AffectedResourceTypes: resourceTypes,
		},
		Function: makeK8sFnSetContainerResources(rp),
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
	resourceTypes = k8skit.PodSpecResourceTypes()
	if err := fh.RegisterFunction("set-pod-defaults", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-pod-defaults",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "pod-security",
					Required:      false,
					Description:   "Enable pod security labels on namespaces (default: false)",
					DataType:      api.DataTypeBool,
					Example:       "true",
				},
				{
					ParameterName: "automount-service-account-token",
					Required:      false,
					Description:   "Set automountServiceAccountToken to false (default: false)",
					DataType:      api.DataTypeBool,
					Example:       "true",
				},
				{
					ParameterName: "security-context",
					Required:      false,
					Description:   "Set security context for pods and containers (default: false)",
					DataType:      api.DataTypeBool,
					Example:       "true",
				},
				{
					ParameterName: "resources",
					Required:      false,
					Description:   "Set minimum resource requests for containers (default: false)",
					DataType:      api.DataTypeBool,
					Example:       "true",
				},
				{
					ParameterName: "probes",
					Required:      false,
					Description:   "Add liveness, readiness, and startup probes to containers (default: false)",
					DataType:      api.DataTypeBool,
					Example:       "true",
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Replayable:            true,
			Description:           "Set default pod settings",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: resourceTypes,
		},
		Function: makeK8sFnSetPodDefaults(rp),
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
	hostnameParameters := []api.FunctionParameter{
		{
			ParameterName:    "hostname",
			Required:         true,
			Description:      "Hostname",
			DataType:         api.DataTypeString,
			Example:          "myapp.example.com",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(dnsSubdomainDomainRegexpString)},
		},
	}
	generic.RegisterPathSetterAndGetter(fh, "hostname", hostnameParameters,
		" the hostname", api.AttributeNameHostname, rp, true, false, false)
	subdomainParameters := []api.FunctionParameter{
		{
			ParameterName:    "subdomain",
			Required:         true,
			Description:      "Subdomain",
			DataType:         api.DataTypeString,
			Example:          "myapp",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(dnsSubdomainRegexpString)},
		},
	}
	generic.RegisterPathSetterAndGetter(fh, "hostname-subdomain", subdomainParameters,
		" the subdomain", api.AttributeNameSubdomain, rp, true, false, false)
	domainParameters := []api.FunctionParameter{
		{
			ParameterName:    "domain",
			Required:         true,
			Description:      "Domain name",
			DataType:         api.DataTypeString,
			Example:          "example.com",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(dnsDomainRegexpString)},
		},
	}
	generic.RegisterPathSetterAndGetter(fh, "hostname-domain", domainParameters,
		" the domain name", api.AttributeNameDomain, rp, true, false, false)
	volumeMountParameters := []api.FunctionParameter{
		{
			ParameterName:    "container-name",
			Required:         true,
			Description:      "Name of the container to add volume mount to",
			DataType:         api.DataTypeString,
			Example:          "main",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(containerNameRegexpString)},
		},
		{
			ParameterName:    "volume-name",
			Required:         true,
			Description:      "Name of the volume to mount",
			DataType:         api.DataTypeString,
			Example:          "config-volume",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(dns1123LabelRegexpString)},
		},
		{
			ParameterName: "volume-path",
			Required:      true,
			Description:   "Path where the volume should be mounted in the container",
			DataType:      api.DataTypeString,
			Example:       "/etc/config",
			// TODO: Add validation for filesystem path?
		},
		{
			ParameterName:    "volume-source",
			Required:         false,
			Description:      "Type of volume source (emptyDir, configMap, secret, persistentVolumeClaim)",
			DataType:         api.DataTypeEnum,
			Example:          "configMap",
			ValueConstraints: api.ValueConstraints{EnumValues: []string{"emptyDir", "configMap", "secret", "persistentVolumeClaim"}},
		},
	}
	if err := fh.RegisterFunction("set-container-volume-mount-path", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "set-container-volume-mount-path",
			Parameters:            volumeMountParameters,
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set a volume mount for a container and ensure the volume exists in the pod spec",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: k8skit.PodSpecResourceTypes(),
		},
		Function: makeK8sFnSetContainerVolumeMountPath(rp),
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
	minPortNumber := 1
	maxPortNumber := 65535
	containerPortParameters := []api.FunctionParameter{
		{
			ParameterName:    "container-name",
			Required:         true,
			Description:      "Name of the container to set port for",
			DataType:         api.DataTypeString,
			Example:          "main",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(containerNameRegexpString)},
		},
		{
			ParameterName:    "port-name",
			Required:         true,
			Description:      "Name for the port",
			DataType:         api.DataTypeString,
			Example:          "http",
			ValueConstraints: api.ValueConstraints{Regexp: convertToFullRegexp(dns1123LabelRegexpString)},
		},
		{
			ParameterName:    "port-number",
			Required:         true,
			Description:      "Port number to expose",
			DataType:         api.DataTypeInt,
			Example:          "8080",
			ValueConstraints: api.ValueConstraints{Min: &minPortNumber, Max: &maxPortNumber},
		},
		{
			ParameterName:    "protocol",
			Required:         false,
			Description:      "Protocol for the port (TCP, UDP, or SCTP)",
			DataType:         api.DataTypeEnum,
			Example:          "TCP",
			ValueConstraints: api.ValueConstraints{EnumValues: []string{"TCP", "UDP", "SCTP"}},
		},
	}
	if err := fh.RegisterFunction("set-container-port", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "set-container-port",
			Parameters:            containerPortParameters,
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set a port for a container, adding it if not present",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: k8skit.ContainerResourceTypes(),
		},
		Function: makeK8sFnSetContainerPort(rp),
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
	reflector := jsonschema.Reflector{}
	validationResultSchema, err := reflector.Reflect(api.ValidationResult{})
	if err != nil {
		slog.Error("couldn't get schema for api.ValidationResult")
	}
	valueFilterSchema, err := reflector.Reflect(api.ValueFilter{})
	if err != nil {
		slog.Error("couldn't get schema for api.ValueFilter")
	}
	imageResourceTypes := yamlkit.ResourceTypesForAttribute(api.AttributeNameContainerImage, rp)
	if err := fh.RegisterFunction("vet-images", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-images",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "image-filter",
					Required:         true,
					Description:      "JSON object with AllowStrings and DenyStrings string lists for filtering container images",
					DataType:         api.DataTypeValueFilter,
					ValueConstraints: api.ValueConstraints{Schema: &valueFilterSchema},
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if all images pass the filter, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Validates that all container images pass the specified allow/deny filter. If AllowStrings is non-empty, all images must be in the allow list. Images in DenyStrings are always rejected.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: imageResourceTypes,
		},
		Function: makeK8sFnVetImages(rp),
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

// Hostname-bearing paths are declared as the hostname attribute in k8skit's resource type specs.

// Image paths:
// https://github.com/kubernetes-sigs/kustomize/blob/master/api/internal/konfig/builtinpluginconsts/images.go
// Image values:
// https://kubernetes.io/docs/concepts/containers/images/
// Specification:
// https://github.com/opencontainers/distribution-spec/blob/main/spec.md#workflow-categories
// https://github.com/opencontainers/image-spec/blob/main/descriptor.md#digests
// DNS subdomain validation:
// https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/apimachinery/pkg/util/validation/validation.go#L166
//
// Image references contain 3 parts:
// 1. Registry host
// 2. Repository namespace
// 3. Reference, which is a tag or digest
// Docker and Kubernetes allow the registry host to be optional, in which case it defaults to DockerHub
// Kubernetes allows the reference to be optional, in which case it defaults to :latest tag
// The registry host can include a port: fictional.registry.example:10443/imagename
// Digests are separated using `@` and contain a colon: registry.k8s.io/pause@sha256:1ff6c18fbef2045af6b9c16bf034cc421a29027b800e4f9b68ae9b1cb3e9ae07
// So we can't assume there's only one colon.
// Official regexps:
// 1. [a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)(:[0-9]+)?
// 2. [a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*(\/[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*)* preceded by a '/'
// 3a. Tag: [a-zA-Z0-9_][a-zA-Z0-9._-]{0,127} preceded by a ':'
// 3b. Digest: [a-z0-9]+([+._-][a-z0-9]+)*:[a-zA-Z0-9=_-]+  preceded by a '@'
// TODO: length limits
// https://pkg.go.dev/regexp allows subexpressions to be named
// https://pkg.go.dev/regexp#Regexp.FindStringSubmatch returns submatches. The first value returned is the whole expression.
// https://pkg.go.dev/regexp#Regexp.SubexpNames returns the names corresponding to submatches. The first value is empty.
// https://pkg.go.dev/regexp/syntax summarizes the syntax
// (re)           numbered capturing group (submatch)
// (?P<name>re)   named & numbered capturing group (submatch)
// (?<name>re)    named & numbered capturing group (submatch)
// (?:re)         non-capturing group
// (?flags)       set flags within current group; non-capturing
// (?flags:re)    set flags during re; non-capturing

// TODO: Perhaps these could be simplified in order to decouple validation and parsing.
const (
	imageRegistryHostRegexpString    = "[a-z0-9](?:[-a-z0-9]*[a-z0-9])?(?:\\.[a-z0-9](?:[-a-z0-9]*[a-z0-9])?)*(?:\\:[0-9]+)?"
	imageRepositoryRegexpString      = "[a-z0-9]+(?:(?:\\.|_|__|-+)[a-z0-9]+)*(?:\\/[a-z0-9]+(?:(?:\\.|_|__|-+)[a-z0-9]+)*)*"
	imageTagReferenceRegexpString    = "[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}"
	imageDigestReferenceRegexpString = "[a-z0-9]+(?:[+._-][a-z0-9]+)*\\:[a-zA-Z0-9=_-]+"
)

var (
	imageURIRegexpString       = fmt.Sprintf("(?P<uri>(?:(?:%s)/)?(?:%s))", imageRegistryHostRegexpString, imageRepositoryRegexpString)
	imageReferenceRegexpString = fmt.Sprintf("(?P<reference>(?:\\:(?:%s)|@(?:%s))?)", imageTagReferenceRegexpString, imageDigestReferenceRegexpString)
	// This expression partitions the image into two pieces, URI and reference.
	// The reference may be empty.
	imageURIReferenceRegexpString = fmt.Sprintf("^%s%s$", imageURIRegexpString, imageReferenceRegexpString)
)

var imageURIReferenceAccessor *yamlkit.RegexpAccessor
var (
	imageRegexp, imageURIReferenceRegexp *regexp.Regexp
)

// Reference from K8s (and the named RFCs: 1035, 1123, etc.):
// https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/apimachinery/pkg/util/validation/validation.go#L165
// Since there's a lot of confusion about whether underscores are allowed in hostnames in URLs,
// for now I'm going to exclude them. And since almost nobody uses a trailing dot, I'll exclude that.
// I'm also going to assume the simplifying restriction that the subdomain is just one DNS label.
// Domains should be required to have at least 2 labels, but I'm allowing one so that the placeholder
// string replacme can be used. Also, I'm capping the number of labels at 10 for expedience.
// Uppercase is not allowed.
const dnsSubdomainRegexpString = "(?P<subdomain>[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?)"
const dnsDomainRegexpString = "(?P<domain>[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?(?:\\.[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?){0,9})"
const dnsSubdomainDomainRegexpString = "^" + dnsSubdomainRegexpString + "\\." + dnsDomainRegexpString + "$"

// TODO: Check max length
const dnsMaxLength = 255

var dnsSubdomainAccessor *yamlkit.RegexpAccessor
var dnsSubdomainRegexp *regexp.Regexp

const (
	attributeNameEnvValue           = api.AttributeName("env-value")
	attributeNameReplicas           = api.AttributeName("replicas")
	attributeNameContainerResources = api.AttributeName("container-resources")
	attributeNameContainerFlag      = api.AttributeName("container-flag")
)

func initContainerFunctions(rp *k8skit.K8sResourceProviderType) {
	// This regular expression breaks down an image into its components, but is more
	// complicated to use for confighubplaceholder. It's not currently used, but is here in case we need it.
	imageRegexpString := fmt.Sprintf("^(?:(?P<registry>%s)/)?(?P<repository>%s)(?:\\:(?P<tag>%s)|@(?P<digest>%s))?$",
		imageRegistryHostRegexpString, imageRepositoryRegexpString, imageTagReferenceRegexpString, imageDigestReferenceRegexpString)
	imageRegexp = regexp.MustCompile(imageRegexpString)
	segmentNames := imageRegexp.SubexpNames()
	if len(segmentNames) != 5 {
		slog.Error("Image regexp doesn't contain exactly 4 segments", "count", len(segmentNames))
		panic("Image regexp doesn't contain exactly 4 segments")
	}

	imageURIReferenceRegexp = regexp.MustCompile(imageURIReferenceRegexpString)
	segmentNames = imageURIReferenceRegexp.SubexpNames()
	if len(segmentNames) != 3 {
		slog.Error("Image URI+reference regexp doesn't contain exactly 2 segments", "count", len(segmentNames))
		panic("Image URI+reference regexp doesn't contain exactly 2 segments")
	}

	dnsSubdomainRegexp = regexp.MustCompile(dnsSubdomainDomainRegexpString)
	segmentNames = dnsSubdomainRegexp.SubexpNames()
	if len(segmentNames) != 3 {
		slog.Error("DNS subdomain+domain regexp doesn't contain exactly 2 segments", "count", len(segmentNames))
		panic("DNS subdomain+domain regexp doesn't contain exactly 2 segments")
	}
}

func makeK8sFnSetImageReferenceByURI(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnSetImageReferenceByURI(rp, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
	}
}

func k8sFnSetImageReferenceByURI(rp *k8skit.K8sResourceProviderType, parsedData gaby.Container, args []api.FunctionArgument, opts *api.FunctionOptions) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	imageURI := args[0].Value.(string)
	newReference := args[1].Value.(string)
	newImage := imageURI + newReference

	resourceTypeToAllImagePaths := yamlkit.GetPathRegistryForAttributeName(rp, api.AttributeNameContainerImage)
	updater := func(currentValue string) string {
		matches := imageURIReferenceRegexp.FindStringSubmatchIndex(currentValue)
		// fmt.Printf("image %s, matches %v", currentValue, matches)
		// The first two elements should be zero and length of the string
		// The second two should be the start and end of the URI
		// The third two should be the start and end of the reference
		if len(matches) != 6 {
			return currentValue
		}
		currentURI := currentValue[matches[2]:matches[3]]
		// fmt.Printf(", URI %s\n", currentURI)
		if currentURI != imageURI {
			return currentValue
		}
		return newImage
	}
	err := yamlkit.UpdateStringPathsFunction(parsedData, resourceTypeToAllImagePaths, []any{"*"}, rp, updater, false, opts)
	return parsedData, nil, err
}

// imageHasRegistry reports whether an image reference begins with an explicit
// registry host. Per the Docker convention, the first slash-separated component
// is a registry only when it contains a "." or ":" (port) or equals "localhost";
// bare names ("adservice") and docker.io-implied paths ("library/nginx") do not.
func imageHasRegistry(image string) bool {
	i := strings.IndexByte(image, '/')
	if i < 0 {
		return false
	}
	first := image[:i]
	return first == "localhost" || strings.ContainsAny(first, ".:")
}

func makeK8sFnSetImageRegistryByRegistry(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnSetImageRegistryByRegistry(rp, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
	}
}

func k8sFnSetImageRegistryByRegistry(rp *k8skit.K8sResourceProviderType, parsedData gaby.Container, args []api.FunctionArgument, opts *api.FunctionOptions) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	imageRegistry := args[0].Value.(string)
	newRegistry := args[1].Value.(string)
	containerName := "*"
	if len(args) > 2 {
		containerName = args[2].Value.(string)
	}

	resourceTypeToAllImagePaths := yamlkit.GetPathRegistryForAttributeName(rp, api.AttributeNameContainerImage)
	updater := func(currentValue string) string {
		// An empty registry matches images that have no explicit registry host
		// (e.g. "adservice" or docker.io-implied "library/nginx") and prepends
		// the new registry, inserting the "/" separator. Images that already
		// carry a registry are left untouched.
		if imageRegistry == "" {
			if imageHasRegistry(currentValue) {
				return currentValue
			}
			return newRegistry + "/" + currentValue
		}
		if !strings.HasPrefix(currentValue, imageRegistry) {
			return currentValue
		}
		return newRegistry + strings.TrimPrefix(currentValue, imageRegistry)
	}
	err := yamlkit.UpdateStringPathsFunction(parsedData, resourceTypeToAllImagePaths, []any{containerName}, rp, updater, false, opts)
	return parsedData, nil, err
}

func makeK8sFnSetEnv(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnSetEnv(rp, fArgs.Options, fArgs.ParsedData, fArgs.Arguments)
	}
}

func k8sFnSetEnv(rp *k8skit.K8sResourceProviderType, options *api.FunctionOptions, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	multiErrs := []error{}
	// The argument value types should be verified before this function is called
	containerName := args[0].Value.(string)
	rest := args[1:]
	// pairs must be key=value format
	pairs := map[string]string{}
	keys := make([]string, 0, len(rest))
	for _, pair := range rest {
		pairString := pair.Value.(string)
		kv := strings.SplitN(pairString, "=", 2)
		if len(kv) != 2 {
			multiErrs = append(multiErrs, fmt.Errorf("invalid key-value pair: %s", pairString))
			continue
		}
		if kv[0] == "" {
			multiErrs = append(multiErrs, fmt.Errorf("invalid key: %s", kv[0]))
			continue
		}
		pairs[kv[0]] = kv[1]
		keys = append(keys, kv[0])
	}
	if len(pairs) == 0 {
		if len(multiErrs) != 0 {
			return parsedData, nil, errors.WithStack(errors.Join(multiErrs...))
		}
		return parsedData, nil, errors.WithStack(errors.New("no valid key-value pairs"))
	}

	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, rp, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		cPaths := k8skit.ContainerArrayPaths(resourceInfo.ResourceType)
		ok := len(cPaths) > 0
		if !ok {
			return output, nil // Skip resource kinds we don't handle
		}

		var visitorErrs []error
		for _, containersPath := range cPaths {
			unresolvedPath := api.UnresolvedPath(containersPath + ".?name=" + containerName)
			resolvedContainersPaths, err := yamlkit.ResolveAssociativePaths(doc, unresolvedPath, "", false, nil)
			if err != nil {
				continue // skip problematic path
			}
			for _, containerPath := range resolvedContainersPaths {
				// Make a copy of the pairs for this container
				thisPairs := map[string]string{}
				for k, v := range pairs {
					thisPairs[k] = v
				}

				container, found, err := yamlkit.YamlSafePathGetDoc(doc, containerPath.Path, true)
				if !found || err != nil {
					continue
				}
				envs := container.Path("env")
				if envs == nil {
					// Create the environment array if it doesn't exist
					ary, err := container.Array("env")
					if err != nil {
						visitorErrs = append(visitorErrs, errors.Wrap(err, "error creating environment array"))
						continue
					}
					envs = ary
				}
				for _, entry := range envs.Children() {
					// Overwrite the value of the environment variable if it exists
					if entry.Exists("name") {
						name, ok := entry.Path("name").Data().(string)
						if !ok {
							continue // skip malformed element
						}
						// An empty string indicates the variable should be removed, which is handled below
						if val, ok := thisPairs[name]; ok && val != "" {
							_, err = entry.SetP(val, "value")
							if err != nil {
								visitorErrs = append(visitorErrs, errors.Newf("error setting environment variable %s: %v", name, err))
							}
							// Remove the key from the thisPairs map
							delete(thisPairs, name)
						}
					}
				}
				// For the remaining pairs, remove or add them to the environment array
				// If the user specifies an empty string for a value, we should remove the environment variable
				// Iterate in the same order as the original args for determinism
				for _, k := range keys {
					v, found := thisPairs[k]
					if !found {
						continue
					}
					if v == "" {
						pairPaths, err := yamlkit.ResolveAssociativePaths(envs, api.UnresolvedPath("?name="+k), "", false, nil)
						if err != nil || len(pairPaths) == 0 {
							// Not found shouldn't be an error
							continue
						}
						if len(pairPaths) > 1 {
							slog.Error("Expected resolveAssociativePaths to return at most one result")
						}
						pairPath := pairPaths[0]
						if err := envs.DeleteP(string(pairPath.Path)); err != nil {
							visitorErrs = append(visitorErrs, errors.Wrapf(err, "error deleting environment variable %s", k))
							continue
						}
					} else {
						// TODO: is there a way to make this an ordered pair?
						val := map[string]interface{}{"name": k, "value": v}
						if err = envs.ArrayAppend(val); err != nil {
							visitorErrs = append(visitorErrs, errors.Wrapf(err, "error appending environment variable %s", k))
							continue
						}
					}
				}
			}
		}
		return output, visitorErrs
	})

	if err != nil {
		// Combine argument parsing errors with visitor errors
		multiErrs = append(multiErrs, err)
	}
	if len(multiErrs) != 0 {
		return parsedData, nil, errors.WithStack(errors.Join(multiErrs...))
	}
	return parsedData, nil, nil
}

const (
	containerResourceOperationAll   = "all"
	containerResourceOperationCap   = "cap"
	containerResourceOperationFloor = "floor"
)

func k8sSetResources(
	resourcesDoc *gaby.YamlDoc,
	operation, cpu, memory, cpuLimit, memoryLimit string,
	cpuQuantity, memoryQuantity, cpuLimitQuantity, memoryLimitQuantity quantity.Quantity,
	factor int,
) (*gaby.YamlDoc, error) {
	// TODO: Maybe the updater should just update currentDoc directly.
	// The advantage of a copy is that we can drop changes on error.
	newDoc, err := gaby.ParseYAML(resourcesDoc.Bytes())
	if err != nil {
		return resourcesDoc, err
	}
	if cpu != "" {
		if operation != containerResourceOperationAll && newDoc.ExistsP("requests.cpu") {
			currentCpuString, present, err := yamlkit.YamlSafePathGetValue[string](newDoc, api.ResolvedPath("requests.cpu"), true)
			if err == nil && present {
				currentCpuQuantity, err := quantity.ParseQuantity(currentCpuString)
				if err == nil {
					switch operation {
					case containerResourceOperationCap:
						if currentCpuQuantity.Cmp(cpuQuantity) < 0 {
							cpu = currentCpuString
						}
					case containerResourceOperationFloor:
						if currentCpuQuantity.Cmp(cpuQuantity) > 0 {
							cpu = currentCpuString
						}
					}
				}
			}
		}
		_, err = newDoc.SetP(cpu, "requests.cpu")
		if err != nil {
			return resourcesDoc, err
		}
	}
	if memory != "" {
		if operation != containerResourceOperationAll && newDoc.ExistsP("requests.memory") {
			currentMemoryString, present, err := yamlkit.YamlSafePathGetValue[string](newDoc, api.ResolvedPath("requests.memory"), true)
			if err == nil && present {
				currentMemoryQuantity, err := quantity.ParseQuantity(currentMemoryString)
				if err == nil {
					switch operation {
					case containerResourceOperationCap:
						if currentMemoryQuantity.Cmp(memoryQuantity) < 0 {
							memory = currentMemoryString
						}
					case containerResourceOperationFloor:
						if currentMemoryQuantity.Cmp(memoryQuantity) > 0 {
							memory = currentMemoryString
						}
					}
				}
			}
		}
		_, err = newDoc.SetP(memory, "requests.memory")
		if err != nil {
			return resourcesDoc, err
		}
	}
	if factor == 0 {
		if newDoc.ExistsP("limits") {
			err = newDoc.DeleteP("limits")
			if err != nil {
				return resourcesDoc, err
			}
		}
	} else {
		if cpuLimit != "" {
			if operation != containerResourceOperationAll && newDoc.ExistsP("limits.cpu") {
				currentCpuString, present, err := yamlkit.YamlSafePathGetValue[string](newDoc, api.ResolvedPath("limits.cpu"), true)
				if err == nil && present {
					currentCpuQuantity, err := quantity.ParseQuantity(currentCpuString)
					if err == nil {
						switch operation {
						case containerResourceOperationCap:
							if currentCpuQuantity.Cmp(cpuLimitQuantity) < 0 {
								cpuLimit = currentCpuString
							}
						case containerResourceOperationFloor:
							if currentCpuQuantity.Cmp(cpuLimitQuantity) > 0 {
								cpuLimit = currentCpuString
							}
						}
					}
				}
			}
			_, err = newDoc.SetP(cpuLimit, "limits.cpu")
			if err != nil {
				return resourcesDoc, err
			}
		}
		if memoryLimit != "" {
			if operation != containerResourceOperationAll && newDoc.ExistsP("limits.memory") {
				currentMemoryString, present, err := yamlkit.YamlSafePathGetValue[string](newDoc, api.ResolvedPath("limits.memory"), true)
				if err == nil && present {
					currentMemoryQuantity, err := quantity.ParseQuantity(currentMemoryString)
					if err == nil {
						switch operation {
						case containerResourceOperationCap:
							if currentMemoryQuantity.Cmp(memoryLimitQuantity) < 0 {
								memoryLimit = currentMemoryString
							}
						case containerResourceOperationFloor:
							if currentMemoryQuantity.Cmp(memoryLimitQuantity) > 0 {
								memoryLimit = currentMemoryString
							}
						}
					}
				}
			}
			_, err = newDoc.SetP(memoryLimit, "limits.memory")
			if err != nil {
				return resourcesDoc, err
			}
		}
	}
	return newDoc, nil
}

func makeK8sFnSetContainerResources(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnSetContainerResources(rp, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
	}
}

func k8sFnSetContainerResources(rp *k8skit.K8sResourceProviderType, parsedData gaby.Container, args []api.FunctionArgument, opts *api.FunctionOptions) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	containerName := args[0].Value.(string)
	operation := args[1].Value.(string)
	cpu := args[2].Value.(string)
	memory := args[3].Value.(string)
	factor := args[4].Value.(int)

	if operation != containerResourceOperationAll && operation != containerResourceOperationCap &&
		operation != containerResourceOperationFloor {
		return parsedData, nil, errors.New("invalid operation " + operation)
	}

	var cpuQuantity, memoryQuantity quantity.Quantity
	var err error
	if cpu != "" {
		cpuQuantity, err = quantity.ParseQuantity(cpu)
		if err != nil {
			return parsedData, nil, errors.Wrap(err, "invalid cpu "+cpu)
		}
	}
	if memory != "" {
		memoryQuantity, err = quantity.ParseQuantity(memory)
		if err != nil {
			return parsedData, nil, errors.Wrap(err, "invalid memory "+memory)
		}
	}
	if cpu == "" && memory == "" {
		return parsedData, nil, errors.New("must specify at least one of cpu and memory")
	}

	var cpuLimit, memoryLimit string
	var cpuLimitQuantity, memoryLimitQuantity quantity.Quantity
	if factor != 0 {
		if cpu != "" {
			cpuLimitQuantity = cpuQuantity.DeepCopy()
			// Discard exact/inexact result for now
			cpuLimitQuantity.Mul(int64(factor))
			cpuLimit = cpuLimitQuantity.String()
		}
		if memory != "" {
			memoryLimitQuantity = memoryQuantity.DeepCopy()
			memoryLimitQuantity.Mul(int64(factor))
			memoryLimit = memoryLimitQuantity.String()
		}
	}

	updater := func(currentDoc *gaby.YamlDoc, _ yamlkit.VisitorContext) *gaby.YamlDoc {
		// TODO: Maybe the updater should just update currentDoc directly.
		// The advantage of a copy is that we can drop changes on error.
		newDoc, err := k8sSetResources(currentDoc, operation,
			cpu, memory, cpuLimit, memoryLimit,
			cpuQuantity, memoryQuantity, cpuLimitQuantity, memoryLimitQuantity, factor)
		if err != nil {
			return currentDoc
		}
		return newDoc
	}

	resourceTypeToResourcesPaths := yamlkit.GetPathRegistryForAttributeName(rp, attributeNameContainerResources)
	err = yamlkit.UpdatePathsFunctionDoc(parsedData, resourceTypeToResourcesPaths, []any{containerName}, rp, updater, true, opts)
	return parsedData, nil, err
}

func makeK8sFnSetPodDefaults(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnSetPodDefaults(rp, fArgs.Options, fArgs.ParsedData, fArgs.Arguments)
	}
}

func k8sFnSetPodDefaults(rp *k8skit.K8sResourceProviderType, options *api.FunctionOptions, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	var err error

	// Parse parameters with default values of false
	defaultValue := false
	podSecurity := defaultValue
	automountServiceAccountToken := defaultValue
	securityContext := defaultValue
	resources := defaultValue
	probes := defaultValue

	for _, arg := range args {
		switch arg.ParameterName {
		case "pod-security":
			podSecurity = arg.Value.(bool)
		case "automount-service-account-token":
			automountServiceAccountToken = arg.Value.(bool)
		case "security-context":
			securityContext = arg.Value.(bool)
		case "resources":
			resources = arg.Value.(bool)
		case "probes":
			probes = arg.Value.(bool)
		}
	}

	namespaceResourceType := api.ResourceType("v1/Namespace")
	_, err = yamlkit.VisitResourcesFiltered(parsedData, nil, rp, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		if resourceInfo.ResourceType == namespaceResourceType && podSecurity {
			// The dots don't need to be escaped when using Set rather than SetP
			var visitorErrs []error
			_, err := doc.Set("baseline", "metadata", "labels", "pod-security.kubernetes.io/enforce")
			if err != nil {
				visitorErrs = append(visitorErrs, err)
			}
			_, err = doc.Set("latest", "metadata", "labels", "pod-security.kubernetes.io/enforce-version")
			if err != nil {
				visitorErrs = append(visitorErrs, err)
			}
			_, err = doc.Set("restricted", "metadata", "labels", "pod-security.kubernetes.io/warn")
			if err != nil {
				visitorErrs = append(visitorErrs, err)
			}
			_, err = doc.Set("latest", "metadata", "labels", "pod-security.kubernetes.io/warn-version")
			if err != nil {
				visitorErrs = append(visitorErrs, err)
			}
			return output, visitorErrs
		}
		podSpecPaths := k8skit.PodSpecPaths(resourceInfo.ResourceType)
		ok := len(podSpecPaths) > 0
		if !ok {
			return output, nil // Skip resource kinds we don't handle
		}
		slog.Info("traversing resource", "resourceType", string(resourceInfo.ResourceType))

		var visitorErrs []error
		for _, podSpecPath := range podSpecPaths {
			// For some of these attributes, we don't care whether or how they were set.
			// We have a "best practice" default we want to use. For others, we have a
			// minimum expected set of values.
			podSpecDoc, hasPodSpec, err := yamlkit.YamlSafePathGetDoc(doc, api.ResolvedPath(podSpecPath), true)
			if err != nil {
				visitorErrs = append(visitorErrs, err)
				continue
			}
			if !hasPodSpec {
				slog.Info("no pod spec")

				// Shouldn't happen, but be resilient
				continue
			}

			// TODO: Register these in the path registry?

			if automountServiceAccountToken {
				_, err = podSpecDoc.Set(false, "automountServiceAccountToken")
				if err != nil {
					visitorErrs = append(visitorErrs, err)
				}
			}

			// terminationGracePeriodSeconds is 30 by default, which should be a good enough starting point.
			// if !podSpecDoc.Exists("terminationGracePeriodSeconds") {
			// 	_, err = podSpecDoc.Set(60, "terminationGracePeriodSeconds")
			// 	if err != nil {
			// 		visitorErrs = append(visitorErrs, err)
			// 	}
			// }

			if securityContext {
				// Pod-level security contexft
				_, err = podSpecDoc.Set("RuntimeDefault", "securityContext", "seccompProfile", "type")
				if err != nil {
					visitorErrs = append(visitorErrs, err)
				}
				_, err = podSpecDoc.Set(true, "securityContext", "runAsNonRoot")
				if err != nil {
					visitorErrs = append(visitorErrs, err)
				}
				// These are arbitrary and can be changed, but I see these used fairly often, perhaps because it's
				// the example in the docs: https://kubernetes.io/docs/tasks/configure-pod-container/security-context/
				if !podSpecDoc.Exists("securityContext", "runAsUser") {
					_, err = podSpecDoc.Set(1000, "securityContext", "runAsUser")
					if err != nil {
						visitorErrs = append(visitorErrs, err)
					}
				}
				if !podSpecDoc.Exists("securityContext", "runAsGroup") {
					_, err = podSpecDoc.Set(3000, "securityContext", "runAsGroup")
					if err != nil {
						visitorErrs = append(visitorErrs, err)
					}
				}
				if !podSpecDoc.Exists("securityContext", "fsGroup") {
					_, err = podSpecDoc.Set(2000, "securityContext", "fsGroup")
					if err != nil {
						visitorErrs = append(visitorErrs, err)
					}
				}

				for _, containerPath := range k8skit.ContainersPaths {
					containersDoc, hasContainers, err := yamlkit.YamlSafePathGetDoc(podSpecDoc, api.ResolvedPath(containerPath), true)
					if err != nil {
						visitorErrs = append(visitorErrs, err)
						continue
					}
					if !hasContainers {
						// Most pods don't have initContainers or ephemeralContainers
						continue
					}
					for _, containerDoc := range containersDoc.Children() {
						// Container-level security context
						_, err = containerDoc.Set(true, "securityContext", "readOnlyRootFilesystem")
						if err != nil {
							visitorErrs = append(visitorErrs, err)
						}
						_, err = containerDoc.Set(false, "securityContext", "allowPrivilegeEscalation")
						if err != nil {
							visitorErrs = append(visitorErrs, err)
						}
						_, err = containerDoc.Set(false, "securityContext", "privileged")
						if err != nil {
							visitorErrs = append(visitorErrs, err)
						}

						// Set capabilities.drop to ALL if not already present
						if !containerDoc.ExistsP("securityContext.capabilities.drop") {
							_, err = containerDoc.Set([]interface{}{"ALL"}, "securityContext", "capabilities", "drop")
							if err != nil {
								visitorErrs = append(visitorErrs, err)
							}
						}

						// TODO: Set imagePullPolicy to Always if not already present.
						// We may want to parameterize this with a larger group of options.
						// if !containerDoc.Exists("imagePullPolicy") {
						// 	_, err = containerDoc.Set("Always", "imagePullPolicy")
						// 	if err != nil {
						// 		visitorErrs = append(visitorErrs, err)
						// 	}
						// }
					}
				}
			}

			if resources {
				for _, containerPath := range k8skit.ContainersPaths {
					containersDoc, hasContainers, err := yamlkit.YamlSafePathGetDoc(podSpecDoc, api.ResolvedPath(containerPath), true)
					if err != nil {
						visitorErrs = append(visitorErrs, err)
						continue
					}
					if !hasContainers {
						// Most pods don't have initContainers or ephemeralContainers
						continue
					}
					for _, containerDoc := range containersDoc.Children() {
						// Set minimum requested resources for all containers
						var resourcesDoc *gaby.YamlDoc
						if !containerDoc.Exists("resources") {
							resourcesDoc, err = containerDoc.Object("resources")
							if err != nil {
								visitorErrs = append(visitorErrs, err)
								continue
							}
						} else {
							resourcesDoc, _, err = yamlkit.YamlSafePathGetDoc(containerDoc, api.ResolvedPath("resources"), false)
							if err != nil {
								visitorErrs = append(visitorErrs, err)
								continue
							}
						}
						cpu := "128m"
						cpuLimit := ""
						memoryLimit := ""
						factor := 0
						memory := "128Mi"
						cpuQuantity := quantity.MustParse(cpu)
						memoryQuantity := quantity.MustParse(memory)
						var cpuLimitQuantity, memoryLimitQuantity quantity.Quantity
						newDoc, err := k8sSetResources(resourcesDoc, containerResourceOperationFloor,
							cpu, memory, cpuLimit, memoryLimit,
							cpuQuantity, memoryQuantity, cpuLimitQuantity, memoryLimitQuantity, factor)
						if err != nil {
							visitorErrs = append(visitorErrs, err)
							continue
						}
						_, err = containerDoc.SetDocP(newDoc, "resources")
						if err != nil {
							visitorErrs = append(visitorErrs, err)
						}
					}
				}
			}

			if probes {
				for _, containerPath := range k8skit.ContainersPaths {
					// Skip initContainers and ephemeralContainers
					if strings.HasSuffix(containerPath, "initContainers") || strings.HasSuffix(containerPath, "ephemeralContainers") {
						continue
					}
					containersDoc, hasContainers, err := yamlkit.YamlSafePathGetDoc(podSpecDoc, api.ResolvedPath(containerPath), true)
					if err != nil {
						visitorErrs = append(visitorErrs, err)
						continue
					}
					if !hasContainers {
						continue
					}
					for _, containerDoc := range containersDoc.Children() {
						errs := setContainerProbeDefaults(containerDoc, probePaths{liveness: "/healthz", readiness: "/healthz", startup: "/healthz"})
						visitorErrs = append(visitorErrs, errs...)
					}
				}
			}
		}
		return output, visitorErrs
	})

	if err != nil {
		return parsedData, nil, err
	}
	return parsedData, nil, nil
}

func makeK8sFnSetContainerVolumeMountPath(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnSetContainerVolumeMountPath(rp, fArgs.Options, fArgs.ParsedData, fArgs.Arguments)
	}
}

func k8sFnSetContainerVolumeMountPath(rp *k8skit.K8sResourceProviderType, options *api.FunctionOptions, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	// Parse arguments
	containerName := args[0].Value.(string)
	volumeName := args[1].Value.(string)
	volumePath := args[2].Value.(string)
	var volumeSource string
	if len(args) > 3 && args[3].Value != nil {
		volumeSource = args[3].Value.(string)
	}

	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, rp, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		podSpecPaths := k8skit.PodSpecPaths(resourceInfo.ResourceType)
		ok := len(podSpecPaths) > 0
		if !ok {
			return output, nil // Skip resource kinds we don't handle
		}

		var visitorErrs []error
		for _, podSpecPath := range podSpecPaths {
			podSpecDoc, hasPodSpec, err := yamlkit.YamlSafePathGetDoc(doc, api.ResolvedPath(podSpecPath), true)
			if err != nil {
				visitorErrs = append(visitorErrs, err)
				continue
			}
			if !hasPodSpec {
				continue
			}

			// Find the container
			cPaths := k8skit.ContainerArrayPaths(resourceInfo.ResourceType)
			ok := len(cPaths) > 0
			if !ok {
				continue
			}

			containerFound := false
			for _, containersPath := range cPaths {
				unresolvedPath := api.UnresolvedPath(containersPath + ".?name=" + containerName)
				resolvedContainersPaths, err := yamlkit.ResolveAssociativePaths(doc, unresolvedPath, "", false, nil)
				if err != nil {
					continue // skip problematic path
				}
				for _, containerPath := range resolvedContainersPaths {
					containerFound = true
					container, found, err := yamlkit.YamlSafePathGetDoc(doc, containerPath.Path, true)
					if !found || err != nil {
						continue
					}

					// Check if volumeMount already exists using ResolveAssociativePaths
					volumeMountPaths, err := yamlkit.ResolveAssociativePaths(container, api.UnresolvedPath("volumeMounts.?name="+volumeName), "", false, nil)

					if err == nil && len(volumeMountPaths) > 0 {
						// Volume mount exists, update the mountPath
						volumeMount, found, err := yamlkit.YamlSafePathGetDoc(container, volumeMountPaths[0].Path, true)
						if found && err == nil {
							_, err = volumeMount.Set(volumePath, "mountPath")
							if err != nil {
								visitorErrs = append(visitorErrs, errors.Wrapf(err, "error setting mountPath for volume %s", volumeName))
							}
						}
					} else {
						// Volume mount doesn't exist, create it
						// Ensure volumeMounts array exists
						volumeMounts := container.Path("volumeMounts")
						if volumeMounts == nil {
							ary, err := container.Array("volumeMounts")
							if err != nil {
								visitorErrs = append(visitorErrs, errors.Wrap(err, "error creating volumeMounts array"))
								continue
							}
							volumeMounts = ary
						}

						// Append new volumeMount with ordered fields
						volumeMount := orderedmap.New[string, interface{}]()
						volumeMount.Set("name", volumeName)
						volumeMount.Set("mountPath", volumePath)
						if err = volumeMounts.ArrayAppend(volumeMount); err != nil {
							visitorErrs = append(visitorErrs, errors.Wrapf(err, "error appending volumeMount %s", volumeName))
							continue
						}
					}
				}
			}

			if !containerFound {
				visitorErrs = append(visitorErrs, errors.Newf("container %s not found", containerName))
				continue
			}

			// Now handle the volume in the pod spec
			// Check if volume already exists using ResolveAssociativePaths
			volumePaths, err := yamlkit.ResolveAssociativePaths(podSpecDoc, api.UnresolvedPath("volumes.?name="+volumeName), "", false, nil)

			if err == nil && len(volumePaths) > 0 {
				// Volume exists
				if volumeSource != "" {
					// Check if the volume source needs to be updated
					volume, found, err := yamlkit.YamlSafePathGetDoc(podSpecDoc, volumePaths[0].Path, true)
					if found && err == nil {
						// Check if the volume already has the correct source type
						hasCorrectType := volume.Exists(volumeSource)

						if !hasCorrectType {
							// Volume exists but has wrong type - delete all fields except "name"
							volumeName, _ := volume.Path("name").Data().(string)

							// Get all field names in the volume
							childrenMap := volume.ChildrenMap()
							for fieldName := range childrenMap {
								if fieldName != "name" {
									_ = volume.DeleteP(fieldName)
								}
							}

							// Add the specified source type
							switch volumeSource {
							case "emptyDir":
								_, err = volume.Set(map[string]interface{}{}, "emptyDir")
							case "configMap":
								_, err = volume.Set("confighubplaceholder", "configMap", "name")
							case "secret":
								_, err = volume.Set("confighubplaceholder", "secret", "secretName")
							case "persistentVolumeClaim":
								_, err = volume.Set("confighubplaceholder", "persistentVolumeClaim", "claimName")
							}
							if err != nil {
								visitorErrs = append(visitorErrs, errors.Wrapf(err, "error setting volume source for volume %s", volumeName))
							}
						}
						// If hasCorrectType is true, we don't modify the volume at all to preserve existing attributes
					}
				}
			} else {
				// Volume doesn't exist
				if volumeSource == "" {
					visitorErrs = append(visitorErrs, errors.Newf("volume %s does not exist and volume-source was not specified", volumeName))
					continue
				}

				// Ensure volumes array exists
				volumes := podSpecDoc.Path("volumes")
				if volumes == nil {
					ary, err := podSpecDoc.Array("volumes")
					if err != nil {
						visitorErrs = append(visitorErrs, errors.Wrap(err, "error creating volumes array"))
						continue
					}
					volumes = ary
				}

				// Create the volume with ordered fields
				volume := orderedmap.New[string, interface{}]()
				volume.Set("name", volumeName)

				switch volumeSource {
				case "emptyDir":
					volume.Set("emptyDir", map[string]interface{}{})
				case "configMap":
					configMapVolume := orderedmap.New[string, interface{}]()
					configMapVolume.Set("name", "confighubplaceholder")
					volume.Set("configMap", configMapVolume)
				case "secret":
					secretVolume := orderedmap.New[string, interface{}]()
					secretVolume.Set("secretName", "confighubplaceholder")
					volume.Set("secret", secretVolume)
				case "persistentVolumeClaim":
					pvcVolume := orderedmap.New[string, interface{}]()
					pvcVolume.Set("claimName", "confighubplaceholder")
					volume.Set("persistentVolumeClaim", pvcVolume)
				}

				if err = volumes.ArrayAppend(volume); err != nil {
					visitorErrs = append(visitorErrs, errors.Wrapf(err, "error appending volume %s", volumeName))
					continue
				}
			}
		}
		return output, visitorErrs
	})

	if err != nil {
		return parsedData, nil, err
	}
	return parsedData, nil, nil
}

func makeK8sFnSetContainerPort(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnSetContainerPort(rp, fArgs.Options, fArgs.ParsedData, fArgs.Arguments)
	}
}

func k8sFnSetContainerPort(rp *k8skit.K8sResourceProviderType, options *api.FunctionOptions, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	// Parse arguments
	containerName := args[0].Value.(string)
	portName := args[1].Value.(string)
	portNumber := args[2].Value.(int)
	var protocol string
	if len(args) > 3 && args[3].Value != nil {
		protocol = args[3].Value.(string)
	} else {
		protocol = "TCP" // Default protocol
	}

	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, rp, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		cPaths := k8skit.ContainerArrayPaths(resourceInfo.ResourceType)
		ok := len(cPaths) > 0
		if !ok {
			return output, nil // Skip resource kinds we don't handle
		}

		var visitorErrs []error
		for _, containersPath := range cPaths {
			unresolvedPath := api.UnresolvedPath(containersPath + ".?name=" + containerName)
			resolvedContainersPaths, err := yamlkit.ResolveAssociativePaths(doc, unresolvedPath, "", false, nil)
			if err != nil {
				continue // skip problematic path
			}
			for _, containerPath := range resolvedContainersPaths {
				container, found, err := yamlkit.YamlSafePathGetDoc(doc, containerPath.Path, true)
				if !found || err != nil {
					continue
				}

				// Check if ports array exists
				ports := container.Path("ports")
				if ports == nil {
					// Create the ports array if it doesn't exist
					ary, err := container.Array("ports")
					if err != nil {
						visitorErrs = append(visitorErrs, errors.Wrap(err, "error creating ports array"))
						continue
					}
					ports = ary
				}

				// Check if a port with the same containerPort and protocol already exists
				// Priority: first check by port number/protocol, then by name
				portFound := false
				for _, portEntry := range ports.Children() {
					if !portEntry.Exists("containerPort") {
						continue // skip malformed element
					}
					existingPort, ok := portEntry.Path("containerPort").Data().(int)
					if !ok {
						continue // skip malformed element
					}

					// Check if this port has the same number
					if existingPort == portNumber {
						// Check protocol - if the existing port has a protocol, it must match
						existingProtocol := "TCP" // Default protocol
						if portEntry.Exists("protocol") {
							if proto, ok := portEntry.Path("protocol").Data().(string); ok {
								existingProtocol = proto
							}
						}

						if existingProtocol == protocol {
							portFound = true
							// Update the name and protocol for this port
							_, err = portEntry.Set(portName, "name")
							if err != nil {
								visitorErrs = append(visitorErrs, errors.Newf("error setting name for port %d: %v", portNumber, err))
							}
							_, err = portEntry.Set(protocol, "protocol")
							if err != nil {
								visitorErrs = append(visitorErrs, errors.Newf("error setting protocol for port %d: %v", portNumber, err))
							}
							break
						}
					}
				}

				// If port not found, add it
				if !portFound {
					port := orderedmap.New[string, interface{}]()
					port.Set("name", portName)
					port.Set("containerPort", portNumber)
					port.Set("protocol", protocol)
					if err = ports.ArrayAppend(port); err != nil {
						visitorErrs = append(visitorErrs, errors.Wrapf(err, "error appending port %s", portName))
						continue
					}
				}
			}
		}
		return output, visitorErrs
	})

	if err != nil {
		return parsedData, nil, err
	}
	return parsedData, nil, nil
}

func makeK8sFnVetImages(rp *k8skit.K8sResourceProviderType) handler.FunctionImplementation {
	return func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
		return k8sFnVetImages(rp, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
	}
}

func k8sFnVetImages(rp *k8skit.K8sResourceProviderType, parsedData gaby.Container, args []api.FunctionArgument, opts *api.FunctionOptions) (gaby.Container, any, error) {
	filterString := args[0].Value.(string)
	var filter api.ValueFilter
	if err := json.Unmarshal([]byte(filterString), &filter); err != nil {
		return parsedData, nil, errors.Wrap(err, "failed to parse image-filter argument")
	}

	result, err := generic.GenericVetValues(rp, parsedData, api.AttributeNameContainerImage, &filter, opts)
	return parsedData, result, err
}
