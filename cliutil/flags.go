// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cliutil

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// OutputFlag registers the standard -o/--output flag, binding it to dest. If
// defaultValue is empty, "json" is used (the agent-friendly default).
func OutputFlag(cmd *cobra.Command, dest *string, defaultValue string) {
	if defaultValue == "" {
		defaultValue = "json"
	}
	cmd.Flags().StringVarP(dest, "output", "o", defaultValue,
		"output format: json | yaml | table | name | jq=<expr> | yq=<expr>")
}

// QueryFlags are the standard read/list scoping flags, plus the Space-label
// shorthands that spell a common scope without writing a predicate.
//
// Bind them all with [QueryFlags.Bind] and [QueryFlags.BindSpaceLabels], or bind
// the groups a command actually supports: a command whose query pins its own
// --select, for instance, wants [QueryFlags.BindWhere] rather than the field
// registered for a caller to override.
//
// [QueryFlags.Predicate] compiles --where and the label shorthands into one
// expression. Filter, Contains and Select are not part of it: they are separate
// query parameters, not clauses.
type QueryFlags struct {
	Where    string
	Filter   string
	Contains string
	Select   string

	// Space-label scopes, registered by [QueryFlags.BindSpaceLabels]. These are
	// the standard Space labels, the ones the `cub variant` commands use.
	Component   string
	Environment string
	Region      string
	Owner       string
	Layer       string
	Variant     string

	extra []*labelScope
}

// labelScope is a Space-label flag beyond the standard set, added with
// [QueryFlags.Label].
type labelScope struct {
	flag  string
	label string
	usage string
	value string
}

// Bind registers --where, --filter, --contains, and --select on cmd. The
// Space-label shorthands are registered separately, by [QueryFlags.BindSpaceLabels].
func (q *QueryFlags) Bind(cmd *cobra.Command) {
	q.BindWhere(cmd)
	q.BindStored(cmd)
	q.BindSelect(cmd)
}

// BindWhere registers --where alone, for a command that scopes a query but does
// not let the caller choose the fields or apply a stored Filter.
func (q *QueryFlags) BindWhere(cmd *cobra.Command) {
	cmd.Flags().StringVar(&q.Where, "where", "",
		"SQL-inspired filter expression (ANDed clauses); may reference Slug, Labels.*, Space.*, Target.* (e.g. \"Target.ProviderType = 'OCI'\")")
}

// BindStored registers --filter and --contains.
func (q *QueryFlags) BindStored(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&q.Filter, "filter", "", "stored Filter to apply, as 'space/filter' or 'filter'")
	f.StringVar(&q.Contains, "contains", "", "free-text search")
}

// BindSelect registers --select. A command whose query needs particular fields
// to build its own model should not register it: a caller who narrowed the
// selection would get those fields back zeroed, with nothing to say they were
// never fetched.
func (q *QueryFlags) BindSelect(cmd *cobra.Command) {
	cmd.Flags().StringVar(&q.Select, "select", "", "comma-separated, PascalCase fields to return")
}

// Label adds a Space-label scope beyond the standard set, for a label a single
// command cares about. Its flag joins the predicate with the others, so a
// command gets its own scoping without every command growing the flag.
//
// Call it before BindSpaceLabels. The terms it adds follow the standard labels;
// a predicate is a flat conjunction, so their position carries no meaning.
func (q *QueryFlags) Label(flag, spaceLabel, usage string) {
	q.extra = append(q.extra, &labelScope{flag: flag, label: spaceLabel, usage: usage})
}

// BindSpaceLabels registers the Space-label shorthands: --component,
// --environment, --region, --owner, --layer, --variant, and any label added with
// [QueryFlags.Label].
//
// One Unit-level predicate can reference Unit, Space and Target metadata, so
// there is no need for separate Space or Target filters: the server does the
// scoping, and only the matching Units come back. These flags are shorthands
// over --where for the scopes that come up most.
//
// They scope by a label on the Space, which is where the standard labels live.
// That costs something: the server compiles a term naming the Unit's own columns
// into SQL, but a term prefixed with another entity it cannot, so it expands the
// Space for every row the query returned and evaluates the term afterwards. The
// answer is the same either way. Where a command has the choice -- a label the
// Units carry themselves -- a --where over Labels.* narrows the read and these
// do not.
func (q *QueryFlags) BindSpaceLabels(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&q.Component, "component", "", "select Units whose Space has Labels.Component = <value>")
	f.StringVar(&q.Environment, "environment", "", "select Units whose Space has Labels.Environment = <value>")
	f.StringVar(&q.Region, "region", "", "select Units whose Space has Labels.Region = <value>")
	f.StringVar(&q.Owner, "owner", "", "select Units whose Space has Labels.Owner = <value>")
	f.StringVar(&q.Layer, "layer", "", "select Units whose Space has Labels.Layer = <value>")
	f.StringVar(&q.Variant, "variant", "", "select Units whose Space has Labels.Variant = <value>")
	for _, e := range q.extra {
		f.StringVar(&e.value, e.flag, "", e.usage)
	}
}

// Predicate compiles --where and the Space-label shorthands into a single
// ConfigHub `where` expression, empty when nothing is set (everything the caller
// can view). A ConfigHub predicate is flat AND-only -- no parentheses, no OR --
// so the shorthands are joined to any raw --where with a bare AND.
//
// A label value carrying a quote or a backslash is rejected. The server screens
// every filter literal with an anchored regexp admitting neither, because it
// builds its SQL by concatenation with no parameter binding and that screen is
// what keeps a filter from being an injection point -- so such a value cannot be
// sent at all, and rewriting it would scope the query to a different value than
// the operator typed. Rejecting it here names the flag they typed, where the
// server can only name a position in an expression they never wrote. The API
// client applies the same rule to the predicates it builds; the rule is restated
// here because this package does not depend on it.
func (q QueryFlags) Predicate() (string, error) {
	var terms []string
	if q.Where != "" {
		terms = append(terms, q.Where)
	}
	eq := func(flag, label, value string) error {
		if value == "" {
			return nil
		}
		if strings.ContainsAny(value, "'\\") {
			return fmt.Errorf("value for --%s cannot appear in a filter literal: %q contains a quote or backslash, which the filter grammar has no way to express", flag, value)
		}
		terms = append(terms, fmt.Sprintf("Space.Labels.%s = '%s'", label, value))
		return nil
	}
	for _, s := range []struct{ flag, label, value string }{
		{"component", "Component", q.Component},
		{"environment", "Environment", q.Environment},
		{"region", "Region", q.Region},
		{"owner", "Owner", q.Owner},
		{"layer", "Layer", q.Layer},
		{"variant", "Variant", q.Variant},
	} {
		if err := eq(s.flag, s.label, s.value); err != nil {
			return "", err
		}
	}
	for _, e := range q.extra {
		if err := eq(e.flag, e.label, e.value); err != nil {
			return "", err
		}
	}
	return strings.Join(terms, " AND "), nil
}

// ProfilesSpaceFlag registers --profiles-space, the Space holding a tool's stored
// profile Invocations. defaultSpace is what the flag defaults to; tools that
// share a profile library pass the same one, so that an operator has one place
// to look rather than one per tool they happen to have installed.
func ProfilesSpaceFlag(cmd *cobra.Command, dest *string, defaultSpace string) {
	cmd.Flags().StringVar(dest, "profiles-space", defaultSpace,
		"Space holding the profile library")
}

// CommitFlags carry the dry-run/commit controls shared by mutating commands.
// Mutations are dry-run by default; --commit performs the write and requires
// --change-desc for provenance.
type CommitFlags struct {
	Commit     bool
	ChangeDesc string
}

// Bind registers --commit and --change-desc on cmd.
func (c *CommitFlags) Bind(cmd *cobra.Command) {
	f := cmd.Flags()
	f.BoolVar(&c.Commit, "commit", false, "perform the change (default is dry-run: preview only)")
	f.StringVar(&c.ChangeDesc, "change-desc", "", "change description recorded with the change (required with --commit)")
}

// Validate enforces the commit policy and returns the change description to
// record (empty on dry-run) plus whether this is a dry-run. summary is suggested
// in the error when --change-desc is missing on commit.
func (c CommitFlags) Validate(summary string) (changeDesc string, dryRun bool, err error) {
	if !c.Commit {
		return "", true, nil
	}
	if strings.TrimSpace(c.ChangeDesc) == "" {
		return "", false, fmt.Errorf("--change-desc is required with --commit (describe the change and why; suggested: %q)", summary)
	}
	return c.ChangeDesc, false, nil
}
