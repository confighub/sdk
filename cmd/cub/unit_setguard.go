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

var unitSetGuardCmd = &cobra.Command{
	Use:   "set-guard <unit-slug> --guard <spec> [--inherited-guard <spec> ...] [--remove-guard <spec> ...]",
	Short: "Record the reasons a path's value is what it is",
	Long: getCommandHelp(`Set or remove guards on a unit's path annotations.

A guard names a class of reason a path holds the value it does, so that a later
operation can be asked to know about that reason before overwriting it. The key
names the class and is the matchable part; the value gives the specific reason.
Two examples of the shape:

  Link=TransformPaths     this field is maintained by a link
  RunAsRoot=LegacyImage   this value breaks a default policy because the image needs it

A guard is enforced on every change to the unit's configuration data -- an
edit, a function invocation, a trigger, a link, a merge: a change that is not
cleared for the reasons at a path does not overwrite it, and reports what it
withheld as a conflict. Editing the guards at a path that already carries some
needs --clearance for those, on the same rule -- a clearance is the statement
that the reasons are understood. Setting the first guard on a path needs none,
since there is nothing yet to understand.

A guard set with --guard stays with this unit. One set with --inherited-guard
travels: a clone of the unit receives it, including a variant created with cub
variant create, and a merge carries it, and any later change to it, to the units
downstream. Use it for policy about the configuration that every variant has to
honor, such as a value a mass change must not normalize away. Setting a guard
again with the other flag changes whether it is inherited, which needs
--clearance for it as changing its value does: it decides whether the units
downstream have it.

A guard may name a path -- or a resource -- the unit does not have. A guard is
policy about the configuration rather than a statement about its current
contents: guarding spec.template.spec.containers is as much about containers
added next year as about the ones there now, and each of those inherits it.

Each --guard and --inherited-guard takes RESOURCE_TYPE:RESOURCE_NAME:PATH=KEY=VALUE and each
--remove-guard takes RESOURCE_TYPE:RESOURCE_NAME:PATH=KEY. Leave PATH empty to
guard the resource as a whole. RESOURCE_TYPE may be left empty when the name
alone picks out one of the unit's resources, and it matches in any API version;
a resource the unit does not have yet is named by both. RESOURCE_NAME is the
whole name, or a name the resource had before a rename; a name without a
namespace (myapp) matches that name in any namespace, and has to match only one
resource. PATH names a list element by merge key or by position, and * stands
for every element or key there is; for --remove-guard it also matches the
guarded paths the data no longer has. Repeat either flag to edit several paths or
resources at once, and combine them to add some guards and retire others in one
revision. Setting guards creates a new revision only if they change; pass
--change-desc to record why on it.

A change can state its own reasons instead, without naming where: cub unit
update --guard and --inherited-guard, cub function do --guard and
--inherited-guard, and the Guards field on a link or a trigger take a bare
KEY=VALUE and record it on the paths that change wrote --
the guard analogue of --protect. Use this command to guard a path no change of
yours is writing, and to remove a guard, which no change can do.

It prints the unit's PathAnnotations with -o json, yaml, jq or yq, or after its
confirmation with --verbose.

Examples:
`+"```"+`
  # Say why this image is what it is: a link maintains it
  cub unit set-guard my-unit \
    --guard "apps/v1/Deployment:default/myapp:spec.template.spec.containers.?name=app.image=Link=TransformPaths"

  # Record on a base that its pod spec needs the host network, for every variant of it
  cub unit set-guard my-unit \
    --inherited-guard "apps/v1/Deployment:default/myapp:spec.template.spec.hostNetwork=HostNetwork=PacketMonitor" \
    --change-desc "The packet monitor needs the host network"

  # Say every container's image is maintained by a link, naming the resource by name alone
  cub unit set-guard my-unit \
    --guard ":myapp:spec.template.spec.containers.*.image=Link=TransformPaths"

  # Stop an inherited guard reaching the units downstream, keeping it on this one
  cub unit set-guard my-unit \
    --guard "apps/v1/Deployment:default/myapp:spec.template.spec.hostNetwork=HostNetwork=PacketMonitor" \
    --clearance "HostNetwork=PacketMonitor"

  # Retire a guard whose reason no longer applies, saying the reason is understood
  cub unit set-guard my-unit \
    --remove-guard "apps/v1/Deployment:default/myapp:spec.template.spec.hostNetwork=HostNetwork" \
    --clearance "HostNetwork=PacketMonitor"
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: unitSetGuardCmdRun,
}

var (
	guardSpecs          []string
	inheritedGuardSpecs []string
	removeGuardSpecs    []string
	setGuardClearance   []string
	setGuardChangeDesc  string
	setGuardChangeSet   string
)

func init() {
	unitSetGuardCmd.Flags().StringArrayVar(&guardSpecs, "guard", nil,
		"guard to set on this unit alone, as RESOURCE_TYPE:RESOURCE_NAME:PATH=KEY=VALUE (repeatable)")
	unitSetGuardCmd.Flags().StringArrayVar(&inheritedGuardSpecs, "inherited-guard", nil,
		"inherited guard to set, which clones receive and merges carry downstream, as RESOURCE_TYPE:RESOURCE_NAME:PATH=KEY=VALUE (repeatable)")
	unitSetGuardCmd.Flags().StringArrayVar(&removeGuardSpecs, "remove-guard", nil,
		"guard to remove, as RESOURCE_TYPE:RESOURCE_NAME:PATH=KEY (repeatable)")
	unitSetGuardCmd.Flags().StringArrayVar(&setGuardClearance, "clearance", nil,
		"class of guarded reason this edit is cleared for, as KEY, KEY=VALUE[;VALUE...], KEY!=VALUE[;VALUE...], or !KEY (repeatable). Required to edit a path that already carries guards")
	unitSetGuardCmd.Flags().StringVar(&setGuardChangeSet, "changeset", "", changeSetFlagHelp)
	unitSetGuardCmd.Flags().StringVar(&setGuardChangeDesc, "change-desc", "",
		"change description recorded on the revision the edit creates")
	addStandardDisplayFlags(unitSetGuardCmd)
	enableOptionalSpace(unitSetGuardCmd)
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
	if len(parts) != 3 || parts[1] == "" {
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
	if len(guardSpecs) == 0 && len(inheritedGuardSpecs) == 0 && len(removeGuardSpecs) == 0 {
		return fmt.Errorf("at least one --guard, --inherited-guard or --remove-guard is required")
	}

	configUnit, err := resolveUnit(args[0], selectedSpaceID, "UnitID,SpaceID")
	if err != nil {
		return err
	}

	// Group by resource, preserving the order the flags were given in, so the request reads
	// the way the command line did.
	type resourceKey struct{ resourceType, resourceName string }
	order := []resourceKey{}
	set := map[resourceKey]map[string]map[string]string{}
	setInherited := map[resourceKey]map[string]map[string]string{}
	remove := map[resourceKey]map[string][]string{}
	keyFor := func(rt, rn string) resourceKey {
		key := resourceKey{rt, rn}
		if _, seen := set[key]; !seen {
			set[key] = map[string]map[string]string{}
			setInherited[key] = map[string]map[string]string{}
			remove[key] = map[string][]string{}
			order = append(order, key)
		}
		return key
	}

	for _, flagSpecs := range []struct {
		flag  string
		specs []string
		into  map[resourceKey]map[string]map[string]string
	}{{"guard", guardSpecs, set}, {"inherited-guard", inheritedGuardSpecs, setInherited}} {
		for _, spec := range flagSpecs.specs {
			rt, rn, path, k, v, perr := parseGuardSpec(flagSpecs.flag, spec, true)
			if perr != nil {
				return perr
			}
			key := keyFor(rt, rn)
			if flagSpecs.into[key][path] == nil {
				flagSpecs.into[key][path] = map[string]string{}
			}
			flagSpecs.into[key][path][k] = v
		}
	}
	for _, spec := range removeGuardSpecs {
		rt, rn, path, k, _, perr := parseGuardSpec("remove-guard", spec, false)
		if perr != nil {
			return perr
		}
		key := keyFor(rt, rn)
		remove[key][path] = append(remove[key][path], k)
	}

	body := goclientnew.UnitGuardRequest{ChangeDescription: setGuardChangeDesc}
	if body.ChangeSetID, err = resolveOptionalChangeSetID(setGuardChangeSet, configUnit.Unit.SpaceID.String()); err != nil {
		return err
	}
	if len(setGuardClearance) > 0 {
		clearance, err := parseClearanceSpecs(setGuardClearance)
		if err != nil {
			return err
		}
		body.Clearance = &clearance
	}
	for _, key := range order {
		guards := goclientnew.ResourceGuards{
			Resource: specResource(key.resourceType, key.resourceName),
		}
		if len(set[key]) > 0 {
			guards.Set = set[key]
		}
		if len(setInherited[key]) > 0 {
			guards.SetInherited = setInherited[key]
		}
		if len(remove[key]) > 0 {
			guards.Remove = remove[key]
		}
		body.ResourceGuards = append(body.ResourceGuards, guards)
	}

	res, err := cubClientNew.SetUnitGuardWithResponse(ctx, configUnit.Unit.SpaceID, configUnit.Unit.UnitID, body)
	if cubapi.IsAPIError(err, res) {
		return withClearanceFlags(cubapi.InterpretErrorGeneric(err, res))
	}

	// The unit's PathAnnotations are what -o and --verbose ask for; by default the command
	// only says it succeeded.
	var pathAnnotations *goclientnew.PathAnnotationList
	if res.JSON200 != nil {
		pathAnnotations = res.JSON200.PathAnnotations
	}
	if renderPayload(pathAnnotations) {
		return nil
	}
	if !quiet {
		fmt.Printf("Guards set on unit %s (%s)\n", args[0], configUnit.Unit.UnitID.String())
	}
	if verbose && pathAnnotations != nil {
		displayJSON(pathAnnotations)
	}
	return nil
}
