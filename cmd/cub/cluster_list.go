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
	Short: "List the clusters this machine manages, and the current organization's",
	Long: `Lists cub-managed clusters from two inventories:

  - This machine's: every cluster with a kubeconfig at
    <config-dir>/clusters/<name>.kubeconfig, written by ` + "`cub cluster up`" + ` or
    ` + "`cub cluster attach`" + `, whichever ConfigHub it is wired to. The record beside
    the kubeconfig names the server and organization; CONTEXT shows the cub
    context for it, starred when it is the current one.
  - The current context's organization's: every cluster Space there
    (Labels."confighub.com/cluster"=true). An attached cluster is listed for
    everyone in the organization; a kind cluster only on the machine that
    created it, since nothing else can reach it.

A cluster in both shows once. STATUS reports drift for clusters in the current
context — a kind cluster that is gone, a kubeconfig with nothing behind it —
and says "other context" for the rest, which are not checked here.

A cluster with a kubeconfig but no record is from before records existed; the
first time this finds its Space in the current context it writes the record,
so the migration needs nothing run by hand.`,
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
	origin      string // clusterOriginKind or clusterOriginAttach
	kubeconfig  string // present if local kubeconfig file exists
	record      *clusterRecord
	kindPresent bool
	inContext   bool   // the cluster is wired to the current context's organization
	spaceSlug   string // present if the Space was found in the current context
	portRange   string
	controller  string // from the Space or record; empty when unknown
	uiPort      string // the controller UI's NodePort: Argo CD's, or the Sveltos dashboard's; none for Flux
	createdAt   time.Time
}

func clusterListRun(out io.Writer) error {
	hostname, _ := os.Hostname()

	// 1. This machine's clusters, with their records.
	local, err := clusterLocalRecords()
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

	// 3. The current organization's cluster Spaces.
	spaces, err := clusterListSpaces()
	if err != nil {
		fmt.Fprintf(out, "warning: could not list ConfigHub spaces: %v\n\n", err)
	}
	spaceByName := map[string]*clusterSpace{}
	for i := range spaces {
		spaceByName[spaces[i].ClusterName] = &spaces[i]
	}

	rows := map[string]*clusterListRow{}
	for name, rec := range local {
		r := &clusterListRow{name: name, kubeconfig: clusterKubeconfigPath(name), record: rec, kindPresent: kindSet[name], origin: clusterOriginKind}
		if rec != nil {
			r.origin = rec.Origin
			r.controller = rec.Controller
			r.inContext = clusterRecordIsActive(rec)
		} else if s := spaceByName[name]; s != nil && s.Host == hostname {
			// A kubeconfig from before records existed, whose Space is here:
			// adopt it, so the next command knows where it is wired.
			r.inContext = true
			rec := clusterNewRecord(name, s.SpaceSlug, s.Origin, s.Controller)
			rec.CreatedAt = s.CreatedAt
			if err := clusterWriteRecord(rec); err != nil {
				fmt.Fprintf(out, "warning: could not record where cluster %q is wired: %v\n\n", name, err)
			} else {
				r.record = &rec
			}
		}
		rows[name] = r
	}
	for _, s := range spaces {
		r, ok := rows[s.ClusterName]
		if !ok {
			// Not on this machine. A kind cluster made elsewhere is unreachable
			// from here and would only be noise; an attached one is the
			// organization's and worth knowing about.
			if s.Origin == clusterOriginKind && s.Host != hostname {
				continue
			}
			r = &clusterListRow{name: s.ClusterName, kindPresent: kindSet[s.ClusterName], inContext: true}
			rows[s.ClusterName] = r
		} else if !r.inContext {
			// Same name, wired elsewhere: the local row stands, the Space here
			// is a different cluster that happens to share the name.
			continue
		}
		r.origin = s.Origin
		r.spaceSlug = s.SpaceSlug
		r.portRange = s.PortRange
		r.controller = s.Controller
		switch s.Controller {
		case clusterControllerSveltos:
			r.uiPort = s.SveltosPort
		case clusterControllerFlux:
			// Flux has no UI.
		default:
			r.uiPort = s.ArgoPort
		}
		r.createdAt = s.CreatedAt
	}

	if len(rows) == 0 {
		fmt.Fprintln(out, "No cub clusters on this machine or in the current context.")
		return nil
	}

	names := make([]string, 0, len(rows))
	for n := range rows {
		names = append(names, n)
	}
	sort.Strings(names)

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tORIGIN\tCONTEXT\tSPACE\tCONTROLLER\tPORTS\tUI\tKUBECONFIG\tSTATUS")
	for _, n := range names {
		r := rows[n]
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.name, r.origin, clusterContextCol(r), clusterDash(r.spaceSlug), clusterDash(r.controller), clusterDash(r.portRange), clusterUICol(r.uiPort), clusterDash(r.kubeconfig), clusterStatusOf(r))
	}
	return tw.Flush()
}

// clusterContextCol names the context a row is wired to: the current one,
// starred; another by name; or where the record points when no context names
// it. A row from the current organization without a record is the current
// context by definition.
func clusterContextCol(r *clusterListRow) string {
	active := contextManager.ActiveContextName()
	switch {
	case r.inContext:
		return "* " + active
	case r.record != nil:
		return clusterRecordWhere(r.record)
	default:
		return "-"
	}
}

// clusterUICol renders the controller UI's endpoint: Argo CD, or the Sveltos
// dashboard, on its NodePort.
func clusterUICol(port string) string {
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

// clusterStatusOf reports drift for a cluster in the current context, and
// otherwise only what can be seen from here. An attached cluster is not a kind
// cluster, so whether kind has one by that name says nothing about it.
func clusterStatusOf(r *clusterListRow) string {
	if !r.inContext {
		switch {
		case r.record == nil:
			return "No record and no Space in current context (list in its context to adopt it)"
		case r.origin == clusterOriginKind && !r.kindPresent:
			return "Other context; kind cluster missing"
		default:
			return "Other context (not checked)"
		}
	}
	if r.origin == clusterOriginAttach {
		switch {
		case r.kubeconfig != "" && r.spaceSlug != "":
			return "Ready"
		case r.kubeconfig != "":
			return "Local only (no Space in current context)"
		default:
			return "No local credentials (attached elsewhere)"
		}
	}
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
