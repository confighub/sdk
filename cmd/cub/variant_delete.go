// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var variantDeleteCmd = &cobra.Command{
	Use:         "delete <variant>",
	Short:       "Delete a variant and its configuration",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Delete a variant: the space that holds it, with every unit and other entity in it.

The variant is named as <component>/<variant>, or by its space slug. Unlike "cub space
delete", which refuses a space with entities in it unless --recursive is given, a variant
is its configuration, so deleting it always deletes the units; a unit with a delete gate
still stops it unless --force is given.

A base is not deleted while deployments cloned from it remain: their units still point
at its units as their upstream. The error names them. Delete the deployments first, or
pass --detach to delete the base anyway and leave the clones without an upstream.

A deployment on a cluster wired with "cub cluster up" or "cub cluster attach" is taken off the
cluster first: its Argo CD Application unit, Flux OCIRepository and Kustomization unit, or Sveltos
ClusterProfile unit, is deleted from the cluster's apps space and the apps space's release
republished, then, once the cluster's root has applied that release, the Application,
Kustomization or ClusterProfile is deleted with everything it deployed. --orphan deletes it and
leaves what it deployed running. The cluster is reached through its kubeconfig, so this has to
run on the machine that wired the cluster; elsewhere the unit is still deleted, and the command
prints what is left in the cluster and how to remove it. On a Sveltos cluster, whose root deletes
the ClusterProfile on its own, a delete the root would not carry out as asked is refused instead.

Examples:
`+"```"+`
  # Delete a deployment
  cub variant delete web/dev

  # Delete a deployment, leaving what Argo CD deployed for it running
  cub variant delete --orphan web/dev

  # Delete the base after its deployments are gone
  cub variant delete web/base

  # Delete a base whose deployments stay, detaching them from it
  cub variant delete --detach web/base
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: variantDeleteCmdRun,
}

var (
	variantDeleteForce  bool
	variantDeleteOrphan bool
)

func init() {
	addStandardDeleteFlags(variantDeleteCmd)
	variantDeleteCmd.Flags().BoolVar(&variantDeleteForce, "force", false,
		"delete the variant's units even if some have delete gates")
	variantDeleteCmd.Flags().BoolVar(&variantDeleteOrphan, "orphan", false,
		"for a deployment on a cub cluster, delete its Argo CD Application, Flux Kustomization or Sveltos ClusterProfile but leave what it deployed running")
	addDetachFlag(variantDeleteCmd)
	variantCmd.AddCommand(variantDeleteCmd)
}

func variantDeleteCmdRun(cmd *cobra.Command, args []string) error {
	space, err := resolveVariantOrSpace(args[0], "SpaceID,Slug,Annotations,ReleaseTargetID")
	if err != nil {
		return err
	}
	if err := deleteVariantClusterApp(cmd.OutOrStdout(), space.Space, !variantDeleteOrphan); err != nil {
		return err
	}
	params := &goclientnew.DeleteSpaceParams{}
	recursiveParam := "true"
	params.Recursive = &recursiveParam
	if variantDeleteForce {
		params.RecursiveForce = &recursiveParam
	}
	params.Detach = detachParam()
	deleteRes, err := cubClientNew.DeleteSpaceWithResponse(ctx, space.Space.SpaceID, params)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteResults("variant", args[0], space.Space.SpaceID.String(), deleteRes)
	return nil
}

// variantClusterRootWait bounds how long `cub variant delete` waits for a
// cluster's root to apply the apps Space's Release without the deleted
// deployment.
const variantClusterRootWait = 3 * time.Minute

// variantClusterApp is what deploys a variant on a cub cluster: the Unit in the
// cluster's apps Space holding its Argo CD Application, or its Flux
// OCIRepository and Kustomization, and how to reach the cluster.
type variantClusterApp struct {
	target        *goclientnew.Target
	unitID        string // the variant Space's ClusterUnitID
	appsSpaceSlug string
	// name is the variant Space slug, which names the Application, or the
	// OCIRepository and Kustomization, in the cluster.
	name           string
	clusterName    string
	kubeconfigPath string
	// inCluster is whether this machine has the cluster's kubeconfig: only the
	// one that attached the cluster does.
	inCluster bool
}

// deleteVariantClusterApp takes a deployment off the cub cluster that deploys
// it, before `cub variant delete` deletes its Space: through
// deleteVariantArgoApp on an Argo CD cluster, deleteVariantFluxApp on a Flux
// one. A Space that no cluster controller deploys is left alone, and so is one
// on a Sveltos cluster.
//
// Everything is found from the Space itself, its release target and
// ClusterUnitID, so a delete that stopped after removing the Unit can be run
// again.
func deleteVariantClusterApp(out io.Writer, space *goclientnew.Space, cascade bool) error {
	unitID := space.Annotations[clusterAnnotationClusterUnitID]
	if unitID == "" || space.ReleaseTargetID == nil {
		return nil
	}
	target, err := resolveTargetCore(space.ReleaseTargetID.String(), "", "TargetID,Slug,SpaceID,Annotations")
	if err != nil {
		return fmt.Errorf("resolve the release target of space %q: %w", space.Slug, err)
	}
	app := &variantClusterApp{target: target, unitID: unitID, name: space.Slug}
	var deleteApp func(io.Writer, *variantClusterApp, bool) error
	switch {
	case clusterAppsSpaceSlug(target, clusterControllerArgo) != "":
		app.appsSpaceSlug, deleteApp = clusterAppsSpaceSlug(target, clusterControllerArgo), deleteVariantArgoApp
	case clusterAppsSpaceSlug(target, clusterControllerFlux) != "":
		app.appsSpaceSlug, deleteApp = clusterAppsSpaceSlug(target, clusterControllerFlux), deleteVariantFluxApp
	case clusterAppsSpaceSlug(target, clusterControllerSveltos) != "":
		app.appsSpaceSlug, deleteApp = clusterAppsSpaceSlug(target, clusterControllerSveltos), deleteVariantSveltosProfile
	default:
		return nil
	}

	clusterSpace, err := resolveSpace(target.SpaceID.String(), "SpaceID,Slug,Annotations")
	if err != nil {
		return fmt.Errorf("resolve the cluster space of target %q: %w", target.Slug, err)
	}
	app.clusterName = clusterSpace.Space.Annotations[clusterAnnotationName]
	if app.clusterName == "" {
		app.clusterName = clusterSpace.Space.Slug
	}
	app.kubeconfigPath = clusterKubeconfigPath(app.clusterName)
	_, statErr := os.Stat(app.kubeconfigPath)
	app.inCluster = statErr == nil

	if jsonOutput {
		out = io.Discard
	}
	return deleteApp(out, app, cascade)
}

// removeUnit deletes the deployment's Unit from the apps Space and republishes
// the apps Space's Release, so the cluster's root stops deploying it. kind
// names what the Unit holds, for the output. A Unit already gone, from an
// earlier delete that stopped before the Space went, is not an error.
func (app *variantClusterApp) removeUnit(out io.Writer, kind string) error {
	appsSpace, err := resolveSpace(app.appsSpaceSlug, "SpaceID")
	if err != nil {
		return fmt.Errorf("resolve apps space %q from target annotation: %w", app.appsSpaceSlug, err)
	}
	unit, err := resolveUnit(app.unitID, appsSpace.Space.SpaceID.String(), "UnitID,SpaceID,Slug")
	if cubapi.IsNotFoundError(err) {
		fmt.Fprintf(out, "%s Unit for %q is already gone from apps space %q.\n", kind, app.name, app.appsSpaceSlug)
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve the %s Unit of space %q: %w", kind, app.name, err)
	}
	fmt.Fprintf(out, "Deleting %s Unit %q from apps space %q...\n", kind, unit.Unit.Slug, app.appsSpaceSlug)
	deleteRes, err := cubClientNew.DeleteUnitWithResponse(ctx, unit.Unit.SpaceID, unit.Unit.UnitID, &goclientnew.DeleteUnitParams{})
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}
	fmt.Fprintf(out, "Publishing apps space %q Release...\n", app.appsSpaceSlug)
	return clusterPublishRelease(appsSpace.Space.SpaceID)
}
