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
	initMetadataFunctions(rp)
	initStandardFunctions(rp)
	initContainerFunctions(rp)
	initDefaultingFunctions(rp)
	registerImmutablePaths(rp)
}

// registerImmutablePaths registers all known Kubernetes immutable field paths with the
// path registry under the "immutable" attribute name. This enables functions like
// vet-immutable to look up immutable paths per resource type.
func registerImmutablePaths(rp *k8skit.K8sResourceProviderType) {
	for resourceType, paths := range k8skit.GetImmutablePaths() {
		pathInfos := make(api.PathToVisitorInfoType, len(paths))
		for _, path := range paths {
			unresolvedPath := api.UnresolvedPath(path)
			pathInfos[unresolvedPath] = &api.PathVisitorInfo{
				Path:          unresolvedPath,
				AttributeName: k8skit.AttributeNameImmutable,
				DataType:      api.DataTypeYAML, // immutable fields can be any type
			}
		}
		yamlkit.RegisterPathsByAttributeName(
			rp,
			k8skit.AttributeNameImmutable,
			resourceType,
			pathInfos,
			nil,   // no getter/setter details
			false, // not needed
			false, // not provided
		)
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
