// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var unitSetProtectionCmd = &cobra.Command{
	Use:   "set-protection <unit-slug> [--unprotect-all] [--protect <spec> ...] [--unprotect <spec> ...]",
	Short: "Keep paths or resources of a unit from being overwritten by a merge (deprecated: use set-guard)",
	Deprecated: "use 'cub unit set-guard --guard RESOURCE_TYPE:RESOURCE_NAME:PATH=Override=Upstream' " +
		"(Override=External for a unit with no upstream unit), and --remove-guard ...=Override to re-open a path",
	Long: getCommandHelp(`Deprecated: use cub unit set-guard with the Override guard.

A local override a merge must not overwrite is an Override guard: Override=Upstream
on a unit with an upstream unit, which its upgrades leave alone, and
Override=External on one without, which an upload of its configuration leaves
alone. This edits that guard. --protect gives a path the unit's Override guard,
--unprotect removes it, and --unprotect-all removes every Override guard the
unit has before the others apply. Any change that is not a merge is cleared for
Override guards unless it says otherwise, so they do not stand in the way of an
edit.

Each --protect and --unprotect takes RESOURCE_TYPE:RESOURCE_NAME:PATH, as
set-guard's flags do. Leave out PATH, as RESOURCE_TYPE:RESOURCE_NAME, for the
resource as a whole. A path that carries another guard of its own is refused,
naming the --clearance set-guard would need.

It creates a new revision only if the guards change; pass --change-desc to
record why on it. Check the result with 'cub unit blame', which marks a guarded
field G.

Examples:
`+"```"+`
  # Keep the downstream replica count from being overwritten by upgrades
  cub unit set-protection my-unit \
    --protect "apps/v1/Deployment:default/myapp:spec.replicas" \
    --change-desc "Prod runs more replicas than the base"

  # The same with set-guard
  cub unit set-guard my-unit \
    --guard "apps/v1/Deployment:default/myapp:spec.replicas=Override=Upstream"

  # Re-open a path so a future merge may overwrite it again
  cub unit set-protection my-unit \
    --unprotect "apps/v1/Deployment:default/myapp:spec.replicas"
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: unitSetProtectionCmdRun,
}

var (
	protectSpecs            []string
	unprotectSpecs          []string
	unprotectAll            bool
	setProtectionChangeDesc string
	setProtectionChangeSet  string
)

func init() {
	unitSetProtectionCmd.Flags().StringArrayVar(&protectSpecs, "protect", nil,
		"path to give the unit's Override guard, as RESOURCE_TYPE:RESOURCE_NAME:PATH, or a whole resource as RESOURCE_TYPE:RESOURCE_NAME; RESOURCE_TYPE may be empty (repeatable)")
	unitSetProtectionCmd.Flags().StringArrayVar(&unprotectSpecs, "unprotect", nil,
		"path to remove the Override guard from, as RESOURCE_TYPE:RESOURCE_NAME:PATH, or a whole resource as RESOURCE_TYPE:RESOURCE_NAME; RESOURCE_TYPE may be empty (repeatable)")
	unitSetProtectionCmd.Flags().BoolVar(&unprotectAll, "unprotect-all", false,
		"remove every Override guard of the unit, before applying --protect and --unprotect")
	unitSetProtectionCmd.Flags().StringVar(&setProtectionChangeSet, "changeset", "", changeSetFlagHelp)
	unitSetProtectionCmd.Flags().StringVar(&setProtectionChangeDesc, "change-desc", "",
		"change description recorded on the revision the edit creates")
	addStandardDisplayFlags(unitSetProtectionCmd)
	enableOptionalSpace(unitSetProtectionCmd)
	unitCmd.AddCommand(unitSetProtectionCmd)
}

// parseProtectionSpec parses a "RESOURCE_TYPE:RESOURCE_NAME:PATH" spec. The path may
// itself contain ':' and '=' (associative segments such as ?name=app;@0), so only the
// first two ':' separate the resource from the path. A spec with no path, or an empty one
// as set-guard spells it, names the resource as a whole, which the API addresses by the
// empty path. An empty type matches any.
func parseProtectionSpec(flag, spec string) (resourceType, resourceName, path string, err error) {
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) < 2 || parts[1] == "" {
		return "", "", "", fmt.Errorf("--%s %q must be RESOURCE_TYPE:RESOURCE_NAME:PATH, or RESOURCE_TYPE:RESOURCE_NAME for the whole resource", flag, spec)
	}
	if len(parts) == 2 {
		return parts[0], parts[1], "", nil
	}
	return parts[0], parts[1], parts[2], nil
}

// specResource is the resource a spec names. A name with no namespace in it names no resource on
// its own: it is the filter for that name in every namespace.
func specResource(resourceType, resourceName string) *goclientnew.ResourceInfo {
	if strings.Contains(resourceName, "/") {
		return &goclientnew.ResourceInfo{ResourceType: resourceType, ResourceName: resourceName}
	}
	return &goclientnew.ResourceInfo{ResourceType: resourceType, ResourceNameWithoutScope: resourceName}
}

func unitSetProtectionCmdRun(_ *cobra.Command, args []string) error {
	if len(protectSpecs) == 0 && len(unprotectSpecs) == 0 && !unprotectAll {
		return fmt.Errorf("at least one --protect, --unprotect or --unprotect-all is required")
	}

	configUnit, err := resolveUnit(args[0], selectedSpaceID, "UnitID,SpaceID")
	if err != nil {
		return err
	}

	// Group specs by resource, preserving the order they were given in.
	type resourceKey struct{ resourceType, resourceName string }
	order := []resourceKey{}
	byResource := map[resourceKey]map[string]bool{}
	add := func(flag string, specs []string, protected bool) error {
		for _, spec := range specs {
			rt, rn, path, perr := parseProtectionSpec(flag, spec)
			if perr != nil {
				return perr
			}
			key := resourceKey{rt, rn}
			if _, ok := byResource[key]; !ok {
				byResource[key] = map[string]bool{}
				order = append(order, key)
			}
			byResource[key][path] = protected
		}
		return nil
	}
	if err := add("protect", protectSpecs, true); err != nil {
		return err
	}
	if err := add("unprotect", unprotectSpecs, false); err != nil {
		return err
	}

	body := goclientnew.UnitProtectionRequest{UnprotectAll: unprotectAll, ChangeDescription: setProtectionChangeDesc}
	if body.ChangeSetID, err = resolveOptionalChangeSetID(setProtectionChangeSet, configUnit.Unit.SpaceID.String()); err != nil {
		return err
	}
	for _, key := range order {
		resourceType := key.resourceType
		resourceName := key.resourceName
		body.ResourceProtection = append(body.ResourceProtection, goclientnew.ResourceProtection{
			Resource:  specResource(resourceType, resourceName),
			Protected: byResource[key],
		})
	}

	res, err := cubClientNew.SetUnitProtectionWithResponse(ctx, configUnit.Unit.SpaceID, configUnit.Unit.UnitID, body)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}

	// The unit's MutationSources, every resource's, are what -o and --verbose ask for; by
	// default the command only says it succeeded.
	var mutationSources *goclientnew.ResourceMutationList
	if res.JSON200 != nil {
		mutationSources = res.JSON200.MutationSources
	}
	if renderPayload(mutationSources) {
		return nil
	}
	if !quiet {
		fmt.Printf("Protection set on unit %s (%s)\n", args[0], configUnit.Unit.UnitID.String())
	}
	if verbose && mutationSources != nil {
		displayJSON(mutationSources)
	}
	return nil
}
