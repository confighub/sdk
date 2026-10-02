// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package entityfilter

import (
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"
)

// The forms of the keys of map attributes, which an expression names after a dot. ConfigHub
// validates the keys it stores against the same expressions.
const (
	// SlugCoreChars are the characters of a slug. A slash is not one, so <space>/<slug> is
	// unambiguous.
	SlugCoreChars = "\\-_.A-Za-z0-9"
	// SlugPrefixRegexpString matches a slug at the start of a string.
	SlugPrefixRegexpString = "^[A-Za-z0-9]([" + SlugCoreChars + "]*[A-Za-z0-9])?"
	// LabelKeyPrefixRegexpString matches a label key at the start of a string: more permissive
	// than Kubernetes, with dots and slashes anywhere inside. Annotation keys and the keys of
	// DeleteGates and DestroyGates have the same form.
	LabelKeyPrefixRegexpString = "^[A-Za-z0-9]([\\-_\\./A-Za-z0-9]*[A-Za-z0-9])?"
)

var (
	slugPrefixRegexp     = regexp.MustCompile(SlugPrefixRegexpString)
	labelKeyPrefixRegexp = regexp.MustCompile(LabelKeyPrefixRegexpString)
)

// The gates maps.
const (
	deleteGatesAttribute  = "DeleteGates"
	destroyGatesAttribute = "DestroyGates"
)

// parseJSONPath parses the optional dotted path following a DataTypeJSON attribute, as in
// `Data.spec.replicas`. An attribute with no path (`Data IS NULL`, `Data ? 'kind'`) is valid
// and yields an empty path.
func parseJSONPath(decodedQueryString string, attributeName string) (string, string, error) {
	if !strings.HasPrefix(decodedQueryString, ".") {
		return decodedQueryString, "", nil
	}
	remaining := decodedQueryString[1:]
	pos := jsonPathRegexp.FindStringIndex(remaining)
	if pos == nil {
		if err := unsupportedJSONPathSyntax(remaining, attributeName); err != nil {
			return decodedQueryString, "", err
		}
		return decodedQueryString, "", errors.Newf("invalid %s path at `%s`", attributeName, remaining)
	}
	path := remaining[pos[0]:pos[1]]
	if len(path) > MaxJSONPathLength {
		return decodedQueryString, "", errors.Newf("%s path exceeds maximum length of %d", attributeName, MaxJSONPathLength)
	}
	rest := remaining[pos[1]:]
	// A path that stops at one of the configuration-data filter's untranslatable constructs
	// would otherwise be reported as an invalid operator, which says nothing about the real
	// problem. An embedded accessor attaches directly to the segment it reads into (`image#tag`),
	// while a pattern follows a separator.
	unconsumed := rest
	if strings.HasPrefix(unconsumed, ".") {
		unconsumed = unconsumed[1:]
	}
	if err := unsupportedJSONPathSyntax(unconsumed, attributeName); err != nil {
		return decodedQueryString, "", err
	}
	return rest, path, nil
}

// unsupportedJSONPathSyntax names the constructs a data path cannot continue with, so that they
// do not surface as a bare "invalid operator", which says nothing about the real problem.
//
// Wildcards and associative matching are supported and translated by the server; they
// reach this function only where the grammar disallows them, which is on the right of a split
// path. Embedded accessors and parameter bindings are never supported: the first reaches inside
// a scalar string using Go accessor code, and the second binds a value to a function argument
// rather than selecting anything, so neither has a SQL/JSON path equivalent.
func unsupportedJSONPathSyntax(remaining string, attributeName string) error {
	switch {
	case strings.HasPrefix(remaining, "#"):
		return errors.Newf(
			"embedded accessors (`#accessor`) are not supported in %s paths; they read inside a "+
				"scalar value, which has no SQL equivalent -- use --where-data on units instead",
			attributeName)
	case strings.HasPrefix(remaining, "@"):
		return errors.Newf(
			"parameter bindings (`@key:name`) are not supported in %s paths; they bind function "+
				"arguments rather than selecting a value",
			attributeName)
	case strings.HasPrefix(remaining, "*"), strings.HasPrefix(remaining, "?"), strings.HasPrefix(remaining, "|"):
		return errors.Newf(
			"patterns are not supported here in %s paths; the right side of a split path (`.|`) "+
				"admits only plain keys and indexes",
			attributeName)
	}
	return nil
}

// singleKeyForms gives the key form of each map keyed by one token. Labels, Annotations, Facts,
// DeleteGates and DestroyGates take label keys, which permit dots and slashes, so a namespaced Fact
// such as Cluster.KubernetesVersion is matched greedily up to the operator and needs no quoting.
// Guards take the AttributeName character class, which is what api.ValidateAnnotationKey enforces
// on the way in, so a dot ends a guard key.
var singleKeyForms = map[string]*regexp.Regexp{
	"Labels":              labelKeyPrefixRegexp,
	"Annotations":         labelKeyPrefixRegexp,
	"Facts":               labelKeyPrefixRegexp,
	deleteGatesAttribute:  labelKeyPrefixRegexp,
	destroyGatesAttribute: labelKeyPrefixRegexp,
	"Guards":              functionAttributeNameRegexp,
}

// parseSingleKeyMapKey parses <map>.<key> for a map keyed by one token, in the form
// singleKeyForms gives that map.
func parseSingleKeyMapKey(decodedQueryString string, attributeName string) (string, string, error) {
	keyRegexp, ok := singleKeyForms[attributeName]
	if !ok {
		return decodedQueryString, "", errors.Newf("%s has no key form", attributeName)
	}
	return parseMapKey(decodedQueryString, attributeName,
		&mapKeyParseSpec{AttributeName: attributeName, Regexes: []*regexp.Regexp{keyRegexp}})
}

// parseValuesMapKey parses the keys of Values, and of ValueTriggerIDs, which is keyed the same way:
// space-slug/trigger-slug/attribute-name, or the 2-part
// trigger-slug/attribute-name that Revisions written before the Space slug was added still hold.
func parseValuesMapKey(decodedQueryString string, attributeName string) (string, string, error) {
	spec3 := &mapKeyParseSpec{
		AttributeName: attributeName,
		Regexes:       []*regexp.Regexp{slugPrefixRegexp, slugPrefixRegexp, functionAttributeNameRegexp},
		Separators:    []string{"/", "/"},
	}
	remaining, mapKey, err := parseMapKey(decodedQueryString, attributeName, spec3)
	if err == nil {
		return remaining, mapKey, nil
	}
	spec2 := &mapKeyParseSpec{
		AttributeName: attributeName,
		Regexes:       []*regexp.Regexp{slugPrefixRegexp, functionAttributeNameRegexp},
		Separators:    []string{"/"},
	}
	return parseMapKey(decodedQueryString, attributeName, spec2)
}

// parseUUIDMapKey parses Field.key syntax for UUIDStringMap attributes
func parseUUIDMapKey(decodedQueryString string, attributeName string) (string, string, error) {
	spec := &mapKeyParseSpec{
		AttributeName: attributeName,
		Regexes:       []*regexp.Regexp{uuidPrefixRegexp},
		Separators:    []string{},
	}
	return parseMapKey(decodedQueryString, attributeName, spec)
}

// isValidationMapAttribute reports whether an attribute holds validation gate names, which are
// keyed by "<space slug>/<trigger slug>/<function name>" rather than by a plain label key. The
// deprecated ApplyGates and ApplyWarnings aliases are keyed the same way.
func isValidationMapAttribute(attributeName string) bool {
	switch attributeName {
	case "ValidationErrors", "ValidationWarnings", "ValidationPassed", "ApplyGates", "ApplyWarnings":
		return true
	}
	return false
}

// parseValidationErrorsMapKey parses gate-name map keys: those of ValidationErrors, ValidationWarnings
// and ValidationPassed, and of ValidationTriggerIDs, which is keyed the same way.
// Supports both 3-part (space-slug/trigger-slug/function-name) and legacy 2-part (trigger-slug/function-name) formats.
// The function name may carry its position in its Trigger's Invocation, as function-name:position.
func parseValidationErrorsMapKey(decodedQueryString string, attributeName string) (string, string, error) {
	// Try 3-part format first: space-slug/trigger-slug/function-name
	spec3 := &mapKeyParseSpec{
		AttributeName: attributeName,
		Regexes:       []*regexp.Regexp{slugPrefixRegexp, slugPrefixRegexp, gateFunctionRegexp},
		Separators:    []string{"/", "/"},
	}
	remaining, mapKey, err := parseMapKey(decodedQueryString, attributeName, spec3)
	if err == nil {
		return remaining, mapKey, nil
	}
	// Fall back to 2-part format: trigger-slug/function-name
	spec2 := &mapKeyParseSpec{
		AttributeName: attributeName,
		Regexes:       []*regexp.Regexp{slugPrefixRegexp, gateFunctionRegexp},
		Separators:    []string{"/"},
	}
	return parseMapKey(decodedQueryString, attributeName, spec2)
}

// parsePermissionsMapKey parses Permissions.<action>.UserIDs syntax
// For LEN(Permissions), we may just have the action key or nothing at all.
// For LEN(Permissions.Edit.UserIDs), we need both action and field.
// For Permissions.Edit.UserIDs ? <uuid>, we need both action and field.
// Returns: remaining string, action key, nested field key, error
func parsePermissionsMapKey(decodedQueryString string, attributeName string, lengthExpr bool) (string, string, string, error) {
	if attributeName != "Permissions" {
		return decodedQueryString, "", "", errors.Newf("unexpected Permissions attribute %s; only Permissions supports this syntax", attributeName)
	}

	// Expect initial dot
	if !strings.HasPrefix(decodedQueryString, ".") {
		// No dot means no key - this is only valid for LEN(Permissions)
		if lengthExpr {
			return decodedQueryString, "", "", nil
		}
		return decodedQueryString, "", "", errors.New("Permissions requires .<action>.UserIDs syntax for containment expressions")
	}
	decodedQueryString = decodedQueryString[1:]

	// Parse the action key (e.g., "Edit", "Manage", "View", etc.)
	// ActionCategory names are PascalCase identifiers
	actionPos := attributeNameRegexp.FindStringIndex(decodedQueryString)
	if actionPos == nil {
		return decodedQueryString, "", "", errors.Newf("invalid Permissions action key at %s", decodedQueryString)
	}
	actionKey := decodedQueryString[actionPos[0]:actionPos[1]]
	decodedQueryString = decodedQueryString[actionPos[1]:]

	// For LEN(Permissions.Edit), the action key is enough
	// Check if there's a second dot for the nested field
	if !strings.HasPrefix(decodedQueryString, ".") {
		// For LEN(Permissions.Edit), this is valid
		if lengthExpr {
			// But we need to know which field - default to UserIDs
			return decodedQueryString, actionKey, "UserIDs", nil
		}
		// For containment expressions, we need the full path
		return decodedQueryString, "", "", errors.New("Permissions requires .<action>.UserIDs syntax for containment expressions")
	}
	decodedQueryString = decodedQueryString[1:]

	// Parse the nested field key (currently only UserIDs is supported)
	nestedPos := attributeNameRegexp.FindStringIndex(decodedQueryString)
	if nestedPos == nil {
		return decodedQueryString, "", "", errors.Newf("invalid Permissions nested field at %s", decodedQueryString)
	}
	nestedKey := decodedQueryString[nestedPos[0]:nestedPos[1]]
	decodedQueryString = decodedQueryString[nestedPos[1]:]

	// Validate the nested key - currently only UserIDs is supported
	if nestedKey != "UserIDs" {
		return decodedQueryString, "", "", errors.Newf("unsupported Permissions field %s; only UserIDs is supported", nestedKey)
	}

	return decodedQueryString, actionKey, nestedKey, nil
}

// mapKeyParseSpec defines how to parse a map key for a specific attribute
type mapKeyParseSpec struct {
	AttributeName string
	Regexes       []*regexp.Regexp
	Separators    []string
}

// parseMapKey parses map key syntax using configurable regex patterns and separators
func parseMapKey(decodedQueryString string, attributeName string, spec *mapKeyParseSpec) (string, string, error) {
	if attributeName != spec.AttributeName {
		return decodedQueryString, "", errors.Newf("unexpected %s attribute %s; only %s supports dot notation", spec.AttributeName, attributeName, spec.AttributeName)
	}

	// Expect initial dot
	if strings.HasPrefix(decodedQueryString, ".") {
		decodedQueryString = decodedQueryString[1:]
	} else {
		// If there is no map key, it should be a "?" operator
		return decodedQueryString, "", nil
	}

	var keyParts []string

	// Process each regex pattern with expected separators
	for i, regex := range spec.Regexes {
		pos := regex.FindStringIndex(decodedQueryString)
		if pos == nil {
			return decodedQueryString, "", errors.Newf("invalid %s key at %s", spec.AttributeName, decodedQueryString)
		}
		part := decodedQueryString[pos[0]:pos[1]]
		keyParts = append(keyParts, part)
		decodedQueryString = decodedQueryString[pos[1]:]

		// Check for expected separator (if not the last regex)
		if i < len(spec.Separators) {
			expectedSep := spec.Separators[i]
			if strings.HasPrefix(decodedQueryString, expectedSep) {
				decodedQueryString = decodedQueryString[len(expectedSep):]
			} else {
				return decodedQueryString, "", errors.Newf("expected '%s' at `%s`", expectedSep, decodedQueryString)
			}
		}
	}

	// Join parts with separators to form the final map key
	mapKey := keyParts[0]
	for i := 1; i < len(keyParts); i++ {
		mapKey += spec.Separators[i-1] + keyParts[i]
	}

	return decodedQueryString, mapKey, nil
}
