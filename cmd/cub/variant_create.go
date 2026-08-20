// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var variantCreateArgs struct {
	target           string
	spacePattern     string
	stage            string
	environment      string
	region           string
	namespace        string
	variantLabels    []string
	spaceAnnotations []string
	unitAnnotations  []string
	spaceDeleteGates []string
	unitDeleteGates  []string
	unitDestroyGates []string
	noArgoApp        bool
	syncback         bool

	includeOutgoingLinksWhere string
}

var variantCreateCmd = &cobra.Command{
	Use:   "create <variant-name> <upstream-space>",
	Short: "Create a variant by cloning a space and its units",
	Args:  cobra.ExactArgs(2),
	Long: getCommandHelp(`Create a variant of an upstream space by cloning the space and all of its units into a new
downstream space.

This is a convenience command that combines two bulk operations:
  1. Clone the upstream space (like "cub space create" in bulk mode), setting the new space's
     "Variant" label to <variant-name>.
  2. Clone every unit from the upstream space into the new space (like "cub unit create" in bulk mode),
     linking each clone to its upstream unit.

The first argument is the variant name, which becomes the value of the "Variant" label on the new
space. The second argument is the slug (or UUID) of the upstream space to clone from. The upstream
space is expected to have labels such as Component, Layer, Owner, Stage, Environment, Region, and
Variant, and may have a "TargetID" annotation referencing the default target for the space, but
none of these are required.

The new space's labels are inherited from the upstream space, with "Variant" overridden to
<variant-name>. Use --stage, --environment, and --region to add or change the "Stage",
"Environment", and "Region" labels, since some values (like Region) commonly differ between
variants. The other well-known labels (Component, Layer, Owner) are inherited and can be overridden
with --variant-labels.

The new space's slug defaults to <component>-<variant>, derived from the cloned space's Component
and Variant labels — the same convention as "cub variant upload" and "cub helm install". When the
cloned space would have no Component label, the server instead derives the slug from the upstream
space's slug and the variant name. Use --space-pattern to override: a Go template evaluated over
the cloned space's labels (and .SourceEntitySlug for the upstream slug), for example
"template:{{.Labels.Component}}-{{.Labels.Variant}}".

The following are copied from the upstream space to the new space: WhereTrigger, TriggerFilterID,
Permissions, and DeleteGates.

Metadata flags are split by what they target, space vs. unit (mirroring "install upload"):
  --space-annotation / --space-delete-gate   set on the new space, merged onto the values copied
                                             from the upstream space.
  --unit-annotation / --unit-delete-gate /   set on every cloned unit, merged onto each clone's
  --unit-destroy-gate                        copied values. Destroy gates are unit-only (spaces have
                                             no destroy gates). Use --unit-delete-gate critical to
                                             protect a prod variant's units.
  --wait                                     wait for the cloned units' triggers to finish (default
                                             true); pass --wait=false to return as soon as the clone
                                             is queued.

To automatically customize the cloned units, create PostClone triggers and select them via the
upstream space's WhereTrigger or TriggerFilterID so that they are copied to the downstream space and
run during the clone. Trigger arguments can reference space metadata in Go templates, such as
"template:{{.SpaceLabels.Region}}" or "template:{{.SpaceAnnotations.host}}" — set the latter with
--space-annotation. Any other changes can be made after the clone completes.

Links between units of the cloned space are copied into the new space and retargeted at the clones.
Links pointing out of it are not, unless --include-outgoing-links-where selects them: a copied
outgoing link keeps the original unit as its to-unit, so the variant reads from the same producer
the upstream reads from.

A variant is normally downstream of its upstream space: changes flow from the upstream units into
the clones, through the UpgradeUnit links this command creates ("cub variant promote"). --syncback
additionally creates a MergeUnits link the other way, from each upstream unit back to its clone,
which makes the variant usable as a draft: clone the space, change the clones, review them, then
merge the changes back into the upstream units with

  cub unit update --patch --space <upstream-space> --where "..." \
      --resolve "Link:ToSpaceID = '<variant-space-id>'"

The syncback links live in the upstream space and are named for the clone they take changes from
("syncback-<variant-space>-<unit>"), so an upstream space can have several drafts open at once.

When --target points at a cub-cluster Argo target (an OCI target carrying the
confighub.com/argo-apps-space annotation that "cub cluster up" stamps), this command also creates the
Argo CD Application that makes the new deployment live: a Kubernetes/YAML unit named after the new
space, added to the cluster's apps space and pulling the new space's OCI Release. The apps space's
Release is republished so the cluster's root app-of-apps picks it up on its next sync. This is a
no-op for ordinary targets; use --no-argo-app to skip it. Publish the deployment's own release
("cub release publish <new-space>") to make its configuration go live.

Examples:
`+"```"+`
  # Clone a space into a "test" variant. With Component=website inherited and Variant overridden to
  # "test", the default slug is "website-test".
  cub variant create test website-prod

  # Clone into a regional staging variant, overriding the Environment and Region labels.
  cub variant create staging website-prod --environment Staging --region us-east2

  # Clone into a canary variant of production, overriding the Stage label.
  cub variant create canary website-prod --stage Canary --environment Prod

  # Point the cloned units at a target and stamp the new space's TargetID annotation.
  # For an OCI target, this also sets the new space's release target, so the variant
  # can be published with "cub release publish" without further setup.
  cub variant create test website-prod --target website-test/cluster

  # Deploy to a "cub cluster up" cluster: clones the base, retargets it, and creates
  # the Argo CD Application so the cluster picks up the deployment (skip with --no-argo-app).
  cub variant create dev cubbychat-base --target dev/target --namespace cubbychat

  # Clone a space as a draft to change and review, with links to merge the changes back.
  cub variant create draft website-prod --syncback

  # Carry the units' links to producers in other spaces into the variant, so its units read
  # from the same ones.
  cub variant create prod website-base --include-outgoing-links-where "UpdateType = 'TransformPaths'"

  # Set a space annotation a PostClone trigger reads, and protect the prod clones with a delete gate.
  cub variant create prod website-base \
    --space-annotation host=website.prod.example.com \
    --unit-delete-gate critical --unit-destroy-gate critical
`+"```"+`
`, ""),
	RunE: variantCreateCmdRun,
}

func init() {
	addStandardDisplayFlags(variantCreateCmd)
	enableAllowExistsFlag(variantCreateCmd)
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.spacePattern, "space-pattern", "", "a pattern string for the new space's slug, prefix 'template:' to use a Go template with .SourceEntitySlug for the upstream slug and .Labels for the cloned space's labels; defaults to 'template:{{.Labels.Component}}-{{.Labels.Variant}}' when the cloned space has a Component label")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.target, "target", "", "target for the cloned units, in <target-slug> or <space-slug>/<target-slug> form; also sets the TargetID annotation on the new space, and for an OCI target the new space's ReleaseTargetID (required by 'cub release publish')")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.stage, "stage", "", "set the \"Stage\" label on the new space (example: \"Canary\")")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.environment, "environment", "", "set the \"Environment\" label on the new space (example: \"Prod\")")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.region, "region", "", "set the \"Region\" label on the new space (example: \"us-east2\")")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.namespace, "namespace", "", "run set-namespace with this value on the cloned Kubernetes/YAML units, replacing the placeholder namespace from the upstream (e.g. a base uploaded with --namespace confighubplaceholder)")
	variantCreateCmd.Flags().StringSliceVar(&variantCreateArgs.variantLabels, "variant-labels", []string{}, "additional variant labels for the new space in the format key1=value1,key2=value2 (the Variant label is always set from <variant-name>)")
	variantCreateCmd.Flags().StringSliceVar(&variantCreateArgs.spaceAnnotations, "space-annotation", []string{}, "annotation key=value to set on the new space (repeatable); merged onto the annotations copied from the upstream space. PostClone trigger args can read these via {{.SpaceAnnotations.<key>}}. \"TargetID\" is reserved (use --target)")
	variantCreateCmd.Flags().StringSliceVar(&variantCreateArgs.unitAnnotations, "unit-annotation", []string{}, "annotation key=value to set on every cloned unit (repeatable); merged onto each unit's copied annotations")
	variantCreateCmd.Flags().StringSliceVar(&variantCreateArgs.spaceDeleteGates, "space-delete-gate", []string{}, "delete gate key[=true] to set on the new space (repeatable); merged onto the delete gates copied from the upstream space")
	variantCreateCmd.Flags().StringSliceVar(&variantCreateArgs.unitDeleteGates, "unit-delete-gate", []string{}, "delete gate key[=true] to set on every cloned unit (repeatable); e.g. --unit-delete-gate critical to protect a prod variant")
	variantCreateCmd.Flags().StringSliceVar(&variantCreateArgs.unitDestroyGates, "unit-destroy-gate", []string{}, "destroy gate key[=true] to set on every cloned unit (repeatable); destroy gates are unit-only (spaces have no destroy gates)")
	variantCreateCmd.Flags().BoolVar(&variantCreateArgs.noArgoApp, "no-argo-app", false, "skip auto-creating the Argo CD Application when --target is a cub-cluster Argo target")
	variantCreateCmd.Flags().BoolVar(&variantCreateArgs.syncback, "syncback", false, "also link each cloned unit back to its upstream unit, with a MergeUnits link in the upstream space, so changes made in the variant can be merged back into the upstream units (see the draft workflow in the long help)")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.includeOutgoingLinksWhere, "include-outgoing-links-where", "", "where expression selecting which of the upstream units' links to units in other spaces to copy onto the clones; each copy keeps the original unit as its to-unit, so the variant reads from the same producers. Links within the cloned space are always copied and retargeted at the clones")
	enableWaitFlag(variantCreateCmd)
	variantCmd.AddCommand(variantCreateCmd)
}

func variantCreateCmdRun(cmd *cobra.Command, args []string) error {
	variantName := args[0]
	upstreamSpaceSlug := args[1]

	// Resolve the upstream space.
	upstreamSpace, err := apiGetSpaceFromSlug(upstreamSpaceSlug, "*")
	if err != nil {
		return err
	}
	upstreamSpaceID := upstreamSpace.SpaceID

	// Step 1: clone the upstream space. WhereTrigger, TriggerFilterID, Permissions, and DeleteGates
	// are copied from the upstream space by the clone (we pass an empty patch so nothing is overridden).
	newSpace, err := cloneVariantSpace(variantName, upstreamSpace)
	if err != nil {
		return err
	}
	if !jsonOutput {
		tprint("Created variant space %s (ID: %s)", newSpace.Slug, newSpace.SpaceID)
	}

	// Step 2: resolve the target (if specified) and stamp the new space's TargetID annotation.
	// Resolved after the space exists so that <new-space-slug>/<target-slug> can be used.
	// For an OCI target, also set the new space's ReleaseTargetID: releases are published
	// per space ("cub release publish <space>"), and publish requires it.
	var targetID *uuid.UUID
	var target *goclientnew.Target
	if variantCreateArgs.target != "" {
		target, err = parseEntityIdentifierSingleAsEntity[goclientnew.Target](
			variantCreateArgs.target,
			EntityTypeTarget,
			"*",
			apiGetTargetFromSlugInSpaceCore,
			func(t *goclientnew.Target) string { return t.TargetID.String() },
		)
		if err != nil {
			return err
		}
		targetID = &target.TargetID

		isOCI := target.ProviderType == string(api.ProviderOCI)
		if err := patchVariantSpaceTarget(newSpace.SpaceID, target.TargetID, isOCI); err != nil {
			return err
		}
	}

	// Step 3: clone the upstream units into the new space, optionally retargeting them.
	responses, statusCode, err := cloneVariantUnits(upstreamSpaceID, newSpace.SpaceID, targetID)
	if err != nil {
		return err
	}

	// Step 4: if requested, set the namespace on the cloned Kubernetes/YAML units.
	// The base is typically uploaded with the confighubplaceholder namespace;
	// set-namespace renames the v1/Namespace resource and stamps metadata.namespace
	// on every namespaced resource, so each variant lands in its own namespace.
	if variantCreateArgs.namespace != "" {
		if err := runCub("function", "do", "--quiet", "--space", newSpace.Slug, "set-namespace", variantCreateArgs.namespace); err != nil {
			return err
		}
		if !jsonOutput {
			tprint("Set namespace %q on the cloned units in %s", variantCreateArgs.namespace, newSpace.Slug)
		}
	}

	// Step 5: if the target is a cub-cluster Argo target (carries the
	// confighub.com/argo-apps-space annotation), auto-create the Argo CD
	// Application Unit for this deployment and republish the cluster's apps
	// Space so Argo becomes aware of it — folding the manual "tell the cluster
	// about the deployment" step into this command. A no-op for ordinary
	// targets; skip explicitly with --no-argo-app.
	if target != nil && !variantCreateArgs.noArgoApp {
		if _, err := createVariantArgoApp(cmd.OutOrStdout(), target, newSpace, *targetID); err != nil {
			return err
		}
	}

	return handleBulkCreateOrUpdateResponse(responses, statusCode, "create", "")
}

// componentVariantPattern is the default slug pattern for a variant space,
// matching the convention used by "cub variant upload" and "cub helm install".
const componentVariantPattern = "template:{{.Labels.Component}}-{{.Labels.Variant}}"

// effectiveComponent returns the Component label the cloned space will have: a
// Component override from --variant-labels (applied last, so it wins) or the
// inherited upstream label.
func effectiveComponent(upstreamSpace *goclientnew.Space) string {
	component := upstreamSpace.Labels["Component"]
	for _, kv := range variantCreateArgs.variantLabels {
		if k, v, ok := strings.Cut(kv, "="); ok && k == "Component" {
			component = v
		}
	}
	return component
}

// cloneVariantSpace clones the upstream space, setting the new space's Variant label to variantName
// (plus any extra --variant-labels) and applying the --space-pattern, defaulted to
// <component>-<variant> when the cloned space has a Component label.
func cloneVariantSpace(variantName string, upstreamSpace *goclientnew.Space) (*goclientnew.Space, error) {
	upstreamSpaceID := upstreamSpace.SpaceID
	// The Variant label is always set from the variant name. --stage, --environment, and --region
	// set the well-known Stage, Environment, and Region labels. Any --variant-labels are applied
	// last so they win.
	variantLabels := []string{"Variant=" + variantName}
	if variantCreateArgs.stage != "" {
		variantLabels = append(variantLabels, "Stage="+variantCreateArgs.stage)
	}
	if variantCreateArgs.environment != "" {
		variantLabels = append(variantLabels, "Environment="+variantCreateArgs.environment)
	}
	if variantCreateArgs.region != "" {
		variantLabels = append(variantLabels, "Region="+variantCreateArgs.region)
	}
	variantLabels = append(variantLabels, variantCreateArgs.variantLabels...)
	variantLabelsStr := strings.Join(variantLabels, ",")

	whereClause := fmt.Sprintf("SpaceID = '%s'", upstreamSpaceID.String())
	include := "OrganizationID"
	params := &goclientnew.BulkCreateSpacesParams{
		Where:         &whereClause,
		Include:       &include,
		VariantLabels: &variantLabelsStr,
	}
	spacePattern := variantCreateArgs.spacePattern
	if spacePattern == "" && effectiveComponent(upstreamSpace) != "" {
		spacePattern = componentVariantPattern
	}
	if spacePattern != "" {
		params.NamePattern = &spacePattern
	}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	// Build the space patch. An empty patch ("null") copies all upstream
	// fields unchanged; --space-annotation / --space-delete-gate add a merge
	// patch (via the shared EnhancePatchData) so the given keys are layered
	// onto the copied annotations/delete-gates (RFC 7386 merge — upstream
	// keys survive). TargetID is reserved as a space annotation; --target
	// sets it post-clone.
	for _, a := range variantCreateArgs.spaceAnnotations {
		switch strings.SplitN(a, "=", 2)[0] {
		case "TargetID":
			return nil, fmt.Errorf("--space-annotation TargetID is reserved; use --target")
		case AnnotationUpstreamSpaceID:
			return nil, fmt.Errorf("--space-annotation %s is reserved", AnnotationUpstreamSpaceID)
		}
	}
	// Stamp the upstream space's ID so "cub variant promote" can find the
	// upstream space to promote from.
	annotations := append([]string{}, variantCreateArgs.spaceAnnotations...)
	annotations = append(annotations, AnnotationUpstreamSpaceID+"="+upstreamSpaceID.String())
	patchJSON, err := EnhancePatchData([]byte("null"),
		annotations, nil, variantCreateArgs.spaceDeleteGates, nil, nil)
	if err != nil {
		return nil, err
	}
	responses, _, err := bulkCreateSpaces(params, patchJSON)
	if err != nil {
		return nil, err
	}

	var newSpace *goclientnew.Space
	for i := range responses {
		r := &responses[i]
		if r.Error != nil {
			return nil, fmt.Errorf("failed to clone space: %s", r.Error.Message)
		}
		if r.Space != nil {
			newSpace = r.Space
		}
	}
	if newSpace == nil {
		return nil, fmt.Errorf("space clone returned no space")
	}
	return newSpace, nil
}

// patchVariantSpaceTarget sets the "TargetID" annotation on the new space to the resolved
// target UUID, mirroring the convention used by upstream spaces. For an OCI target it also
// sets the space's ReleaseTargetID, which "cub release publish" requires.
func patchVariantSpaceTarget(spaceID uuid.UUID, targetID uuid.UUID, releaseTarget bool) error {
	patchMap := map[string]interface{}{}
	if releaseTarget {
		patchMap["ReleaseTargetID"] = targetID.String()
	}
	patchData, err := json.Marshal(patchMap)
	if err != nil {
		return err
	}
	if _, err := patchSpace(spaceID, patchData); err != nil {
		return fmt.Errorf("failed to set target on variant space: %w", err)
	}
	return nil
}

// cloneVariantUnits clones all units from the upstream space into the new space, optionally
// setting their TargetID to the resolved target.
func cloneVariantUnits(upstreamSpaceID, newSpaceID uuid.UUID, targetID *uuid.UUID) (*[]goclientnew.UnitCreateOrUpdateResponse, int, error) {
	whereClause := fmt.Sprintf("SpaceID = '%s'", upstreamSpaceID.String())
	whereSpace := fmt.Sprintf("SpaceID = '%s'", newSpaceID.String())
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	params := &goclientnew.BulkCreateUnitsParams{
		Where:      &whereClause,
		WhereSpace: &whereSpace,
		Include:    &include,
	}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}
	if variantCreateArgs.syncback {
		params.Syncback = &variantCreateArgs.syncback
	}
	if variantCreateArgs.includeOutgoingLinksWhere != "" {
		params.IncludeOutgoingLinksWhere = &variantCreateArgs.includeOutgoingLinksWhere
	}

	// Build the unit merge patch via the shared EnhancePatchData, which
	// layers --unit-annotation and --unit-delete-gate onto each clone's
	// copied fields. The retarget (TargetID) and --unit-destroy-gate are
	// unit-specific fields EnhancePatchData doesn't model, so they go through
	// the enhancer callback. The enhancer is nil when neither applies, so a
	// flagless clone keeps the prior "null" no-op patch.
	destroyGates := map[string]bool{}
	if err := setGatesFromSlice(variantCreateArgs.unitDestroyGates, &destroyGates); err != nil {
		return nil, 0, fmt.Errorf("invalid --unit-destroy-gate: %w", err)
	}
	var enhancer PatchEnhancer
	if targetID != nil || len(destroyGates) > 0 {
		enhancer = func(m map[string]interface{}) {
			if targetID != nil {
				m["TargetID"] = targetID.String()
			}
			if len(destroyGates) > 0 {
				dg := make(map[string]interface{}, len(destroyGates))
				for k, v := range destroyGates {
					dg[k] = v
				}
				m["DestroyGates"] = dg
			}
		}
	}
	patchJSON, err := EnhancePatchData([]byte("null"),
		variantCreateArgs.unitAnnotations, nil, variantCreateArgs.unitDeleteGates, nil, enhancer)
	if err != nil {
		return nil, 0, err
	}

	return bulkCreateUnits(params, patchJSON)
}
