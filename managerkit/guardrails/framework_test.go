// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
)

// A framework control stands where the furthest of the checks corresponding to it does: a check
// installed in a rule takes the rule's stage and counts, one not installed is NotInstalled, and a
// control no check covers is Uncovered.
func TestFrameworkReport(t *testing.T) {
	p := testCatalog()
	p.Checks = slices.Clone(p.Checks)
	p.Checks[0].Corresponds = []string{"KS:C-0057", "CIS:5.2.2"} // privileged, in test-baseline
	p.Checks[1].Corresponds = []string{"KS:C-0041"}              // host-network, in test-baseline
	p.Checks[3].Corresponds = []string{"KS:C-0013"}              // run-as-non-root, not installed
	p.Checks[4].Corresponds = []string{"KS:C-0055"}              // seccomp, in test-pss
	other := Pack{Group: "other", Checks: []Check{{Control: "CH-0901", Name: "other-privileged", Corresponds: []string{"KS:C-0057"}}}}
	f := Framework{Name: "cis-test", Title: "CIS test", Controls: []FrameworkControl{
		{ID: "5.2.2", Title: "Minimize privileged containers", Corresponds: []string{"KS:C-0057"}},
		{ID: "5.2.5", Title: "Minimize host network", Corresponds: []string{"KS:C-0041"}},
		{ID: "5.2.7", Title: "Minimize root containers", Corresponds: []string{"KS:C-0013", "KS:C-0055"}},
		{ID: "5.2.8", Title: "Minimize NET_RAW", Corresponds: []string{"KS:C-0046"}},
		{ID: "5.2.9", Title: "Minimize root user", Corresponds: []string{"KS:C-0013"}},
	}}
	rules := []EnforcementRow{
		{Rule: "test-baseline", Stage: StageEnforced, Controls: []string{"CH-0301", "CH-0302"}, Warnings: 1, Errors: 2},
		{Rule: "test-pss", Stage: StageAudited, Controls: []string{"CH-0305"}, Warnings: 4},
	}
	rep := frameworkReport(f, []Pack{p, other}, rules)

	stages := map[string]string{}
	for _, row := range rep.Controls {
		stages[row.Control] = row.Stage
	}
	want := map[string]string{"5.2.2": StageEnforced, "5.2.5": StageEnforced, "5.2.7": StageAudited, "5.2.8": "", "5.2.9": StageNotInstalled}
	if !maps.Equal(stages, want) {
		t.Errorf("stages = %v, want %v", stages, want)
	}
	if !maps.Equal(rep.ByStage, map[string]int{StageEnforced: 2, StageAudited: 1, StageNotInstalled: 1}) || rep.Uncovered != 1 {
		t.Errorf("byStage = %v, uncovered %d", rep.ByStage, rep.Uncovered)
	}
	privileged := rep.Controls[0].Checks
	if len(privileged) != 2 || privileged[0] != (FrameworkCheck{Check: "privileged", Control: "CH-0301", Group: "test",
		Rule: "test-baseline", Stage: StageEnforced, Warnings: 1, Errors: 2}) ||
		privileged[1].Check != "other-privileged" || privileged[1].Stage != StageNotInstalled {
		t.Errorf("5.2.2 checks = %+v", privileged)
	}

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	PrintFramework(cmd, rep)
	for _, line := range strings.Split(out.String(), "\n") {
		if fields := strings.Fields(line); len(fields) > 1 && fields[0] == "5.2.8" && fields[1] != "Uncovered" {
			t.Errorf("5.2.8 is %s, want Uncovered", fields[1])
		}
	}
	for _, s := range []string{"privileged (test-baseline Enforced, 1 warned, 2 blocked)", "run-as-non-root (NotInstalled)",
		"5 controls: 2 Enforced, 1 Audited, 1 NotInstalled, 1 Uncovered"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("table lacks %q:\n%s", s, out.String())
		}
	}
}

// --framework names one of the frameworks given, takes --policy-space, and is not taken with
// --by-rule.
func TestStatusFrameworkFlag(t *testing.T) {
	if StatusCmd(nil, nil, nil).Flags().Lookup("framework") != nil {
		t.Error("--framework without frameworks")
	}
	frameworks := []Framework{{Name: "cis-test"}}
	if cmd := StatusCmd(nil, nil, frameworks); !strings.Contains(cmd.Short, "--framework") {
		t.Errorf("Short = %q", cmd.Short)
	}
	// Flags that pass validation reach the preflight, which stands in for the server here.
	reached := errors.New("reached preflight")
	preflight := func(context.Context) (*cubapi.Client, error) { return nil, reached }
	for args, want := range map[string]string{
		"--framework nonesuch":                                 `unknown framework "nonesuch": want one of cis-test`,
		"--framework cis-test --by-rule":                       "mutually exclusive",
		"--framework cis-test --rule test-baseline":            "--rule goes with --by-rule",
		"--policy-space common":                                "--policy-space goes with --by-rule or --framework",
		"--framework cis-test --policy-space common":           reached.Error(),
		"--by-rule --rule test-baseline --policy-space common": reached.Error(),
	} {
		cmd := StatusCmd(preflight, nil, frameworks)
		cmd.SetArgs(strings.Fields(args))
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", args, err, want)
		}
	}
}
