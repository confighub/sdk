// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package confighub

import (
	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/function-impl/generic"
)

// Every entity has Labels and Annotations, under the same attribute names as a Kubernetes
// resource's, so get-label, set-label, get-annotation and set-annotation mean the same thing in
// both toolchains.
var metadataMapPaths = map[api.AttributeName]api.UnresolvedPath{
	api.AttributeNameLabelValue:      "Labels.@%s:label-key",
	api.AttributeNameAnnotationValue: "Annotations.@%s:annotation-key",
}

func initMetadataFunctions(rp *cubkit.ConfigHubResourceProviderType) {
	for attributeName, path := range metadataMapPaths {
		yamlkit.RegisterPathsByAttributeName(rp, attributeName, api.ResourceTypeAny, api.PathToVisitorInfoType{
			path: {
				Path:          path,
				AttributeName: attributeName,
				DataType:      api.DataTypeString,
			},
		}, nil, false, false)
	}
}

func registerMetadataFunctions(fh handler.FunctionRegistry, rp *cubkit.ConfigHubResourceProviderType) {
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
		" an annotation", api.AttributeNameAnnotationValue, rp, true, true, false)

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
		" a label", api.AttributeNameLabelValue, rp, true, true, false)
}
