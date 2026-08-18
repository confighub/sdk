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

var unitSetGuardCmd = &cobra.Command{
	Use:   "set-guard <unit-slug> --guard <spec> [--remove-guard <spec> ...]",
	Short: "Record the reasons a path's value is what it is",
	Long: getCommandHelp(`Set or remove guards on a unit's path annotations.

A guard names a class of reason a path holds the value it does, so that a later
operation can be asked to know about that reason before overwriting it. The key
names the class and is the matchable part; the value distinguishes cases within
it. Two examples of the shape:

  owner=transform-link          this field is maintained by a link
  policy-exception=host-network this value breaks a default policy on purpose

A guard is enforced by merges: a merge that is not cleared for the reasons at a
path does not overwrite it, and reports what it withheld. Editing the guards at a
path that already carries some needs --clearance for those, on the same rule --
a clearance is the statement that the reasons are understood. Setting the first
guard on a path needs none, since there is nothing yet to understand.

A guard may name a path -- or a resource -- the unit does not have. A guard is
policy about the configuration rather than a statement about its current
contents: guarding spec.template.spec.containers is as much about containers
added next year as about the ones there now, and each of those inherits it.

Each --guard takes RESOURCE_TYPE:RESOURCE_NAME:PATH=KEY=VALUE and each
--remove-guard takes RESOURCE_TYPE:RESOURCE_NAME:PATH=KEY. Leave PATH empty to
guard the resource as a whole. Repeat either flag to edit several paths or
resources at once, and combine them to add some guards and retire others in one
revision. Setting guards creates a new revision only if they change.

Examples:
`+"```"+`
  # Say why this image is what it is: a link maintains it
  cub unit set-guard my-unit \
    --guard "apps/v1/Deployment:default/myapp:spec.template.spec.containers.?name=app.image=owner=transform-link"

  # Record a deliberate policy exception on the pod spec
  cub unit set-guard my-unit \
    --guard "apps/v1/Deployment:default/myapp:spec.template.spec=policy-exception=host-network"

  # Retire a guard whose reason no longer applies, saying the reason is understood
  cub unit set-guard my-unit \
    --remove-guard "apps/v1/Deployment:default/myapp:spec.template.spec=policy-exception" \
    --clearance "policy-exception=host-network"
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: unitSetGuardCmdRun,
}

var (
	guardSpecs        []string
	removeGuardSpecs  []string
	setGuardClearance []string
)

func init() {
	unitSetGuardCmd.Flags().StringArrayVar(&guardSpecs, "guard", nil,
		"guard to set, as RESOURCE_TYPE:RESOURCE_NAME:PATH=KEY=VALUE (repeatable)")
	unitSetGuardCmd.Flags().StringArrayVar(&removeGuardSpecs, "remove-guard", nil,
		"guard to remove, as RESOURCE_TYPE:RESOURCE_NAME:PATH=KEY (repeatable)")
	unitSetGuardCmd.Flags().StringArrayVar(&setGuardClearance, "clearance", nil,
		"class of guarded reason this edit is cleared for, as KEY, KEY=VALUE[,VALUE...], KEY!=VALUE[,VALUE...], or !KEY (repeatable). Required to edit a path that already carries guards")
	addStandardDisplayFlags(unitSetGuardCmd)
	unitCmd.AddCommand(unitSetGuardCmd)
}

// parseGuardSpec splits RESOURCE_TYPE:RESOURCE_NAME:PATH[=KEY[=VALUE]].
//
// The resource is separated from the rest by the first two ':', as the protection specs are,
// because a path may itself contain ':'. The key and value are then taken from the *last* '='
// pairs, because a path may contain '=' too -- an associative segment is ?name=app -- while a
// guard key and value may not: they take the AttributeName character class, which has no '='.
// So splitting from the right is unambiguous where splitting from the left is not.
func parseGuardSpec(flag, spec string, wantValue bool) (resourceType, resourceName, path, key, value string, err error) {
	malformed := func() (string, string, string, string, string, error) {
		form := "RESOURCE_TYPE:RESOURCE_NAME:PATH=KEY"
		if wantValue {
			form += "=VALUE"
		}
		return "", "", "", "", "", fmt.Errorf("--%s %q must be %s", flag, spec, form)
	}

	parts := strings.SplitN(spec, ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return malformed()
	}
	resourceType, resourceName, rest := parts[0], parts[1], parts[2]

	if wantValue {
		keyAndPath, v, found := cutLast(rest, "=")
		if !found {
			return malformed()
		}
		p, k, found := cutLast(keyAndPath, "=")
		if !found {
			return malformed()
		}
		if k == "" {
			return malformed()
		}
		return resourceType, resourceName, p, k, v, nil
	}

	p, k, found := cutLast(rest, "=")
	if !found || k == "" {
		return malformed()
	}
	return resourceType, resourceName, p, k, "", nil
}

// cutLast is strings.Cut around the last occurrence of sep rather than the first.
func cutLast(s, sep string) (before, after string, found bool) {
	index := strings.LastIndex(s, sep)
	if index < 0 {
		return s, "", false
	}
	return s[:index], s[index+len(sep):], true
}

func unitSetGuardCmdRun(_ *cobra.Command, args []string) error {
	if len(guardSpecs) == 0 && len(removeGuardSpecs) == 0 {
		return fmt.Errorf("at least one --guard or --remove-guard is required")
	}

	configUnit, err := apiGetUnitFromSlug(args[0], "UnitID,SpaceID")
	if err != nil {
		return err
	}

	// Group by resource, preserving the order the flags were given in, so the request reads
	// the way the command line did.
	type resourceKey struct{ resourceType, resourceName string }
	order := []resourceKey{}
	set := map[resourceKey]map[string]map[string]string{}
	remove := map[resourceKey]map[string][]string{}
	keyFor := func(rt, rn string) resourceKey {
		key := resourceKey{rt, rn}
		if _, seen := set[key]; !seen {
			set[key] = map[string]map[string]string{}
			remove[key] = map[string][]string{}
			order = append(order, key)
		}
		return key
	}

	for _, spec := range guardSpecs {
		rt, rn, path, k, v, perr := parseGuardSpec("guard", spec, true)
		if perr != nil {
			return perr
		}
		key := keyFor(rt, rn)
		if set[key][path] == nil {
			set[key][path] = map[string]string{}
		}
		set[key][path][k] = v
	}
	for _, spec := range removeGuardSpecs {
		rt, rn, path, k, _, perr := parseGuardSpec("remove-guard", spec, false)
		if perr != nil {
			return perr
		}
		key := keyFor(rt, rn)
		remove[key][path] = append(remove[key][path], k)
	}

	body := goclientnew.UnitGuardRequest{}
	if len(setGuardClearance) > 0 {
		clearance, err := parseClearanceSpecs(setGuardClearance)
		if err != nil {
			return err
		}
		body.Clearance = &clearance
	}
	for _, key := range order {
		guards := goclientnew.ResourceGuards{
			Resource: &goclientnew.ResourceInfo{
				ResourceType: key.resourceType,
				ResourceName: key.resourceName,
			},
		}
		if len(set[key]) > 0 {
			guards.Set = set[key]
		}
		if len(remove[key]) > 0 {
			guards.Remove = remove[key]
		}
		body.ResourceGuards = append(body.ResourceGuards, guards)
	}

	res, err := cubClientNew.SetUnitGuardWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID, body)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}

	resp := res.JSON200
	if !quiet {
		fmt.Printf("Guards set on unit %s (%s)\n", args[0], configUnit.UnitID.String())
	}
	if resp != nil && resp.PathAnnotations != nil {
		displayJSON(resp.PathAnnotations)
	}
	return nil
}
