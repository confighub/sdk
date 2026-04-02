// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
)

// ValidationResult specifies whether a single validation function or sequence of validation
// functions passed for the given configuration Unit.
type ValidationResult struct {
	Passed           bool               `description:"True if valid, false otherwise"`
	Index            int                `description:"Index of the function invocation corresponding to the result. Useful in the case that multiple function invocations in the same executor call return ValidationResultList output."`
	FunctionName     string             `json:",omitempty" description:"Name of the function invocation corresponding to the result"`
	MaxScore         Score              `json:",omitempty" description:"Maximum score of all findings"`
	Details          []string           `json:",omitempty" description:"Deprecated. Use Issues or FailedAttributes instead. Optional list of failure details when not associated with specific attributes/paths."`
	Issues           []Issue            `json:",omitempty" description:"Issues found with the configuration unit that are not associated with specific attributes/paths. Use FailedAttributes where possible."`
	FailedAttributes AttributeValueList `json:",omitempty" description:"optional list of failed attributes/paths and issues found for them. Preferred over Issues and Details."`
}

type ValidationResultList []ValidationResult

var (
	ValidationResultTrue  = ValidationResult{Passed: true}
	ValidationResultFalse = ValidationResult{Passed: false}
)

type Score string

const (
	ScoreCritical = Score("Critical")
	ScoreHigh     = Score("High")
	ScoreMedium   = Score("Medium")
	ScoreLow      = Score("Low")
	ScoreNone     = Score("")
)

var ScoreToNumber = map[Score]int{
	ScoreCritical: 4,
	ScoreHigh:     3,
	ScoreMedium:   2,
	ScoreLow:      1,
	ScoreNone:     0,
}

var NumberToScore = map[int]Score{
	4: ScoreCritical,
	3: ScoreHigh,
	2: ScoreMedium,
	1: ScoreLow,
	0: ScoreNone,
}

func ScoreMax(a, b Score) Score {
	anum := ScoreToNumber[a]
	bnum := ScoreToNumber[b]
	return NumberToScore[max(anum, bnum)]
}

// ValidateScore converts a score string to a Score.
func ValidateScore(s string) (Score, error) {
	score := Score(s)
	_, valid := ScoreToNumber[score]
	if valid && s != "" {
		return score, nil
	}
	return ScoreNone, fmt.Errorf("invalid score %q: must be Critical, High, Medium, or Low", s)
}

// ValueFilter specifies allow and deny rules for value validation.
// For strings: AllowStrings/DenyStrings use exact match sets; AllowRegexp/DenyRegexp use regular expressions.
// AllowStrings and AllowRegexp are mutually exclusive. DenyStrings and DenyRegexp are mutually exclusive.
// For bools: AllowBool specifies the required boolean value (pointer; nil means any bool is allowed).
// For ints: Min and Max specify an inclusive range (pointer; nil means unbounded).
// If AllowStrings is non-empty, only values in the map are allowed.
// Values in DenyStrings are always rejected.
type ValueFilter struct {
	AllowStrings map[string]bool `json:",omitempty" description:"Set of allowed string values; if non-empty, only these values are permitted. Mutually exclusive with AllowRegexp."`
	DenyStrings  map[string]bool `json:",omitempty" description:"Set of denied string values; these values are always rejected. Mutually exclusive with DenyRegexp."`
	AllowRegexp  string          `json:",omitempty" description:"Regular expression for allowed string values; if non-empty, only matching values are permitted. Mutually exclusive with AllowStrings."`
	DenyRegexp   string          `json:",omitempty" description:"Regular expression for denied string values; matching values are always rejected. Mutually exclusive with DenyStrings."`
	AllowBool    *bool           `json:",omitempty" description:"Required boolean value; if set, only this boolean value is permitted"`
	Min          *int            `json:",omitempty" description:"Minimum allowed integer value (inclusive)"`
	Max          *int            `json:",omitempty" description:"Maximum allowed integer value (inclusive)"`
}
