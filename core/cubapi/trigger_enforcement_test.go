// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"slices"
	"testing"
)

func TestParseTriggerPriority(t *testing.T) {
	for in, want := range map[string]string{"high": "High", "MEDIUM": "Medium", "Low": "Low", "": "", "unspecified": "", "-": ""} {
		got, err := ParseTriggerPriority(in)
		if err != nil || got != want {
			t.Errorf("ParseTriggerPriority(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseTriggerPriority("urgent"); err == nil {
		t.Error("ParseTriggerPriority(urgent) accepted an unknown priority")
	}
}

func TestParseEnforcementTime(t *testing.T) {
	for in, want := range map[string]string{
		"2026-12-31":                "2026-12-31T00:00:00Z",
		"2026-12-31T08:00:00-08:00": "2026-12-31T16:00:00Z",
		"":                          "",
		"-":                         "",
	} {
		got, err := ParseEnforcementTime(in)
		if err != nil || got != want {
			t.Errorf("ParseEnforcementTime(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseEnforcementTime("next week"); err == nil {
		t.Error("ParseEnforcementTime accepted text that is not a time")
	}
}

func TestTriggerControls(t *testing.T) {
	got := TriggerControls(map[string]string{"Controls": "CH-0301, CH-0302,,CH-0310"})
	if want := []string{"CH-0301", "CH-0302", "CH-0310"}; !slices.Equal(got, want) {
		t.Errorf("TriggerControls = %q, want %q", got, want)
	}
	if got := TriggerControls(nil); got != nil {
		t.Errorf("TriggerControls(nil) = %q, want none", got)
	}
}

func TestFormatControls(t *testing.T) {
	for _, tc := range []struct {
		controls []string
		want     string
	}{
		{nil, ""},
		{[]string{"CH-0605"}, "CH-0605"},
		{[]string{"CH-0385", "CH-0386"}, "CH-0385,CH-0386"},
		{[]string{"CH-0301", "CH-0302", "CH-0303", "CH-0304"}, "CH-0301–CH-0304"},
		{[]string{"CH-0312", "CH-0313", "CH-0315", "CH-0316", "CH-0317"}, "CH-0312,CH-0313,CH-0315–CH-0317"},
		{[]string{"CH-0101", "CH-0102", "KS-0103"}, "CH-0101,CH-0102,KS-0103"},
		{[]string{"CH-0999", "CH-1000", "CH-1001"}, "CH-0999–CH-1001"},
		{[]string{"CH-099", "CH-0100", "CH-0101"}, "CH-099,CH-0100,CH-0101"},
		{[]string{"C-0012", "custom", "C-0013"}, "C-0012,custom,C-0013"},
	} {
		if got := FormatControls(tc.controls); got != tc.want {
			t.Errorf("FormatControls(%q) = %q, want %q", tc.controls, got, tc.want)
		}
	}
}
