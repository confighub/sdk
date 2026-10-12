// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout returns what fn printed to stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	output := make(chan string)
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, r)
		output <- b.String()
	}()
	defer func() {
		os.Stdout = stdout
	}()
	fn()
	_ = w.Close()
	return <-output
}

// A command that waits for triggers after printing its result must not add to a result a
// program parses: `cub link update -o json ...` has to print JSON and nothing else.
func TestPrintAwaitingTriggers(t *testing.T) {
	tests := []struct {
		name         string
		quiet        bool
		outputFormat string
		jsonOutput   bool
		showSection  string
		want         bool
	}{
		{name: "default output", want: true},
		{name: "wide output", outputFormat: "wide", want: true},
		{name: "custom columns", outputFormat: "custom-columns=Slug", want: true},
		{name: "quiet", quiet: true, want: false},
		{name: "json", outputFormat: "json", want: false},
		{name: "yaml", outputFormat: "yaml", want: false},
		{name: "jq", outputFormat: "jq=.Slug", want: false},
		{name: "yq", outputFormat: "yq=.Slug", want: false},
		{name: "name", outputFormat: "name", want: false},
		{name: "deprecated --json", jsonOutput: true, want: false},
		{name: "function --show data", showSection: "data", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			savedQuiet, savedOutputFormat, savedJSON, savedShow := quiet, outputFormat, jsonOutput, showSection
			t.Cleanup(func() {
				quiet, outputFormat, jsonOutput, showSection = savedQuiet, savedOutputFormat, savedJSON, savedShow
			})
			quiet, outputFormat, jsonOutput, showSection = tc.quiet, tc.outputFormat, tc.jsonOutput, tc.showSection

			got := captureStdout(t, printAwaitingTriggers)
			if printed := strings.Contains(got, "Awaiting triggers"); printed != tc.want {
				t.Errorf("printed %q; want the message printed: %v", got, tc.want)
			}
		})
	}
}

// Every command that waits for triggers says so through printAwaitingTriggers, so that one rule
// decides when the message is left out of the output.
func TestAwaitingTriggersPrintedOnlyByHelper(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		count := strings.Count(string(content), `"Awaiting triggers...`)
		if file == "unit_update.go" {
			count--
		}
		if count != 0 {
			t.Errorf("%s prints \"Awaiting triggers...\" itself; call printAwaitingTriggers instead", file)
		}
	}
}
