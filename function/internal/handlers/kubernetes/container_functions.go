// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/labstack/gommon/log"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/function/internal/handlers/generic"
	"github.com/confighub/sdk/third_party/gaby"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	quantity "k8s.io/apimachinery/pkg/api/resource"
)

var setImageHandler, setImageUriHandler, setImageReferenceHandler, setImageReferenceByUriHandler handler.FunctionImplementation

// See:
// https://github.com/kubernetes/apimachinery/blob/master/pkg/util/validation/validation.go
// https://github.com/kubernetes/kubernetes/blob/master/pkg/apis/core/validation/validation.go

const dns1123LabelRegexpString = "[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?"
const containerNameRegexpString = "\\*|[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?"
const envVarRegexpString = "[-._a-zA-Z][-._a-zA-Z0-9]*"

func convertToFullRegexp(regexp string) string {
	return "^" + regexp + "$"
}

func registerContainerFunctions(fh handler.FunctionRegistry) {
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
		" the container name", api.AttributeNameContainerName, k8skit.K8sResourceProvider, false, false)

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
	generic.RegisterPathSetterAndGetter(fh, "image", imageParameters,
		" the image for a container", api.AttributeNameContainerImage, k8skit.K8sResourceProvider, true, false)
	setImageHandler = fh.GetHandlerImplementation("set-image") // for testing
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
	generic.RegisterPathSetterAndGetter(fh, "image-uri", imageURIParameters,
		" the image repository URI for a container", api.AttributeNameContainerRepositoryURI, k8skit.K8sResourceProvider, true, false)
	setImageUriHandler = fh.GetHandlerImplementation("set-image-uri") // for testing
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
	generic.RegisterPathSetterAndGetter(fh, "image-reference", imageReferenceParameters,
		" the image reference for a container", api.AttributeNameContainerImageReference, k8skit.K8sResourceProvider, true, false)
	setImageReferenceHandler = fh.GetHandlerImplementation("set-image-reference") // for testing
	resourceTypes := yamlkit.ResourceTypesForAttribute(api.AttributeNameContainerImages, k8skit.K8sResourceProvider)
	fh.RegisterFunction("set-image-reference-by-uri", &handler.FunctionRegistration{
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
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set the reference for a specified image URI",
			FunctionType:          api.FunctionTypeCustom,
			AttributeName:         api.AttributeNameContainerImages,
			AffectedResourceTypes: resourceTypes,
		},
		Function: k8sFnSetImageReferenceByURI,
	})
	setImageReferenceByUriHandler = fh.GetHandlerImplementation("set-image-reference-by-uri") // for testing
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
		" the replicas for workload controllers", attributeNameReplicas, k8skit.K8sResourceProvider, true, false)
	resourceTypes = yamlkit.ResourceTypesForPathMap(resourceTypeToContainersPaths)
	fh.RegisterFunction("set-env", &handler.FunctionRegistration{
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
			Description:           "Set environment variables for a container using <key>=<value> syntax",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: resourceTypes,
		},
		Function: k8sFnSetEnv,
	})
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
		" an environment variable for a container", attributeNameEnvValue, k8skit.K8sResourceProvider, true, false)
	resourceTypes = yamlkit.ResourceTypesForAttribute(attributeNameContainerResources, k8skit.K8sResourceProvider)
	minFactor := 0
	maxFactor := 10
	fh.RegisterFunction("set-container-resources", &handler.FunctionRegistration{
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
					ParameterName: "cpu",
					Required:      true,
					Description:   "Request cpu represented as a Kubernetes resource quantity, such as 500m; ignored if empty",
					DataType:      api.DataTypeString,
					Example:       "500m",
					// TODO: regexp?
				}, {
					ParameterName: "memory",
					Required:      true,
					Description:   "Request memory represented as a Kubernetes resource quantity, such as 256Mi; ignored if empty",
					DataType:      api.DataTypeString,
					Example:       "256Mi",
					// TODO: regexp?
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
			Description:           "Set resource requests and limits for a container",
			FunctionType:          api.FunctionTypeCustom,
			AttributeName:         attributeNameContainerResources,
			AffectedResourceTypes: resourceTypes,
		},
		Function: k8sFnSetContainerResources,
	})
	resourceTypes = yamlkit.ResourceTypesForPathMap(resourceTypeToPodSpecPaths)
	fh.RegisterFunction("set-pod-defaults", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-pod-defaults",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "pod-security",
					Required:      false,
					Description:   "Enable pod security labels on namespaces (default: true)",
					DataType:      api.DataTypeBool,
					Example:       "true",
				},
				{
					ParameterName: "automount-service-account-token",
					Required:      false,
					Description:   "Set automountServiceAccountToken to false (default: true)",
					DataType:      api.DataTypeBool,
					Example:       "true",
				},
				{
					ParameterName: "security-context",
					Required:      false,
					Description:   "Set security context for pods and containers (default: true)",
					DataType:      api.DataTypeBool,
					Example:       "true",
				},
				{
					ParameterName: "resources",
					Required:      false,
					Description:   "Set minimum resource requests for containers (default: true)",
					DataType:      api.DataTypeBool,
					Example:       "true",
				},
				{
					ParameterName: "probes",
					Required:      false,
					Description:   "Add liveness, readiness, and startup probes to containers (default: true)",
					DataType:      api.DataTypeBool,
					Example:       "true",
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set default pod settings",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: resourceTypes,
		},
		Function: k8sFnSetPodDefaults,
	})
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
		" the hostname", api.AttributeNameHostname, k8skit.K8sResourceProvider, true, false)
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
		" the subdomain", api.AttributeNameSubdomain, k8skit.K8sResourceProvider, true, false)
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
		" the domain name", api.AttributeNameDomain, k8skit.K8sResourceProvider, true, false)
}

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

var resourceTypeToPodSpecPaths = map[api.ResourceType][]string{
	api.ResourceType("apps/v1/Deployment"):  {"spec.template.spec"},
	api.ResourceType("apps/v1/ReplicaSet"):  {"spec.template.spec"},
	api.ResourceType("apps/v1/DaemonSet"):   {"spec.template.spec"},
	api.ResourceType("apps/v1/StatefulSet"): {"spec.template.spec"},
	api.ResourceType("batch/v1/Job"):        {"spec.template.spec"},
	api.ResourceType("batch/v1/CronJob"):    {"spec.jobTemplate.spec.template.spec"},
	api.ResourceType("v1/Pod"):              {"spec"},
}

var containersPaths = []string{"containers", "initContainers", "ephemeralContainers"}

var resourceTypeToContainersPaths = map[api.ResourceType][]string{
	api.ResourceType("apps/v1/Deployment"):  {"spec.template.spec.containers", "spec.template.spec.initContainers", "spec.template.spec.ephemeralContainers"},
	api.ResourceType("apps/v1/ReplicaSet"):  {"spec.template.spec.containers", "spec.template.spec.initContainers", "spec.template.spec.ephemeralContainers"},
	api.ResourceType("apps/v1/DaemonSet"):   {"spec.template.spec.containers", "spec.template.spec.initContainers", "spec.template.spec.ephemeralContainers"},
	api.ResourceType("apps/v1/StatefulSet"): {"spec.template.spec.containers", "spec.template.spec.initContainers", "spec.template.spec.ephemeralContainers"},
	api.ResourceType("batch/v1/Job"):        {"spec.template.spec.containers", "spec.template.spec.initContainers", "spec.template.spec.ephemeralContainers"},
	api.ResourceType("batch/v1/CronJob"):    {"spec.jobTemplate.spec.template.spec.containers", "spec.jobTemplate.spec.template.spec.initContainers", "spec.jobTemplate.spec.template.spec.ephemeralContainers"},
	api.ResourceType("v1/Pod"):              {"spec.containers", "spec.initContainers", "spec.ephemeralContainers"},
}

var resourceTypeToNeededHostnamePaths = map[api.ResourceType][]string{
	api.ResourceType("networking.k8s.io/v1/Ingress"): {"spec.rules.*.host"},
	api.ResourceType("v1/Service"):                   {"metadata.annotations." + yamlkit.EscapeDotsInPathSegment("external-dns.alpha.kubernetes.io/hostname")},
}

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
	imageReferenceRegexpString = fmt.Sprintf("(?P<reference>\\:(?:%s)|@(?:%s))", imageTagReferenceRegexpString, imageDigestReferenceRegexpString)
	// This expression partitions the image into two pieces, URI and reference
	imageURIReferenceRegexpString = fmt.Sprintf("^%s%s?$", imageURIRegexpString, imageReferenceRegexpString)
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

var replicatedControllerResourceTypes = []api.ResourceType{
	api.ResourceType("apps/v1/Deployment"),
	api.ResourceType("apps/v1/ReplicaSet"),
	api.ResourceType("apps/v1/StatefulSet"),
}

const (
	attributeNameEnvValue           = api.AttributeName("env-value")
	attributeNameReplicas           = api.AttributeName("replicas")
	attributeNameContainerResources = api.AttributeName("container-resources")
)

func initContainerFunctions() {
	// This regular expression breaks down an image into its components, but is more
	// complicated to use for replacement. It's not currently used, but is here in case we need it.
	imageRegexpString := fmt.Sprintf("^(?:(?P<registry>%s)/)?(?P<repository>%s)(?:\\:(?P<tag>%s)|@(?P<digest>%s))?$",
		imageRegistryHostRegexpString, imageRepositoryRegexpString, imageTagReferenceRegexpString, imageDigestReferenceRegexpString)
	imageRegexp = regexp.MustCompile(imageRegexpString)
	segmentNames := imageRegexp.SubexpNames()
	if len(segmentNames) != 5 {
		log.Fatalf("Image regexp doesn't contain exactly 4 segments: %d", len(segmentNames))
	}

	imageURIReferenceRegexp = regexp.MustCompile(imageURIReferenceRegexpString)
	segmentNames = imageURIReferenceRegexp.SubexpNames()
	if len(segmentNames) != 3 {
		log.Fatalf("Image URI+reference regexp doesn't contain exactly 2 segments: %d", len(segmentNames))
	}

	for resourceType, containerPaths := range resourceTypeToContainersPaths {
		for _, pathPrefix := range containerPaths {
			var attributePath api.UnresolvedPath
			var pathInfo *api.PathVisitorInfo

			containerNameGetterFunctionInvocation := &api.FunctionInvocation{
				FunctionName: "get-container-name",
				// Arguments will be added during traversal
			}

			// All container names
			attributePath = api.UnresolvedPath(pathPrefix + ".*.name")
			pathInfo = &api.PathVisitorInfo{
				Path:          attributePath,
				AttributeName: api.AttributeNameContainerName,
				DataType:      api.DataTypeString,
			}
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameContainerName,
				resourceType,
				api.PathToVisitorInfoType{attributePath: pathInfo},
				containerNameGetterFunctionInvocation,
				nil, // no setter
				true,
			)
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameGeneral,
				resourceType,
				api.PathToVisitorInfoType{attributePath: pathInfo},
				containerNameGetterFunctionInvocation,
				nil, // no setter
				true,
			)

			imageGetterFunctionInvocation := &api.FunctionInvocation{
				FunctionName: "get-image",
				// Arguments will be added during traversal
			}
			imageSetterFunctionInvocation := &api.FunctionInvocation{
				FunctionName: "set-image",
				// Arguments will be added during traversal
			}

			// Specific container image
			attributePath = api.UnresolvedPath(pathPrefix + ".?name:container-name=%s.image")
			pathInfo = &api.PathVisitorInfo{
				Path:          attributePath,
				AttributeName: api.AttributeNameContainerImage,
				DataType:      api.DataTypeString,
			}
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameContainerImage,
				resourceType,
				api.PathToVisitorInfoType{attributePath: pathInfo},
				imageGetterFunctionInvocation,
				imageSetterFunctionInvocation,
				false,
			)

			// All container images
			attributePath = api.UnresolvedPath(pathPrefix + ".*?name:container-name.image")
			pathInfo = &api.PathVisitorInfo{
				Path:          attributePath,
				AttributeName: api.AttributeNameContainerImage,
				DataType:      api.DataTypeString,
			}
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameGeneral,
				resourceType,
				api.PathToVisitorInfoType{attributePath: pathInfo},
				imageGetterFunctionInvocation,
				imageSetterFunctionInvocation,
				true,
			)
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameContainerImages,
				resourceType,
				api.PathToVisitorInfoType{attributePath: pathInfo},
				nil,
				nil,
				true,
			)

			repoURIGetterFunctionInvocation := &api.FunctionInvocation{
				FunctionName: "get-image-uri",
				// Arguments will be added during traversal
			}
			repoURISetterFunctionInvocation := &api.FunctionInvocation{
				FunctionName: "set-image-uri",
				// Arguments will be added during traversal
			}

			// Specific repo URI
			attributePath = api.UnresolvedPath(pathPrefix + ".?name:container-name=%s.image#uri")
			pathInfo = &api.PathVisitorInfo{
				Path:                   attributePath,
				AttributeName:          api.AttributeNameContainerRepositoryURI,
				DataType:               api.DataTypeString,
				EmbeddedAccessorType:   api.EmbeddedAccessorRegexp,
				EmbeddedAccessorConfig: imageURIReferenceRegexpString,
			}
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameContainerRepositoryURI,
				resourceType,
				api.PathToVisitorInfoType{attributePath: pathInfo},
				repoURIGetterFunctionInvocation,
				repoURISetterFunctionInvocation,
				false,
			)

			// All repo URIs
			attributePath = api.UnresolvedPath(pathPrefix + ".*?name:container-name.image#uri")
			pathInfo = &api.PathVisitorInfo{
				Path:                   attributePath,
				AttributeName:          api.AttributeNameContainerRepositoryURI,
				DataType:               api.DataTypeString,
				EmbeddedAccessorType:   api.EmbeddedAccessorRegexp,
				EmbeddedAccessorConfig: imageURIReferenceRegexpString,
			}
			pathInfos := api.PathToVisitorInfoType{attributePath: pathInfo}
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameGeneral,
				resourceType,
				pathInfos,
				repoURIGetterFunctionInvocation,
				repoURISetterFunctionInvocation,
				true,
			)
			yamlkit.RegisterNeededPaths(k8skit.K8sResourceProvider, resourceType, pathInfos, repoURISetterFunctionInvocation)
			addDescriptionToPathInfos(resourceType, pathInfos)
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameDetail,
				resourceType,
				pathInfos,
				nil,
				nil,
				true,
			)

			imageRefGetterFunctionInvocation := &api.FunctionInvocation{
				FunctionName: "get-image-reference",
				// Arguments will be added during traversal
			}
			imageRefSetterFunctionInvocation := &api.FunctionInvocation{
				FunctionName: "set-image-reference",
				// Arguments will be added during traversal
			}

			// Specific image reference
			attributePath = api.UnresolvedPath(pathPrefix + ".?name:container-name=%s.image#reference")
			pathInfo = &api.PathVisitorInfo{
				Path:                   attributePath,
				AttributeName:          api.AttributeNameContainerImageReference,
				DataType:               api.DataTypeString,
				EmbeddedAccessorType:   api.EmbeddedAccessorRegexp,
				EmbeddedAccessorConfig: imageURIReferenceRegexpString,
			}
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameContainerImageReference,
				resourceType,
				api.PathToVisitorInfoType{attributePath: pathInfo},
				imageRefGetterFunctionInvocation,
				imageRefSetterFunctionInvocation,
				false,
			)

			// All image references
			attributePath = api.UnresolvedPath(pathPrefix + ".*?name:container-name.image#reference")
			pathInfo = &api.PathVisitorInfo{
				Path:                   attributePath,
				AttributeName:          api.AttributeNameContainerImageReference,
				DataType:               api.DataTypeString,
				EmbeddedAccessorType:   api.EmbeddedAccessorRegexp,
				EmbeddedAccessorConfig: imageURIReferenceRegexpString,
			}
			pathInfos = api.PathToVisitorInfoType{attributePath: pathInfo}
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameGeneral,
				resourceType,
				pathInfos,
				imageRefGetterFunctionInvocation,
				imageRefSetterFunctionInvocation,
				true,
			)
			addDescriptionToPathInfos(resourceType, pathInfos)
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameDetail,
				resourceType,
				pathInfos,
				nil,
				nil,
				true,
			)

			envVarGetterFunctionInvocation := &api.FunctionInvocation{
				FunctionName: "get-env-var",
				// Arguments will be added during traversal
			}
			envVarSetterFunctionInvocation := &api.FunctionInvocation{
				FunctionName: "set-env-var",
				// Arguments will be added during traversal
			}

			// Specific env var. All env vars maybe not useful.
			attributePath = api.UnresolvedPath(pathPrefix + ".?name:container-name=%s.env.?name:env-var=%s.value")
			pathInfo = &api.PathVisitorInfo{
				Path:          attributePath,
				AttributeName: attributeNameEnvValue,
				DataType:      api.DataTypeString,
			}
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				attributeNameEnvValue,
				resourceType,
				api.PathToVisitorInfoType{attributePath: pathInfo},
				envVarGetterFunctionInvocation,
				envVarSetterFunctionInvocation,
				false,
			)

			// Specific container resources
			attributePath = api.UnresolvedPath(pathPrefix + ".?name:container-name=%s.resources")
			pathInfo = &api.PathVisitorInfo{
				Path:          attributePath,
				AttributeName: attributeNameContainerResources,
				DataType:      api.DataTypeYAML,
			}
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				attributeNameContainerResources,
				resourceType,
				api.PathToVisitorInfoType{attributePath: pathInfo},
				nil, // don't register getters and setters for now
				nil,
				false,
			)
		}
	}

	replicasGetterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "get-replicas",
		// Arguments will be added during traversal
	}
	replicasSetterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "set-replicas",
		// Arguments will be added during traversal
	}

	for _, resourceType := range replicatedControllerResourceTypes {
		attributePath := api.UnresolvedPath("spec.replicas")
		pathInfos := api.PathToVisitorInfoType{
			attributePath: {
				Path:          attributePath,
				AttributeName: attributeNameReplicas,
				DataType:      api.DataTypeInt,
			},
		}
		yamlkit.RegisterPathsByAttributeName(
			k8skit.K8sResourceProvider,
			attributeNameReplicas,
			resourceType,
			pathInfos,
			replicasGetterFunctionInvocation,
			replicasSetterFunctionInvocation,
			false,
		)
		yamlkit.RegisterPathsByAttributeName(
			k8skit.K8sResourceProvider,
			api.AttributeNameGeneral,
			resourceType,
			pathInfos,
			replicasGetterFunctionInvocation,
			replicasSetterFunctionInvocation,
			true,
		)
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

	hostnameGetterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "get-hostname",
		// Arguments will be added during traversal
	}
	hostnameSetterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "set-hostname",
		// Arguments will be added during traversal
	}

	dnsSubdomainRegexp = regexp.MustCompile(dnsSubdomainDomainRegexpString)
	segmentNames = dnsSubdomainRegexp.SubexpNames()
	if len(segmentNames) != 3 {
		log.Fatalf("DNS subdomain+domain regexp doesn't contain exactly 2 segments: %d", len(segmentNames))
	}

	subdomainGetterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "get-hostname-subdomain",
		// Arguments will be added during traversal
	}
	subdomainSetterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "set-hostname-subdomain",
		// Arguments will be added during traversal
	}

	domainGetterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "get-hostname-domain",
		// Arguments will be added during traversal
	}
	domainSetterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "set-hostname-domain",
		// Arguments will be added during traversal
	}

	for resourceType, paths := range resourceTypeToNeededHostnamePaths {
		for _, attributePath := range paths {
			pathInfos := api.PathToVisitorInfoType{
				api.UnresolvedPath(attributePath): {
					Path:          api.UnresolvedPath(attributePath),
					AttributeName: api.AttributeNameHostname,
					DataType:      api.DataTypeString,
				},
			}
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameHostname,
				resourceType,
				pathInfos,
				hostnameGetterFunctionInvocation,
				hostnameSetterFunctionInvocation,
				false,
			)
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameGeneral,
				resourceType,
				pathInfos,
				hostnameGetterFunctionInvocation,
				hostnameSetterFunctionInvocation,
				true,
			)
			// This is already added to details in standard_functions.go

			// Split hostname into 2 parts for needs/provides

			pathInfos = api.PathToVisitorInfoType{
				api.UnresolvedPath(attributePath + "#subdomain"): {
					Path:                   api.UnresolvedPath(attributePath + "#subdomain"),
					AttributeName:          api.AttributeNameSubdomain,
					DataType:               api.DataTypeString,
					EmbeddedAccessorType:   api.EmbeddedAccessorRegexp,
					EmbeddedAccessorConfig: dnsSubdomainDomainRegexpString,
				},
			}
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameSubdomain,
				resourceType,
				pathInfos,
				subdomainGetterFunctionInvocation,
				subdomainSetterFunctionInvocation,
				false,
			)
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameGeneral,
				resourceType,
				pathInfos,
				subdomainGetterFunctionInvocation,
				subdomainSetterFunctionInvocation,
				true,
			)

			pathInfos = api.PathToVisitorInfoType{
				api.UnresolvedPath(attributePath + "#domain"): {
					Path:                   api.UnresolvedPath(attributePath + "#domain"),
					AttributeName:          api.AttributeNameDomain,
					DataType:               api.DataTypeString,
					EmbeddedAccessorType:   api.EmbeddedAccessorRegexp,
					EmbeddedAccessorConfig: dnsSubdomainDomainRegexpString,
				},
			}
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameDomain,
				resourceType,
				pathInfos,
				domainGetterFunctionInvocation,
				domainSetterFunctionInvocation,
				false,
			)
			yamlkit.RegisterPathsByAttributeName(
				k8skit.K8sResourceProvider,
				api.AttributeNameGeneral,
				resourceType,
				pathInfos,
				domainGetterFunctionInvocation,
				domainSetterFunctionInvocation,
				true,
			)
			yamlkit.RegisterNeededPaths(k8skit.K8sResourceProvider, resourceType, pathInfos, domainSetterFunctionInvocation)
		}
	}
}

func k8sFnSetImageReferenceByURI(_ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	imageURI := args[0].Value.(string)
	newReference := args[1].Value.(string)
	newImage := imageURI + newReference

	resourceTypeToAllImagePaths := yamlkit.GetPathRegistryForAttributeName(k8skit.K8sResourceProvider, api.AttributeNameContainerImages)
	updater := func(currentValue string) string {
		matches := imageURIReferenceRegexp.FindStringSubmatchIndex(currentValue)
		// fmt.Printf("image %s, matches %v", currentValue, matches)
		// The first two elements should be zero and length of the string
		// The second two should be the start and end of the URI
		// The third two should be the start and end of the reference
		if len(matches) != 6 {
			fmt.Printf("\n")
			return currentValue
		}
		currentURI := currentValue[matches[2]:matches[3]]
		// fmt.Printf(", URI %s\n", currentURI)
		if currentURI != imageURI {
			return currentValue
		}
		return newImage
	}
	err := yamlkit.UpdateStringPathsFunction(parsedData, resourceTypeToAllImagePaths, []any{}, k8skit.K8sResourceProvider, updater, false)
	return parsedData, nil, err
}

func k8sFnSetEnv(_ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
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

	var err error
	for _, doc := range parsedData {
		var resourceType api.ResourceType
		resourceType, err = k8skit.K8sResourceProvider.ResourceTypeGetter(doc)
		if err != nil {
			continue // Skip malformed resources
		}
		containersPaths, ok := resourceTypeToContainersPaths[resourceType]
		if !ok {
			continue // Skip resource kinds we don't handle
		}

		for _, containersPath := range containersPaths {
			var resolvedContainersPaths []yamlkit.ResolvedPathInfo
			unresolvedPath := api.UnresolvedPath(containersPath + ".?name=" + containerName)
			resolvedContainersPaths, err = yamlkit.ResolveAssociativePaths(doc, unresolvedPath, "", false)
			if err != nil {
				continue // skip problematic path
			}
			for _, containerPath := range resolvedContainersPaths {
				// Make a copy of the pairs for this container
				thisPairs := map[string]string{}
				for k, v := range pairs {
					thisPairs[k] = v
				}

				var container *gaby.YamlDoc
				var found bool
				container, found, err = yamlkit.YamlSafePathGetDoc(doc, containerPath.Path, true)
				if !found || err != nil {
					continue
				}
				envs := container.Path("env")
				if envs == nil {
					var ary *gaby.YamlDoc
					// Create the environment array if it doesn't exist
					ary, err = container.Array("env")
					if err != nil {
						multiErrs = append(multiErrs, errors.Wrap(err, "error creating environment array"))
						continue
					}
					envs = ary
				}
				for _, entry := range envs.Children() {
					var name string
					// Overwrite the value of the environment variable if it exists
					if entry.Exists("name") {
						name, ok = entry.Path("name").Data().(string)
						if !ok {
							continue // skip malformed element
						}
						var val string
						// An empty string indicates the variable should be removed, which is handled below
						if val, ok = thisPairs[name]; ok && val != "" {
							_, err = entry.SetP(val, "value")
							if err != nil {
								multiErrs = append(multiErrs, errors.Newf("error setting environment variable %s: %v", name, err))
							}
							// Remove the key from the thisPairs map
							delete(thisPairs, name)
						}
					}
				}
				// For the remaining pairs, remove or add them to the environment array
				// If the user specifies an empty string for a value, we should remove the environment variable
				// Iterate in the same order as the original args for determinism
				var v string
				for _, k := range keys {
					v, found = thisPairs[k]
					if !found {
						continue
					}
					if v == "" {
						var pairPaths []yamlkit.ResolvedPathInfo
						pairPaths, err = yamlkit.ResolveAssociativePaths(envs, api.UnresolvedPath("?name="+k), "", false)
						if err != nil || len(pairPaths) == 0 {
							// Not found shouldn't be an error
							continue
						}
						if len(pairPaths) > 1 {
							log.Error("Expected resolveAssociativePaths to return at most one result")
						}
						pairPath := pairPaths[0]
						if err := envs.DeleteP(string(pairPath.Path)); err != nil {
							multiErrs = append(multiErrs, errors.Wrapf(err, "error deleting environment variable %s", k))
							continue
						}
					} else {

						// TODO: is there a way to make this an ordered pair?
						val := map[string]interface{}{"name": k, "value": v}
						if err = envs.ArrayAppend(val); err != nil {
							multiErrs = append(multiErrs, errors.Wrapf(err, "error appending environment variable %s", k))
							continue
						}
					}
				}
			}
		}
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

func k8sFnSetContainerResources(_ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
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

	updater := func(currentDoc *gaby.YamlDoc) *gaby.YamlDoc {
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

	resourceTypeToResourcesPaths := yamlkit.GetPathRegistryForAttributeName(k8skit.K8sResourceProvider, attributeNameContainerResources)
	// TODO: consider setting upsert to true
	err = yamlkit.UpdatePathsFunctionDoc(parsedData, resourceTypeToResourcesPaths, []any{containerName}, k8skit.K8sResourceProvider, updater, false)
	return parsedData, nil, err
}

func k8sFnSetPodDefaults(_ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	multiErrs := []error{}
	var err error

	// Parse parameters with default values of true
	podSecurity := true
	automountServiceAccountToken := true
	securityContext := true
	resources := true
	probes := true

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
	for _, doc := range parsedData {
		var resourceType api.ResourceType
		resourceType, err = k8skit.K8sResourceProvider.ResourceTypeGetter(doc)
		if err != nil {
			continue // Skip malformed resources
		}
		if resourceType == namespaceResourceType && podSecurity {
			// The dots don't need to be escaped when using Set rather than SetP
			_, err = doc.Set("baseline", "metadata", "labels", "pod-security.kubernetes.io/enforce")
			if err != nil {
				multiErrs = append(multiErrs, err)
			}
			_, err = doc.Set("latest", "metadata", "labels", "pod-security.kubernetes.io/enforce-version")
			if err != nil {
				multiErrs = append(multiErrs, err)
			}
			_, err = doc.Set("restricted", "metadata", "labels", "pod-security.kubernetes.io/warn")
			if err != nil {
				multiErrs = append(multiErrs, err)
			}
			_, err = doc.Set("latest", "metadata", "labels", "pod-security.kubernetes.io/warn-version")
			if err != nil {
				multiErrs = append(multiErrs, err)
			}
			continue
		}
		podSpecPaths, ok := resourceTypeToPodSpecPaths[resourceType]
		if !ok {
			continue // Skip resource kinds we don't handle
		}
		log.Infof("traversing resource of type " + string(resourceType))

		for _, podSpecPath := range podSpecPaths {
			// For some of these attributes, we don't care whether or how they were set.
			// We have a "best practice" default we want to use. For others, we have a
			// minimum expected set of values.
			podSpecDoc, hasPodSpec, err := yamlkit.YamlSafePathGetDoc(doc, api.ResolvedPath(podSpecPath), true)
			if err != nil {
				multiErrs = append(multiErrs, err)
				continue
			}
			if !hasPodSpec {
				log.Infof("no pod spec")

				// Shouldn't happen, but be resilient
				continue
			}

			// TODO: Register these in the path registry?

			if automountServiceAccountToken {
				_, err = podSpecDoc.Set(false, "automountServiceAccountToken")
				if err != nil {
					multiErrs = append(multiErrs, err)
				}
			}

			// Set terminationGracePeriodSeconds to 60 if not already present
			if !podSpecDoc.Exists("terminationGracePeriodSeconds") {
				_, err = podSpecDoc.Set(60, "terminationGracePeriodSeconds")
				if err != nil {
					multiErrs = append(multiErrs, err)
				}
			}

			if securityContext {
				// Pod-level security contexft
				_, err = podSpecDoc.Set("RuntimeDefault", "securityContext", "seccompProfile", "type")
				if err != nil {
					multiErrs = append(multiErrs, err)
				}

				for _, containerPath := range containersPaths {
					containersDoc, hasContainers, err := yamlkit.YamlSafePathGetDoc(podSpecDoc, api.ResolvedPath(containerPath), true)
					if err != nil {
						multiErrs = append(multiErrs, err)
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
							multiErrs = append(multiErrs, err)
						}
						_, err = containerDoc.Set(true, "securityContext", "runAsNonRoot")
						if err != nil {
							multiErrs = append(multiErrs, err)
						}
						_, err = containerDoc.Set(false, "securityContext", "allowPrivilegeEscalation")
						if err != nil {
							multiErrs = append(multiErrs, err)
						}
						_, err = containerDoc.Set(false, "securityContext", "privileged")
						if err != nil {
							multiErrs = append(multiErrs, err)
						}

						// Set capabilities.drop to ALL if not already present
						if !containerDoc.ExistsP("securityContext.capabilities.drop") {
							_, err = containerDoc.Set([]interface{}{"ALL"}, "securityContext", "capabilities", "drop")
							if err != nil {
								multiErrs = append(multiErrs, err)
							}
						}

						// Set imagePullPolicy to Always if not already present
						if !containerDoc.Exists("imagePullPolicy") {
							_, err = containerDoc.Set("Always", "imagePullPolicy")
							if err != nil {
								multiErrs = append(multiErrs, err)
							}
						}
					}
				}
			}

			if resources {
				for _, containerPath := range containersPaths {
					containersDoc, hasContainers, err := yamlkit.YamlSafePathGetDoc(podSpecDoc, api.ResolvedPath(containerPath), true)
					if err != nil {
						multiErrs = append(multiErrs, err)
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
								multiErrs = append(multiErrs, err)
								continue
							}
						} else {
							resourcesDoc, _, err = yamlkit.YamlSafePathGetDoc(containerDoc, api.ResolvedPath("resources"), false)
							if err != nil {
								multiErrs = append(multiErrs, err)
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
							multiErrs = append(multiErrs, err)
							continue
						}
						_, err = containerDoc.SetDocP(newDoc, "resources")
						if err != nil {
							multiErrs = append(multiErrs, err)
						}
					}
				}
			}

			if probes {
				for _, containerPath := range containersPaths {
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

						// Check existing probes to determine what to add
						hasReadinessProbe := containerDoc.Exists("readinessProbe")
						hasLivenessProbe := containerDoc.Exists("livenessProbe")
						hasStartupProbe := containerDoc.Exists("startupProbe")

						// If readiness probe exists, don't add any probes
						if hasReadinessProbe {
							continue
						}

						// If liveness probe exists, copy its values for readiness probe
						if hasLivenessProbe {
							livenessProbeDoc, _, err := yamlkit.YamlSafePathGetDoc(containerDoc, api.ResolvedPath("livenessProbe"), false)
							if err == nil && livenessProbeDoc != nil {
								// Copy liveness probe settings for readiness probe
								readinessProbe := orderedmap.New[string, interface{}]()
								for key, childDoc := range livenessProbeDoc.ChildrenMap() {
									readinessProbe.Set(key, childDoc.Data())
								}
								// Adjust readiness probe timing to be more responsive
								readinessProbe.Set("initialDelaySeconds", 5)
								readinessProbe.Set("periodSeconds", 10)
								_, err = containerDoc.Set(readinessProbe, "readinessProbe")
								if err != nil {
									multiErrs = append(multiErrs, err)
								}
							}
						} else if hasStartupProbe {
							// If startup probe exists, copy its values for readiness probe
							startupProbeDoc, _, err := yamlkit.YamlSafePathGetDoc(containerDoc, api.ResolvedPath("startupProbe"), false)
							if err == nil && startupProbeDoc != nil {
								readinessProbe := orderedmap.New[string, interface{}]()
								for key, childDoc := range startupProbeDoc.ChildrenMap() {
									readinessProbe.Set(key, childDoc.Data())
								}
								// Adjust readiness probe timing
								readinessProbe.Set("initialDelaySeconds", 5)
								readinessProbe.Set("periodSeconds", 10)
								_, err = containerDoc.Set(readinessProbe, "readinessProbe")
								if err != nil {
									multiErrs = append(multiErrs, err)
								}
							}
						} else {
							// No existing probes, add all three with appropriate defaults

							// Create HTTP GET action for probes
							httpGet := orderedmap.New[string, interface{}]()
							httpGet.Set("path", "/replaceme")
							httpGet.Set("port", probePort)

							// Startup probe - most lenient, runs first
							startupProbe := orderedmap.New[string, interface{}]()
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

							// Liveness probe - detects when to restart
							livenessProbe := orderedmap.New[string, interface{}]()
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

							// Readiness probe - controls traffic routing
							readinessProbe := orderedmap.New[string, interface{}]()
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
					}
				}
			}
		}
	}

	if len(multiErrs) != 0 {
		return parsedData, nil, errors.WithStack(errors.Join(multiErrs...))
	}
	return parsedData, nil, nil
}
