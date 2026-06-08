// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitSetPredicatesCmd = &cobra.Command{
	Use:   "set-predicates <unit-slug> --predicate <spec> [--predicate <spec> ...]",
	Short: "Set mutation predicates on a unit",
	Long: getCommandHelp(`Set the Predicate flags stored on a unit's MutationSources.

A Predicate records whether a path is eligible to be overwritten by a merge:
true means a merge may patch it, false marks it a protected local override.
Merges consult these stored values when no WhereMutation filter is supplied —
most importantly when the merge's subtraction step is disabled (via
--merge-disable-subtraction or a link's MergeDisableSubtraction), where the
stored Predicate is the only mechanism preserving local overrides.

Each --predicate has the form RESOURCE_TYPE:RESOURCE_NAME:PATH=BOOL, where
RESOURCE_TYPE and RESOURCE_NAME identify the resource (e.g. apps/v1/Deployment
and default/myapp), PATH is a resolved path within that resource, and BOOL is
true or false. Repeat --predicate to set several paths or resources at once.
A path that is not already present inherits the closest ancestor mutation's
provenance. Setting predicates creates a new revision only if they change.

Examples:
`+"```"+`
  # Protect the downstream replica count from being overwritten by upgrades
  cub unit set-predicates my-unit \
    --predicate "apps/v1/Deployment:default/myapp:spec.replicas=false"

  # Re-open a path so a future merge may overwrite it again
  cub unit set-predicates my-unit \
    --predicate "apps/v1/Deployment:default/myapp:spec.replicas=true"
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: unitSetPredicatesCmdRun,
}

var setPredicateSpecs []string

func init() {
	unitSetPredicatesCmd.Flags().StringArrayVar(&setPredicateSpecs, "predicate", nil,
		"predicate to set, as RESOURCE_TYPE:RESOURCE_NAME:PATH=BOOL (repeatable)")
	addStandardDisplayFlags(unitSetPredicatesCmd)
	unitCmd.AddCommand(unitSetPredicatesCmd)
}

// parsePredicateSpec parses a "RESOURCE_TYPE:RESOURCE_NAME:PATH=BOOL" spec.
// The BOOL is taken from the final '=' so that '=' inside an associative path
// segment (e.g. ?name=app;@0) is preserved.
func parsePredicateSpec(spec string) (resourceType, resourceName, path string, value bool, err error) {
	eq := strings.LastIndex(spec, "=")
	if eq < 0 {
		return "", "", "", false, fmt.Errorf("predicate %q must end with =true or =false", spec)
	}
	boolStr := strings.TrimSpace(spec[eq+1:])
	switch boolStr {
	case "true":
		value = true
	case "false":
		value = false
	default:
		return "", "", "", false, fmt.Errorf("predicate %q must end with =true or =false, got %q", spec, boolStr)
	}
	left := spec[:eq]
	parts := strings.SplitN(left, ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false, fmt.Errorf("predicate %q must be RESOURCE_TYPE:RESOURCE_NAME:PATH=BOOL", spec)
	}
	return parts[0], parts[1], parts[2], value, nil
}

func unitSetPredicatesCmdRun(_ *cobra.Command, args []string) error {
	if len(setPredicateSpecs) == 0 {
		return fmt.Errorf("at least one --predicate is required")
	}

	configUnit, err := apiGetUnitFromSlug(args[0], "UnitID,SpaceID")
	if err != nil {
		return err
	}

	// Group predicate specs by resource, preserving order.
	type resourceKey struct{ resourceType, resourceName string }
	order := []resourceKey{}
	byResource := map[resourceKey]map[string]bool{}
	for _, spec := range setPredicateSpecs {
		rt, rn, path, value, perr := parsePredicateSpec(spec)
		if perr != nil {
			return perr
		}
		key := resourceKey{rt, rn}
		if _, ok := byResource[key]; !ok {
			byResource[key] = map[string]bool{}
			order = append(order, key)
		}
		byResource[key][path] = value
	}

	body := goclientnew.UnitPredicatesRequest{}
	for _, key := range order {
		resourceType := key.resourceType
		resourceName := key.resourceName
		body.ResourcePredicates = append(body.ResourcePredicates, goclientnew.ResourcePredicates{
			Resource: &goclientnew.ResourceInfo{
				ResourceType: resourceType,
				ResourceName: resourceName,
			},
			Predicates: byResource[key],
		})
	}

	res, err := cubClientNew.SetUnitPredicatesWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID, body)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}

	resp := res.JSON200
	if !quiet {
		fmt.Printf("Predicates set on unit %s (%s)\n", args[0], configUnit.UnitID.String())
	}
	if resp != nil && resp.MutationSources != nil {
		displayJSON(resp.MutationSources)
	}
	return nil
}
