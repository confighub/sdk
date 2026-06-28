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

// QueryFlags are the standard read/list scoping flags. Bind them with [QueryFlags.Bind].
type QueryFlags struct {
	Where    string
	Filter   string
	Contains string
	Select   string
}

// Bind registers --where, --filter, --contains, and --select on cmd.
func (q *QueryFlags) Bind(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&q.Where, "where", "", "SQL-inspired filter expression (ANDed clauses)")
	f.StringVar(&q.Filter, "filter", "", "stored Filter to apply, as 'space/filter' or 'filter'")
	f.StringVar(&q.Contains, "contains", "", "free-text search")
	f.StringVar(&q.Select, "select", "", "comma-separated, PascalCase fields to return")
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
