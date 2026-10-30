// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

// The command surface over a Pack. pack.go and query.go are API-shaped and have
// no CLI in them; this file is the CLI half.

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/cliutil"
	"github.com/confighub/sdk/core/cubapi"

	"github.com/confighub/sdk/managerkit"
)

// Preflight verifies a tool's ConfigHub session and returns the client. Each
// tool has one; the commands take it rather than reaching for a global.
type Preflight func(context.Context) (*cubapi.Client, error)

// InstallCmd is `guardrails install` for this pack.
func (p Pack) InstallCmd(preflight Preflight) *cobra.Command {
	var policySpace, output string
	var scope Scope
	var commit bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the guardrail group and add it to in-scope Spaces (dry-run by default)",
		Long: `install creates the policy Space and the Warn=true guardrail Triggers, labeled
` + GroupLabel + `: ` + p.Group + `, then adds the group to each in-scope Space's WhereTrigger.

A Space opts into guardrails by listing groups, from one policy Space:

  ` + p.exampleSelection() + `

so installing another tool's group adds its rules only to the Spaces that
install wires, and a Trigger labeled into a group anywhere else is never
selected. Rules that check only released Units are labeled ` + AppliedToLabel + `: ` + AppliedToTarget + `
and selected by Targets instead.

Each rule skips the resources annotated ` + ExceptionAnnotationPrefix + `<rule>,
whose value says why the resource is excepted.

Dry-run by default: it prints the plan and changes nothing. Re-run with --commit
to apply. Spaces that already select Triggers another way are reported, not
modified. Narrow which Spaces get wired with --where-space. Re-running updates
the Triggers to this version of the rules.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := preflight(cmd.Context())
			if err != nil {
				return err
			}
			plan, err := p.BuildPlan(cmd.Context(), client, policySpace, scope)
			if err != nil {
				return err
			}
			return p.Report(cmd, client, plan, commit, output)
		},
	}
	p.BindInstallFlags(cmd, &policySpace, &commit, &output)
	cmd.Flags().StringVar(&scope.WhereSpace, "where-space", "", "ConfigHub filter over Spaces to narrow which Spaces get wired")
	if p.hasTargetRules() {
		cmd.Flags().StringVar(&scope.WhereTarget, "where-target", "", "ConfigHub filter over Targets to narrow which Targets get wired")
	}
	return cmd
}

// BindInstallFlags binds the flags every install command shares, for a tool
// that builds its own install over PlanFor.
func (p Pack) BindInstallFlags(cmd *cobra.Command, policySpace *string, commit *bool, output *string) {
	cmd.Flags().StringVar(policySpace, "policy-space", DefaultPolicySpace,
		"Space holding the guardrail Triggers; every tool defaults to the same one")
	cmd.Flags().BoolVar(commit, "commit", false, "apply the plan (default is dry-run)")
	managerkit.AddOutputFlag(cmd, output)
}

// Report applies plan when commit is set, then renders it.
func (p Pack) Report(cmd *cobra.Command, client *cubapi.Client, plan Plan, commit bool, output string) error {
	if commit {
		out := managerkit.ProgressWriter(cmd, output)
		if err := p.Execute(cmd.Context(), client, plan.PolicySpace, plan,
			func(line string) { cliutil.Fprintln(out, line) }); err != nil {
			return err
		}
		plan.Committed = true
	}
	if done, err := managerkit.Render(cmd.OutOrStdout(), output, plan); done || err != nil {
		return err
	}
	PrintPlan(cmd, plan)
	return nil
}

// exampleSelection is the selection of a Space that has opted into this group
// alone, for help text.
func (p Pack) exampleSelection() string {
	where, _ := Selection{PolicySpace: DefaultPolicySpace, Groups: []string{p.Group}}.Where()
	return where
}

// StatusCmd is `guardrails status`: which Units the Triggers have marked.
func (p Pack) StatusCmd(preflight Preflight) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Units carrying ValidationWarnings or ValidationErrors",
		Long: `status lists the Units a Trigger has marked — ValidationWarnings from an advisory
rule, ValidationErrors from one promoted to blocking.

It reports what any Trigger attached, not only this pack's: a Unit does not
record which rule marked it, and reporting only this pack's rules would hide a
Unit that is blocked for some other reason.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := preflight(cmd.Context())
			if err != nil {
				return err
			}
			rows, err := Status(cmd.Context(), client)
			if err != nil {
				return err
			}
			if done, err := managerkit.Render(cmd.OutOrStdout(), output, rows); done || err != nil {
				return err
			}
			PrintStatus(cmd, rows)
			return nil
		},
	}
	managerkit.AddOutputFlag(cmd, &output)
	return cmd
}

// PrintPlan renders an install plan as a table.
func PrintPlan(cmd *cobra.Command, plan Plan) {
	out := cmd.OutOrStdout()
	verb := "Plan (dry-run)"
	if plan.Committed {
		verb = "Applied"
	}
	cliutil.Fprintln(out, fmt.Sprintf("%s — guardrail group %q in policy Space %q", verb, plan.Group, plan.PolicySpace))
	cliutil.Fprintln(out, "  triggers: "+strings.Join(plan.Triggers, ", "))
	if len(plan.Wire) == 0 {
		cliutil.Fprintln(out, "  to wire: none")
	} else {
		cliutil.Fprintln(out, fmt.Sprintf("  to wire (%d): %s", len(plan.Wire), strings.Join(names(plan.Wire), ", ")))
	}
	if len(plan.AlreadyWired) > 0 {
		cliutil.Fprintln(out, fmt.Sprintf("  already wired (%d): %s", len(plan.AlreadyWired), strings.Join(names(plan.AlreadyWired), ", ")))
	}
	if len(plan.Skipped) > 0 {
		tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "  SKIPPED\tREASON")
		for _, s := range plan.Skipped {
			fmt.Fprintf(tw, "  %s\t%s\n", s.Name(), s.Reason)
		}
		_ = tw.Flush()
	}
	if !plan.Committed {
		cliutil.Fprintln(out, "\nNothing changed. Re-run with --commit to apply.")
	}
}

func names(ws []Wiring) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.Name()
	}
	return out
}

// PrintStatus renders the marked Units as a table.
func PrintStatus(cmd *cobra.Command, rows []StatusRow) {
	out := cmd.OutOrStdout()
	if len(rows) == 0 {
		cliutil.Fprintln(out, "No Units carry ValidationWarnings or ValidationErrors.")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "SPACE\tUNIT\tWARNINGS\tERRORS")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\n", r.Space, r.Unit, r.Warnings, r.Errors)
	}
	_ = tw.Flush()
	cliutil.Fprintln(out, fmt.Sprintf("\n%d Unit(s) marked.", len(rows)))
}
