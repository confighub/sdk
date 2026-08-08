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

var unitSetProtectionCmd = &cobra.Command{
	Use:   "set-protection <unit-slug> --protect <spec> [--unprotect <spec> ...]",
	Short: "Protect paths of a unit from being overwritten by a merge",
	Long: getCommandHelp(`Set the Protected flags stored on a unit's MutationSources.

A protected path is a local override a merge must not overwrite. An unprotected
path holds a value that came from somewhere else -- a clone, an upgrade, a
merge -- and is the merge's to update. Merges consult these stored values when
no WhereMutation filter is supplied, which is the default: the stored
protection is then the only mechanism preserving local overrides. Turning the
merge's subtraction step on (via --merge-enable-subtraction or a link's
MergeEnableSubtraction) preserves them by a second mechanism, and the stored
values are not consulted.

Each --protect and --unprotect takes RESOURCE_TYPE:RESOURCE_NAME:PATH, where
RESOURCE_TYPE and RESOURCE_NAME identify the resource (e.g. apps/v1/Deployment
and default/myapp) and PATH is a resolved path within that resource. Repeat
either flag to set several paths or resources at once, and combine them to
protect some paths and re-open others in one revision. A path that is not
already present inherits the closest ancestor mutation's provenance. Setting
protection creates a new revision only if it changes.

Examples:
`+"```"+`
  # Protect the downstream replica count from being overwritten by upgrades
  cub unit set-protection my-unit \
    --protect "apps/v1/Deployment:default/myapp:spec.replicas"

  # Re-open a path so a future merge may overwrite it again
  cub unit set-protection my-unit \
    --unprotect "apps/v1/Deployment:default/myapp:spec.replicas"
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: unitSetProtectionCmdRun,
}

var (
	protectSpecs   []string
	unprotectSpecs []string
)

func init() {
	unitSetProtectionCmd.Flags().StringArrayVar(&protectSpecs, "protect", nil,
		"path to protect from merges, as RESOURCE_TYPE:RESOURCE_NAME:PATH (repeatable)")
	unitSetProtectionCmd.Flags().StringArrayVar(&unprotectSpecs, "unprotect", nil,
		"path to re-open to merges, as RESOURCE_TYPE:RESOURCE_NAME:PATH (repeatable)")
	addStandardDisplayFlags(unitSetProtectionCmd)
	unitCmd.AddCommand(unitSetProtectionCmd)
}

// parseProtectionSpec parses a "RESOURCE_TYPE:RESOURCE_NAME:PATH" spec. The path may
// itself contain ':' and '=' (associative segments such as ?name=app;@0), so only the
// first two ':' separate the resource from the path.
func parseProtectionSpec(flag, spec string) (resourceType, resourceName, path string, err error) {
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", fmt.Errorf("--%s %q must be RESOURCE_TYPE:RESOURCE_NAME:PATH", flag, spec)
	}
	return parts[0], parts[1], parts[2], nil
}

func unitSetProtectionCmdRun(_ *cobra.Command, args []string) error {
	if len(protectSpecs) == 0 && len(unprotectSpecs) == 0 {
		return fmt.Errorf("at least one --protect or --unprotect is required")
	}

	configUnit, err := apiGetUnitFromSlug(args[0], "UnitID,SpaceID")
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

	body := goclientnew.UnitProtectionRequest{}
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

	res, err := cubClientNew.SetUnitProtectionWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID, body)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}

	resp := res.JSON200
	if !quiet {
		fmt.Printf("Protection set on unit %s (%s)\n", args[0], configUnit.UnitID.String())
	}
	if resp != nil && resp.MutationSources != nil {
		displayJSON(resp.MutationSources)
	}
	return nil
}
