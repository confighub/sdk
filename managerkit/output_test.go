// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package managerkit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type row struct {
	Slug     string `json:"slug"`
	Replicas int    `json:"replicas"`
}

func TestRenderHandlesValueProjections(t *testing.T) {
	v := []row{{Slug: "web", Replicas: 3}}
	for _, tc := range []struct {
		output string
		want   string
	}{
		{"json", `"slug": "web"`},
		{"", `"slug": "web"`}, // an empty -o is the default, which is JSON
		{"yaml", "slug: web"},
		{"jq=.[0].slug", "web"},
		{"yq=.[0].replicas", "3"},
	} {
		var buf bytes.Buffer
		done, err := Render(&buf, tc.output, v)
		if err != nil {
			t.Errorf("-o %q: %v", tc.output, err)
			continue
		}
		if !done {
			t.Errorf("-o %q was not handled", tc.output)
			continue
		}
		if !strings.Contains(buf.String(), tc.want) {
			t.Errorf("-o %q wrote %q, want it to contain %q", tc.output, buf.String(), tc.want)
		}
	}
}

// A table is the one format Render declines: its columns are a decision about
// what matters, so the command renders it.
func TestRenderDeclinesTable(t *testing.T) {
	var buf bytes.Buffer
	done, err := Render(&buf, "table", []row{{Slug: "web"}})
	if err != nil {
		t.Fatalf("table: %v", err)
	}
	if done {
		t.Error("table was handled, leaving the command's own table unrendered")
	}
	if buf.Len() != 0 {
		t.Errorf("table wrote %q, want nothing", buf.String())
	}
}

// A format cliutil can parse but this command cannot render is refused the same
// way an unparseable one is, and both name the same list -- listing a format the
// command would then refuse is worse than not offering it.
func TestRenderRefusesColumnFormatsAndGarbage(t *testing.T) {
	for _, output := range []string{"name", "wide", "custom-columns=SLUG:.slug", "toml", "jq"} {
		var buf bytes.Buffer
		done, err := Render(&buf, output, []row{{Slug: "web"}})
		if err == nil {
			t.Fatalf("-o %q was accepted", output)
		}
		if done {
			t.Errorf("-o %q reported handled", output)
		}
		if got := err.Error(); !strings.Contains(got, "json, yaml, table, jq=<expr> or yq=<expr>") {
			t.Errorf("-o %q error names the wrong formats: %v", output, got)
		}
		for _, absent := range []string{"custom-columns=<spec>", "wide"} {
			if strings.Contains(err.Error(), absent) && !strings.Contains(output, absent) {
				t.Errorf("-o %q error offers %q, which the command refuses", output, absent)
			}
		}
	}
}

func TestProgressWriterKeepsStructuredStdoutClean(t *testing.T) {
	for _, tc := range []struct {
		output     string
		wantStdout bool
	}{
		{"table", true},
		{"json", false},
		{"", false}, // the default is JSON
		{"yaml", false},
		{"jq=.", false},
		{"yq=.", false},
	} {
		var stdout, stderr bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		_, _ = ProgressWriter(cmd, tc.output).Write([]byte("wired x\n"))
		if got := stdout.Len() > 0; got != tc.wantStdout {
			t.Errorf("-o %q: progress on stdout = %v, want %v (stdout %q, stderr %q)",
				tc.output, got, tc.wantStdout, stdout.String(), stderr.String())
		}
	}
}
