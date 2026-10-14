// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

func TestWrapBlameText(t *testing.T) {
	path := "spec.template.spec.containers.?name=confighub.image"
	if got := wrapBlameText(path, 0, ".,"); got != path {
		t.Errorf("width 0 should leave the path whole, got %q", got)
	}
	if got := wrapBlameText(path, 100, ".,"); got != path {
		t.Errorf("a path that fits should be left whole, got %q", got)
	}
	got := wrapBlameText(path, 20, ".,")
	if want := "spec.template.spec.\ncontainers.\n?name=confighub.\nimage"; got != want {
		t.Errorf("wrapped = %q, want %q", got, want)
	}
	if strings.ReplaceAll(got, "\n", "") != path {
		t.Error("joining the lines should give the path back")
	}
	associative := "spec.?topologyKey=kubernetes~1io/hostname,whenUnsatisfiable=ScheduleAnyway.maxSkew"
	for _, line := range strings.Split(wrapBlameText(associative, 45, ".,"), "\n") {
		if len(line) > 45 {
			t.Errorf("line %q is wider than 45: an associative segment breaks after its commas", line)
		}
	}
	if got := wrapBlameText("abcdefghij", 4, "."); got != "abcd\nefgh\nij" {
		t.Errorf("a piece with nowhere to break should be split at the width, got %q", got)
	}
	if got := wrapBlameText("link deployment-to-configmap-hash", 20, " ", "-@/._"); got != "link deployment-to-\nconfigmap-hash" {
		t.Errorf("a name wider than the column should break after its punctuation, got %q", got)
	}
	if got := wrapBlameText("spec.initContainers.?name=confighub-db-init-and-migrate.image", 30, ".,/", "-_"); got != "spec.initContainers.\n?name=confighub-db-init-and-\nmigrate.image" {
		t.Errorf("a segment wider than the column should break inside its name and nowhere else, got %q", got)
	}
}

// TestFitBlameTable: the table, its trailing padding included, has to be narrower than the
// terminal, or every row wraps onto a second line of blanks. Below about 125 columns the
// columns are at their minimums and the table stops narrowing, which is not tested.
func TestFitBlameTable(t *testing.T) {
	header := blameHeader(false)
	row := []string{
		"8ae5432", "PXLC", "spec.template.spec.containers.?name=confighub.livenessProbe.failureThreshold",
		"link deployment-to-configmap-hash", "jesper@confighub.com", "prod-use2-confighub-app", "219", "222d ago",
	}
	for _, terminal := range []int{130, 160, 200} {
		fitted := [][]string{append([]string(nil), row...)}
		fitBlameTable(fitted, header, false, terminal)
		total := 0
		for column := range header {
			widest := len(header[column])
			for _, line := range strings.Split(fitted[0][column], "\n") {
				widest = max(widest, len(line))
			}
			total += widest + len(tablePadding)
		}
		if total >= terminal {
			t.Errorf("at %d columns the table is %d wide", terminal, total)
		}
		if strings.ReplaceAll(fitted[0][2], "\n", "") != row[2] {
			t.Errorf("at %d columns the path lost characters: %q", terminal, fitted[0][2])
		}
	}
	for name, terminal := range map[string]int{"not a terminal": 0, "-o wide": 100} {
		unfitted := [][]string{append([]string(nil), row...)}
		fitBlameTable(unfitted, blameHeader(name == "-o wide"), name == "-o wide", terminal)
		if unfitted[0][2] != row[2] {
			t.Errorf("%s should not wrap", name)
		}
	}
}
