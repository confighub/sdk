// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

// Exceptions: the annotation ExceptionAnnotation(rule) on a resource makes that
// rule's Trigger skip it, and its value says why. These commands write, remove
// and list those annotations, so that what is excepted, and why, can be read
// back rather than found by grepping configuration.

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/cliutil"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/cubapi"
	api "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"

	"github.com/confighub/sdk/managerkit"
	"github.com/confighub/sdk/managerkit/write"
)

// Exception is one resource excepted from one rule.
type Exception struct {
	Rule         string `json:"rule"`
	Space        string `json:"space"`
	Unit         string `json:"unit"`
	ResourceType string `json:"resourceType"`
	ResourceName string `json:"resourceName"`
	Reason       string `json:"reason"`
}

// Resource names the excepted resource as --resource takes it: its type, then
// its namespace, if it has one, and name.
func (e Exception) Resource() string { return resourceRef(e.ResourceType, e.ResourceName) }

// ExceptionReport is what `exception list` prints.
type ExceptionReport struct {
	Exceptions []Exception `json:"exceptions"`
	Total      int         `json:"total"`
	// ByRule counts the exceptions to each rule listed, including the rules that
	// have none, so that what is not excepted is as plain as what is.
	ByRule map[string]int `json:"byRule"`
	// Filter is the Unit scope the exceptions were read under.
	Filter string `json:"filter,omitempty"`
}

// ListExceptions finds the resources excepted from each of rules, in the Units
// where selects; an empty where means every Unit the caller can view.
//
// It queries once per rule. The filter language can test whether one
// annotation is present but cannot match a prefix of annotation keys, so the
// rules have to be named.
func ListExceptions(ctx context.Context, client *cubapi.Client, rules []string, where string) ([]Exception, error) {
	var inScope map[goclientnew.UUID]bool
	var unitIDs []goclientnew.UUID
	if where != "" {
		units, err := cubapi.ListUnits(ctx, client,
			cubapi.Where{}.Eq("ToolchainType", cubapi.DefaultToolchainType).And(where),
			cubapi.ListOpts{Select: "UnitID,Slug,SpaceID"})
		if err != nil {
			return nil, fmt.Errorf("list units: %w", err)
		}
		inScope = make(map[goclientnew.UUID]bool, len(units))
		for _, eu := range units {
			if eu.Unit != nil {
				inScope[eu.Unit.UnitID] = true
				unitIDs = append(unitIDs, eu.Unit.UnitID)
			}
		}
		if len(unitIDs) == 0 {
			return []Exception{}, nil
		}
	}

	out := []Exception{}
	for _, rule := range rules {
		key := ExceptionAnnotation(rule)
		w := cubapi.Where{}.Eq("ToolchainType", cubapi.DefaultToolchainType).
			And("Data." + annotationPath(key) + " IS NOT NULL")
		// The Unit IDs only narrow the read; the inScope check below is what
		// decides, so a fleet too large to name in one filter reads more rows
		// rather than failing.
		if inScope != nil {
			if scoped := w.In("UnitID", unitIDs); len(scoped.String()) <= cubapi.MaxFilterLength {
				w = scoped
			}
		}
		resources, err := cubapi.ListResources(ctx, client, w, cubapi.ListOpts{})
		if err != nil {
			return nil, fmt.Errorf("list exceptions to %s: %w", rule, err)
		}
		for _, er := range resources {
			r := er.Resource
			if r == nil || (inScope != nil && !inScope[r.UnitID]) {
				continue
			}
			reason, _ := annotations(r.Data)[key].(string)
			out = append(out, Exception{
				Rule: rule, Space: r.SpaceSlug, Unit: r.UnitSlug,
				ResourceType: r.ResourceType, ResourceName: r.ResourceName, Reason: reason,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Space != b.Space {
			return a.Space < b.Space
		}
		if a.Unit != b.Unit {
			return a.Unit < b.Unit
		}
		if a.Resource() != b.Resource() {
			return a.Resource() < b.Resource()
		}
		return a.Rule < b.Rule
	})
	return out, nil
}

// ExceptionCmd is `guardrails exception`: add, remove and list exceptions to
// the rules of packs. A rule's slug names it across packs, so one command serves
// every pack a tool has.
func ExceptionCmd(preflight Preflight, packs ...Pack) *cobra.Command {
	rules := ruleSlugs(packs)
	cmd := &cobra.Command{
		Use:   "exception",
		Short: "Except a resource from a guardrail rule, remove an exception, or list them",
		Long: `exception manages the annotations that except a resource from one guardrail rule:

  ` + ExceptionAnnotationPrefix + `<rule>: <why>

A rule's Trigger skips every resource carrying it, so the resource gets no
warning, or after the rule is promoted, no error, from that rule alone. The
value records why, and 'exception list' reads them all back.

An exception is part of the resource's configuration: adding or removing one is
a change to its Unit, dry-run unless --commit --change-desc, and released like
any other.`,
	}
	cmd.AddCommand(exceptionAddCmd(preflight, rules), exceptionRemoveCmd(preflight, rules), exceptionListCmd(preflight, rules))
	return cmd
}

func exceptionAddCmd(preflight Preflight, rules []string) *cobra.Command {
	var output, rule, reason, resource string
	var commit cliutil.CommitFlags
	cmd := &cobra.Command{
		Use:   "add <space>/<unit> --rule <rule> --reason <why>",
		Short: "Except a resource from a guardrail rule",
		Long: `add annotates a resource ` + ExceptionAnnotationPrefix + `<rule>, with --reason
as the value, so the rule's Trigger skips it.

Name the resource with --resource when the Unit holds more than one: its type
and name, as 'exception list' and 'cub resource list' show them, such as
apps/v1/Deployment/shop/web. The kind may stand in for the type, as in
Deployment/shop/web, and the name may leave out the namespace, wherever that
still names one resource. Adding an exception that is already there replaces
its reason.

Rules:
` + wrapList(rules),
		Example: "  exception add prod/web --rule workload-pdb-coverage --reason \"singleton by design\"\n" +
			"  exception add prod/web --resource apps/v1/Deployment/shop/web --rule workload-pdb-coverage --reason \"…\" --commit --change-desc \"…\"",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkRule(rule, rules); err != nil {
				return err
			}
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("--reason is required: it records why the resource is excepted")
			}
			changeDesc, dryRun, err := commit.Validate(fmt.Sprintf("except %s from %s: %s", args[0], rule, reason))
			if err != nil {
				return err
			}
			client, err := preflight(cmd.Context())
			if err != nil {
				return err
			}
			ref, all, err := unitResources(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			chosen, err := pickResources(args[0], all, resource)
			if err != nil {
				return err
			}
			fns := make([]api.FunctionInvocation, 0, len(chosen))
			for _, r := range chosen {
				where, err := r.whereResource()
				if err != nil {
					return err
				}
				fns = append(fns, api.FunctionInvocation{
					FunctionName: "set-annotation",
					Arguments: []api.FunctionArgument{
						{ParameterName: "annotation-key", Value: ExceptionAnnotation(rule)},
						{ParameterName: "annotation-value", Value: reason},
					},
					WhereResource: where,
				})
			}
			res, err := cubapi.InvokeFunctions(cmd.Context(), client, fns, ref.Selector(), write.Change(changeDesc, dryRun))
			if err != nil {
				return err
			}
			return write.ReportMutations(cmd, "exception add", ref.SpaceSlug, dryRun, output, res)
		},
	}
	managerkit.AddOutputFlag(cmd, &output)
	cmd.Flags().StringVar(&rule, "rule", "", "the rule to except the resource from (required)")
	cmd.Flags().StringVar(&reason, "reason", "", "why the resource is excepted, recorded as the annotation's value (required)")
	cmd.Flags().StringVar(&resource, "resource", "", "the resource, as <type>/[<namespace>/]<name>, such as apps/v1/Deployment/shop/web; required when the Unit holds more than one")
	commit.Bind(cmd)
	return cmd
}

func exceptionRemoveCmd(preflight Preflight, rules []string) *cobra.Command {
	var output, rule, resource string
	var commit cliutil.CommitFlags
	cmd := &cobra.Command{
		Use:   "remove <space>/<unit> --rule <rule>",
		Short: "Remove a resource's exception from a guardrail rule",
		Long: `remove deletes the ` + ExceptionAnnotationPrefix + `<rule> annotation, so the rule
checks the resource again. Without --resource, it removes the exception from
every resource in the Unit that has one.

Rules:
` + wrapList(rules),
		Example: "  exception remove prod/web --rule workload-pdb-coverage --commit --change-desc \"web has a PDB now\"",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkRule(rule, rules); err != nil {
				return err
			}
			changeDesc, dryRun, err := commit.Validate(fmt.Sprintf("remove the exception of %s from %s", args[0], rule))
			if err != nil {
				return err
			}
			client, err := preflight(cmd.Context())
			if err != nil {
				return err
			}
			ref, all, err := unitResources(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			key := ExceptionAnnotation(rule)
			var excepted []unitResource
			for _, r := range all {
				if _, ok := r.annotations[key]; ok {
					excepted = append(excepted, r)
				}
			}
			if len(excepted) == 0 {
				return fmt.Errorf("%s has no exception to %s", args[0], rule)
			}
			chosen := excepted
			if resource != "" {
				if chosen, err = matchResource(all, resource); err != nil {
					return fmt.Errorf("%s: %w", args[0], err)
				}
				if _, ok := chosen[0].annotations[key]; !ok {
					return fmt.Errorf("%s in %s has no exception to %s", chosen[0].ref(), args[0], rule)
				}
			}
			fns := make([]api.FunctionInvocation, 0, len(chosen))
			for _, r := range chosen {
				where, err := r.whereResource()
				if err != nil {
					return err
				}
				// The last annotation takes the map with it, so that adding and
				// removing an exception leaves the resource as it was.
				path := annotationPath(key)
				if len(r.annotations) == 1 {
					path = "metadata.annotations"
				}
				fns = append(fns, api.FunctionInvocation{
					FunctionName: "delete-path",
					Arguments: []api.FunctionArgument{
						{ParameterName: "resource-type", Value: r.resourceType},
						{ParameterName: "path", Value: path},
					},
					WhereResource: where,
				})
			}
			res, err := cubapi.InvokeFunctions(cmd.Context(), client, fns, ref.Selector(), write.Change(changeDesc, dryRun))
			if err != nil {
				return err
			}
			return write.ReportMutations(cmd, "exception remove", ref.SpaceSlug, dryRun, output, res)
		},
	}
	managerkit.AddOutputFlag(cmd, &output)
	cmd.Flags().StringVar(&rule, "rule", "", "the rule whose exception to remove (required)")
	cmd.Flags().StringVar(&resource, "resource", "", "only this resource, as <type>/[<namespace>/]<name>, such as apps/v1/Deployment/shop/web")
	commit.Bind(cmd)
	return cmd
}

func exceptionListCmd(preflight Preflight, rules []string) *cobra.Command {
	var output, only string
	var filter cliutil.QueryFlags
	cmd := &cobra.Command{
		Use:   "list [<space>/<unit>]",
		Short: "List the resources excepted from guardrail rules, and why",
		Long: `list reports every resource excepted from a guardrail rule, with the reason its
annotation records, and counts the exceptions to each rule, including the rules
that have none.

Scope it to one Unit by naming it, or to a set of Units with --where and the
label shorthands. --rule narrows it to some rules.`,
		Example: "  exception list -o table\n  exception list --environment prod --rule netpol-no-wide-cidr-egress -o table",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			listed, err := parseRules(only, rules)
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
			if len(args) == 1 {
				ref, err := write.ParseUnitRef(cmd.Context(), client, args[0])
				if err != nil {
					return err
				}
				w := cubapi.Where{}.SpaceID(ref.SpaceID).Slug(ref.UnitSlug).And(where)
				if err := w.Err(); err != nil {
					return err
				}
				where = w.String()
			}
			exceptions, err := ListExceptions(cmd.Context(), client, listed, where)
			if err != nil {
				return err
			}
			rep := ExceptionReport{Exceptions: exceptions, Total: len(exceptions), ByRule: map[string]int{}, Filter: where}
			for _, r := range listed {
				rep.ByRule[r] = 0
			}
			for _, e := range exceptions {
				rep.ByRule[e.Rule]++
			}
			if done, err := managerkit.Render(cmd.OutOrStdout(), output, rep); done || err != nil {
				return err
			}
			printExceptions(cmd, rep, listed)
			return nil
		},
	}
	managerkit.AddOutputFlag(cmd, &output)
	filter.BindWhere(cmd)
	filter.BindSpaceLabels(cmd)
	managerkit.BindComponentResolver(cmd, &filter, preflight)
	cmd.Flags().StringVar(&only, "rule", "", "only exceptions to these rules, comma-separated")
	return cmd
}

func printExceptions(cmd *cobra.Command, rep ExceptionReport, listed []string) {
	out := cmd.OutOrStdout()
	if rep.Total == 0 {
		cliutil.Fprintln(out, "No resources are excepted from these rules.")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "RULE\tSPACE\tUNIT\tRESOURCE\tREASON")
	for _, e := range rep.Exceptions {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.Rule, e.Space, e.Unit, e.Resource(), cliutil.Dash(e.Reason))
	}
	_ = tw.Flush()
	var counts, none []string
	for _, r := range listed {
		if n := rep.ByRule[r]; n > 0 {
			counts = append(counts, fmt.Sprintf("%d %s", n, r))
		} else {
			none = append(none, r)
		}
	}
	noun := "exceptions"
	if rep.Total == 1 {
		noun = "exception"
	}
	cliutil.Fprintln(out, fmt.Sprintf("\n%d %s: %s", rep.Total, noun, strings.Join(counts, ", ")))
	if len(none) > 0 {
		cliutil.Fprintln(out, "No exceptions to: "+strings.Join(none, ", "))
	}
}

// wrapList renders words as an indented, comma-separated list wrapped to fit a
// terminal, for help text.
func wrapList(words []string) string {
	var lines []string
	line := " "
	for i, w := range words {
		if i < len(words)-1 {
			w += ","
		}
		if len(line)+1+len(w) > 80 && line != " " {
			lines = append(lines, line)
			line = " "
		}
		line += " " + w
	}
	return strings.Join(append(lines, line), "\n")
}

// ruleSlugs are the slugs of every rule in packs, sorted.
func ruleSlugs(packs []Pack) []string {
	var slugs []string
	for _, p := range packs {
		for _, r := range p.Rules {
			slugs = append(slugs, r.Slug)
		}
	}
	slices.Sort(slugs)
	return slices.Compact(slugs)
}

func checkRule(rule string, rules []string) error {
	if rule == "" {
		return fmt.Errorf("--rule is required: one of %s", strings.Join(rules, ", "))
	}
	if !slices.Contains(rules, rule) {
		return fmt.Errorf("unknown rule %q: want one of %s", rule, strings.Join(rules, ", "))
	}
	return nil
}

// parseRules is a comma-separated --rule, every rule when it is empty.
func parseRules(flag string, rules []string) ([]string, error) {
	var out []string
	for _, r := range strings.Split(flag, ",") {
		if r = strings.TrimSpace(r); r == "" {
			continue
		}
		if !slices.Contains(rules, r) {
			return nil, fmt.Errorf("unknown rule %q: want one of %s", r, strings.Join(rules, ", "))
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return rules, nil
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// annotationPath is the path of annotation key in a resource, with the dots in
// the key escaped so the path has the segments it means.
func annotationPath(key string) string {
	return "metadata.annotations." + yamlkit.EscapeDotsInPathSegment(key)
}

// annotations is a resource's metadata.annotations, nil when it has none.
func annotations(data map[string]any) map[string]any {
	metadata, _ := data["metadata"].(map[string]any)
	annotations, _ := metadata["annotations"].(map[string]any)
	return annotations
}

// unitResource is one resource of the Unit an exception is added to or removed
// from.
type unitResource struct {
	resourceType, resourceName string
	annotations                map[string]any
}

func (r unitResource) ref() string { return resourceRef(r.resourceType, r.resourceName) }

// whereResource selects this resource alone among its Unit's.
func (r unitResource) whereResource() (string, error) {
	w := cubapi.Where{}.Eq("ConfigHub.ResourceType", r.resourceType).Eq("ConfigHub.ResourceName", r.resourceName)
	return w.String(), w.Err()
}

// unitResources resolves a <space>/<unit> and reads its resources.
func unitResources(ctx context.Context, client *cubapi.Client, arg string) (write.UnitRef, []unitResource, error) {
	ref, err := write.ParseUnitRef(ctx, client, arg)
	if err != nil {
		return ref, nil, err
	}
	unit, err := cubapi.ResolveUnit(ctx, client, cubapi.ParseRef(ref.UnitSlug), cubapi.ResolveOpts{Space: ref.SpaceID})
	if err != nil {
		return ref, nil, fmt.Errorf("resolve unit %q: %w", arg, err)
	}
	ers, err := cubapi.ListResources(ctx, client, cubapi.Where{}.In("UnitID", []goclientnew.UUID{unit.Unit.UnitID}), cubapi.ListOpts{})
	if err != nil {
		return ref, nil, fmt.Errorf("list the resources of %s: %w", arg, err)
	}
	out := make([]unitResource, 0, len(ers))
	for _, er := range ers {
		if r := er.Resource; r != nil {
			out = append(out, unitResource{resourceType: r.ResourceType, resourceName: r.ResourceName, annotations: annotations(r.Data)})
		}
	}
	return ref, out, nil
}

// pickResources is the resource --resource names, or the Unit's only resource
// when it does not name one. An exception is a decision about one resource, so
// a Unit holding several never has them all excepted by default.
func pickResources(unit string, all []unitResource, spec string) ([]unitResource, error) {
	if spec != "" {
		r, err := matchResource(all, spec)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", unit, err)
		}
		return r, nil
	}
	switch len(all) {
	case 0:
		return nil, fmt.Errorf("%s holds no resources", unit)
	case 1:
		return all, nil
	}
	return nil, fmt.Errorf("%s holds %d resources; name one with --resource: %s", unit, len(all), strings.Join(refs(all), ", "))
}

// matchResource is the one resource spec names. Its full form is the
// resource's type and name, as ConfigHub records them:
// apps/v1/Deployment/shop/web, or rbac.authorization.k8s.io/v1/ClusterRole/admin
// for one that is not namespaced. Two shorthands are taken as well, and only
// when they name one resource: the kind in place of the type, matched without
// regard to case, since the kind alone does not tell Services of different API
// groups apart; and the name without its namespace.
func matchResource(all []unitResource, spec string) ([]unitResource, error) {
	if !strings.Contains(spec, "/") {
		return nil, fmt.Errorf("--resource must be <type>/[<namespace>/]<name>, such as apps/v1/Deployment/shop/web, got %q", spec)
	}
	var exact, short []unitResource
	for _, r := range all {
		name := strings.TrimPrefix(r.resourceName, "/")
		bare := name[strings.LastIndex(name, "/")+1:]
		kind := kindOf(r.resourceType)
		byKind := func(n string) bool {
			return len(spec) == len(kind)+1+len(n) && strings.EqualFold(spec[:len(kind)], kind) && spec[len(kind):] == "/"+n
		}
		switch {
		case spec == r.resourceType+"/"+name:
			exact = append(exact, r)
		case spec == r.resourceType+"/"+bare, byKind(name), byKind(bare):
			short = append(short, r)
		}
	}
	if len(exact) == 1 {
		return exact, nil
	}
	switch len(short) {
	case 0:
		return nil, fmt.Errorf("no resource %s; it holds %s", spec, strings.Join(refs(all), ", "))
	case 1:
		return short, nil
	}
	return nil, fmt.Errorf("%s names %d resources; name one in full: %s", spec, len(short), strings.Join(refs(short), ", "))
}

func refs(rs []unitResource) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.ref()
	}
	return out
}

// resourceRef renders a resource in the full form --resource takes: its type,
// then its namespace, if it has one, and name.
func resourceRef(resourceType, resourceName string) string {
	return resourceType + "/" + strings.TrimPrefix(resourceName, "/")
}

// kindOf is the kind of a Kubernetes ResourceType, apiVersion/kind.
func kindOf(resourceType string) string {
	return resourceType[strings.LastIndex(resourceType, "/")+1:]
}
