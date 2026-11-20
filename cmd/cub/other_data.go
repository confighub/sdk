// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"maps"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// A function that reads configuration besides the unit it runs on, such as vet-immutable's
// baseline or vet-validating-admission-policy's policy, is passed it as OtherData: another
// revision of the unit, which the function's signature names, or another unit under a key the
// function reads it by. A trigger or invocation names other units with --other-unit; function do
// names what the functions it runs are passed with --other-data-source and --other-unit.

// otherUnitFlags holds --other-unit on the trigger and invocation commands.
var otherUnitFlags []string

// addStoredOtherDataFlags adds --other-unit to a command that writes a trigger or an invocation.
// update says whether a key may be removed.
func addStoredOtherDataFlags(cmd *cobra.Command, entity string, update bool) {
	usage := "another unit to pass the " + entity + "'s functions, as key=unit, where the functions read it under key " +
		"(e.g. policy=my-policy for vet-validating-admission-policy) and unit is a slug, space/slug or UUID; can be repeated"
	if update {
		usage += "; key= stops passing the unit under key"
	}
	cmd.Flags().StringArrayVar(&otherUnitFlags, "other-unit", nil, usage)
}

// parseOtherUnitFlags resolves --other-unit to the units it names by key. A key given no unit maps
// to nil, which removes it; only an update allows that. Unqualified slugs are looked up in spaceID.
func parseOtherUnitFlags(spaceID string, update bool) (map[string]*uuid.UUID, error) {
	if len(otherUnitFlags) == 0 {
		return nil, nil
	}
	units := map[string]*uuid.UUID{}
	for _, flag := range otherUnitFlags {
		key, ref, found := strings.Cut(flag, "=")
		if !found {
			return nil, errors.Newf("--other-unit %q is not key=unit", flag)
		}
		if err := api.ValidateOtherDataKey(key); err != nil {
			return nil, errors.Wrapf(err, "--other-unit %q", flag)
		}
		if ref == "" {
			if !update {
				return nil, errors.Newf("--other-unit %q names no unit", flag)
			}
			units[key] = nil
			continue
		}
		unit, err := resolveUnit(ref, spaceID, idSelect("UnitID"))
		if err != nil {
			return nil, errors.Wrapf(err, "--other-unit %q", flag)
		}
		unitID := unit.Unit.UnitID
		units[key] = &unitID
	}
	return units, nil
}

// applyOtherUnits applies parsed --other-unit flags to the units an entity names.
func applyOtherUnits(existing map[string]goclientnew.UUID, units map[string]*uuid.UUID) map[string]goclientnew.UUID {
	if units == nil {
		return existing
	}
	result := maps.Clone(existing)
	if result == nil {
		result = map[string]goclientnew.UUID{}
	}
	for key, unitID := range units {
		if unitID == nil {
			delete(result, key)
			continue
		}
		result[key] = *unitID
	}
	return result
}

// addOtherDataToPatch adds --other-unit to a merge patch of a trigger or an invocation. It
// changes only the keys it names.
func addOtherDataToPatch(patch map[string]interface{}, units map[string]*uuid.UUID) {
	if units == nil {
		return
	}
	patchUnits := map[string]interface{}{}
	for key, unitID := range units {
		if unitID == nil {
			patchUnits[key] = nil
			continue
		}
		patchUnits[key] = unitID.String()
	}
	patch["OtherUnitIDs"] = patchUnits
}

// formatOtherUnits shows the units a trigger or invocation passes its functions, by key.
func formatOtherUnits(units map[string]goclientnew.UUID) string {
	entries := make([]string, 0, len(units))
	for _, key := range slices.Sorted(maps.Keys(units)) {
		entries = append(entries, key+"="+units[key].String())
	}
	return strings.Join(entries, ",")
}

// A trigger can also select other units for each space that selects it, under a key, with
// --other-unit-selection-where, --other-unit-selection-filter and
// --other-unit-selection-where-resource, each key=value. The where may name the space as
// EvaluatingSpace. The value - clears that field of the selection, and a selection with none of
// them left is removed.

// otherUnitSelectionFlags holds the --other-unit-selection-* flags of the trigger commands.
var otherUnitSelectionFlags struct {
	where         []string
	filter        []string
	whereResource []string
}

// addOtherUnitSelectionFlags adds the --other-unit-selection-* flags to a command that writes a
// trigger.
func addOtherUnitSelectionFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&otherUnitSelectionFlags.where, "other-unit-selection-where", nil,
		"select other units to pass the trigger's function under a key, as key=where, for each space that selects the trigger: "+
			"a unit where expression, which may name that space as EvaluatingSpace, "+
			"e.g. \"policies=Space.ReleaseTargetID = EvaluatingSpace.ReleaseTargetID\"; - clears it; can be repeated")
	cmd.Flags().StringArrayVar(&otherUnitSelectionFlags.filter, "other-unit-selection-filter", nil,
		"a filter (From=Unit) the units selected under a key must also match, as key=filter, where filter is a slug, space/slug or UUID; - clears it; can be repeated")
	cmd.Flags().StringArrayVar(&otherUnitSelectionFlags.whereResource, "other-unit-selection-where-resource", nil,
		"select the units holding a resource a WhereResource expression matches under a key, and pass the function only the resources it matches, as key=expression, "+
			"e.g. \"policies=ConfigHub.ResourceType = 'networking.k8s.io/v1/NetworkPolicy'\"; - clears it; can be repeated")
}

// otherUnitSelectionEdit is what the flags say of one selection's fields: nil leaves a field
// alone, and an empty value clears it.
type otherUnitSelectionEdit struct {
	where         *string
	filterID      *uuid.UUID
	clearFilter   bool
	whereResource *string
}

// parseOtherUnitSelectionFlags reads the --other-unit-selection-* flags by key.
func parseOtherUnitSelectionFlags() (map[string]*otherUnitSelectionEdit, error) {
	edits := map[string]*otherUnitSelectionEdit{}
	edit := func(flagName, flag string) (*otherUnitSelectionEdit, string, error) {
		key, value, found := strings.Cut(flag, "=")
		if !found {
			return nil, "", errors.Newf("--%s %q is not key=value", flagName, flag)
		}
		if err := api.ValidateOtherDataKey(key); err != nil {
			return nil, "", errors.Wrapf(err, "--%s %q", flagName, flag)
		}
		if value == "" {
			return nil, "", errors.Newf("--%s %q has no value; - clears it", flagName, flag)
		}
		if edits[key] == nil {
			edits[key] = &otherUnitSelectionEdit{}
		}
		if value == "-" {
			value = ""
		}
		return edits[key], value, nil
	}
	for _, flag := range otherUnitSelectionFlags.where {
		selection, value, err := edit("other-unit-selection-where", flag)
		if err != nil {
			return nil, err
		}
		selection.where = &value
	}
	for _, flag := range otherUnitSelectionFlags.whereResource {
		selection, value, err := edit("other-unit-selection-where-resource", flag)
		if err != nil {
			return nil, err
		}
		selection.whereResource = &value
	}
	for _, flag := range otherUnitSelectionFlags.filter {
		selection, value, err := edit("other-unit-selection-filter", flag)
		if err != nil {
			return nil, err
		}
		if value == "" {
			selection.clearFilter = true
			continue
		}
		filterID, err := resolveFilterID(value)
		if err != nil {
			return nil, errors.Wrapf(err, "--other-unit-selection-filter %q", flag)
		}
		selection.filterID = &filterID
	}
	if len(edits) == 0 {
		return nil, nil
	}
	return edits, nil
}

// applyOtherUnitSelections applies parsed --other-unit-selection-* flags to a trigger's
// selections. A selection left with no field is removed.
func applyOtherUnitSelections(existing map[string]goclientnew.OtherUnitSelection, edits map[string]*otherUnitSelectionEdit) map[string]goclientnew.OtherUnitSelection {
	if edits == nil {
		return existing
	}
	result := maps.Clone(existing)
	if result == nil {
		result = map[string]goclientnew.OtherUnitSelection{}
	}
	for key, edit := range edits {
		selection := result[key]
		if edit.where != nil {
			selection.Where = *edit.where
		}
		if edit.whereResource != nil {
			selection.WhereResource = *edit.whereResource
		}
		if edit.clearFilter {
			selection.FilterID = nil
		} else if edit.filterID != nil {
			filterID := goclientnew.UUID(*edit.filterID)
			selection.FilterID = &filterID
		}
		if selection.Where == "" && selection.WhereResource == "" && selection.FilterID == nil {
			delete(result, key)
			continue
		}
		result[key] = selection
	}
	return result
}

// addOtherUnitSelectionsToPatch adds the --other-unit-selection-* flags to a merge patch of a
// trigger. It changes only the fields it names; a field cleared is removed.
func addOtherUnitSelectionsToPatch(patch map[string]interface{}, edits map[string]*otherUnitSelectionEdit) {
	if edits == nil {
		return
	}
	patchSelections := map[string]interface{}{}
	for key, edit := range edits {
		fields := map[string]interface{}{}
		optional := func(field string, value *string) {
			if value == nil {
				return
			}
			if *value == "" {
				fields[field] = nil
				return
			}
			fields[field] = *value
		}
		optional("Where", edit.where)
		optional("WhereResource", edit.whereResource)
		if edit.clearFilter {
			fields["FilterID"] = nil
		} else if edit.filterID != nil {
			fields["FilterID"] = edit.filterID.String()
		}
		patchSelections[key] = fields
	}
	patch["OtherUnitSelections"] = patchSelections
}

// formatOtherUnitSelections shows a trigger's selections of other units, by key.
func formatOtherUnitSelections(selections map[string]goclientnew.OtherUnitSelection) string {
	entries := make([]string, 0, len(selections))
	for _, key := range slices.Sorted(maps.Keys(selections)) {
		selection := selections[key]
		var parts []string
		if selection.Where != "" {
			parts = append(parts, "Where: "+selection.Where)
		}
		if selection.FilterID != nil {
			parts = append(parts, "Filter: "+selection.FilterID.String())
		}
		if selection.WhereResource != "" {
			parts = append(parts, "WhereResource: "+selection.WhereResource)
		}
		entries = append(entries, key+" ("+strings.Join(parts, "; ")+")")
	}
	return strings.Join(entries, ", ")
}

// functionOtherDataSources and functionOtherUnitFlags hold --other-data-source and --other-unit on
// the commands that invoke functions.
var (
	functionOtherDataSources []string
	functionOtherUnitFlags   []string
)

func addFunctionOtherDataFlags(flags *pflag.FlagSet) {
	flags.StringSliceVar(&functionOtherDataSources, "other-data-source", nil,
		"another revision of each unit to pass the functions, such as LastReleasedRevisionNum, optionally prefixed with Before: "+
			"(can be repeated or comma-separated); defaults to the revisions each function expects. Triggers pass what they name")
	flags.StringArrayVar(&functionOtherUnitFlags, "other-unit", nil,
		"another unit to pass the functions, as key=unit, where the functions read it under key "+
			"(e.g. policy=my-policy for vet-validating-admission-policy) and unit is a slug, space/slug or UUID; can be repeated. Triggers pass what they name")
}

// otherDataSourceParam is the other_data_source parameter --other-data-source and --other-unit
// name, or nil for none. Unqualified unit slugs are looked up in spaceID.
func otherDataSourceParam(spaceID string) (*string, error) {
	sources := slices.Clone(functionOtherDataSources)
	for _, flag := range functionOtherUnitFlags {
		key, ref, found := strings.Cut(flag, "=")
		if !found || ref == "" {
			return nil, errors.Newf("--other-unit %q is not key=unit", flag)
		}
		if err := api.ValidateOtherDataKey(key); err != nil {
			return nil, errors.Wrapf(err, "--other-unit %q", flag)
		}
		unit, err := resolveUnit(ref, spaceID, idSelect("UnitID"))
		if err != nil {
			return nil, errors.Wrapf(err, "--other-unit %q", flag)
		}
		sources = append(sources, api.FormatOtherDataUnit(key, unit.Unit.UnitID))
	}
	if len(sources) == 0 {
		return nil, nil
	}
	param := strings.Join(sources, ",")
	return &param, nil
}
