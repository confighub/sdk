// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package profiles

// The command surface over a Library. profiles.go is API-shaped and has no CLI
// in it; this file is the CLI half.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/cliutil"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"

	"github.com/confighub/sdk/managerkit"
	"github.com/confighub/sdk/managerkit/write"
)

// Command is the `profile` command group: install, list, apply.
func (l Library) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: fmt.Sprintf("The %s library — reusable, parameterized edits (stored Invocations)", l.noun()),
		Long: fmt.Sprintf(`profile manages the profile library (--profiles-space):
named %ss stored as ConfigHub Invocations.

A profile bundles a function with preset arguments and exposes whatever has to
vary as a parameter. 'profile apply' invokes one over %s, dry-run by
default.`, l.noun(), l.Target),
	}
	cmd.AddCommand(l.installCmd(), l.listCmd(), l.applyCmd())
	return cmd
}

// installCmd seeds the profile library. It is idempotent: every entity is
// ensured rather than created, so re-running after adding a profile installs the
// new one and leaves the rest alone.
func (l Library) installCmd() *cobra.Command {
	var profilesSpace string
	cmd := &cobra.Command{
		Use:   "install",
		Short: fmt.Sprintf("Create the profile library Space and seed the default %ss", l.noun()),
		Long: fmt.Sprintf(`install creates the profile library Space and seeds the default %ss.

Re-running it is safe: a profile that is already there is left alone, so
installing after adding one installs only the new one. That also means it does
not update a profile whose definition has changed -- a new description, argument
or parameter reaches the library only once the old Invocation is deleted:

  cub invocation delete <slug> --space %s`, l.noun(), managerkit.CommonSpace),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, err := l.Preflight(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			lib, err := cubapi.EnsureSpace(ctx, client, goclientnew.Space{
				Slug:   profilesSpace,
				Labels: map[string]string{"app": l.Tool, "role": "profiles"},
			})
			if err != nil {
				return fmt.Errorf("create profiles space: %w", err)
			}
			cliutil.Fprintln(out, "Space "+profilesSpace+" ready")
			for _, spec := range l.Profiles {
				if _, err := cubapi.EnsureInvocation(ctx, client, l.buildInvocation(lib.SpaceID, spec)); err != nil {
					return fmt.Errorf("create profile %s: %w", spec.Slug, err)
				}
				cliutil.Fprintln(out, "Profile "+profilesSpace+"/"+spec.Slug+" ready")
			}
			cliutil.Fprintln(out, "\nProfiles installed. Apply one with: "+
				l.InvocationName()+" profile apply <slug> <space>/<unit>")
			return nil
		},
	}
	cliutil.ProfilesSpaceFlag(cmd, &profilesSpace, managerkit.CommonSpace)
	return cmd
}

// row is one profile as listed.
type row struct {
	Slug        string   `json:"slug"`
	Function    string   `json:"function"`
	Parameters  []string `json:"parameters,omitempty"`
	Description string   `json:"description,omitempty"`
}

func (l Library) listCmd() *cobra.Command {
	var profilesSpace, output string
	cmd := &cobra.Command{
		Use:   "list",
		Short: fmt.Sprintf("List the %ss in the profile library", l.noun()),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, err := l.Preflight(ctx)
			if err != nil {
				return err
			}
			sp, err := cubapi.ResolveSpace(ctx, client, cubapi.ParseRef(profilesSpace), cubapi.ResolveOpts{})
			if err != nil {
				return fmt.Errorf("%s: %w", l.installHint(profilesSpace), err)
			}
			invs, err := cubapi.ListInvocations(ctx, client, (cubapi.Where{}).SpaceID(sp.Space.SpaceID),
				cubapi.ListOpts{Select: "Slug,FunctionInvocations,Parameters,Annotations"})
			if err != nil {
				return err
			}
			rows := make([]row, 0, len(invs))
			for _, ei := range invs {
				if ei.Invocation == nil {
					continue
				}
				inv := ei.Invocation
				r := row{Slug: inv.Slug, Function: strings.Join(cubapi.InvocationFunctionNames(inv), ", ")}
				for _, p := range inv.Parameters {
					r.Parameters = append(r.Parameters, p.ParameterName)
				}
				r.Description = inv.Annotations[DescriptionAnnotation]
				rows = append(rows, r)
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].Slug < rows[j].Slug })

			if done, err := managerkit.Render(cmd.OutOrStdout(), output, rows); done || err != nil {
				return err
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "PROFILE\tFUNCTION\tPARAMS\tDESCRIPTION")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
					r.Slug, r.Function, cliutil.Dash(strings.Join(r.Parameters, ",")), r.Description)
			}
			return tw.Flush()
		},
	}
	managerkit.AddOutputFlag(cmd, &output)
	cliutil.ProfilesSpaceFlag(cmd, &profilesSpace, managerkit.CommonSpace)
	return cmd
}

// paramHelp is the --param line of the apply help, with the tool's own example
// when it gave one.
func (l Library) paramHelp() string {
	if l.ParamExample == "" {
		return "Supply profile parameters with --param name=value."
	}
	return "Supply profile parameters with --param name=value\n(e.g. " + l.ParamExample + ")."
}

func (l Library) applyCmd() *cobra.Command {
	var profilesSpace, output string
	var params []string
	var commit cliutil.CommitFlags
	cmd := &cobra.Command{
		Use:   "apply <profile> <space>/<unit>",
		Short: fmt.Sprintf("Apply %s to %s (dry-run unless --commit)", article(l.noun()), l.Target),
		Long: fmt.Sprintf(`apply invokes a stored %s over %s.
%s

Dry-run unless --commit --change-desc; never bypasses ValidationErrors.`,
			l.noun(), l.Target, l.paramHelp()),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileSlug := args[0]
			paramMap, err := ParseParams(params)
			if err != nil {
				return err
			}
			changeDesc, dryRun, err := commit.Validate(
				fmt.Sprintf("apply %s %s to %s", l.noun(), profileSlug, args[1]))
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := l.Preflight(ctx)
			if err != nil {
				return err
			}
			inv, err := l.resolveProfile(ctx, client, profilesSpace, profileSlug)
			if err != nil {
				return err
			}
			ref, err := write.ParseUnitRef(ctx, client, args[1])
			if err != nil {
				return err
			}
			res, err := cubapi.InvokeStoredInvocation(ctx, client, inv.InvocationID, paramMap,
				ref.Selector(), write.Change(changeDesc, dryRun))
			if err != nil {
				return err
			}
			return write.ReportMutations(cmd, "profile apply "+profileSlug, ref.SpaceSlug, dryRun, output, res)
		},
	}
	managerkit.AddOutputFlag(cmd, &output)
	commit.Bind(cmd)
	cmd.Flags().StringArrayVar(&params, "param", nil, "profile parameter as name=value (repeatable)")
	cliutil.ProfilesSpaceFlag(cmd, &profilesSpace, managerkit.CommonSpace)
	return cmd
}

// FleetEditCommand is `fleet-edit`: one profile applied to every Unit a selector
// matches, in one server-side operation -- the bulk analog of `profile apply`.
func (l Library) FleetEditCommand() *cobra.Command {
	var profilesSpace, output, profileSlug string
	var filter cliutil.QueryFlags
	var params []string
	var commit cliutil.CommitFlags
	cmd := &cobra.Command{
		Use:   "fleet-edit --profile <slug> [--where …]",
		Short: fmt.Sprintf("Apply %s across a selector of Units (bulk, dry-run unless --commit)", article(l.noun())),
		Long: fmt.Sprintf(`fleet-edit applies one %s to every Unit a selector
matches, in one server-side operation — the bulk analog of 'profile apply'.

It is scoped to %s,
so a profile never reaches a Unit it has nothing to say about.

Scope with --where and the label shorthands (e.g. --environment prod).
%s

Dry-run unless --commit --change-desc.%s`,
			l.noun(), l.FleetEdit.scope(), l.paramHelp(), l.FleetEdit.example()),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if profileSlug == "" {
				return fmt.Errorf("--profile is required")
			}
			paramMap, err := ParseParams(params)
			if err != nil {
				return err
			}
			changeDesc, dryRun, err := commit.Validate(fmt.Sprintf("fleet-edit: apply %s %s", l.noun(), profileSlug))
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := l.Preflight(ctx)
			if err != nil {
				return err
			}
			inv, err := l.resolveProfile(ctx, client, profilesSpace, profileSlug)
			if err != nil {
				return err
			}
			scope, err := filter.Predicate()
			if err != nil {
				return err
			}
			where := (cubapi.Where{}).Eq("ToolchainType", cubapi.DefaultToolchainType).And(scope)
			if err := where.Err(); err != nil {
				return err
			}
			sel := cubapi.Selector{Where: where.String(), WhereData: l.FleetEdit.WhereData}
			res, err := cubapi.InvokeStoredInvocation(ctx, client, inv.InvocationID, paramMap, sel,
				write.Change(changeDesc, dryRun))
			if err != nil {
				return err
			}
			return write.ReportMutations(cmd, "fleet-edit "+profileSlug, "", dryRun, output, res)
		},
	}
	managerkit.AddOutputFlag(cmd, &output)
	filter.BindWhere(cmd)
	filter.BindSpaceLabels(cmd)
	commit.Bind(cmd)
	cmd.Flags().StringVar(&profileSlug, "profile", "",
		fmt.Sprintf("%s (stored Invocation) to apply (required)", l.noun()))
	cmd.Flags().StringArrayVar(&params, "param", nil, "profile parameter as name=value (repeatable)")
	cliutil.ProfilesSpaceFlag(cmd, &profilesSpace, managerkit.CommonSpace)
	return cmd
}

// resolveProfile finds one stored profile, reporting a missing library as
// something to install rather than as a missing Space.
func (l Library) resolveProfile(ctx context.Context, client *cubapi.Client, profilesSpace, slug string) (*goclientnew.Invocation, error) {
	lib, err := cubapi.ResolveSpace(ctx, client, cubapi.ParseRef(profilesSpace), cubapi.ResolveOpts{})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", l.installHint(profilesSpace), err)
	}
	inv, err := cubapi.ResolveInvocation(ctx, client, cubapi.ParseRef(slug), cubapi.ResolveOpts{Space: lib.Space.SpaceID})
	if err != nil {
		return nil, fmt.Errorf("resolve profile %q: %w", slug, err)
	}
	return inv.Invocation, nil
}

func (f FleetEdit) scope() string {
	if f.Scope != "" {
		return f.Scope
	}
	return "the Units a profile can act on"
}

func (f FleetEdit) example() string {
	if f.Example == "" {
		return ""
	}
	return "\n\nExample:\n  " + f.Example
}

// article prefixes a noun with "a" or "an". The nouns here are a closed set --
// "profile" and a handful of adjective-prefixed variants -- so the vowel test is
// enough.
func article(noun string) string {
	if noun == "" {
		return "a profile"
	}
	if strings.ContainsRune("aeiou", rune(noun[0])) {
		return "an " + noun
	}
	return "a " + noun
}
