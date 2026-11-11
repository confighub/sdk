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
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"

	"github.com/confighub/sdk/managerkit"
	"github.com/confighub/sdk/managerkit/write"
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
	// Withheld is what the Unit's guards kept the annotation from writing, or on a dry run
	// would keep it from writing. A Unit with any is not counted as annotated.
	Withheld []goclientnew.MutationConflict `json:"withheld,omitempty"`
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
	var commit write.CommitFlags

	cmd := &cobra.Command{
		Use:   "annotate",
		Short: spec.Short,
		Long:  spec.Long,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			change, err := commit.Change("annotate Units with " + p.Group + " guardrail findings")
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

			// Each Unit is annotated by a write of its own, so the ChangeSet is opened on all of
			// them first: all of them or none, rather than up to the first one another
			// ChangeSet holds.
			if change.ChangeSet != "" {
				ids, err := targetUnitIDs(cmd.Context(), client, targets)
				if err != nil {
					return err
				}
				if _, err := change.OpenChangeSetOnUnits(cmd.Context(), client, ids); err != nil {
					return err
				}
			}

			results := make([]Result, 0, len(targets))
			var writes []*cubapi.Result
			for _, t := range targets {
				r := Result{
					Cluster: t.Cluster, Namespace: t.Namespace, Space: t.Space,
					Unit: t.UnitSlug, Value: t.Value, Detail: t.Detail,
				}
				// A dry run asks the server too, so it reports what guards would withhold.
				res, err := Annotate(cmd.Context(), client, t.SpaceID, t.UnitSlug, spec.Key, t.Value, change)
				if err != nil {
					r.Error = err.Error()
				} else {
					writes = append(writes, res)
					r.OK = true
					for _, o := range res.Outcomes {
						if !o.Success {
							r.OK, r.Error = false, o.Error
						}
						r.Withheld = append(r.Withheld, o.Conflicts...)
					}
					if len(r.Withheld) > 0 {
						r.OK = false
					}
				}
				results = append(results, r)
			}
			sortResults(results)

			if done, err := managerkit.Render(cmd.OutOrStdout(), output, results); done || err != nil {
				return err
			}
			printAnnotate(cmd, results, spec.noun(), change.DryRun())
			write.WriteWithheld(cmd.OutOrStdout(), write.Summarize("annotate", "", change.DryRun(), writes...))
			return nil
		},
	}
	managerkit.AddOutputFlag(cmd, &output)
	filter.BindWhere(cmd)
	filter.BindSpaceLabels(cmd)
	managerkit.BindComponentResolver(cmd, &filter, preflight)
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
		if len(r.Withheld) > 0 {
			line += fmt.Sprintf("  WITHHELD: %d path(s)", len(r.Withheld))
		}
		cliutil.Fprintln(out, line)
	}
	annotated := 0
	for _, r := range results {
		if r.OK {
			annotated++
		}
	}
	if dryRun {
		cliutil.Fprintln(out, fmt.Sprintf(
			"\nDry run — %d of %d %s(s) would be annotated. Re-run with --commit --change-desc \"…\".",
			annotated, len(results), noun))
		return
	}
	cliutil.Fprintln(out, fmt.Sprintf("\nAnnotated %d of %d %s(s).", annotated, len(results), noun))
}

// targetUnitIDs are the IDs of the targets' Units, which a target names by Space and slug.
func targetUnitIDs(ctx context.Context, client *cubapi.Client, targets []Target) ([]goclientnew.UUID, error) {
	slugsBySpace := map[string][]string{}
	for _, t := range targets {
		slugsBySpace[t.SpaceID] = append(slugsBySpace[t.SpaceID], t.UnitSlug)
	}
	var ids []goclientnew.UUID
	for spaceID, slugs := range slugsBySpace {
		for start := 0; start < len(slugs); start += 100 {
			where := cubapi.Where{}.Eq("SpaceID", spaceID).InStrings("Slug", slugs[start:min(start+100, len(slugs))])
			units, err := cubapi.ListUnits(ctx, client, where, cubapi.ListOpts{Select: "UnitID"})
			if err != nil {
				return nil, err
			}
			for _, eu := range units {
				if eu.Unit != nil {
					ids = append(ids, eu.Unit.UnitID)
				}
			}
		}
	}
	return ids, nil
}
