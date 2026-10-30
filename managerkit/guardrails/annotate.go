// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

// The annotate command: the producing half of annotate-then-validate. A manager
// cannot attach a ValidationWarning itself -- only a failed Trigger can -- so a
// finding that no single resource expresses is written onto the Unit as an
// annotation, and a rule in the pack warns for as long as it is there.
//
// What differs per tool is only which Units to annotate and with what. The rest
// -- scoping, the dry run, writing, sorting, the report -- is here.

import (
	"context"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/cliutil"
	"github.com/confighub/sdk/core/cubapi"

	"github.com/confighub/sdk/managerkit"
)

// Target is one Unit to annotate.
type Target struct {
	SpaceID  string
	UnitSlug string
	// Value is written as the annotation, and shown in the report.
	Value string

	// Cluster, Namespace and Space locate the Unit in the report. Set whichever
	// the tool knows; the report renders what it is given.
	Cluster   string
	Namespace string
	Space     string
	// Detail is extra context for the report when the Value alone does not say
	// why -- the Service that has no ServiceMonitor, say. Not written.
	Detail string
}

// Result is one Target's outcome. Fields the tool did not set stay out of the
// JSON rather than appearing empty.
type Result struct {
	Cluster   string `json:"cluster,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Space     string `json:"space,omitempty"`
	Unit      string `json:"unit"`
	Value     string `json:"value"`
	Detail    string `json:"detail,omitempty"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
}

// AnnotateSpec is the tool-specific half of an annotate command.
type AnnotateSpec struct {
	// Key is the annotation written onto each Unit. The pack's own rule warns
	// while it is present, so the two have to agree.
	Key string
	// Noun names what is annotated, for the report: "Unit", "workload Unit".
	Noun  string
	Short string
	Long  string
	// Targets computes what to annotate. where is the predicate the scope flags
	// compiled to; cluster is the --cluster filter, empty for every cluster.
	Targets func(ctx context.Context, c *cubapi.Client, where, cluster string) ([]Target, error)
}

func (s AnnotateSpec) noun() string {
	if s.Noun != "" {
		return s.Noun
	}
	return "Unit"
}

// AnnotateCmd is `guardrails annotate` for this pack.
func (p Pack) AnnotateCmd(preflight Preflight, spec AnnotateSpec) *cobra.Command {
	var output, clusterFilter string
	var filter cliutil.QueryFlags
	var commit cliutil.CommitFlags

	cmd := &cobra.Command{
		Use:   "annotate",
		Short: spec.Short,
		Long:  spec.Long,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			changeDesc, dryRun, err := commit.Validate("annotate Units with " + p.Group + " guardrail findings")
			if err != nil {
				return err
			}
			client, err := preflight(cmd.Context())
			if err != nil {
				return err
			}
			where, err := filter.Predicate()
			if err != nil {
				return err
			}
			targets, err := spec.Targets(cmd.Context(), client, where, clusterFilter)
			if err != nil {
				return err
			}

			results := make([]Result, 0, len(targets))
			for _, t := range targets {
				r := Result{
					Cluster: t.Cluster, Namespace: t.Namespace, Space: t.Space,
					Unit: t.UnitSlug, Value: t.Value, Detail: t.Detail,
				}
				if !dryRun {
					err := Annotate(cmd.Context(), client, t.SpaceID, t.UnitSlug, spec.Key, t.Value, changeDesc)
					r.OK = err == nil
					if err != nil {
						r.Error = err.Error()
					}
				}
				results = append(results, r)
			}
			sortResults(results)

			if done, err := managerkit.Render(cmd.OutOrStdout(), output, results); done || err != nil {
				return err
			}
			printAnnotate(cmd, results, spec.noun(), dryRun)
			return nil
		},
	}
	managerkit.AddOutputFlag(cmd, &output)
	filter.BindWhere(cmd)
	filter.BindSpaceLabels(cmd)
	cmd.Flags().StringVar(&clusterFilter, "cluster", "", "only annotate Units in this cluster")
	commit.Bind(cmd)
	return cmd
}

func sortResults(rs []Result) {
	sort.Slice(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		if a.Cluster != b.Cluster {
			return a.Cluster < b.Cluster
		}
		if a.Space != b.Space {
			return a.Space < b.Space
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Unit < b.Unit
	})
}

// location renders wherever the tool placed the Unit: a cluster and namespace
// when it tracks both, otherwise the Space.
//
// The Space comes before a bare cluster deliberately. A tool that annotates
// per-Unit rather than per-namespace has no namespace to pair the cluster with,
// and for a Unit whose Space has no release Target the cluster is the ClusterNone
// bucket -- "None" tells the reader nothing, where the Space names the Unit.
func location(r Result) string {
	switch {
	case r.Cluster != "" && r.Namespace != "":
		return r.Cluster + "/" + r.Namespace
	case r.Space != "":
		return r.Space
	default:
		return r.Cluster
	}
}

func printAnnotate(cmd *cobra.Command, results []Result, noun string, dryRun bool) {
	out := cmd.OutOrStdout()
	for _, r := range results {
		line := fmt.Sprintf("%s  %s  %s", location(r), r.Unit, r.Value)
		if r.Detail != "" {
			line += "  (" + r.Detail + ")"
		}
		if r.Error != "" {
			line += "  ERROR: " + r.Error
		}
		cliutil.Fprintln(out, line)
	}
	if dryRun {
		cliutil.Fprintln(out, fmt.Sprintf(
			"\nDry run — %d %s(s) would be annotated. Re-run with --commit --change-desc \"…\".",
			len(results), noun))
		return
	}
	cliutil.Fprintln(out, fmt.Sprintf("\nAnnotated %d %s(s).", len(results), noun))
}
