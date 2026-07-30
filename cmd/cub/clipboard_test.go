// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

func TestClipboardCandidates(t *testing.T) {
	noEnv := func(string) string { return "" }
	wayland := func(k string) string {
		if k == "WAYLAND_DISPLAY" {
			return "wayland-0"
		}
		return ""
	}

	cases := []struct {
		name string
		goos string
		env  func(string) string
		want []string // argv[0] of each candidate, in order
	}{
		{"darwin", "darwin", noEnv, []string{"pbcopy"}},
		{"windows", "windows", noEnv, []string{"clip"}},
		{"linux x11", "linux", noEnv, []string{"xclip", "xsel", "wl-copy", "clip.exe"}},
		{"linux wayland", "linux", wayland, []string{"wl-copy", "xclip", "xsel", "clip.exe"}},
	}
	for _, c := range cases {
		var got []string
		for _, argv := range clipboardCandidates(c.goos, c.env) {
			got = append(got, argv[0])
		}
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: candidates = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestClipboardCommandReportsWhatItTried(t *testing.T) {
	// Whether or not the tools are installed on the machine running the tests,
	// every candidate up to the one that resolved is reported, so the caller can
	// explain why it fell back to printing the value.
	argv, tried := clipboardCommand("linux", func(string) string { return "" })
	if len(tried) == 0 {
		t.Fatal("tried should never be empty")
	}
	if argv == nil {
		if want := []string{"xclip", "xsel", "wl-copy", "clip.exe"}; strings.Join(tried, ",") != strings.Join(want, ",") {
			t.Errorf("with no clipboard tool available, tried = %v, want %v", tried, want)
		}
		return
	}
	if !strings.HasSuffix(argv[0], tried[len(tried)-1]) {
		t.Errorf("resolved %q should be the last candidate tried (%v)", argv[0], tried)
	}
}
