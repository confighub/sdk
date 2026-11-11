// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

// Enforcement: where each rule stands between being written and being enforced.
// A rule installs in audit, warning on what fails it; its warnings are worked
// down by fixing configuration or adding exceptions; then it is enforced, and
// what still fails it is blocked. The stage is computed from the Trigger and
// the Spaces and Targets that select it. Only what cannot be computed -- how
// urgent the rule is, when it is due to be enforced, when it was -- is stored,
// as annotations on the Trigger.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/cliutil"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"

	"github.com/confighub/sdk/managerkit"
)

// The stages of a rule's enforcement.
const (
	// StageCreated is a rule no Space or Target selects yet: it checks nothing.
	StageCreated = "Created"
	// StageAudited is a selected rule that warns.
	StageAudited = "Audited"
	// StageEnforced is a selected rule that blocks.
	StageEnforced = "Enforced"
	// StageDisabled is a disabled rule, which checks nothing wherever it is selected.
	StageDisabled = "Disabled"
)

// Stage is the enforcement stage of a Trigger that selected says some Space or
// Target selects.
func Stage(trigger *goclientnew.Trigger, selected bool) string {
	switch {
	case trigger.Disabled:
		return StageDisabled
	case !selected:
		return StageCreated
	case trigger.Warn:
		return StageAudited
	default:
		return StageEnforced
	}
}

// EnforcementRow is where one rule stands.
type EnforcementRow struct {
	Rule       string `json:"rule"`
	Group      string `json:"group,omitempty"`
	Stage      string `json:"stage"`
	Priority   string `json:"priority"`
	EnforcedBy string `json:"enforcedBy,omitempty"`
	EnforcedAt string `json:"enforcedAt,omitempty"`
	// Spaces and Targets count what selects the rule.
	Spaces  int `json:"spaces"`
	Targets int `json:"targets"`
	// Passed, Warnings and Errors count the Units by the rule's last verdict
	// on their head Revision.
	Passed   int `json:"passed"`
	Warnings int `json:"warnings"`
	Errors   int `json:"errors"`
}

// Enforcement reports where each validating Trigger in policySpace stands, by rule.
// rules narrows it to some; empty means all.
func Enforcement(ctx context.Context, client *cubapi.Client, policySpace string, rules []string) ([]EnforcementRow, error) {
	triggers, err := cubapi.ListTriggers(ctx, client,
		cubapi.Where{}.Eq("Space.Slug", policySpace).And("Validating = true"),
		cubapi.ListOpts{Select: "TriggerID,SpaceID,Slug,ToolchainType,Labels,Annotations,Warn,Disabled"})
	if err != nil {
		return nil, fmt.Errorf("list triggers in %s: %w", policySpace, err)
	}
	spaces, err := cubapi.ListSpaces(ctx, client, cubapi.Where{}, cubapi.ListOpts{Select: "SpaceID,TriggerIDs"})
	if err != nil {
		return nil, fmt.Errorf("list spaces: %w", err)
	}
	targets, err := cubapi.ListTargets(ctx, client, cubapi.Where{}, cubapi.ListOpts{Select: "TargetID,SpaceID,TriggerIDs"})
	if err != nil {
		return nil, fmt.Errorf("list targets: %w", err)
	}
	units, err := cubapi.ListUnits(ctx, client, cubapi.Where{},
		cubapi.ListOpts{Select: "UnitID,SpaceID,ToolchainType,ValidationErrors,ValidationWarnings,ValidationTriggerIDs"})
	if err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}

	// A Space selects its own Triggers unless told otherwise, so the policy Space
	// selects every rule in it. That is not a enforcement: a rule's own Space counts
	// only when it holds Units the rule could check.
	toolchains := map[goclientnew.UUID]map[string]bool{}
	for _, eu := range units {
		if eu.Unit == nil {
			continue
		}
		if toolchains[eu.Unit.SpaceID] == nil {
			toolchains[eu.Unit.SpaceID] = map[string]bool{}
		}
		toolchains[eu.Unit.SpaceID][eu.Unit.ToolchainType] = true
	}
	triggerByID := map[goclientnew.UUID]*goclientnew.Trigger{}

	rows := map[goclientnew.UUID]*EnforcementRow{}
	for _, et := range triggers {
		t := et.Trigger
		if t == nil || (len(rules) > 0 && !slices.Contains(rules, t.Slug)) {
			continue
		}
		triggerByID[t.TriggerID] = t
		// Stage is filled in once the selections are counted.
		rows[t.TriggerID] = &EnforcementRow{
			Rule:       t.Slug,
			Group:      t.Labels[GroupLabel],
			Priority:   priorityOf(t.Annotations),
			EnforcedBy: t.Annotations[constants.TriggerEnforcedByAnnotation],
			EnforcedAt: t.Annotations[constants.TriggerEnforcedAtAnnotation],
		}
	}
	for _, es := range spaces {
		if es.Space == nil {
			continue
		}
		for _, id := range es.Space.TriggerIDs {
			r, t := rows[id], triggerByID[id]
			if r == nil || (t.SpaceID == es.Space.SpaceID && !toolchains[t.SpaceID][t.ToolchainType]) {
				continue
			}
			r.Spaces++
		}
	}
	for _, et := range targets {
		if et.Target == nil {
			continue
		}
		for _, id := range et.Target.TriggerIDs {
			if r := rows[id]; r != nil {
				r.Targets++
			}
		}
	}
	for _, eu := range units {
		if eu.Unit == nil {
			continue
		}
		for id, verdict := range unitVerdicts(eu.Unit) {
			r := rows[id]
			if r == nil {
				continue
			}
			switch verdict {
			case verdictError:
				r.Errors++
			case verdictWarning:
				r.Warnings++
			default:
				r.Passed++
			}
		}
	}

	out := make([]EnforcementRow, 0, len(rows))
	for _, et := range triggers {
		t := et.Trigger
		if t == nil || rows[t.TriggerID] == nil {
			continue
		}
		r := rows[t.TriggerID]
		r.Stage = Stage(t, r.Spaces+r.Targets > 0)
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := stageOrder(out[i].Stage), stageOrder(out[j].Stage); a != b {
			return a < b
		}
		return out[i].Rule < out[j].Rule
	})
	return out, nil
}

func priorityOf(annotations map[string]string) string {
	if p, err := cubapi.ParseTriggerPriority(annotations[constants.TriggerPriorityAnnotation]); err == nil && p != "" {
		return p
	}
	return cubapi.TriggerPriorityUnspecified
}

func stageOrder(stage string) int {
	return slices.Index([]string{StageCreated, StageAudited, StageEnforced, StageDisabled}, stage)
}

type verdict int

const (
	verdictPassed verdict = iota
	verdictWarning
	verdictError
)

// unitVerdicts is the last verdict of each Trigger that ran on a Unit's head
// Revision. A Trigger that has been renamed can have passed under its old gate
// name and failed under the new one, so a failure under any of its names wins.
func unitVerdicts(u *goclientnew.Unit) map[goclientnew.UUID]verdict {
	out := map[goclientnew.UUID]verdict{}
	for gate, id := range u.ValidationTriggerIDs {
		v := verdictPassed
		if u.ValidationErrors[gate] {
			v = verdictError
		} else if u.ValidationWarnings[gate] {
			v = verdictWarning
		}
		if v >= out[id] {
			out[id] = v
		}
	}
	return out
}

// PrintEnforcement renders the enforcement report as a table.
func PrintEnforcement(cmd *cobra.Command, rows []EnforcementRow) {
	out := cmd.OutOrStdout()
	if len(rows) == 0 {
		cliutil.Fprintln(out, "No guardrail rules.")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "RULE\tGROUP\tSTAGE\tPRIORITY\tENFORCED-BY\tENFORCED-AT\tSPACES\tTARGETS\tPASSED\tWARNINGS\tERRORS")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\n", r.Rule, r.Group, r.Stage, r.Priority,
			dateOf(r.EnforcedBy), dateOf(r.EnforcedAt), r.Spaces, r.Targets, r.Passed, r.Warnings, r.Errors)
	}
	_ = tw.Flush()
}

// dateOf shortens an RFC 3339 time to its date for a table, and leaves anything
// else as it is.
func dateOf(s string) string {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Format(time.DateOnly)
	}
	return s
}

// EnforcementUpdate is what `guardrails update` changes on each rule. A nil field is
// left alone; an empty string removes the annotation.
type EnforcementUpdate struct {
	Priority   *string
	EnforcedBy *string
	// Enforce makes the rule block (Warn false) when true, and warn when false.
	Enforce *bool
}

// EnforcementChange is one rule an update changes.
type EnforcementChange struct {
	Rule string `json:"rule"`
	// Fields are the changed fields, as "<field>: <old> -> <new>".
	Fields    []string `json:"fields"`
	Committed bool     `json:"committed"`
	// Reevaluated is how many Units the rule was run on again, those it failed, so that each
	// failure moved between the Unit's warnings and the gates that block its release.
	Reevaluated int `json:"reevaluated,omitempty"`

	spaceID, triggerID goclientnew.UUID
	toolchain          string
	patch              map[string]any
}

// PlanEnforcementUpdate works out what update changes on rules in policySpace. It
// writes nothing. A rule the update would leave as it is is not in the result.
func PlanEnforcementUpdate(ctx context.Context, client *cubapi.Client, policySpace string, rules []string, update EnforcementUpdate, now time.Time) ([]EnforcementChange, error) {
	var changes []EnforcementChange
	for _, rule := range rules {
		et, err := cubapi.ResolveTrigger(ctx, client, cubapi.NewRef(policySpace, rule),
			cubapi.ResolveOpts{Select: "TriggerID,SpaceID,Slug,ToolchainType,Annotations,Warn,Validating"})
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", rule, err)
		}
		t := et.Trigger
		if !t.Validating {
			return nil, fmt.Errorf("rule %s: trigger %s/%s is not validating", rule, policySpace, rule)
		}
		if c, ok := planEnforcementChange(rule, t, update, now); ok {
			changes = append(changes, c)
		}
	}
	return changes, nil
}

// planEnforcementChange is what update changes on one rule's Trigger, and false when it would
// leave the Trigger as it is.
func planEnforcementChange(rule string, t *goclientnew.Trigger, update EnforcementUpdate, now time.Time) (EnforcementChange, bool) {
	c := EnforcementChange{Rule: rule, spaceID: t.SpaceID, triggerID: t.TriggerID, toolchain: t.ToolchainType, patch: map[string]any{}}
	annotations := map[string]any{}
	setAnnotation := func(key, value string) {
		old := t.Annotations[key]
		if old == value {
			return
		}
		if value == "" {
			annotations[key] = nil
		} else {
			annotations[key] = value
		}
		c.Fields = append(c.Fields, fmt.Sprintf("%s: %s -> %s", key, orNone(old), orNone(value)))
	}
	if update.Priority != nil {
		setAnnotation(constants.TriggerPriorityAnnotation, *update.Priority)
	}
	if update.EnforcedBy != nil {
		setAnnotation(constants.TriggerEnforcedByAnnotation, *update.EnforcedBy)
	}
	if update.Enforce != nil && t.Warn == *update.Enforce {
		c.patch["Warn"] = !*update.Enforce
		c.Fields = append(c.Fields, fmt.Sprintf("Warn: %t -> %t", t.Warn, !*update.Enforce))
		if *update.Enforce {
			setAnnotation(constants.TriggerEnforcedAtAnnotation, cubapi.FormatEnforcementTime(now))
		} else {
			setAnnotation(constants.TriggerEnforcedAtAnnotation, "")
		}
	}
	if len(annotations) > 0 {
		c.patch["Annotations"] = annotations
	}
	return c, len(c.Fields) > 0
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// ExecuteEnforcementUpdate applies the changes PlanEnforcementUpdate worked out.
//
// A rule enforced or returned to audit is run again on the Units it fails, so that each failure
// moves between the Unit's warnings and the gates that block its release; a Unit it passes has
// neither, so nothing about it changes. Without the run the failing Units would keep their old
// verdicts until each of them next changed, since a change to a Trigger re-evaluates only the
// Units of its own Space.
func ExecuteEnforcementUpdate(ctx context.Context, client *cubapi.Client, changes []EnforcementChange) error {
	for i := range changes {
		c := &changes[i]
		body, err := json.Marshal(c.patch)
		if err != nil {
			return err
		}
		res, err := client.API.PatchTriggerWithBodyWithResponse(ctx, c.spaceID, c.triggerID,
			&goclientnew.PatchTriggerParams{}, mergePatchContentType, bytes.NewReader(body))
		if cubapi.IsAPIError(err, res) {
			return fmt.Errorf("update %s: %w", c.Rule, cubapi.InterpretErrorGeneric(err, res))
		}
		c.Committed = true
		if _, warnChanged := c.patch["Warn"]; warnChanged {
			if c.Reevaluated, err = reevaluateFailing(ctx, client, c.triggerID, c.toolchain); err != nil {
				return fmt.Errorf("re-evaluate %s: %w", c.Rule, err)
			}
		}
	}
	return nil
}

// evaluateChunk is how many Units one evaluation names, so a rule failing across a fleet makes
// several requests rather than one whose filter is too long.
const evaluateChunk = 100

// reevaluateFailing runs trigger again on the Units it fails, recording the new verdicts, and
// returns how many it ran on.
func reevaluateFailing(ctx context.Context, client *cubapi.Client, trigger goclientnew.UUID, toolchain string) (int, error) {
	units, err := cubapi.ListUnits(ctx, client, cubapi.Where{}.Eq("ToolchainType", toolchain),
		cubapi.ListOpts{Select: "UnitID,ValidationErrors,ValidationWarnings,ValidationTriggerIDs"})
	if err != nil {
		return 0, fmt.Errorf("list units: %w", err)
	}
	var failing []string
	for _, eu := range units {
		if eu.Unit != nil && unitVerdicts(eu.Unit)[trigger] != verdictPassed {
			failing = append(failing, "'"+eu.Unit.UnitID.String()+"'")
		}
	}
	var failed []string
	for start := 0; start < len(failing); start += evaluateChunk {
		chunk := failing[start:min(start+evaluateChunk, len(failing))]
		res, err := cubapi.EvaluateTriggers(ctx, client, []goclientnew.UUID{trigger}, cubapi.Selector{
			Where:         "UnitID IN (" + strings.Join(chunk, ", ") + ")",
			ToolchainType: toolchain,
		})
		if err != nil {
			return 0, err
		}
		for _, o := range res.Failed() {
			failed = append(failed, o.SpaceSlug+"/"+o.UnitSlug+": "+o.Error)
		}
	}
	if len(failed) > 0 {
		return len(failing), fmt.Errorf("not evaluated on %d Unit(s): %s", len(failed), strings.Join(failed, "; "))
	}
	return len(failing), nil
}

// UpdateCmd is `guardrails update`: a rule's priority, the date it is due to be
// enforced, and whether it is.
func UpdateCmd(preflight Preflight) *cobra.Command {
	var policySpace, output, priority, enforcedBy string
	var enforce, audit, commit bool
	cmd := &cobra.Command{
		Use:   "update <rule>... [--priority <p>] [--enforced-by <date>] [--enforce | --audit]",
		Short: "Set a guardrail rule's priority and enforcement date, or enforce it (dry-run by default)",
		Long: `update changes how a guardrail rule is rolled out:

  --priority      how urgent it is: High, Medium or Low; - removes it
                  (the ` + constants.TriggerPriorityAnnotation + ` annotation)
  --enforced-by   when it is due to be enforced: a date such as 2026-12-31, or an
                  RFC 3339 time; - removes it (the ` + constants.TriggerEnforcedByAnnotation + ` annotation)
  --enforce       make it block: what fails it gets ValidationErrors rather than
                  ValidationWarnings, and is not released. Records when in the
                  ` + constants.TriggerEnforcedAtAnnotation + ` annotation
  --audit         make it warn again, and remove ` + constants.TriggerEnforcedAtAnnotation + `

Enforcing a rule, or returning it to audit, runs it again on the Units it fails,
so what it warned on becomes blocked, or the reverse. 'guardrails status --by-rule' shows how many
Units each rule still warns on, which is what enforcing it would block.

Dry-run by default: it prints what would change. Re-run with --commit to apply.`,
		Example: "  guardrails update workload-runs-nonroot --priority High --enforced-by 2026-12-31\n" +
			"  guardrails update workload-runs-nonroot --enforce --commit",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var update EnforcementUpdate
			// An empty value is a flag not given; - removes the annotation.
			if priority != "" {
				p, err := cubapi.ParseTriggerPriority(priority)
				if err != nil {
					return err
				}
				update.Priority = &p
			}
			if enforcedBy != "" {
				t, err := cubapi.ParseEnforcementTime(enforcedBy)
				if err != nil {
					return err
				}
				update.EnforcedBy = &t
			}
			switch {
			case enforce && audit:
				return fmt.Errorf("--enforce and --audit are mutually exclusive")
			case enforce || audit:
				update.Enforce = &enforce
			}
			if update == (EnforcementUpdate{}) {
				return fmt.Errorf("nothing to update: give --priority, --enforced-by, --enforce or --audit")
			}
			client, err := preflight(cmd.Context())
			if err != nil {
				return err
			}
			changes, err := PlanEnforcementUpdate(cmd.Context(), client, policySpace, args, update, time.Now())
			if err != nil {
				return err
			}
			if commit {
				if err := ExecuteEnforcementUpdate(cmd.Context(), client, changes); err != nil {
					return err
				}
			}
			if changes == nil {
				changes = []EnforcementChange{}
			}
			if done, err := managerkit.Render(cmd.OutOrStdout(), output, changes); done || err != nil {
				return err
			}
			printEnforcementChanges(cmd, changes, commit)
			return nil
		},
	}
	cmd.Flags().StringVar(&policySpace, "policy-space", DefaultPolicySpace, "Space holding the guardrail Triggers")
	cmd.Flags().StringVar(&priority, "priority", "", "High, Medium or Low; - removes it")
	cmd.Flags().StringVar(&enforcedBy, "enforced-by", "", "when the rule is due to be enforced: a date (2026-12-31) or RFC 3339; - removes it")
	cmd.Flags().BoolVar(&enforce, "enforce", false, "make the rule block, and record when")
	cmd.Flags().BoolVar(&audit, "audit", false, "make the rule warn rather than block")
	cmd.Flags().BoolVar(&commit, "commit", false, "apply the change (default is dry-run)")
	managerkit.AddOutputFlag(cmd, &output)
	return cmd
}

func printEnforcementChanges(cmd *cobra.Command, changes []EnforcementChange, commit bool) {
	out := cmd.OutOrStdout()
	if len(changes) == 0 {
		cliutil.Fprintln(out, "Nothing to change.")
		return
	}
	verb := "Would change"
	if commit {
		verb = "Changed"
	}
	for _, c := range changes {
		cliutil.Fprintln(out, fmt.Sprintf("%s %s: %s", verb, c.Rule, strings.Join(c.Fields, "; ")))
		if c.Reevaluated > 0 {
			cliutil.Fprintln(out, fmt.Sprintf("  ran it again on the %d Unit(s) it fails", c.Reevaluated))
		}
	}
	if !commit {
		cliutil.Fprintln(out, "\nNothing changed. Re-run with --commit to apply.")
	}
}
