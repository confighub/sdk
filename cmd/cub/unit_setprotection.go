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
	Short: "Protect paths or resources of a unit from being overwritten by a merge",
	Long: getCommandHelp(`Set the Protected flags stored on a unit's MutationSources.

A protected path is a local override a merge must not overwrite. An unprotected
path holds a value that came from somewhere else -- a clone, an upgrade, a
merge -- and is the merge's to update. Merges consult these stored values by
default, and they are then the only mechanism preserving local overrides; a
WhereMutation filter protects further paths on top of them. Turning the merge's
subtraction step on (via --merge-enable-subtraction or a link's
MergeEnableSubtraction) preserves them by a second mechanism, and the stored
values are not consulted.

Each --protect and --unprotect takes RESOURCE_TYPE:RESOURCE_NAME:PATH, where
RESOURCE_TYPE and RESOURCE_NAME identify the resource (e.g. apps/v1/Deployment
and default/myapp) and PATH is a resolved path within that resource. Repeat
either flag to set several paths or resources at once, and combine them to
protect some paths and re-open others in one revision.

Leave out PATH, as RESOURCE_TYPE:RESOURCE_NAME, to protect or re-open the
resource as a whole. A merge withholds every change to a resource protected as
a whole -- deleting it and replacing it included -- whatever its paths say,
and its paths' own protection takes effect again once it is re-opened. That is
how to protect a resource a change added whole, such as a ServiceAccount a
variant added, since such a resource has no paths of its own to protect. A
resource the unit deleted can be protected as a whole too, which keeps a merge
from adding it back. A path that is not
already present inherits the closest ancestor mutation's provenance. Protecting
a path does not protect a path beneath it that a change of its own wrote, such
as an env entry's value set after the entry was added: name that path too, and
check the result with 'cub unit blame', which marks a protected field P.

--unprotect-all clears every protected path and resource of the unit, so a
future merge may overwrite all of them. Combined with --protect, it clears the rest first, and
the paths named are the only ones protected afterwards.

Setting protection creates a new revision only if it changes.

It prints the unit's MutationSources with -o json, yaml, jq or yq, or after its
confirmation with --verbose.

Examples:
`+"```"+`
  # Protect the downstream replica count from being overwritten by upgrades
  cub unit set-protection my-unit \
    --protect "apps/v1/Deployment:default/myapp:spec.replicas"

  # Re-open a path so a future merge may overwrite it again
  cub unit set-protection my-unit \
    --unprotect "apps/v1/Deployment:default/myapp:spec.replicas"

  # Keep a ServiceAccount the variant added from being replaced by upgrades
  cub unit set-protection my-unit \
    --protect "v1/ServiceAccount:default/myapp"

  # Re-open every protected path of the unit
  cub unit set-protection my-unit --unprotect-all

  # Make the replica count the only protected path
  cub unit set-protection my-unit --unprotect-all \
    --protect "apps/v1/Deployment:default/myapp:spec.replicas"
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: unitSetProtectionCmdRun,
}

var (
	protectSpecs   []string
	unprotectSpecs []string
	unprotectAll   bool
)

func init() {
	unitSetProtectionCmd.Flags().StringArrayVar(&protectSpecs, "protect", nil,
		"path to protect from merges, as RESOURCE_TYPE:RESOURCE_NAME:PATH, or a whole resource as RESOURCE_TYPE:RESOURCE_NAME (repeatable)")
	unitSetProtectionCmd.Flags().StringArrayVar(&unprotectSpecs, "unprotect", nil,
		"path to re-open to merges, as RESOURCE_TYPE:RESOURCE_NAME:PATH, or a whole resource as RESOURCE_TYPE:RESOURCE_NAME (repeatable)")
	unitSetProtectionCmd.Flags().BoolVar(&unprotectAll, "unprotect-all", false,
		"re-open every protected path of the unit to merges, before applying --protect and --unprotect")
	addStandardDisplayFlags(unitSetProtectionCmd)
	enableOptionalSpace(unitSetProtectionCmd)
	unitCmd.AddCommand(unitSetProtectionCmd)
}

// parseProtectionSpec parses a "RESOURCE_TYPE:RESOURCE_NAME:PATH" spec. The path may
// itself contain ':' and '=' (associative segments such as ?name=app;@0), so only the
// first two ':' separate the resource from the path. A spec with no path, or an empty one
// as set-guard spells it, names the resource as a whole, which the API addresses by the
// empty path.
func parseProtectionSpec(flag, spec string) (resourceType, resourceName, path string, err error) {
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", fmt.Errorf("--%s %q must be RESOURCE_TYPE:RESOURCE_NAME:PATH, or RESOURCE_TYPE:RESOURCE_NAME for the whole resource", flag, spec)
	}
	if len(parts) == 2 {
		return parts[0], parts[1], "", nil
	}
	return parts[0], parts[1], parts[2], nil
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

	body := goclientnew.UnitProtectionRequest{UnprotectAll: unprotectAll}
	for _, key := range order {
		resourceType := key.resourceType
		resourceName := key.resourceName
		body.ResourceProtection = append(body.ResourceProtection, goclientnew.ResourceProtection{
			Resource: &goclientnew.ResourceInfo{
				ResourceType: resourceType,
				ResourceName: resourceName,
			},
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
