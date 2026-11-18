// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

// Temporary exceptions: a rule is enforced sooner when what fails it today is
// excepted, with a reason marking each exception as one to fix, so that nothing
// new can fail it while the old failures are worked down. They are ordinary
// exceptions whose reason starts with TemporaryExceptionPrefix, and are listed
// apart from the exceptions meant to stay.

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/cliutil"
	"github.com/confighub/sdk/core/cubapi"
	api "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"

	"github.com/confighub/sdk/managerkit"
	"github.com/confighub/sdk/managerkit/write"
)

// TemporaryExceptionPrefix starts the reason of a temporary exception: one
// recorded for a failure that is to be fixed, so the rule could be enforced
// without waiting for it. The UI reads the same prefix.
const TemporaryExceptionPrefix = "ToBeFixed"

// TemporaryReason is the reason a temporary exception records: the prefix, then
// why the failure is not fixed yet.
func TemporaryReason(reason string) string {
	return TemporaryExceptionPrefix + ": " + strings.TrimSpace(reason)
}

// IsTemporary reports whether an exception's reason marks it as temporary.
func IsTemporary(reason string) bool {
	rest, ok := strings.CutPrefix(strings.TrimSpace(reason), TemporaryExceptionPrefix)
	return ok && (rest == "" || rest[0] == ':' || rest[0] == ' ')
}

// failedResource is a resource a rule's results name as failing. Type is empty
// when the results name it only by name.
type failedResource struct {
	resourceType, resourceName string
}

// vet-cel reports a resource whose expression returned false only in a detail
// line, not as a FailedAttribute, so the line is read for its name as well.
var detailResource = regexp.MustCompile(`^resource (\S+) failed validation`)

// failedResourcesOf are the resources results name as failing: those of their
// FailedAttributes, and those their details name.
func failedResourcesOf(results []goclientnew.ValidationResult) []failedResource {
	var out []failedResource
	add := func(r failedResource) {
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	for _, result := range results {
		if result.Passed {
			continue
		}
		if result.FailedAttributes != nil {
			for _, attr := range *result.FailedAttributes {
				if attr.ResourceName != "" {
					add(failedResource{resourceType: attr.ResourceType, resourceName: attr.ResourceName})
				}
			}
		}
		for _, detail := range result.Details {
			if m := detailResource.FindStringSubmatch(detail); m != nil &&
				!slices.ContainsFunc(out, func(r failedResource) bool { return sameName(r.resourceName, m[1]) }) {
				add(failedResource{resourceName: m[1]})
			}
		}
	}
	return out
}

// sameName compares resource names as functions and the resource list write
// them: a cluster-scoped resource's may or may not start with a slash.
func sameName(a, b string) bool {
	return strings.TrimPrefix(a, "/") == strings.TrimPrefix(b, "/")
}

// NotExceptedKind is why a failing Unit is excepted from nothing, as a kind
// that Units can be counted by. The UI names the same kinds.
type NotExceptedKind string

const (
	// NotExceptedUnnamed: its results name no resource, and it holds several.
	NotExceptedUnnamed NotExceptedKind = "Unnamed"
	// NotExceptedNotHeld: its results name a resource it does not hold.
	NotExceptedNotHeld NotExceptedKind = "NotHeld"
	// NotExceptedAmbiguous: its results name a resource without its type, and
	// it holds more than one of that name.
	NotExceptedAmbiguous NotExceptedKind = "Ambiguous"
	// NotExceptedAlreadyExcepted: what it fails on is excepted already, so its
	// verdict is from before.
	NotExceptedAlreadyExcepted NotExceptedKind = "AlreadyExcepted"
)

// notExcepted is why a Unit is excepted from nothing: its kind, and the
// Unit's own particulars.
type notExcepted struct {
	kind NotExceptedKind
	why  string
}

// resourcesToExcept is which resources of one Unit to except from a rule it
// fails: the resources its results name, or, when they name none, its only
// resource. A Unit whose failures cannot all be placed is excepted from
// nothing, and why is returned instead, so that every Unit it reports is one
// that still fails. Resources already excepted are left out; when that leaves
// nothing, the Unit's verdict is from before the exception, and it is said so.
func resourcesToExcept(failed []failedResource, all []unitResource, key string) ([]unitResource, *notExcepted) {
	var chosen []unitResource
	if len(failed) == 0 {
		if len(all) != 1 {
			return nil, &notExcepted{NotExceptedUnnamed, fmt.Sprintf("its results do not name the resource that fails, and it holds %d", len(all))}
		}
		chosen = all
	}
	for _, f := range failed {
		var matches []unitResource
		for _, r := range all {
			if sameName(r.resourceName, f.resourceName) && (f.resourceType == "" || r.resourceType == f.resourceType) {
				matches = append(matches, r)
			}
		}
		switch len(matches) {
		case 0:
			named := f.resourceName
			if f.resourceType != "" {
				named = resourceRef(f.resourceType, f.resourceName)
			}
			return nil, &notExcepted{NotExceptedNotHeld, fmt.Sprintf("its results name %s, which it does not hold", named)}
		case 1:
			if !slices.ContainsFunc(chosen, func(r unitResource) bool { return r.ref() == matches[0].ref() }) {
				chosen = append(chosen, matches[0])
			}
		default:
			return nil, &notExcepted{NotExceptedAmbiguous, fmt.Sprintf("its results name %s without its type, and it holds %s", f.resourceName, strings.Join(refs(matches), " and "))}
		}
	}
	var out []unitResource
	for _, r := range chosen {
		if _, ok := r.annotations[key]; !ok {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil, &notExcepted{NotExceptedAlreadyExcepted, "already excepted; its verdict is from before, so evaluate the rule on it again"}
	}
	return out, nil
}

// TemporaryException is one resource to except, or excepted, temporarily.
type TemporaryException struct {
	Space        string `json:"space"`
	Unit         string `json:"unit"`
	ResourceType string `json:"resourceType"`
	ResourceName string `json:"resourceName"`
}

// Resource names the resource as --resource takes it.
func (e TemporaryException) Resource() string { return resourceRef(e.ResourceType, e.ResourceName) }

// NotExcepted is a Unit failing the rule that add-temporary leaves as it is.
type NotExcepted struct {
	Space string          `json:"space"`
	Unit  string          `json:"unit"`
	Kind  NotExceptedKind `json:"kind"`
	Why   string          `json:"why"`
}

// TemporaryPlan is what add-temporary excepts, Unit by Unit.
type TemporaryPlan struct {
	Rule string
	// Excepted are the resources to except, in order of Space, Unit and resource.
	Excepted []TemporaryException
	// NotExcepted are the failing Units no resource of which is excepted.
	NotExcepted []NotExcepted
	// groups are the Units sharing one set of resources to except, so that each
	// set is written with one request whatever the number of Units.
	groups []*temporaryGroup
}

type temporaryGroup struct {
	resources []unitResource
	unitIDs   []goclientnew.UUID
}

// unitsChunk is how many Units one request names, so that a rule failing
// across a fleet makes several requests rather than one whose filter is too
// long.
const unitsChunk = 100

// PlanTemporaryExceptions finds the resources failing rule -- in ValidationErrors
// or ValidationWarnings, from any of its Triggers -- in the Units where selects,
// every Unit the caller can view when it is empty.
func PlanTemporaryExceptions(ctx context.Context, client *cubapi.Client, rule string, groups []string, where string) (*TemporaryPlan, error) {
	triggers, err := cubapi.ListTriggers(ctx, client,
		cubapi.Where{}.Eq("Slug", rule).InStrings("Labels."+GroupLabel, groups).And("Validating = true"),
		cubapi.ListOpts{Select: "TriggerID,Slug"})
	if err != nil {
		return nil, fmt.Errorf("list the Triggers of %s: %w", rule, err)
	}
	ruleTriggers := map[goclientnew.UUID]bool{}
	for _, t := range triggers {
		if t.Trigger != nil {
			ruleTriggers[t.Trigger.TriggerID] = true
		}
	}
	if len(ruleTriggers) == 0 {
		return nil, fmt.Errorf("no validating Trigger of %s is installed; a check of a rule that runs several is a rule of its own only once separated, with 'guardrails update <rule> --separate %s'", rule, rule)
	}
	plan := &TemporaryPlan{Rule: rule}

	// A where expression is a conjunction, so failing with warnings and failing
	// with errors take one query each.
	base := cubapi.Where{}.Eq("ToolchainType", cubapi.DefaultToolchainType).And(where)
	failingGates := map[goclientnew.UUID][]string{}
	var failingIDs []goclientnew.UUID
	for _, cond := range []string{"LEN(ValidationWarnings) > 0", "LEN(ValidationErrors) > 0"} {
		units, err := cubapi.ListUnits(ctx, client, base.And(cond),
			cubapi.ListOpts{Select: "UnitID,ValidationWarnings,ValidationErrors,ValidationTriggerIDs"})
		if err != nil {
			return nil, fmt.Errorf("list the failing Units: %w", err)
		}
		for _, eu := range units {
			u := eu.Unit
			if u == nil || failingGates[u.UnitID] != nil {
				continue
			}
			var gates []string
			for gate, id := range u.ValidationTriggerIDs {
				if ruleTriggers[id] && (u.ValidationErrors[gate] || u.ValidationWarnings[gate]) {
					gates = append(gates, gate)
				}
			}
			if len(gates) > 0 {
				failingGates[u.UnitID] = gates
				failingIDs = append(failingIDs, u.UnitID)
			}
		}
	}

	key := ExceptionAnnotation(rule)
	byKey := map[string]*temporaryGroup{}
	for start := 0; start < len(failingIDs); start += unitsChunk {
		chunk := failingIDs[start:min(start+unitsChunk, len(failingIDs))]
		units, err := cubapi.ListUnits(ctx, client, cubapi.Where{}.In("UnitID", chunk),
			cubapi.ListOpts{Include: "SpaceID", Select: "UnitID,Slug,SpaceID,ValidationResults"})
		if err != nil {
			return nil, fmt.Errorf("read the failing Units' results: %w", err)
		}
		resources, err := cubapi.ListResources(ctx, client, cubapi.Where{}.In("UnitID", chunk), cubapi.ListOpts{})
		if err != nil {
			return nil, fmt.Errorf("list the failing Units' resources: %w", err)
		}
		byUnit := map[goclientnew.UUID][]unitResource{}
		for _, er := range resources {
			if r := er.Resource; r != nil {
				byUnit[r.UnitID] = append(byUnit[r.UnitID], unitResource{resourceType: r.ResourceType, resourceName: r.ResourceName, annotations: annotations(r.Data)})
			}
		}
		for _, eu := range units {
			u := eu.Unit
			if u == nil {
				continue
			}
			space := ""
			if eu.Space != nil {
				space = eu.Space.Slug
			}
			var results []goclientnew.ValidationResult
			for _, gate := range failingGates[u.UnitID] {
				results = append(results, u.ValidationResults[gate]...)
			}
			chosen, skip := resourcesToExcept(failedResourcesOf(results), byUnit[u.UnitID], key)
			if skip != nil {
				plan.NotExcepted = append(plan.NotExcepted, NotExcepted{Space: space, Unit: u.Slug, Kind: skip.kind, Why: skip.why})
				continue
			}
			sort.Slice(chosen, func(i, j int) bool { return chosen[i].ref() < chosen[j].ref() })
			groupKey := strings.Join(refs(chosen), "\n")
			g := byKey[groupKey]
			if g == nil {
				g = &temporaryGroup{resources: chosen}
				byKey[groupKey] = g
				plan.groups = append(plan.groups, g)
			}
			g.unitIDs = append(g.unitIDs, u.UnitID)
			for _, r := range chosen {
				plan.Excepted = append(plan.Excepted, TemporaryException{Space: space, Unit: u.Slug, ResourceType: r.resourceType, ResourceName: r.resourceName})
			}
		}
	}
	sort.Slice(plan.Excepted, func(i, j int) bool {
		a, b := plan.Excepted[i], plan.Excepted[j]
		if a.Space != b.Space {
			return a.Space < b.Space
		}
		if a.Unit != b.Unit {
			return a.Unit < b.Unit
		}
		return a.Resource() < b.Resource()
	})
	// By kind first, so the Units left out for one reason are read together.
	sort.Slice(plan.NotExcepted, func(i, j int) bool {
		a, b := plan.NotExcepted[i], plan.NotExcepted[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Space != b.Space {
			return a.Space < b.Space
		}
		return a.Unit < b.Unit
	})
	return plan, nil
}

// ExecuteTemporaryExceptions writes plan's exceptions, each with reason as its
// value, as change says: a dry run unless it has a description.
func ExecuteTemporaryExceptions(ctx context.Context, client *cubapi.Client, plan *TemporaryPlan, reason string, change cubapi.Change) ([]*cubapi.Result, error) {
	var results []*cubapi.Result
	for _, g := range plan.groups {
		fns := make([]api.FunctionInvocation, 0, len(g.resources))
		for _, r := range g.resources {
			where, err := r.whereResource()
			if err != nil {
				return results, err
			}
			fns = append(fns, api.FunctionInvocation{
				FunctionName: "set-annotation",
				Arguments: []api.FunctionArgument{
					{ParameterName: "annotation-key", Value: ExceptionAnnotation(plan.Rule)},
					{ParameterName: "annotation-value", Value: reason},
				},
				WhereResource: where,
			})
		}
		for start := 0; start < len(g.unitIDs); start += unitsChunk {
			chunk := g.unitIDs[start:min(start+unitsChunk, len(g.unitIDs))]
			sel := cubapi.Selector{Where: cubapi.Where{}.In("UnitID", chunk).String()}
			res, err := cubapi.InvokeFunctions(ctx, client, fns, sel, change)
			if err != nil {
				return results, err
			}
			results = append(results, res)
		}
	}
	return results, nil
}

// TemporaryReport is what `exception add-temporary` prints: the write report,
// the resources excepted, and the failing Units left as they are.
type TemporaryReport struct {
	write.Report
	Rule        string               `json:"rule"`
	Reason      string               `json:"reason"`
	Excepted    []TemporaryException `json:"excepted"`
	NotExcepted []NotExcepted        `json:"notExcepted"`
}

func exceptionAddTemporaryCmd(preflight Preflight, rules, groups []string) *cobra.Command {
	var output, rule, reason string
	var filter cliutil.QueryFlags
	var commit write.CommitFlags
	cmd := &cobra.Command{
		Use:   "add-temporary --rule <rule> --reason <why>",
		Short: "Except every resource that fails a guardrail rule now, until it is fixed",
		Long: `add-temporary excepts each resource that fails a rule today -- with a warning
while the rule is audited, or an error once it is enforced -- so the rule can be
enforced without waiting for them, and blocks anything new that fails it. Each
exception's reason is ` + TemporaryExceptionPrefix + `: <why>, which marks it as
temporary: 'exception list --temporary' lists them, to be fixed and removed one
by one.

A Unit's failing resources are those its results from the rule name. A Unit
whose results name none is excepted only when it holds one resource; a Unit
whose failures cannot all be placed is left as it is, and listed, to except by
hand with 'exception add --resource' or fix.

Scope it to some Units with --where and the label shorthands; without them it
covers every Unit failing the rule. Like any exception, it is a change to each
Unit, dry-run unless --commit --change-desc.

Rules, and those installed with --policy-unit, named after their Units:
` + wrapList(rules),
		Example: "  exception add-temporary --rule workload-runs-nonroot --reason \"images rebuilt in Q1\"\n" +
			"  exception add-temporary --rule workload-runs-nonroot --environment prod --reason \"…\" --commit --change-desc \"…\"",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if rule == "" {
				return checkRule(rule, rules)
			}
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("--reason is required: it records why the failures are not fixed yet")
			}
			value := TemporaryReason(reason)
			change, err := commit.Change(fmt.Sprintf("temporarily except the resources failing %s: %s", rule, value))
			if err != nil {
				return err
			}
			client, err := preflight(cmd.Context())
			if err != nil {
				return err
			}
			if err := checkInstalledRule(cmd.Context(), client, rule, rules, groups); err != nil {
				return err
			}
			where, err := filter.Predicate()
			if err != nil {
				return err
			}
			plan, err := PlanTemporaryExceptions(cmd.Context(), client, rule, groups, where)
			if err != nil {
				return err
			}
			results, err := ExecuteTemporaryExceptions(cmd.Context(), client, plan, value, change)
			if err != nil {
				return err
			}
			rep := TemporaryReport{
				Report:   write.Summarize("exception add-temporary", "", change.DryRun(), results...),
				Rule:     rule,
				Reason:   value,
				Excepted: plan.Excepted, NotExcepted: plan.NotExcepted,
			}
			if rep.Excepted == nil {
				rep.Excepted = []TemporaryException{}
			}
			if rep.NotExcepted == nil {
				rep.NotExcepted = []NotExcepted{}
			}
			if done, err := managerkit.Render(cmd.OutOrStdout(), output, rep); done || err != nil {
				return err
			}
			printTemporary(cmd, rep)
			return nil
		},
	}
	managerkit.AddOutputFlag(cmd, &output)
	filter.BindWhere(cmd)
	filter.BindSpaceLabels(cmd)
	managerkit.BindComponentResolver(cmd, &filter, preflight)
	cmd.Flags().StringVar(&rule, "rule", "", "the rule to except the failing resources from (required)")
	cmd.Flags().StringVar(&reason, "reason", "", "why the failures are not fixed yet, recorded after "+TemporaryExceptionPrefix+": as each annotation's value (required)")
	commit.Bind(cmd)
	return cmd
}

func printTemporary(cmd *cobra.Command, rep TemporaryReport) {
	out := cmd.OutOrStdout()
	if len(rep.Excepted) == 0 && len(rep.NotExcepted) == 0 {
		cliutil.Fprintln(out, "No resource fails "+rep.Rule+".")
		return
	}
	if len(rep.Excepted) > 0 {
		tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "SPACE\tUNIT\tRESOURCE")
		for _, e := range rep.Excepted {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Space, e.Unit, e.Resource())
		}
		_ = tw.Flush()
		verb := "Excepted"
		if rep.DryRun {
			verb = "Would except"
		}
		cliutil.Fprintln(out, fmt.Sprintf("\n%s %d resource(s) in %d Unit(s) from %s, with the reason %q%s",
			verb, len(rep.Excepted), len(rep.Outcomes), rep.Rule, rep.Reason, write.DryRunSuffix(rep.DryRun)))
		for _, o := range rep.Outcomes {
			if o.Error != "" {
				cliutil.Fprintln(out, fmt.Sprintf("  %s/%s: %s", o.Space, o.Unit, o.Error))
			}
		}
		write.WriteDiffs(out, rep.Command, rep.Report)
		write.WriteWithheld(out, rep.Report)
	}
	if len(rep.NotExcepted) > 0 {
		cliutil.Fprintln(out, fmt.Sprintf("\nNot excepted: %d Unit(s) failing %s:", len(rep.NotExcepted), rep.Rule))
		tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "SPACE\tUNIT\tWHY")
		for _, n := range rep.NotExcepted {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", n.Space, n.Unit, n.Why)
		}
		_ = tw.Flush()
	}
	if rep.Committed > 0 {
		cliutil.Fprintln(out, "\n"+write.ReleaseNote(""))
	}
}
