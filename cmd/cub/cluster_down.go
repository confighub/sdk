// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

var clusterDownArgs struct {
	name         string
	force        bool
	deleteConfig bool
}

var clusterDownCmd = &cobra.Command{
	Use:   "down <name>",
	Args:  cobra.MaximumNArgs(1),
	Short: "Tear down a kind cluster that up created (optionally its ConfigHub config too)",
	Long: `The inverse of "cub cluster up": detaches the cluster from ConfigHub, deletes
the kind cluster, and removes the local kubeconfig, env file and record. For a
cluster that "cub cluster attach" wired in, which cub did not create, use
"cub cluster detach" instead; down refuses it.

By default the ConfigHub side is left in place — the cluster Space with its OCI
target and argobot's Channel, the apps Space holding the Argo Application Units
(or, for a --controller sveltos cluster, the Sveltos ClusterProfile Units, and
for a --controller flux cluster, the Flux OCIRepository and Kustomization
Units), any deployment Spaces bound to the target, and the cluster's service
account. That
config outlives the cluster and can be re-applied to a new one, so tearing down
the live cluster does not throw it away.

Pass --delete-config to also delete that config: down removes every Space whose
release target is the cluster's OCI target (the apps Space, the argobot variant
Space, and any deployment variant Spaces), then the cluster Space itself, then
the service account. The cluster Space is deleted after the others because ON
DELETE RESTRICT blocks deleting its target while those Spaces still reference
it. This is destructive and includes your deployment variant Spaces — it
deletes their config, not just the live cluster.

The cluster's record (written beside its kubeconfig by up) says which ConfigHub
server and organization it is wired to, and down acts there, whichever cub
context is current. A cluster from before records existed is looked up in the
current context instead. If no Space is found, down fails with an "are you in
the right context?" hint; pass --force to delete the local kind cluster and
files anyway.`,
	RunE: clusterDownCmdRun,
}

func init() {
	clusterDownCmd.Flags().StringVar(&clusterDownArgs.name, "name", "", "cluster name (the argument; kept as a flag for older scripts)")
	_ = clusterDownCmd.Flags().MarkHidden("name")
	clusterDownCmd.Flags().BoolVar(&clusterDownArgs.force, "force", false, "delete the local kind cluster and files even if no matching ConfigHub Space (or context for the cluster's record) is found")
	clusterDownCmd.Flags().BoolVar(&clusterDownArgs.deleteConfig, "delete-config", false, "also delete the ConfigHub config: the cluster's spaces (target, channel, apps, argobot, and deployment variants bound to the target) and its service account. Destructive.")
	clusterCmd.AddCommand(clusterDownCmd)
}

func clusterDownCmdRun(cmd *cobra.Command, args []string) error {
	name, err := clusterNameArg(args, clusterDownArgs.name, "down", true)
	if err != nil {
		return err
	}
	return clusterDownRun(cmd.OutOrStdout(), name, clusterDownArgs.force, clusterDownArgs.deleteConfig)
}

func clusterDownRun(out io.Writer, name string, force, deleteConfig bool) error {
	rec, match, err := clusterLocate(out, name, force)
	if err != nil {
		return err
	}
	// down is for clusters cub created. An attached cluster is someone else's;
	// detach is the command that leaves it running.
	if origin := clusterOriginFrom(rec, match); origin == clusterOriginAttach {
		return fmt.Errorf("cluster %q was attached, not created by cub; 'cub cluster detach %s' removes the wiring and leaves it running", name, name)
	}

	fmt.Fprintf(out, "Deleting kind cluster %q...\n", name)
	if err := clusterKindDelete(ctx, name, clusterKubeconfigPath(name), out); err != nil {
		fmt.Fprintf(out, "  warning: %v\n", err)
	}
	clusterRemoveLocalFiles(out, name)
	return clusterFinishConfig(out, name, match, deleteConfig)
}

// clusterLocate finds a cub-managed cluster's Space, switching to the context
// its record says it is wired to. Without a record (a cluster from before they
// existed, or managed from another machine) the current context and this
// host's annotation are all there is. A cluster that cannot be found is an
// error unless force is set, in which case the caller proceeds with whatever
// was found, possibly nothing.
func clusterLocate(out io.Writer, name string, force bool) (*clusterRecord, *clusterSpace, error) {
	rec, err := clusterReadRecord(name)
	if err != nil {
		return nil, nil, err
	}
	if err := clusterUseRecordedContext(out, rec); err != nil {
		if !force {
			return nil, nil, err
		}
		fmt.Fprintf(out, "  warning: %v\n", err)
		return rec, nil, nil
	}

	var match *clusterSpace
	if rec != nil {
		match, err = clusterSpaceByNameInOrg(name)
	} else {
		hostname, _ := os.Hostname()
		var spaces []clusterSpace
		spaces, err = clusterListSpacesForHost(hostname)
		for i := range spaces {
			if spaces[i].ClusterName == name {
				match = &spaces[i]
				break
			}
		}
	}
	if err != nil {
		return nil, nil, fmt.Errorf("look up cluster spaces: %w", err)
	}
	if match == nil && !force {
		if rec != nil {
			return nil, nil, fmt.Errorf("no cub-managed Space for %q in %s, where its record says it is wired. Pass --force to remove the local cluster and files anyway", name, clusterRecordWhere(rec))
		}
		return nil, nil, fmt.Errorf("no cub-managed Space for %q in the current cub context (are you in the right context?). Pass --force to proceed anyway", name)
	}
	return rec, match, nil
}

// clusterOriginFrom says how a cluster came to be managed: the Space says,
// then the record, and with neither it is a kind cluster, the only kind there
// was before origins were recorded.
func clusterOriginFrom(rec *clusterRecord, match *clusterSpace) string {
	switch {
	case match != nil:
		return match.Origin
	case rec != nil && rec.Origin != "":
		return rec.Origin
	default:
		return clusterOriginKind
	}
}

// clusterRemoveLocalFiles removes the cluster's kubeconfig, env file and
// record; each absence is fine, each other failure a warning.
func clusterRemoveLocalFiles(out io.Writer, name string) {
	if err := os.Remove(clusterKubeconfigPath(name)); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(out, "  warning: removing kubeconfig: %v\n", err)
	}
	if err := os.Remove(clusterEnvFilePath(name)); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(out, "  warning: removing env file: %v\n", err)
	}
	if err := clusterRemoveRecord(name); err != nil {
		fmt.Fprintf(out, "  warning: removing record: %v\n", err)
	}
}

// clusterFinishConfig is the ConfigHub-side ending of down and detach: delete
// the config with --delete-config, or say what was left in place and how to
// delete it by hand.
func clusterFinishConfig(out io.Writer, name string, match *clusterSpace, deleteConfig bool) error {
	switch {
	case deleteConfig && match == nil:
		fmt.Fprintf(out, "\n--delete-config: no cub-managed Space for %q found; no config to delete.\n", name)
	case deleteConfig:
		if err := clusterDeleteClusterConfig(out, match); err != nil {
			return err
		}
	case match != nil:
		appsSlug, appsKind := match.SpaceSlug+clusterAppsSuffix(match.Controller), clusterAppsKind(match.Controller)
		argobotSlug := clusterBotSpaceOf(match)
		serviceAccountSlug := clusterServiceAccountSlug(match.ClusterName)
		verb := "down"
		if match.Origin == clusterOriginAttach {
			verb = "detach"
		}
		fmt.Fprintf(out, "\nLeft the ConfigHub config in place: space %q (OCI target + argobot channel),\n"+
			"space %q (%s), the argobot variant space %q (if\n"+
			"installed), any deployment variant spaces you created, and the service\n"+
			"account %q. Delete it all in one step with\n"+
			"'cub cluster %s %s --delete-config', or by hand — the cluster space\n"+
			"after every other space, since their release targets point cross-space at\n"+
			"its OCI target (deleting the cluster space first is blocked by those\n"+
			"references):\n"+
			"  cub space delete %s --recursive\n"+
			"  cub space delete %s --recursive   # if argobot was installed\n"+
			"  # ...plus any deployment variant spaces bound to %s/%s...\n"+
			"  cub space delete %s --recursive\n"+
			"  cub serviceaccount delete %s\n",
			match.SpaceSlug, appsSlug, appsKind, argobotSlug, serviceAccountSlug, verb, name,
			appsSlug, argobotSlug, match.SpaceSlug, clusterTargetSlug, match.SpaceSlug, serviceAccountSlug)
	}
	fmt.Fprintf(out, "\nDone.\n")
	return nil
}

// clusterBotSpaceOf returns the cluster's argobot variant Space slug: as
// recorded on the cluster Space, or by the naming convention for a cluster
// from before it was recorded.
func clusterBotSpaceOf(c *clusterSpace) string {
	if c != nil && c.BotSpace != "" {
		return c.BotSpace
	}
	name := clusterArgobotComponent + "-"
	if c != nil {
		return name + c.ClusterName
	}
	return name
}

// clusterDeleteClusterConfig deletes the ConfigHub config an attachment
// created. It deletes the Spaces that reference the cluster's OCI target
// cross-Space (the apps Space, the argobot variant Space, and any deployment
// variant Spaces) first, then the cluster Space that holds the target and
// channel — that order is required because ON DELETE RESTRICT blocks deleting
// the target while any Space still references it — and last the service
// account.
func clusterDeleteClusterConfig(out io.Writer, c *clusterSpace) error {
	deps, err := clusterDependentSpaceSlugs(c.SpaceID)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nDeleting the ConfigHub config for %q...\n", c.SpaceSlug)
	for _, slug := range deps {
		fmt.Fprintf(out, "  deleting space %q...\n", slug)
		if err := clusterDeleteSpace(slug, true); err != nil {
			return fmt.Errorf("delete space %q: %w", slug, err)
		}
	}
	fmt.Fprintf(out, "  deleting cluster space %q (target + channel)...\n", c.SpaceSlug)
	if err := clusterDeleteSpace(c.SpaceSlug, true); err != nil {
		return fmt.Errorf("delete cluster space %q: %w", c.SpaceSlug, err)
	}
	// The service account is org-scoped, so no Space takes it along. Its keys
	// and tokens go with it; a cluster from before it existed has none to delete.
	serviceAccountSlug := clusterServiceAccountSlug(c.ClusterName)
	fmt.Fprintf(out, "  deleting service account %q (if any)...\n", serviceAccountSlug)
	if err := clusterDeleteServiceAccount(serviceAccountSlug); err != nil {
		return fmt.Errorf("delete service account %q: %w", serviceAccountSlug, err)
	}
	return nil
}
