// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"strings"

	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func getOptions(doc *gaby.YamlDoc, resourceProvider ResourceProvider, optionField string) []string {
	optionsPath := resourceProvider.ContextPath(optionField)
	if optionsPath == "" {
		return []string{}
	}
	value, found, err := YamlSafePathGetValue[string](doc, api.ResolvedPath(optionsPath), true)
	if err != nil || !found {
		return []string{}
	}
	options := strings.Split(value, ",")
	for i := range options {
		options[i] = strings.TrimSpace(options[i])
	}
	return options
}

// GetMutationOptions reads the MutationOptions value from a YAML document using the
// resource provider's context path.
func GetMutationOptions(doc *gaby.YamlDoc, resourceProvider ResourceProvider) []string {
	return getOptions(doc, resourceProvider, constants.MutationOptionsKeySuffix)
}

// GetVisitorOptions reads the VisitorOptions value from a YAML document using the
// resource provider's context path.
func GetVisitorOptions(doc *gaby.YamlDoc, resourceProvider ResourceProvider) []string {
	return getOptions(doc, resourceProvider, constants.VisitorOptionsKeySuffix)
}
