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
