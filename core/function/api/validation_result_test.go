// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import "testing"

func TestParseScore(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Score
	}{
		{"", ScoreNone},
		{"High", ScoreHigh},
		{"high", ScoreHigh},
		{"HIGH", ScoreHigh},
		{"critical", ScoreCritical},
		{"Medium", ScoreMedium},
		{"low", ScoreLow},
	} {
		got, err := ParseScore(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseScore(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"urgent", "info", "hi", "none"} {
		if _, err := ParseScore(bad); err == nil {
			t.Errorf("ParseScore(%q) was accepted", bad)
		}
	}
}

// ValidateScore is the stricter sibling: it requires the canonical spelling and
// treats the empty string as an error rather than as "no score".
func TestValidateScoreIsStricter(t *testing.T) {
	if _, err := ValidateScore(""); err == nil {
		t.Error(`ValidateScore("") was accepted`)
	}
	if _, err := ValidateScore("high"); err == nil {
		t.Error(`ValidateScore("high") was accepted`)
	}
	if got, err := ValidateScore("High"); err != nil || got != ScoreHigh {
		t.Errorf("ValidateScore(\"High\") = %q, %v", got, err)
	}
}

// Every Score the vocabulary ranks has a name ParseScore resolves, so a severity
// flag can name any score a finding can carry.
func TestParseScoreCoversTheVocabulary(t *testing.T) {
	for score := range ScoreToNumber {
		if score == ScoreNone {
			continue
		}
		got, err := ParseScore(string(score))
		if err != nil || got != score {
			t.Errorf("ParseScore(%q) = %q, %v", score, got, err)
		}
	}
}
