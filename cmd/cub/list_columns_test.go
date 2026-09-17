// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var listColumnsForCall = regexp.MustCompile(`listColumnsFor\("([^"]+)"\)`)

// TestListColumnsForNamesItsOwnCommand pins each listColumnsFor path to a command that
// exists and takes --columns. A path that matches no command is never the running one, so
// the list would fetch its default fields and render "?" for every other column asked for.
func TestListColumnsForNamesItsOwnCommand(t *testing.T) {
	files, err := filepath.Glob("*_list.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range listColumnsForCall.FindAllStringSubmatch(string(src), -1) {
			seen++
			cmd := findCommand(t, strings.TrimPrefix(m[1], "cub "))
			if cmd.CommandPath() != m[1] {
				t.Errorf("%s: listColumnsFor(%q) resolves to %q", file, m[1], cmd.CommandPath())
			}
			if cmd.Flags().Lookup("columns") == nil {
				t.Errorf("%s: %q has no --columns flag", file, m[1])
			}
		}
	}
	if seen == 0 {
		t.Fatal("found no listColumnsFor calls; the pattern no longer matches the source")
	}
}

// columnsHandledElsewhere names the file that renders a list whose display function is
// shared with other commands and so lives outside the list file.
var columnsHandledElsewhere = map[string]string{
	"user_key_list.go": "user_key.go",
}

// TestColumnsFlagIsNeverIgnored checks that every command file registering --columns
// either renders the requested columns or refuses them. The flag used to be accepted and
// dropped by every list but unit list.
func TestColumnsFlagIsNeverIgnored(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || file == "flags.go" {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`(?m)^\s*addStandardList(Display)?Flags\(`).Match(src) {
			continue
		}
		if other, ok := columnsHandledElsewhere[file]; ok {
			if src, err = os.ReadFile(other); err != nil {
				t.Fatal(err)
			}
		}
		if !regexp.MustCompile(`displayRequestedColumns\(|rejectRequestedColumns\(|effectiveColumns\(\)`).Match(src) {
			t.Errorf("%s registers --columns but neither renders nor rejects requested columns", file)
		}
	}
}

func TestListColumnsForOnlyServesTheRunningCommand(t *testing.T) {
	defer func(c []string, p string) { columns, runningCommandPath = c, p }(columns, runningCommandPath)
	columns = []string{"Unit.Slug"}
	runningCommandPath = "cub unit tree"
	if got := listColumnsFor("cub link list"); got != nil {
		t.Errorf("columns for another command leaked into link list: %v", got)
	}
	runningCommandPath = "cub link list"
	if got := listColumnsFor("cub link list"); len(got) != 1 || got[0] != "Unit.Slug" {
		t.Errorf("running command did not get its columns: %v", got)
	}
}

// A User is not wrapped in an ExtendedUser, so "User.Username" has no User field to step into.
func TestGetValueEntityPrefixOnPlainStruct(t *testing.T) {
	user := &goclientnew.User{Username: "ada"}
	provider := NewDynamicColumnProvider(new(goclientnew.User))
	for _, col := range []string{"Username", "User.Username"} {
		if got := provider.GetValue(user, col); got != "ada" {
			t.Errorf("GetValue(%q) = %q, want %q", col, got, "ada")
		}
	}
}
