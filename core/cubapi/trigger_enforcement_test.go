// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import "testing"

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
