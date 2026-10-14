// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Validation is a validating Trigger's failed check that names a field: the function did not
// pass on the unit's configuration, and its result named this field, or a value containing
// it, as one that failed. A check that failed without naming fields is about the unit as a
// whole, and is on no field.
type Validation struct {
	// Gate names the check: "<space slug>/<trigger slug>/<function name>".
	Gate string
	// Warning is true for a Trigger with Warn set, whose failures do not block a release.
	Warning bool `json:",omitempty"`
	// Function is the validating function that failed.
	Function string `json:",omitempty"`
	// Path is what the result named, when it named an ancestor of the field or the field
	// by its position in an array rather than by the path blame uses.
	Path string `json:",omitempty"`
	// Score is the severity the function gave the finding, if it gives one.
	Score string `json:",omitempty"`
	// Issues say what is wrong, as the function put it: the failed attribute's own, or the
	// result's when the attribute has none, as a CEL expression's cannot.
	Issues []goclientnew.Issue `json:",omitempty"`
	// Details are the result's free-text messages, where the function gave no Issues.
	Details []string `json:",omitempty"`
	// Value is what the function saw at the path, when it reported it.
	Value any `json:",omitempty"`
}

// blameValidationsForPath returns the unit's failed checks that name one field. A check is a
// warning when its gate is in ValidationWarnings, and an error otherwise: a gate is in
// ValidationErrors or ValidationWarnings by the Warn setting of the Trigger that ran it.
//
// A result names a field the way its function chose to. vet-placeholders and the functions
// built on the path registry name an array element by its merge key, as blame does; a CEL
// expression names whatever path it wrote, often by position, as in containers.0.image. The
// field's stored path carries its position as an anchor (?name=web;@0), which is what lets
// both forms be matched.
func blameValidationsForPath(identifier api.ResourceIdentifier, unit *goclientnew.Unit,
	resourceType, resourceName, resourceNameCore, path, storedPath string) []Validation {
	if len(unit.ValidationResults) == 0 {
		return nil
	}
	positional := positionalPath(storedPath)
	gates := make([]string, 0, len(unit.ValidationResults))
	for gate := range unit.ValidationResults {
		gates = append(gates, gate)
	}
	sort.Strings(gates)

	var matched []Validation
	for _, gate := range gates {
		warning := unit.ValidationWarnings[gate]
		if !warning && !unit.ValidationErrors[gate] {
			continue
		}
		for _, result := range unit.ValidationResults[gate] {
			if result.Passed || result.FailedAttributes == nil {
				continue
			}
			for _, attribute := range *result.FailedAttributes {
				failedPath := AuthoredPath(attribute.Path)
				if !blamePathCovers(failedPath, path) && !blamePathCovers(failedPath, positional) {
					continue
				}
				if _, found := matchBlameResourceInfo(identifier, []*goclientnew.ResourceInfo{{
					ResourceType:             attribute.ResourceType,
					ResourceName:             attribute.ResourceName,
					ResourceNameWithoutScope: attribute.ResourceNameWithoutScope,
				}}, resourceType, resourceName, resourceNameCore); !found {
					continue
				}
				validation := Validation{
					Gate:     gate,
					Warning:  warning,
					Function: result.FunctionName,
					Score:    attribute.Score,
					Issues:   attribute.Issues,
					Value:    attribute.Value,
				}
				if len(validation.Issues) == 0 {
					validation.Issues = result.Issues
				}
				if len(validation.Issues) == 0 {
					validation.Details = result.Details
				}
				if failedPath != path {
					validation.Path = failedPath
				}
				if validation.Score == "" {
					validation.Score = result.MaxScore
				}
				matched = append(matched, validation)
			}
		}
	}
	return matched
}

// positionalPath is a stored path with each associative segment written as the position its
// anchor records: spec.containers.?name=web;@0.image is spec.containers.0.image. A segment
// with no anchor is left as it is.
func positionalPath(storedPath string) string {
	if !strings.Contains(storedPath, ";@") {
		return storedPath
	}
	segments := strings.Split(storedPath, ".")
	for i, segment := range segments {
		if !strings.HasPrefix(segment, "?") {
			continue
		}
		if _, anchor, found := strings.Cut(segment, ";@"); found {
			if _, err := strconv.Atoi(anchor); err == nil {
				segments[i] = anchor
			}
		}
	}
	return strings.Join(segments, ".")
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
