// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"fmt"
	"strings"
	"time"

	"github.com/confighub/sdk/core/constants"
)

// ClearValue is the value a command-line flag takes to remove what it sets.
const ClearValue = "-"

// TriggerPriorityUnspecified is what a Trigger without a TriggerPriorityAnnotation is said to
// have. It is never stored: setting it removes the annotation.
const TriggerPriorityUnspecified = "Unspecified"

// ParseTriggerPriority returns the TriggerPriorityAnnotation value s names, matched without
// regard to case. It returns "" for TriggerPriorityUnspecified, for "-" (the CLI's value that
// clears a field), or for an empty s, all of which remove the annotation.
func ParseTriggerPriority(s string) (string, error) {
	for _, p := range []string{constants.TriggerPriorityHigh, constants.TriggerPriorityMedium, constants.TriggerPriorityLow} {
		if strings.EqualFold(s, p) {
			return p, nil
		}
	}
	if s == "" || s == ClearValue || strings.EqualFold(s, TriggerPriorityUnspecified) {
		return "", nil
	}
	return "", fmt.Errorf("priority %q is not one of %s, %s, %s or %s, or - to remove it", s,
		constants.TriggerPriorityHigh, constants.TriggerPriorityMedium, constants.TriggerPriorityLow, TriggerPriorityUnspecified)
}

// ParseEnforcementTime returns s as an RFC 3339 time in UTC, the form of every time in the API, for
// a time annotation such as TriggerEnforcedByAnnotation. It takes RFC 3339, or a date
// (2026-12-31), which is the start of that day in UTC. An empty s, or "-" (the CLI's value that
// clears a field), returns "", which removes the annotation.
func ParseEnforcementTime(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == ClearValue {
		return "", nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return FormatEnforcementTime(t), nil
	}
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return FormatEnforcementTime(t), nil
	}
	return "", fmt.Errorf("time %q is neither RFC 3339 (2026-12-31T00:00:00Z) nor a date (2026-12-31), nor - to remove it", s)
}

// FormatEnforcementTime writes t the way ParseEnforcementTime returns it.
func FormatEnforcementTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
