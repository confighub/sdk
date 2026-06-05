// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var clusterListCmd = &cobra.Command{
	Use:   "list",
	Short: "List local kind clusters managed by cub",
	Long: `Lists clusters that are cub-managed on this host.

Sources of truth (no state file):
  - Per-cluster kubeconfig at <config-dir>/clusters/<name>.kubeconfig (created by ` + "`cub cluster up`" + `)
  - kind clusters present locally
  - ConfigHub Spaces in the current cub context with Labels."confighub.com/cluster"=true
    and matching this host's annotation

The shown union is everything that has either a local kubeconfig file or a
matching Space — so a cluster registered with a different cub context still
shows up (with a status note). The STATUS column reports any drift.`,
	RunE: clusterListCmdRun,
}

func init() {
	clusterCmd.AddCommand(clusterListCmd)
}

func clusterListCmdRun(cmd *cobra.Command, args []string) error {
	return clusterListRun(cmd.OutOrStdout())
}

// clusterListRow is the merged view of a single cub-managed cluster.
type clusterListRow struct {
	name        string
	kubeconfig  string // present if local kubeconfig file exists
	kindPresent bool
	spaceSlug   string // present if matching Space found in current context
	portRange   string
	argoPort    string
	createdAt   time.Time
}

func clusterListRun(out io.Writer) error {
	hostname, _ := os.Hostname()

	// 1. Local kubeconfigs cub has created.
	localNames, err := clusterLocalNames()
	if err != nil {
		return err
	}

	// 2. kind clusters on this host.
	kindNames, err := clusterKindList(ctx)
	if err != nil {
		return fmt.Errorf("list kind clusters: %w", err)
	}
	kindSet := map[string]bool{}
	for _, n := range kindNames {
		kindSet[n] = true
	}

	// 3. Spaces in current cub context with the cluster label + matching host.
	lkSpaces, err := clusterListSpacesForHost(hostname)
	if err != nil {
		fmt.Fprintf(out, "warning: could not list ConfigHub spaces: %v\n\n", err)
	}

	// Union by cluster name.
	rows := map[string]*clusterListRow{}
	for _, n := range localNames {
		rows[n] = &clusterListRow{name: n, kubeconfig: clusterKubeconfigPath(n), kindPresent: kindSet[n]}
	}
	for _, s := range lkSpaces {
		r, ok := rows[s.ClusterName]
		if !ok {
			r = &clusterListRow{name: s.ClusterName, kindPresent: kindSet[s.ClusterName]}
			rows[s.ClusterName] = r
		}
		r.spaceSlug = s.SpaceSlug
		r.portRange = s.PortRange
		r.argoPort = s.ArgoPort
		r.createdAt = s.CreatedAt
	}

	if len(rows) == 0 {
		fmt.Fprintln(out, "No cub clusters tracked on this host.")
		return nil
	}

	names := make([]string, 0, len(rows))
	for n := range rows {
		names = append(names, n)
	}
	sort.Strings(names)

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSPACE\tPORTS\tARGO\tKUBECONFIG\tSTATUS")
	for _, n := range names {
		r := rows[n]
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.name, clusterDash(r.spaceSlug), clusterDash(r.portRange), clusterArgoCol(r.argoPort), clusterDash(r.kubeconfig), clusterStatusOf(r))
	}
	return tw.Flush()
}

func clusterArgoCol(port string) string {
	if port == "" {
		return "-"
	}
	return "http://localhost:" + port
}

func clusterDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func clusterStatusOf(r *clusterListRow) string {
	switch {
	case r.kubeconfig != "" && r.kindPresent && r.spaceSlug != "":
		return "Ready"
	case r.kubeconfig != "" && r.kindPresent && r.spaceSlug == "":
		return "Local only (no Space in current context)"
	case r.kubeconfig != "" && !r.kindPresent && r.spaceSlug != "":
		return "Drift: kind cluster missing"
	case r.kubeconfig != "" && !r.kindPresent && r.spaceSlug == "":
		return "Stale kubeconfig (no kind cluster, no Space)"
	case r.kubeconfig == "" && r.kindPresent && r.spaceSlug != "":
		return "Stranded: kubeconfig missing"
	case r.kubeconfig == "" && !r.kindPresent && r.spaceSlug != "":
		return "ConfigHub only (no local cluster)"
	default:
		return "Unknown"
	}
}
