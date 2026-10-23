// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var variantCreateArgs struct {
	target           string
	releaseTarget    string
	permissions      []string
	spacePattern     string
	stage            string
	environment      string
	region           string
	layer            string
	owner            string
	namespace        string
	variantLabels    []string
	spaceLabels      []string
	spaceAnnotations []string
	unitLabels       []string
	unitAnnotations  []string
	changeDesc       string
	changesetSlug    string
	spaceDeleteGates []string
	unitDeleteGates  []string
	unitDestroyGates []string
	noArgoApp        bool
	prune            bool
	syncback         bool

	includeOutgoingLinksWhere string
}

var variantCreateCmd = &cobra.Command{
	Use:   "create <variant-name> <upstream-variant>",
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
space is expected to have labels such as Layer, Owner, Stage, Environment, Region, and Variant,
and may have a "TargetID" annotation referencing the default target for the space, but none of
these are required.

The new space's labels are inherited from the upstream space, with "Variant" overridden to
<variant-name>. Use --stage, --environment, --region, and --layer to add or change the well-known
"Stage", "Environment", "Region", and "Layer" labels, since some values (like Region) commonly
differ between variants, and --space-label to add or change any other label. The new space is in
the upstream space's Component.

--owner sets the Owner label of that Component, not of the new space; an Owner label the upstream
space has is still inherited like its other labels. A Component's owner is its own Owner label, or
else the Owner label all of its spaces share. When the Component already has a different owner,
--owner is refused before anything is created, and the error gives the
"cub component update --patch <component> --label Owner=<owner>" command that changes it. When it
has the same owner, the Component's label is written only if it is missing. --owner needs an
upstream space that is in a Component.

The new space's slug defaults to <component>-<variant>, derived from the slug of the cloned space's
Component and its Variant label — the same convention as "cub variant upload" and "cub helm install".
When the cloned space would have no Component, the server instead derives the slug from the upstream
space's slug and the variant name. Use --space-pattern to override: a Go template evaluated over
the cloned space's Component and labels (and .SourceEntitySlug for the upstream slug), for example
"template:{{.Component.Slug}}-{{.Labels.Variant}}".

The new space selects the Triggers the upstream space selects: its WhereTrigger and TriggerFilterID
are copied, and when it has neither, which means its own Triggers, the new space's WhereTrigger
selects the Triggers in the upstream space. Permissions are copied too. Triggers are not copied:
they run where they are, so every variant runs the same ones.

Metadata flags are split by what they target, space vs. unit, as on "cub variant upload":
  --space-label / --space-annotation        set on the new space, merged onto the values copied
                                             from the upstream space.
  --space-delete-gate                        the new space's delete gates. The upstream space's
                                             are not copied.
  --unit-label / --unit-annotation           set on every cloned unit, merged onto each clone's
                                             copied values.
  --unit-delete-gate / --unit-destroy-gate   every cloned unit's gates. The upstream units' are
                                             not copied. Destroy gates are unit-only (spaces have
                                             no destroy gates). Use --unit-delete-gate critical to
                                             protect a prod variant's units.
  --permission                               add to, or with a leading "-" remove from, the
                                             permissions copied from the upstream space.
  --release-target                           the new space's release Target, which its units take;
                                             --target sets it too, so use one or the other.
  --change-desc / --changeset                describe the clones' first revision and put the clones
                                             in a changeset (<space>/<slug> names one in another
                                             space).
  --wait                                     wait for the cloned units' triggers to finish (default
                                             true); pass --wait=false to return as soon as the clone
                                             is queued.

To automatically customize the cloned units, create PostClone triggers in the upstream space, or
select them with its WhereTrigger or TriggerFilterID. The downstream space gets the same selection,
so they run during the clone. Trigger arguments can reference space metadata in Go templates, such as
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
Because they link to the draft's units, discarding a draft takes --detach, which deletes them:

  cub space delete --recursive --detach <variant-space>

When --target points at a cub-cluster target (an OCI target carrying the confighub.com/apps-space
annotation that "cub cluster up" stamps) whose confighub.com/cluster-controller is argo, this command also creates the
Argo CD Application that makes the new deployment live: a Kubernetes/YAML unit named after the new
space, added to the cluster's apps space and pulling the new space's OCI Release. The apps space's
Release is republished so the cluster's root app-of-apps picks it up on its next sync. For a
cluster from "cub cluster up --controller sveltos" (whose target's
confighub.com/cluster-controller is sveltos), it adds a Sveltos ClusterProfile unit instead, but does not
republish the apps space: a ClusterProfile whose Release does not exist yet fails, and Sveltos then
backs it off. The new space's first "cub release publish" republishes the apps space with it, and
the cluster's root ClusterProfile applies it. For a cluster from "cub cluster up --controller flux"
(whose target's confighub.com/cluster-controller is flux), it adds a unit holding a Flux OCIRepository and
Kustomization. With argobot on the cluster, the apps space is republished at once, as for Argo CD;
with --no-k8sbot, the new space's first "cub release publish" republishes it, as for Sveltos, since
Flux backs off an OCIRepository whose Release does not exist yet. This is a no-op for ordinary
targets; use --no-argo-app to skip it. Publish the deployment's own release
("cub release publish <new-space>") to make its configuration go live.

The Argo CD Application never deletes anything unless it has prune: a resource whose unit is
emptied stays running, and deleting the Application leaves what it deployed running. --prune gives
it prune and Argo's resources finalizer, so an emptied unit's resource is deleted by the next
release, and so is everything the deployment deployed when its Application is deleted. A cluster
from "cub cluster up --prune" gives every deployment both without the flag. An empty release
then deletes the whole deployment, so publish with care. On a Flux cluster the same holds for the
Kustomization: it prunes what it applied only with --prune, or on a cluster from "cub cluster up
--controller flux --prune". On a Sveltos cluster the ClusterProfile withdraws what it deployed
when it is deleted only with --prune, or on a cluster from "cub cluster up --controller sveltos
--prune"; without, it has stopMatchingBehavior LeavePolicies. Sveltos always deletes a resource
whose unit is emptied, with or without --prune.

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
  cub variant create test website-prod --target infra/test-cluster

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
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.spacePattern, "space-pattern", "", "a pattern string for the new space's slug, prefix 'template:' to use a Go template with .SourceEntitySlug for the upstream slug, .Component for the cloned space's Component, and .Labels for the cloned space's labels; defaults to 'template:{{.Component.Slug}}-{{.Labels.Variant}}' when the cloned space has a Component")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.target, "target", "", "target for the cloned units, in <target-slug> or <space-slug>/<target-slug> form; also sets the TargetID annotation on the new space, and for an OCI target the new space's ReleaseTargetID (required by 'cub release publish')")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.releaseTarget, "release-target", "", "Target to use as the new space's release Target, addressed as <target-space>/<target-slug> (a bare <target-slug> resolves in --space; a Target ID is also accepted). The cloned units that are released through a Target take it as theirs. --target sets the same Target on the space and the units and also records it in the space's TargetID annotation, so the two flags cannot be combined")
	variantCreateCmd.MarkFlagsMutuallyExclusive("target", "release-target")
	variantCreateCmd.Flags().StringSliceVar(&variantCreateArgs.permissions, "permission", []string{}, "permission on the new space in format Action:UserIDOrUsername, Action:group:GroupSlugOrID or Action:serviceaccount:ServiceAccountSlugOrID to add to those copied from the upstream space, or the same prefixed with - to remove one of them (e.g., Manage:user@example.com, View:group:platform, View:serviceaccount:deploy-bot, -View:user@example.com, can be repeated)")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.stage, "stage", "", "set the \"Stage\" label on the new space (example: \"Canary\")")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.environment, "environment", "", "set the \"Environment\" label on the new space (example: \"Prod\")")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.region, "region", "", "set the \"Region\" label on the new space (example: \"us-east2\")")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.layer, "layer", "", "set the \"Layer\" label on the new space (example: \"App\")")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.owner, "owner", "", "set the \"Owner\" label on the Component the new space is in, not on the space; refused when the Component already has a different owner (example: \"Engineering\")")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.namespace, "namespace", "", "run set-namespace with this value on the cloned Kubernetes/YAML units, replacing the placeholder namespace from the upstream (e.g. a base uploaded with --namespace confighubplaceholder), and record it as the new space's Namespace label, which cub variant promote applies to units it adds later")
	variantCreateCmd.Flags().StringSliceVar(&variantCreateArgs.variantLabels, "variant-labels", []string{}, "additional variant labels for the new space in the format key1=value1,key2=value2 (the Variant label is always set from <variant-name>)")
	variantCreateCmd.Flags().StringSliceVar(&variantCreateArgs.spaceLabels, "space-label", []string{}, "label key=value to set on the new space (repeatable); merged onto the labels copied from the upstream space. It may not name a label another flag sets, such as Region")
	variantCreateCmd.Flags().StringSliceVar(&variantCreateArgs.spaceAnnotations, "space-annotation", []string{}, "annotation key=value to set on the new space (repeatable); merged onto the annotations copied from the upstream space. PostClone trigger args can read these via {{.SpaceAnnotations.<key>}}. \"TargetID\" is reserved (use --target)")
	variantCreateCmd.Flags().StringSliceVar(&variantCreateArgs.unitAnnotations, "unit-annotation", []string{}, "annotation key=value to set on every cloned unit (repeatable); merged onto each unit's copied annotations")
	variantCreateCmd.Flags().StringSliceVar(&variantCreateArgs.unitLabels, "unit-label", []string{}, "label key=value to set on every cloned unit (repeatable); merged onto each unit's copied labels")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.changeDesc, "change-desc", "", "change description recorded on each cloned unit")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.changesetSlug, "changeset", "", "changeset to put the cloned units in, as <slug> or <space>/<slug>")
	variantCreateCmd.Flags().StringSliceVar(&variantCreateArgs.spaceDeleteGates, "space-delete-gate", []string{}, "delete gate key[=true] to set on the new space (repeatable); the upstream space's delete gates are not copied")
	variantCreateCmd.Flags().StringSliceVar(&variantCreateArgs.unitDeleteGates, "unit-delete-gate", []string{}, "delete gate key[=true] to set on every cloned unit (repeatable); e.g. --unit-delete-gate critical to protect a prod variant")
	variantCreateCmd.Flags().StringSliceVar(&variantCreateArgs.unitDestroyGates, "unit-destroy-gate", []string{}, "destroy gate key[=true] to set on every cloned unit (repeatable); destroy gates are unit-only (spaces have no destroy gates)")
	variantCreateCmd.Flags().BoolVar(&variantCreateArgs.noArgoApp, "no-argo-app", false, "skip auto-creating the Argo CD Application, Sveltos ClusterProfile, or Flux OCIRepository and Kustomization, when --target is a cub-cluster target")
	variantCreateCmd.Flags().BoolVar(&variantCreateArgs.prune, "prune", false, "let the Argo CD Application, Flux Kustomization or Sveltos ClusterProfile created for a cub-cluster --target delete what it deployed: resources that leave the deployment's release (always, on Sveltos), and the whole deployment when the Application, Kustomization or ClusterProfile is deleted")
	variantCreateCmd.MarkFlagsMutuallyExclusive("prune", "no-argo-app")
	variantCreateCmd.Flags().BoolVar(&variantCreateArgs.syncback, "syncback", false, "also link each cloned unit back to its upstream unit, with a MergeUnits link in the upstream space, so changes made in the variant can be merged back into the upstream units (see the draft workflow in the long help)")
	variantCreateCmd.Flags().StringVar(&variantCreateArgs.includeOutgoingLinksWhere, "include-outgoing-links-where", "", "where expression selecting which of the upstream units' links to units in other spaces to copy onto the clones; each copy keeps the original unit as its to-unit, so the variant reads from the same producers. Links within the cloned space are always copied and retargeted at the clones")
	enableWaitFlag(variantCreateCmd)
	variantCmd.AddCommand(variantCreateCmd)
}

func variantCreateCmdRun(cmd *cobra.Command, args []string) (retErr error) {
	variantName := args[0]
	upstreamSpaceSlug := args[1]

	// Resolve the upstream space.
	upstreamSpace, err := resolveVariantOrSpace(upstreamSpaceSlug, "*")
	if err != nil {
		return err
	}
	upstreamSpaceID := upstreamSpace.Space.SpaceID

	// Checked before anything is created, so a bad flag leaves nothing behind.
	if err := checkSpaceLabelFlags(variantCreateArgs.spaceLabels, variantCreateReservedLabels); err != nil {
		return err
	}
	if variantCreateArgs.prune && variantCreateArgs.target == "" {
		return fmt.Errorf("--prune applies to the Argo CD Application, Flux Kustomization or Sveltos ClusterProfile created for --target; give --target")
	}
	ownerComponent, setOwner, err := checkVariantCreateOwner(upstreamSpace.Space, variantCreateArgs.owner)
	if err != nil {
		return err
	}
	var changesetID *uuid.UUID
	if variantCreateArgs.changesetSlug != "" {
		id, err := resolveChangeSetID(variantCreateArgs.changesetSlug)
		if err != nil {
			return fmt.Errorf("failed to get changeset: %w", err)
		}
		changesetID = &id
	}

	// The target names each clone's Target and the space's release target: releases are
	// published per space ("cub release publish <space>"), and publish requires one.
	// --release-target sets only the space's, and leaves the units to the server, which gives a
	// space's release Target to the units released through one. Resolved, like the patches,
	// before anything is created, so a bad flag leaves nothing behind.
	var targetID *uuid.UUID
	var target *goclientnew.Target
	if variantCreateArgs.target != "" {
		target, err = resolveTargetCore(variantCreateArgs.target, defaultSpaceID(), "*")
		if err != nil {
			return err
		}
		targetID = &target.TargetID
	}
	var releaseTargetID *uuid.UUID
	if variantCreateArgs.releaseTarget != "" {
		id, err := resolveTargetID(variantCreateArgs.releaseTarget)
		if err != nil {
			return err
		}
		releaseTargetID = &id
	}
	spacePatch, err := variantSpacePatch()
	if err != nil {
		return err
	}
	unitPatch, err := variantUnitPatch()
	if err != nil {
		return err
	}

	// Steps 1-4: clone the upstream space, which selects the upstream's Triggers and copies its
	// Permissions; set its release target; clone the units into it; and with --namespace run
	// set-namespace over them. The base is typically uploaded with the confighubplaceholder
	// namespace; set-namespace renames the v1/Namespace resource and stamps metadata.namespace
	// on every namespaced resource, so each variant lands in its own namespace. When the
	// upstream space records its namespace, only that namespace moves, so a component's
	// resources in other namespaces, such as kube-system, stay put.
	//
	// From the moment the space exists, a failure leaves it behind without the owner --owner
	// names, so the error says so and gives the command that sets it.
	ownerPending := false
	defer func() {
		if retErr != nil && ownerPending {
			retErr = componentOwnerSkipped(retErr, ownerComponent.Slug, variantCreateArgs.owner)
		}
	}()
	result, err := cubapi.CreateVariant(ctx, cubClient, cubapi.VariantSpec{
		Upstream:                  upstreamSpaceID,
		Name:                      variantName,
		VariantLabels:             variantSpaceLabels(),
		SpacePattern:              variantCreateArgs.spacePattern,
		SpacePatch:                spacePatch,
		UnitPatch:                 unitPatch,
		Target:                    targetID,
		ReleaseTarget:             releaseTargetID,
		Namespace:                 variantCreateArgs.namespace,
		ChangeSetID:               changesetID,
		ChangeDescription:         variantCreateArgs.changeDesc,
		Syncback:                  variantCreateArgs.syncback,
		IncludeOutgoingLinksWhere: variantCreateArgs.includeOutgoingLinksWhere,
		IncludeHidden:             includeHidden,
		AllowExists:               allowExists,
		DryRun:                    dryRun,
	})
	if result != nil && result.Space != nil {
		ownerPending = setOwner
		if !jsonOutput {
			tprint("Created variant space %s (ID: %s)", result.Space.Slug, result.Space.SpaceID)
		}
	}
	if err != nil {
		return err
	}
	newSpace := result.Space
	if variantCreateArgs.namespace != "" && !jsonOutput {
		tprint("Set namespace %q on the cloned units in %s", variantCreateArgs.namespace, newSpace.Slug)
	}

	// Step 5: if the target is a cub-cluster Argo target (carries the
	// confighub.com/apps-space annotation, with confighub.com/cluster-controller
	// argo), auto-create the Argo CD
	// Application Unit for this deployment and republish the cluster's apps
	// Space so Argo becomes aware of it — folding the manual "tell the cluster
	// about the deployment" step into this command. A no-op for ordinary
	// targets; skip explicitly with --no-argo-app.
	if target != nil && !variantCreateArgs.noArgoApp {
		createdArgoApp, err := createVariantArgoApp(cmd.OutOrStdout(), target, newSpace, *targetID, variantCreateArgs.prune)
		if err != nil {
			return err
		}
		// The Sveltos counterpart: a target from `cub cluster up --controller
		// sveltos` carries confighub.com/cluster-controller sveltos instead.
		createdSveltosProfile, err := createVariantSveltosProfile(cmd.OutOrStdout(), target, newSpace, *targetID, variantCreateArgs.prune)
		if err != nil {
			return err
		}
		// And the Flux one: confighub.com/cluster-controller flux.
		createdFluxApp, err := createVariantFluxApp(cmd.OutOrStdout(), target, newSpace, *targetID, variantCreateArgs.prune)
		if err != nil {
			return err
		}
		if variantCreateArgs.prune && !createdArgoApp && !createdSveltosProfile && !createdFluxApp && !jsonOutput {
			tprint("--prune had no effect: target %s is not a cub-cluster target", target.Slug)
		}
	}

	// Step 6: --owner names the owner of the Component, which is written last so that a failure
	// here leaves a complete variant behind and the error can say so.
	if setOwner {
		ownerPending = false
		wrote, err := writeComponentOwner(ownerComponent.ComponentID.String(), variantCreateArgs.owner)
		if err != nil {
			return componentOwnerNotSet("variant space "+newSpace.Slug, ownerComponent.Slug, variantCreateArgs.owner, err)
		}
		if wrote && !jsonOutput {
			tprint("Set the Owner label of component %s to %q", ownerComponent.Slug, variantCreateArgs.owner)
		}
	}

	return handleBulkCreateOrUpdateResponse(&result.Units, result.UnitsStatus, "create", "")
}

// checkVariantCreateOwner checks --owner against the Component the upstream space is in, before
// anything is created, and reports whether its Owner label has to be written. A write that is
// needed is tried as a dry run too, so a missing permission stops the command here rather than
// after the variant exists.
func checkVariantCreateOwner(upstreamSpace *goclientnew.Space, owner string) (*goclientnew.Component, bool, error) {
	if owner == "" {
		return nil, false, nil
	}
	if err := validateOwnerValue(owner); err != nil {
		return nil, false, err
	}
	if upstreamSpace.ComponentID == nil {
		return nil, false, fmt.Errorf("--owner sets the Owner label of the Component the new space is in, and space %s is in no Component", upstreamSpace.Slug)
	}
	entity, err := resolveComponent(upstreamSpace.ComponentID.String(), "*")
	if err != nil {
		return nil, false, err
	}
	component := entity.Component
	setOwner, err := checkComponentOwner(component, owner)
	if err != nil {
		return nil, false, err
	}
	if setOwner {
		if err := applyComponentOwner(component.ComponentID, owner, true); err != nil {
			return nil, false, fmt.Errorf("--owner cannot set the Owner label of component %s: %w", component.Slug, err)
		}
	}
	return component, setOwner, nil
}

// variantCreateReservedLabels are the space labels variant create sets through a flag of its
// own, each mapped to that flag. --space-label may not name them, so no label has two flags
// and no question of which one wins.
var variantCreateReservedLabels = map[string]string{
	"Component":   "--variant-labels",
	"Variant":     "the <variant-name> argument",
	"Stage":       "--stage",
	"Environment": "--environment",
	"Region":      "--region",
	"Layer":       "--layer",
	"Owner":       "--owner, which sets it on the Component",
	"Namespace":   "--namespace",
}

// checkSpaceLabelFlags refuses a --space-label naming a label that reserved says another flag
// sets.
func checkSpaceLabelFlags(spaceLabels []string, reserved map[string]string) error {
	for _, kv := range spaceLabels {
		key, _, _ := strings.Cut(kv, "=")
		if flag, ok := reserved[key]; ok {
			return fmt.Errorf("--space-label %s: set the %s label with %s", key, key, flag)
		}
	}
	return nil
}

// variantSpaceLabels are the labels --stage, --environment, --region, --layer and
// --variant-labels give the new space, in that order so --variant-labels win. The Variant and
// Namespace labels come from the variant name and --namespace, set by cubapi.CreateVariant.
func variantSpaceLabels() []string {
	var labels []string
	if variantCreateArgs.stage != "" {
		labels = append(labels, "Stage="+variantCreateArgs.stage)
	}
	if variantCreateArgs.environment != "" {
		labels = append(labels, "Environment="+variantCreateArgs.environment)
	}
	if variantCreateArgs.region != "" {
		labels = append(labels, "Region="+variantCreateArgs.region)
	}
	if variantCreateArgs.layer != "" {
		labels = append(labels, "Layer="+variantCreateArgs.layer)
	}
	return append(labels, variantCreateArgs.variantLabels...)
}

// variantSpacePatch is the merge patch --space-label, --space-annotation and --permission layer
// onto the cloned space; the upstream's keys survive (RFC 7386). --space-delete-gate sets the
// clone's delete gates, since a clone never inherits the upstream's. The
// TargetID and UpstreamSpaceID annotations are reserved: --target sets the one, and
// cubapi.CreateVariant stamps the other.
func variantSpacePatch() (json.RawMessage, error) {
	for _, a := range variantCreateArgs.spaceAnnotations {
		switch strings.SplitN(a, "=", 2)[0] {
		case "TargetID":
			return nil, fmt.Errorf("--space-annotation TargetID is reserved; use --target")
		case AnnotationUpstreamSpaceID:
			return nil, fmt.Errorf("--space-annotation %s is reserved", AnnotationUpstreamSpaceID)
		}
	}
	return EnhancePatchData([]byte("null"),
		variantCreateArgs.spaceAnnotations, variantCreateArgs.spaceLabels, variantCreateArgs.spaceDeleteGates, variantCreateArgs.permissions, nil)
}

// variantUnitPatch is the merge patch --unit-label and --unit-annotation layer onto every cloned
// unit. --unit-delete-gate and --unit-destroy-gate set its gates, since a clone never inherits
// the upstream unit's. The target, change description and changeset are cubapi.CreateVariant's
// to add.
func variantUnitPatch() (json.RawMessage, error) {
	destroyGates := map[string]bool{}
	if err := setGatesFromSlice(variantCreateArgs.unitDestroyGates, &destroyGates); err != nil {
		return nil, fmt.Errorf("invalid --unit-destroy-gate: %w", err)
	}
	var enhancer PatchEnhancer
	if len(destroyGates) > 0 {
		enhancer = func(m map[string]interface{}) {
			dg := make(map[string]interface{}, len(destroyGates))
			for k, v := range destroyGates {
				dg[k] = v
			}
			m["DestroyGates"] = dg
		}
	}
	return EnhancePatchData([]byte("null"),
		variantCreateArgs.unitAnnotations, variantCreateArgs.unitLabels, variantCreateArgs.unitDeleteGates, nil, enhancer)
}
