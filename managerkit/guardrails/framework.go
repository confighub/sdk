// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/cliutil"
	"github.com/confighub/sdk/core/cubapi"
)

// Framework is a set of controls a standard, such as the CIS Kubernetes Benchmark, defines, each
// mapped to the upstream controls the packs' checks correspond to. It is a way to report on the
// checks, not to organize them: `guardrails status --framework` reports how far the installed
// rules go towards it.
type Framework struct {
	// Name is how --framework names it, such as cis-v1.12.
	Name  string
	Title string
	// Source is where the mapping came from, as repository@commit, and License its license.
	Source  string
	License string
	// Controls are the framework's controls.
	Controls []FrameworkControl
}

// FrameworkControl is one control of a Framework.
type FrameworkControl struct {
	// ID is the framework's own number for it, such as 5.2.2.
	ID    string
	Title string
	// Corresponds are the upstream controls that implement it, written as Check.Corresponds
	// writes them, such as KS:C-0057 or CIS:5.2.2. A check corresponding to any of them covers it.
	Corresponds []string
}

// StageNotInstalled is the stage of a check whose rule is not in the policy Space.
const StageNotInstalled = "NotInstalled"

// FrameworkCheck is a check covering a framework control, and where its rule stands.
type FrameworkCheck struct {
	Check   string `json:"check"`
	Control string `json:"control"`
	Group   string `json:"group"`
	// Rule is the rule the check runs in, empty when it is not installed. Stage is the rule's,
	// or StageNotInstalled, and Warnings and Errors count the Units the rule marked, for all of
	// its checks.
	Rule     string `json:"rule,omitempty"`
	Stage    string `json:"stage"`
	Warnings int    `json:"warnings"`
	Errors   int    `json:"errors"`
}

// FrameworkRow is where one framework control stands: the checks covering it, and the furthest
// any of them is enforced. A control no check covers has Stage "" and no checks.
type FrameworkRow struct {
	Control string           `json:"control"`
	Title   string           `json:"title"`
	Stage   string           `json:"stage"`
	Checks  []FrameworkCheck `json:"checks"`
}

// FrameworkReport is what `guardrails status --framework` reports.
type FrameworkReport struct {
	Framework string         `json:"framework"`
	Title     string         `json:"title"`
	Source    string         `json:"source,omitempty"`
	Controls  []FrameworkRow `json:"controls"`
	// ByStage counts the controls at each stage; Uncovered those no check covers.
	ByStage   map[string]int `json:"byStage"`
	Uncovered int            `json:"uncovered"`
}

// frameworkStageOrder ranks how far a check is towards enforcement, so that a control stands
// where the furthest of its checks does.
func frameworkStageOrder(stage string) int {
	return slices.Index([]string{StageNotInstalled, StageDisabled, StageCreated, StageAudited, StageEnforced}, stage)
}

// FrameworkStatus reports how far the rules in policySpace go towards framework f, by the checks
// of packs.
func FrameworkStatus(ctx context.Context, client *cubapi.Client, policySpace string, f Framework, packs []Pack) (FrameworkReport, error) {
	rows, err := Enforcement(ctx, client, policySpace, nil)
	if err != nil {
		return FrameworkReport{}, err
	}
	return frameworkReport(f, packs, rows), nil
}

// frameworkReport is FrameworkStatus over where the rules stand.
func frameworkReport(f Framework, packs []Pack, rules []EnforcementRow) FrameworkReport {
	ruleOf := map[string]EnforcementRow{}
	for _, r := range rules {
		for _, control := range r.Controls {
			ruleOf[control] = r
		}
	}
	report := FrameworkReport{Framework: f.Name, Title: f.Title, Source: f.Source, ByStage: map[string]int{}}
	for _, fc := range f.Controls {
		row := FrameworkRow{Control: fc.ID, Title: fc.Title, Checks: []FrameworkCheck{}}
		for _, p := range packs {
			for _, c := range p.Checks {
				if !slices.ContainsFunc(c.Corresponds, func(s string) bool { return slices.Contains(fc.Corresponds, s) }) {
					continue
				}
				check := FrameworkCheck{Check: c.Name, Control: c.Control, Group: p.Group, Stage: StageNotInstalled}
				if r, ok := ruleOf[c.Control]; ok {
					check.Rule, check.Stage, check.Warnings, check.Errors = r.Rule, r.Stage, r.Warnings, r.Errors
				}
				row.Checks = append(row.Checks, check)
				if frameworkStageOrder(check.Stage) > frameworkStageOrder(row.Stage) {
					row.Stage = check.Stage
				}
			}
		}
		if len(row.Checks) == 0 {
			report.Uncovered++
		} else {
			report.ByStage[row.Stage]++
		}
		report.Controls = append(report.Controls, row)
	}
	return report
}

// PrintFramework renders a FrameworkReport as a table.
func PrintFramework(cmd *cobra.Command, rep FrameworkReport) {
	out := cmd.OutOrStdout()
	cliutil.Fprintln(out, fmt.Sprintf("%s (%s)", rep.Title, rep.Framework))
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "CONTROL\tSTAGE\tCHECKS\tTITLE")
	for _, row := range rep.Controls {
		var checks []string
		for _, c := range row.Checks {
			where := c.Stage
			if c.Rule != "" {
				where = fmt.Sprintf("%s %s, %d warned, %d blocked", c.Rule, c.Stage, c.Warnings, c.Errors)
			}
			checks = append(checks, fmt.Sprintf("%s (%s)", c.Check, where))
		}
		stage := row.Stage
		if len(row.Checks) == 0 {
			stage, checks = "Uncovered", []string{"-"}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", row.Control, stage, strings.Join(checks, "; "), row.Title)
	}
	_ = tw.Flush()
	var counts []string
	for _, stage := range []string{StageEnforced, StageAudited, StageCreated, StageDisabled, StageNotInstalled} {
		if n := rep.ByStage[stage]; n > 0 {
			counts = append(counts, fmt.Sprintf("%d %s", n, stage))
		}
	}
	if rep.Uncovered > 0 {
		counts = append(counts, fmt.Sprintf("%d Uncovered", rep.Uncovered))
	}
	cliutil.Fprintln(out, fmt.Sprintf("\n%d controls: %s", len(rep.Controls), strings.Join(counts, ", ")))
}
