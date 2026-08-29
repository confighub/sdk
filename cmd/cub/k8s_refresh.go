// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/k8sutil/cleanup"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

var k8sRefreshArgs struct {
	dryRun bool
}

var k8sRefreshCmd = &cobra.Command{
	Use:   "refresh <type> <name>",
	Short: "Bring cluster-side changes to one Kubernetes resource back into its unit",
	Long: getCommandHelp(`Read one Kubernetes resource from a live cluster, work out what changed in the
cluster since the unit's configuration was last released, and write just that back
into the unit.

The resource is traced back to its unit by the ConfigHub annotations the release
stamped on it: confighub.com/origin, or the legacy confighub.com/SpaceID and
confighub.com/UnitSlug pair. There is no --space flag; the annotations name the space.

What refresh actually does, in order:

  1. Reads the live resource and strips everything that is cluster state rather
     than configuration: status, managedFields, internal metadata, cluster-internal
     annotations and labels, and every field owned only by a controller (the
     workload controllers, HPA/VPA, cert-manager, the API server's own admission
     plugins). Resource quantities are normalized so "2000m" and "2" don't read
     as a change. "k8s-mf cleanup" shows exactly this transformation.
  2. Diffs what is left against the unit's LastReleasedRevisionNum -- the revision
     the cluster was actually given. Diffing against the released revision rather
     than the unit's head is what isolates cluster-side drift from changes made in
     ConfigHub since the release.
  3. Patches that drift onto the unit's head, restricted to this one resource and
     to the paths that actually changed. Paths the unit protects as local overrides
     are not overwritten, and are reported as conflicts instead.

A unit that has never been released has nothing to diff against, and refresh says so
rather than treating the whole live object as a change.

Only the named resource is touched. Other resources in the same unit are left alone
even when the unit holds several. Comments are left alone too: they live in the unit
and never reach the cluster, so nothing the cluster says is evidence about them.

One caveat worth knowing about ArgoCD. Its default resource tracking stamps
app.kubernetes.io/instance on everything it manages, and that is one of the Kubernetes
recommended labels -- Helm charts and hand-written manifests set it as real
configuration. Refresh cannot tell ArgoCD's bookkeeping copy from a value you meant, so
it brings the label back like any other field. Switching ArgoCD to annotation tracking
(application.resourceTrackingMethod: annotation in argocd-cm) avoids this: the
argocd.argoproj.io/tracking-id annotation it writes instead is stripped as cluster state.

Examples:
`+"```"+`
  # Bring a deployment's cluster-side changes back into its unit
  cub k8s refresh deployment my-app --namespace my-namespace

  # See what refresh would change, without changing it
  cub k8s refresh deployment my-app -n prod --dry-run

  # A cluster-scoped resource
  cub k8s refresh clusterrole my-role

  # Read from a specific cluster
  cub k8s refresh deployment my-app --kubeconfig /path/to/config --kube-context prod
`+"```"+`
`, `Use this to answer "someone changed this in the cluster -- what, and can I keep it?".
Run it with --dry-run first: the mutation table names each path that drifted, which is
the diagnosis. Without --dry-run it writes a new unit revision, which still has to be
released before the cluster and ConfigHub agree again.

Refresh is per-resource by design. To sweep a fleet, drive it from "cub k8s get -o name",
which prints one resource per line.`),
	Args:        cobra.ExactArgs(2),
	RunE:        k8sRefreshCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	addK8sClusterFlags(k8sRefreshCmd)
	addStandardDisplayFlags(k8sRefreshCmd)
	enableDisplayMutationsFlag(k8sRefreshCmd)
	addClearanceFlag(k8sRefreshCmd)
	k8sRefreshCmd.Flags().StringVar(&changeDescription, "change-desc", "", "change description")
	k8sRefreshCmd.Flags().BoolVar(&k8sRefreshArgs.dryRun, "dry-run", false, "report what would change without writing it")
	k8sCmd.AddCommand(k8sRefreshCmd)
}

func k8sRefreshCmdRun(_ *cobra.Command, args []string) error {
	kind, name := args[0], args[1]

	dynamicClient, mapper, err := newK8sClusterClients()
	if err != nil {
		return err
	}
	live, err := getK8sResource(dynamicClient, mapper, kind, name, k8sNamespace, k8sApiVersion)
	if err != nil {
		return err
	}

	annotations := live.GetAnnotations()
	if annotations == nil {
		return fmt.Errorf("resource %s/%s has no annotations, so it cannot be traced back to a unit", kind, name)
	}
	spaceID, unitSlug, _, err := resolveConfigHubOrigin(annotations)
	if err != nil {
		return fmt.Errorf("resource %s/%s: %w", kind, name, err)
	}

	// The GVK of what came back, not what was asked for: the type name may have been a
	// short name or a Kind resolved against whatever version the cluster serves.
	gvk := live.GroupVersionKind()
	resourceType := string(cleanup.GvkToResourceType(&gvk))

	// Reduce the live object to the configuration someone authored. Field ownership is
	// what decides which fields survive, so this has to see the object with its
	// managedFields still on it -- ExtraCleanupObjects reads them and then strips them.
	cleaned := cleanup.ExtraCleanupObjects([]*unstructured.Unstructured{live})
	liveData, err := yaml.Marshal(cleaned[0].Object)
	if err != nil {
		return fmt.Errorf("failed to serialize the cleaned %s %s: %w", resourceType, name, err)
	}

	unit, err := apiGetUnitFromSlugInSpace(unitSlug, spaceID,
		"UnitID,SpaceID,Slug,LastReleasedRevisionNum")
	if err != nil {
		return fmt.Errorf("failed to get unit %s in space %s: %w", unitSlug, spaceID, err)
	}
	if unit.LastReleasedRevisionNum == 0 {
		return fmt.Errorf("unit %s has never been released, so there is no revision to compare the cluster against", unit.Slug)
	}

	releasedRevision, err := apiGetRevisionFromNumberInSpace(unit.LastReleasedRevisionNum, unit.UnitID.String(), spaceID, "RevisionID")
	if err != nil {
		return fmt.Errorf("failed to get revision %d of unit %s: %w", unit.LastReleasedRevisionNum, unit.Slug, err)
	}
	releasedData, err := fetchRevisionData(unit.SpaceID, unit.UnitID, releasedRevision.RevisionID)
	if err != nil {
		return fmt.Errorf("failed to read revision %d of unit %s: %w", unit.LastReleasedRevisionNum, unit.Slug, err)
	}

	drift, err := computeClusterDrift(releasedData, string(liveData), resourceType, name)
	if err != nil {
		return err
	}
	if len(drift) == 0 {
		if !quiet {
			tprint("%s %s matches revision %d of unit %s; nothing to refresh",
				resourceType, name, unit.LastReleasedRevisionNum, unit.Slug)
		}
		return nil
	}

	return applyClusterDrift(unit, spaceID, resourceType, name, drift)
}

// computeClusterDrift diffs the released revision against the cleaned live resource and
// returns the mutations that would carry the cluster's version of that one resource back.
//
// The diff runs locally rather than on the server because both sides are already in hand
// and compute-mutations is hermetic; this is the same local invocation "cub unit blame"
// uses to flatten a unit's fields.
//
// Two kinds of entry are dropped. The released revision may hold resources the live object
// does not, and compute-mutations reports each of those as a whole-resource deletion;
// WhereResource keeps them from being written either way, but a --dry-run that listed them
// would be describing a change refresh will not make. And compute-mutations reports every
// resource it walked, including one it found identical, as an entry with no path mutations
// and a resource-level type of None -- so "no drift" arrives as a no-op entry rather than as
// an empty list, and has to be recognized as such.
func computeClusterDrift(releasedData, liveData, resourceType, name string) (api.ResourceMutationList, error) {
	// Comments are an authoring artifact: they live in the unit and never reach the
	// cluster. Left in, every comment on the released side reads as a deletion, and a
	// refresh would silently strip the comments off the resource it refreshed. Stripping
	// them from the base is what the bridge's Refresh did, and it beats discarding comment
	// mutations afterwards -- the diff never sees an asymmetry it has to be told to ignore.
	// The unit's own data is untouched by this, so its comments survive the patch.
	baseData, err := yamlkit.StripComments([]byte(releasedData))
	if err != nil {
		return nil, fmt.Errorf("failed to strip comments from revision data: %w", err)
	}

	// Positionally: config-doc-list (the previous side), function-index, already-converted,
	// reverse. The unit's configuration is the previous side and the live object is the
	// modified side, so the mutations read released -> live.
	restore := quietSlog()
	resp, err := invokeLocalFunction(liveData, "compute-mutations",
		[]string{string(baseData), "0", "true", "false"}, "Kubernetes/YAML")
	restore()
	if err != nil {
		return nil, fmt.Errorf("failed to compare %s %s against the released revision: %w", resourceType, name, err)
	}
	if resp == nil || !resp.Success {
		message := "compute-mutations reported no result"
		if resp != nil && len(resp.ErrorMessages) > 0 {
			message = strings.Join(resp.ErrorMessages, "; ")
		}
		return nil, fmt.Errorf("failed to compare %s %s against the released revision: %s", resourceType, name, message)
	}
	raw, ok := resp.Outputs[api.OutputTypeResourceMutationList]
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	var mutations api.ResourceMutationList
	if err := json.Unmarshal(raw, &mutations); err != nil {
		return nil, fmt.Errorf("failed to read the comparison of %s %s: %w", resourceType, name, err)
	}

	drift := make(api.ResourceMutationList, 0, 1)
	for _, rm := range mutations {
		if string(rm.Resource.ResourceType) != resourceType || !matchesResourceName(rm.Resource, name) {
			continue
		}
		if isNoOpMutation(rm) {
			continue
		}
		drift = append(drift, rm)
	}
	return drift, nil
}

// isNoOpMutation reports whether a resource mutation says nothing changed. Array ordering
// and element renames count as changes even with no path mutations: they are how a reordered
// or renamed list element is expressed.
func isNoOpMutation(rm api.ResourceMutation) bool {
	return rm.ResourceMutationInfo.MutationType == api.MutationTypeNone &&
		len(rm.PathMutationMap) == 0 &&
		len(rm.ArrayOrders) == 0 &&
		len(rm.ArrayElementAliases) == 0
}

// applyClusterDrift patches the drift onto the unit's head, scoped to the one resource.
func applyClusterDrift(unit *goclientnew.Unit, spaceID, resourceType, name string, drift api.ResourceMutationList) error {
	patchData, err := json.Marshal(drift)
	if err != nil {
		return fmt.Errorf("failed to encode the changes to %s %s: %w", resourceType, name, err)
	}

	// The unit's own MutationSources carry the Protected flags that say which paths refresh
	// may not overwrite. An empty list protects nothing, which is the right answer for a unit
	// nothing has claimed a path in.
	protection := goclientnew.ResourceMutationList{}
	stored, err := fetchUnitMutationSources(unit.SpaceID, unit.UnitID)
	if err != nil {
		return fmt.Errorf("failed to read what set the values in unit %s: %w", unit.Slug, err)
	}
	if stored != nil {
		protection = *stored
	}
	protectionData, err := json.Marshal(protection)
	if err != nil {
		return fmt.Errorf("failed to encode unit %s's protected paths: %w", unit.Slug, err)
	}

	body := &goclientnew.FunctionInvocationsRequest{
		ToolchainType:     "Kubernetes/YAML",
		ChangeDescription: changeDescription,
		// This, not the shape of the patch, is what guarantees no other resource in the
		// unit is touched: patch-mutations skips every resource the filter excludes.
		WhereResource: whereResourceForOneResource(resourceType, name),
		FunctionInvocations: &[]goclientnew.FunctionInvocation{{
			FunctionName: "patch-mutations",
			Arguments: []goclientnew.FunctionArgument{
				argFromString("mutation-protection", string(protectionData)),
				argFromString("mutation-patch", string(patchData)),
			},
		}},
	}

	// The mutation table is not extra detail here, it is the answer -- which fields drifted
	// and what they became -- so refresh shows it unless the caller asked for another shape.
	showDrift := shouldDisplayMutations() || (!isAlternativeOutput() && !quiet)

	var priorUnits map[string]priorUnitInfo
	where := fmt.Sprintf("UnitID = '%s'", unit.UnitID.String())
	savedSpaceID := selectedSpaceID
	selectedSpaceID = spaceID
	defer func() { selectedSpaceID = savedSpaceID }()

	if showDrift {
		priorUnits = savePriorUnitInfoInSpace(spaceID, where, false)
	}

	resp, err := invokeFunctionsOnUnits(&invokeArgs{
		Where:     where,
		DryRun:    k8sRefreshArgs.dryRun,
		Clearance: clearanceJSON(),
		Body:      body,
	})
	if err != nil {
		return err
	}
	if resp == nil {
		resp = &[]goclientnew.FunctionInvocationsResponse{}
	}

	if !renderFunctionResponse(resp, false) {
		verb := "Refreshed"
		if k8sRefreshArgs.dryRun {
			verb = "Would refresh"
		}
		tprint("%s %s %s in unit %s from the cluster", verb, resourceType, name, unit.Slug)
		reportRefreshConflicts(resp)
	}
	if showDrift {
		displayMutationsFromFunctionResponse(resp, k8sRefreshArgs.dryRun, priorUnits, "refresh")
	}
	return nil
}

// whereResourceForOneResource selects exactly the resource being refreshed.
//
// The name is matched without its scope because a unit's stored resource often has no
// metadata.namespace -- the applier supplies it -- and would then be "/name" where the live
// object is "namespace/name". Within one unit, type plus name identifies a resource.
func whereResourceForOneResource(resourceType, name string) string {
	return fmt.Sprintf("ConfigHub.ResourceType = '%s' AND ConfigHub.ResourceNameWithoutScope = '%s'",
		resourceType, name)
}

// matchesResourceName reports whether a mutation's resource is the one named on the command
// line, comparing without scope for the same reason whereResourceForOneResource does.
func matchesResourceName(info api.ResourceInfo, name string) bool {
	if string(info.ResourceNameWithoutScope) == name {
		return true
	}
	_, withoutScope, found := strings.Cut(string(info.ResourceName), "/")
	return found && withoutScope == name
}

// reportRefreshConflicts prints the paths patch-mutations declined to write -- paths the
// unit protects as local overrides, or that no longer resolve against its data. They are the
// answer to "the cluster has this value but the unit still doesn't", so they are reported
// rather than left in an output blob nobody reads.
func reportRefreshConflicts(resp *[]goclientnew.FunctionInvocationsResponse) {
	if quiet {
		return
	}
	for _, r := range *resp {
		if !r.Success {
			continue
		}
		encoded, ok := r.Outputs[string(api.OutputTypeMutationConflictList)]
		if !ok || encoded == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			continue
		}
		var conflicts api.MutationConflictList
		if err := json.Unmarshal(decoded, &conflicts); err != nil || len(conflicts) == 0 {
			continue
		}
		tprint("%d change(s) from the cluster were not written:", len(conflicts))
		for _, c := range conflicts {
			// A resource-level conflict carries no path; name the resource instead.
			subject := string(c.Path)
			if subject == "" {
				subject = string(c.Resource.ResourceType) + " " + string(c.Resource.ResourceName)
			}
			line := "  " + subject + ": " + string(c.Reason)
			if c.Details != "" {
				line += " (" + c.Details + ")"
			}
			tprintRaw(line)
		}
	}
}
