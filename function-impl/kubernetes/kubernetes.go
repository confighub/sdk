// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"log/slog"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
)

func initFunctions(rp *k8skit.K8sResourceProviderType) {
	err := InitSchemaFinder()
	if err != nil {
		slog.Error("error", "error", err)
	}
	initReferenceFunctions(rp)
	initMetadataFunctions(rp)
	initStandardFunctions(rp)
	initContainerFunctions(rp)
	initDefaultingFunctions(rp)
	registerDeclaredAttributePaths(rp)
}

// attributeDescriptors say what each attribute declared in the resource-type specs is: its
// data type, and how it is read and written. The specs say where each one lives. Everything
// here is per attribute name and nothing is per resource type, which is the split that lets
// registering a type be one edit rather than one edit per attribute it has.
// attributeDescriptors is a function rather than a package variable because some of the
// regexps an accessor is configured with are themselves assembled during init, and a package
// variable would capture them before that.
func attributeDescriptors() map[api.AttributeName]yamlkit.AttributeDescriptor {
	return map[api.AttributeName]yamlkit.AttributeDescriptor{
		// Nothing reads the value at an immutable path -- the attribute is a predicate on the
		// path, read by vet-immutable -- so it has no getter, no setter, and a data type that
		// admits anything.
		k8skit.AttributeNameImmutable: {DataType: api.DataTypeYAML},
		// Written separately because only one of them is safe to change freely, and read together
		// through the workload-labels group by get/set-workload-labels.
		k8skit.AttributeNamePodTemplateLabels: {DataType: api.DataTypeYAML},
		k8skit.AttributeNamePodLabelSelector:  {DataType: api.DataTypeYAML},

		// Provided at a ConfigMap's own annotation and needed at a pod template's, which is
		// why its paths state their own role rather than taking the descriptor's.
		attributeNameConfigMapHash: {DataType: api.DataTypeString},

		// A hostname declares one path per type; the subdomain and the domain within it are
		// registered at a suffix of it, through a regexp accessor that splits the value.
		api.AttributeNameHostname: {
			DataType:         api.DataTypeString,
			DescribePaths:    true,
			Derived: []yamlkit.DerivedAttribute{
				{AttributeName: api.AttributeNameSubdomain, PathSuffix: "#subdomain"},
				{AttributeName: api.AttributeNameDomain, PathSuffix: "#domain"},
			},
		},
		api.AttributeNameSubdomain: {
			DataType:               api.DataTypeString,
			EmbeddedAccessorType:   api.EmbeddedAccessorRegexp,
			EmbeddedAccessorConfig: dnsSubdomainDomainRegexpString,
			DescribePaths:          true,
		},
		api.AttributeNameDomain: {
			DataType:               api.DataTypeString,
			EmbeddedAccessorType:   api.EmbeddedAccessorRegexp,
			EmbeddedAccessorConfig: dnsSubdomainDomainRegexpString,
			DescribePaths:          true,
			IsNeeded:               true,
		},

		// Defaulting attributes. Each declared path carries the value written there, so the
		// descriptor says only what kind of value it is; the setter is built per path.
		attributeNameAutomountServiceAccountToken:    {DataType: api.DataTypeBool},
		attributeNamePodContainerSecurityCtxDefaults: {},
		attributeNameContainerResourcesDefaults:      {},

		// The container attributes. Each is declared on the Containers shape at the element it
		// selects, so a setter writes through the path it is registered at.
		api.AttributeNameContainerName: {
			DataType:         api.DataTypeString,
			DescribePaths:    true,
		},
		api.AttributeNameContainerImage: {
			DataType:         api.DataTypeString,
			DescribePaths:    true,
		},
		api.AttributeNameContainerRepositoryURI: {
			DataType:               api.DataTypeString,
			EmbeddedAccessorType:   api.EmbeddedAccessorRegexp,
			EmbeddedAccessorConfig: imageURIReferenceRegexpString,
			DescribePaths:          true,
		},
		api.AttributeNameContainerImageReference: {
			DataType:               api.DataTypeString,
			EmbeddedAccessorType:   api.EmbeddedAccessorRegexp,
			EmbeddedAccessorConfig: imageURIReferenceRegexpString,
			DescribePaths:          true,
		},
		attributeNameEnvValue: {
			DataType:         api.DataTypeString,
			DescribePaths:    true,
		},
		// No getter or setter: set-container-resources is not a path setter. It takes an
		// operation (all, cap or floor) alongside the cpu and memory to set, and decides per
		// value whether to write, so it reaches the path itself.
		attributeNameContainerResources: {
			DataType:      api.DataTypeYAML,
			DescribePaths: true,
		},
		attributeNameContainerFlag: {
			DataType:               api.DataTypeString,
			EmbeddedAccessorType:   api.EmbeddedAccessorRegexp,
			EmbeddedAccessorConfig: pflagRegexpString,
			DescribePaths:          true,
		},

		attributeNameReplicas: {
			DataType:         api.DataTypeInt,
			DescribePaths:    true,
		},
	}
}

// registerDeclaredAttributePaths registers every attribute path the built-in specs declare.
// It replaces loops over per-resource-type tables; a type absent from the specs registers
// nothing, as it did before.
func registerDeclaredAttributePaths(rp *k8skit.K8sResourceProviderType) {
	k8skit.RegisterWorkloadLabelsGroup(rp)
	if err := k8skit.RegisterDeclaredAttributePaths(rp, attributeDescriptors(), addDescriptionToPathInfos); err != nil {
		// The specs are embedded in k8skit and the descriptors are in this file, so a failure
		// is a mismatch between the two rather than anything a caller did.
		panic("kubernetes: registering declared attribute paths: " + err.Error())
	}
}

// RegisterFunctions registers all Kubernetes functions onto the provided FunctionHandler
// using the given registrar's resource provider.
func RegisterFunctions(rp *k8skit.K8sResourceProviderType, fh handler.FunctionRegistry) {
	initFunctions(rp)

	registerStandardFunctions(fh, rp)
	registerMetadataFunctions(fh, rp)
	registerContainerFunctions(fh, rp)
	registerDefaultingFunctions(fh, rp)
	registerK8sCELFunctions(fh, rp)
	registerAccessFunctions(fh)
	registerPruneConfigMaps(fh, rp)
}
