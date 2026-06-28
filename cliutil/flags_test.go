// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cliutil

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestOutputFlagDefault(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	var out string
	OutputFlag(cmd, &out, "")
	if got, _ := cmd.Flags().GetString("output"); got != "json" {
		t.Fatalf("default output = %q, want json", got)
	}
}

func TestQueryFlagsBind(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	var q QueryFlags
	q.Bind(cmd)
	if err := cmd.Flags().Parse([]string{"--where", "A = 1", "--select", "Slug"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if q.Where != "A = 1" || q.Select != "Slug" {
		t.Fatalf("query flags = %+v", q)
	}
}

func TestCommitFlagsValidate(t *testing.T) {
	// Dry-run by default.
	desc, dryRun, err := (CommitFlags{}).Validate("scale checkout")
	if err != nil || !dryRun || desc != "" {
		t.Fatalf("dry-run: desc=%q dryRun=%v err=%v", desc, dryRun, err)
	}
	// Commit without description -> error.
	if _, _, err := (CommitFlags{Commit: true}).Validate("scale checkout"); err == nil {
		t.Fatal("commit without --change-desc = nil error, want error")
	}
	// Commit with description.
	desc, dryRun, err = (CommitFlags{Commit: true, ChangeDesc: "scale to 3"}).Validate("scale checkout")
	if err != nil || dryRun || desc != "scale to 3" {
		t.Fatalf("commit: desc=%q dryRun=%v err=%v", desc, dryRun, err)
	}
}
