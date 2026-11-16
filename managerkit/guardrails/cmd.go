// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

// The command surface over a Pack. pack.go and query.go are API-shaped and have
// no CLI in them; this file is the CLI half.

import (
	"context"
	"fmt"
	"slices"
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
	var policySpace, output, priority, enforcedBy string
	var scope Scope
	var commit bool
	var sel CheckSelection
	var params []string
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
whose value says why the resource is excepted. 'guardrails exception' adds,
removes and lists them.

Dry-run by default: it prints the plan and changes nothing. Re-run with --commit
to apply. Spaces that already select Triggers another way are reported, not
modified. Narrow which Spaces get wired with --where-space. Re-running updates
the Triggers to this version of the rules, and leaves a rule promoted with
--unwarn blocking.

--priority and --enforced-by record how urgent the rules are and when they are
due to be enforced; 'guardrails status --by-rule' reports them, with where each rule
stands, and 'guardrails update' changes them one rule at a time.

--policy-unit <space>/<unit> installs a rule running the ValidatingAdmissionPolicies
of a Unit of your own, as vet-validating-admission-policy, so the policies checked
before release are the ones your clusters enforce at admission. The rule is named
after the Unit, and its policies see the resources their bindings in the Unit
select; a paramRef is looked up in the same Unit. Installing again without the
flag leaves the rule as it is.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := preflight(cmd.Context())
			if err != nil {
				return err
			}
			if sel.Params, err = parseParams(params); err != nil {
				return err
			}
			plan, err := p.BuildPlan(cmd.Context(), client, policySpace, scope, sel)
			if err != nil {
				return err
			}
			if plan.Priority, err = cubapi.ParseTriggerPriority(priority); err != nil {
				return err
			}
			if plan.EnforcedBy, err = cubapi.ParseEnforcementTime(enforcedBy); err != nil {
				return err
			}
			return p.Report(cmd, client, plan, commit, output)
		},
	}
	p.BindInstallFlags(cmd, &policySpace, &commit, &output)
	cmd.Flags().StringVar(&priority, "priority", "", "priority of the group's rules, recorded on each Trigger: High, Medium or Low")
	cmd.Flags().StringVar(&enforcedBy, "enforced-by", "", "when the group's rules are due to be enforced, recorded on each Trigger: a date (2026-12-31) or RFC 3339")
	cmd.Flags().StringVar(&scope.WhereSpace, "where-space", "", "ConfigHub filter over Spaces to narrow which Spaces get wired")
	cmd.Flags().StringSliceVar(&sel.PolicyUnits, "policy-unit", nil, "a Unit of your own ValidatingAdmissionPolicies, <space>/<unit>, to install a rule running; repeat or comma-separate")
	if p.hasTargetRules() {
		cmd.Flags().StringVar(&scope.WhereTarget, "where-target", "", "ConfigHub filter over Targets to narrow which Targets get wired")
	}
	if len(p.Checks) > 0 {
		cmd.Long += `

The pack's checks are grouped into rules, each a Trigger running the checks it
can run together; a finding names its check. Installing adds the pack's default
checks, and keeps every check already installed. --check adds an optional check,
and --all every one. --param <check>.<name>=<value> gives a check a parameter; a
check missing a required one is left out until it has it, and the plan says so.`
		cmd.Flags().StringSliceVar(&sel.Checks, "check", nil, "optional checks to install, by name; repeat or comma-separate")
		cmd.Flags().BoolVar(&sel.All, "all", false, "install every optional check")
		cmd.Flags().StringArrayVar(&params, "param", nil, "a check's parameter, <check>.<name>=<value>; repeat for each")
	}
	if len(p.Libraries) > 0 {
		cmd.Long += `

Some checks run ValidatingAdmissionPolicies the pack adopts from a library, such as
Kubescape's. Install writes each library into the policy Space as a Unit,
guardrails-` + p.Group + `-<library>, replaced with this version's policies every time,
and the library's parameters as a Unit, guardrails-<library>-params, created once
with the library's defaults and never overwritten: edit it to set them, such as
the image registries allowed.`
	}
	return cmd
}

// parseParams reads --param values, <check>.<name>=<value>.
func parseParams(params []string) (map[string]string, error) {
	if len(params) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for _, p := range params {
		key, value, ok := strings.Cut(p, "=")
		if !ok || !strings.Contains(key, ".") {
			return nil, fmt.Errorf("--param %q: write <check>.<name>=<value>", p)
		}
		out[key] = value
	}
	return out, nil
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

// StatusCmd is `guardrails status`: which Units the Triggers have marked, or,
// with --by-rule, where each rule stands.
func (p Pack) StatusCmd(preflight Preflight) *cobra.Command {
	return StatusCmd(preflight, nil, nil)
}

// StatusCmd is `guardrails status` over the checks of packs. Given frameworks, it takes
// --framework, which reports how far the installed rules go towards one of them.
func StatusCmd(preflight Preflight, packs []Pack, frameworks []Framework) *cobra.Command {
	var output, policySpace, only, framework string
	var byRule bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Units carrying ValidationWarnings or ValidationErrors, or with --by-rule, each rule's enforcement",
		Long: `status lists the Units a Trigger has marked — ValidationWarnings from an advisory
rule, ValidationErrors from one promoted to blocking. It reports what any Trigger
attached, not only guardrails: a Unit blocked for some other reason is listed
too.

With --by-rule, it reports each guardrail rule in the policy Space instead: its
stage, its priority, when it is due to be enforced and when it was, what
selects it, and how many Units it passed, warned on and blocked:

  ` + StageCreated + `    no Space or Target selects it yet
  ` + StageAudited + `   selected, and warns: what fails it gets ValidationWarnings
  ` + StageEnforced + `   selected, and blocks: what fails it gets ValidationErrors
  ` + StageDisabled + `   disabled, so it checks nothing

A rule is enforced in steps: installing it selects it in audit; its warnings
are worked down by fixing configuration or adding exceptions; then
'guardrails update --enforce' enforces it. Priority, and the date the rule is
due to be enforced, are set with 'guardrails update' too.`,
		Example: "  guardrails status -o table\n" +
			"  guardrails status --by-rule -o table\n" +
			"  guardrails status --by-rule --rule workload-runs-nonroot -o json",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if byRule && framework != "" {
				return fmt.Errorf("--by-rule and --framework are mutually exclusive")
			}
			if !byRule && only != "" {
				return fmt.Errorf("--rule goes with --by-rule")
			}
			if !byRule && framework == "" && cmd.Flags().Changed("policy-space") {
				return fmt.Errorf("--policy-space goes with --by-rule or --framework")
			}
			var f Framework
			if framework != "" {
				i := slices.IndexFunc(frameworks, func(f Framework) bool { return f.Name == framework })
				if i < 0 {
					return fmt.Errorf("unknown framework %q: want one of %s", framework, strings.Join(frameworkNames(frameworks), ", "))
				}
				f = frameworks[i]
			}
			client, err := preflight(cmd.Context())
			if err != nil {
				return err
			}
			if framework != "" {
				rep, err := FrameworkStatus(cmd.Context(), client, policySpace, f, packs)
				if err != nil {
					return err
				}
				if done, err := managerkit.Render(cmd.OutOrStdout(), output, rep); done || err != nil {
					return err
				}
				PrintFramework(cmd, rep)
				return nil
			}
			if byRule {
				var rules []string
				for _, r := range strings.Split(only, ",") {
					if r = strings.TrimSpace(r); r != "" {
						rules = append(rules, r)
					}
				}
				rows, err := Enforcement(cmd.Context(), client, policySpace, rules)
				if err != nil {
					return err
				}
				if done, err := managerkit.Render(cmd.OutOrStdout(), output, rows); done || err != nil {
					return err
				}
				PrintEnforcement(cmd, rows)
				return nil
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
	cmd.Flags().BoolVar(&byRule, "by-rule", false, "report each guardrail rule's enforcement rather than the marked Units")
	cmd.Flags().StringVar(&policySpace, "policy-space", DefaultPolicySpace, "with --by-rule or --framework, the Space holding the guardrail Triggers")
	cmd.Flags().StringVar(&only, "rule", "", "with --by-rule, only these rules, comma-separated")
	if len(frameworks) > 0 {
		cmd.Short += ", or with --framework, how far they go towards a framework"
		cmd.Long += `

With --framework, it reports each control of a framework, such as the CIS
Kubernetes Benchmark: the checks that correspond to it, the rule each runs in and
where that rule stands, and how far the control has got, which is as far as the
furthest of its checks. A control no check covers is Uncovered. The frameworks:
` + wrapList(frameworkNames(frameworks))
		cmd.Example += "\n  guardrails status --framework " + frameworks[0].Name + " -o table"
		cmd.Flags().StringVar(&framework, "framework", "", "report each control of this framework rather than the marked Units")
	}
	managerkit.AddOutputFlag(cmd, &output)
	return cmd
}

func frameworkNames(frameworks []Framework) []string {
	names := make([]string, len(frameworks))
	for i, f := range frameworks {
		names[i] = f.Name
	}
	return names
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
	if len(plan.PolicyUnits) > 0 {
		cliutil.Fprintln(out, "  policy units: "+strings.Join(plan.PolicyUnits, ", "))
	}
	if plan.NarrowPolicySpace {
		cliutil.Fprintln(out, fmt.Sprintf("  policy space selection: %s's own Triggers in no group (WhereTrigger \"SpaceID = '<id>' AND Labels.%s IS NULL\"), so the rules skip the policy units", plan.PolicySpace, GroupLabel))
	}
	if len(plan.Wire) == 0 {
		cliutil.Fprintln(out, "  to wire: none")
	} else {
		cliutil.Fprintln(out, fmt.Sprintf("  to wire (%d): %s", len(plan.Wire), strings.Join(names(plan.Wire), ", ")))
	}
	if len(plan.AlreadyWired) > 0 {
		cliutil.Fprintln(out, fmt.Sprintf("  already wired (%d): %s", len(plan.AlreadyWired), strings.Join(names(plan.AlreadyWired), ", ")))
	}
	if len(plan.SkippedChecks) > 0 {
		tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "  CHECK LEFT OUT\tREASON")
		for _, s := range plan.SkippedChecks {
			fmt.Fprintf(tw, "  %s\t%s\n", s.Check, s.Reason)
		}
		_ = tw.Flush()
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
