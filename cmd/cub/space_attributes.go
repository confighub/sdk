// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// spaceAttributeFlagValues holds --where-attribute and --attribute-filter, which select the
// Attributes a Space's function executor is built from.
type spaceAttributeFlagValues struct {
	whereAttribute  string
	attributeFilter string
}

// addSpaceAttributeFlags registers --where-attribute and --attribute-filter. The returned values
// are applied to a Space written whole with apply, and to a patch with patchEnhancer.
func addSpaceAttributeFlags(cmd *cobra.Command) *spaceAttributeFlagValues {
	values := &spaceAttributeFlagValues{}
	cmd.Flags().StringVar(&values.whereAttribute, "where-attribute", "", "filter expression to identify Attributes that should be registered for functions run on Units within this Space; with neither it nor an attribute filter, the Attributes in the Space are (use '-' to clear)")
	cmd.Flags().StringVar(&values.attributeFilter, "attribute-filter", "", "Filter slug or UUID to identify Attributes that should be registered for functions run on Units within this Space (use '-' to clear)")
	return values
}

// attributeFilterID resolves --attribute-filter, returning nil when the flag was not given or
// clears the field.
func (values *spaceAttributeFlagValues) attributeFilterID() (*uuid.UUID, error) {
	if values.attributeFilter == "" || values.attributeFilter == clearFlagValue {
		return nil, nil
	}
	filterID, err := parseFilterFlag(values.attributeFilter)
	if err != nil {
		return nil, err
	}
	parsed := uuid.MustParse(filterID)
	return &parsed, nil
}

// apply sets the fields the flags name on a Space that is written whole.
func (values *spaceAttributeFlagValues) apply(space *goclientnew.Space) error {
	filterID, err := values.attributeFilterID()
	if err != nil {
		return err
	}
	if values.whereAttribute == clearFlagValue {
		space.WhereAttribute = ""
	} else if values.whereAttribute != "" {
		space.WhereAttribute = values.whereAttribute
	}
	if values.attributeFilter == clearFlagValue {
		space.AttributeFilterID = nil
	} else if filterID != nil {
		space.AttributeFilterID = filterID
	}
	return nil
}

// patchEnhancer resolves the flags and returns what adds the fields they name to a patch.
func (values *spaceAttributeFlagValues) patchEnhancer() (PatchEnhancer, error) {
	filterID, err := values.attributeFilterID()
	if err != nil {
		return nil, err
	}
	return func(patchMap map[string]interface{}) {
		if values.whereAttribute == clearFlagValue {
			patchMap["WhereAttribute"] = ""
		} else if values.whereAttribute != "" {
			patchMap["WhereAttribute"] = values.whereAttribute
		}
		if values.attributeFilter == clearFlagValue {
			patchMap["AttributeFilterID"] = nil
		} else if filterID != nil {
			patchMap["AttributeFilterID"] = filterID.String()
		}
	}, nil
}
