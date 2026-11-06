// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"fmt"
	"sort"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// FormatGuards renders a field's guards the way --guard takes them, sorted so a listing
// is diffable.
func FormatGuards(guards map[string]string) string {
	if len(guards) == 0 {
		return ""
	}
	specs := make([]string, 0, len(guards))
	for key, value := range guards {
		specs = append(specs, fmt.Sprintf("%s=%s", key, value))
	}
	sort.Strings(specs)
	return strings.Join(specs, " ")
}

// FormatConflictDismissal renders a recorded conflict dismissal on one line: what it dismisses,
// the way cub unit conflicts takes it, and where it is recorded.
func FormatConflictDismissal(dismissal ConflictDismissal) string {
	text := dismissal.Reason
	if dismissal.Guard != "" {
		text += " by guard " + dismissal.Guard
		if dismissal.GuardValue != "" {
			text += "=" + dismissal.GuardValue
		}
	}
	if dismissal.Path == "" {
		return text + ", on the resource as a whole"
	}
	return text + ", at " + dismissal.Path
}

// FormatNeeded renders one of a field's needed paths on one line: the attribute, the Link that
// bound it, and what it asks of the value that fills it.
func FormatNeeded(needed NeededPath) string {
	text := attributeName(needed.AttributeName)
	details := needed.Details
	if needed.BoundLink != "" {
		text += ", bound by link " + needed.BoundLink
	} else {
		text += ", not bound by a link"
	}
	var parts []string
	if details != nil {
		if details.Description != "" {
			parts = append(parts, details.Description)
		}
		if len(details.NeededRequired) > 0 {
			parts = append(parts, "requires "+formatAttributeProperties(details.NeededRequired))
		}
		if len(details.NeededPreferred) > 0 {
			parts = append(parts, "prefers "+formatAttributeProperties(details.NeededPreferred))
		}
	}
	// The value is the placeholder the unit had at the path when it was first needed, which
	// is what is left to say what it asked for once a resolve has replaced it.
	if needed.Value != nil {
		parts = append(parts, fmt.Sprintf("placeholder %q", fmt.Sprint(needed.Value)))
	}
	return joinAttributeParts(text, parts)
}

// FormatProvided renders one of a field's provided paths on one line: the attribute, and what
// it offers a needed path to match on.
func FormatProvided(attribute goclientnew.AttributeInfo) string {
	var parts []string
	if details := attribute.Details; details != nil {
		if details.Description != "" {
			parts = append(parts, details.Description)
		}
		if len(details.ProvidedProperties) > 0 {
			parts = append(parts, "offers "+formatAttributeProperties(details.ProvidedProperties))
		}
	}
	return joinAttributeParts(attributeName(attribute.AttributeName), parts)
}

func attributeName(name string) string {
	if name == "" {
		return "(unnamed attribute)"
	}
	return name
}

func joinAttributeParts(text string, parts []string) string {
	if len(parts) == 0 {
		return text
	}
	return text + ": " + strings.Join(parts, "; ")
}

// formatAttributeProperties renders matching properties as k=v pairs, in key order.
func formatAttributeProperties(properties map[string]string) string {
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+properties[key])
	}
	return strings.Join(pairs, ",")
}

// FormatValidation renders one failed check on one line: whether it blocks a release, the
// gate, and what the function said.
func FormatValidation(v Validation) string {
	kind := "error"
	if v.Warning {
		kind = "warning"
	}
	text := kind + " " + v.Gate
	if v.Score != "" {
		text += " (" + v.Score + ")"
	}
	if v.Path != "" {
		text += " at " + v.Path
	}
	var messages []string
	for _, issue := range v.Issues {
		if issue.Message != "" {
			messages = append(messages, issue.Message)
		}
	}
	messages = append(messages, v.Details...)
	if len(messages) == 0 && v.Value != nil {
		// A function that says nothing about a field, as vet-placeholders does, still says
		// what it found there.
		messages = append(messages, fmt.Sprintf("found %q", fmt.Sprint(v.Value)))
	}
	if len(messages) > 0 {
		text += ": " + strings.Join(messages, "; ")
	}
	return text
}

// AuthoredPath renders a stored path the way someone would type it: an associative
// segment keeps its selector and drops the ";@N" anchor, which is positional
// bookkeeping the merge engine keeps and no command accepts.
func AuthoredPath(path string) string {
	if !strings.Contains(path, ";@") {
		return path
	}
	segments := strings.Split(path, ".")
	for i, segment := range segments {
		if !strings.HasPrefix(segment, "?") {
			continue
		}
		if selector, _, found := strings.Cut(segment, ";@"); found {
			segments[i] = selector
		}
	}
	return strings.Join(segments, ".")
}
