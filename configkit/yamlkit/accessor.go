// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
)

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

// EmbeddedAccessor is used to access attributes embedded in data formats encoded within
// string values within a YAML document. For instance, YAML might be encoded within a YAML
// value. Or it could be as simple as a structured string with distinct sections and separators,
// such as a container image or URL.
type EmbeddedAccessor interface {
	// ExistsP reports whether the specified attribute or subpart exists within
	// the string at the specified YAML document node.
	ExistsP(scalarYamlDoc *gaby.YamlDoc, path string) bool

	// SetP sets the specified attribute or subpart within the string at the
	// specified YAML document node.
	SetP(scalarYamlDoc *gaby.YamlDoc, value any, path string) error

	// Data returns the value of the specified attribute or subpart embedded
	// within the string at the specified YAML document node.
	Data(scalarYamlDoc *gaby.YamlDoc, path string) any

	// Replace replaces the value of the specified attribute or subpart within
	// the provided string.
	Replace(currentFieldValue string, value any, path string) (string, error)

	// Extract returns the value of the specified attribute or subpart within the
	// provided string.
	Extract(currentFieldValue, path string) any
}

// RegexpAccessor is an EmbeddedAccessor that uses regular expressions to extract
// and insert subparts of a structured string value.
type RegexpAccessor struct {
	RegexpString string
	Regexp       *regexp.Regexp
	SubexpNames  []string
}

var embeddedAccessorMap = map[string]EmbeddedAccessor{}

func newEmbeddedAccessor(embeddedAccessorType api.EmbeddedAccessorType, config string) (EmbeddedAccessor, error) {
	switch embeddedAccessorType {
	case api.EmbeddedAccessorRegexp:
		a, err := newRegexpAccessor(config)
		return a, err
	default:
		return nil, errors.New("accessor type not supported")
	}
}

func GetEmbeddedAccessor(embeddedAccessorType api.EmbeddedAccessorType, config string) (EmbeddedAccessor, error) {
	memokey := string(embeddedAccessorType) + "/" + config
	a, memoized := embeddedAccessorMap[memokey]
	if !memoized {
		var err error
		a, err = newEmbeddedAccessor(embeddedAccessorType, config)
		if err != nil {
			return a, err
		}
		embeddedAccessorMap[memokey] = a
	}
	return a, nil
}

func newRegexpAccessor(regexpString string) (*RegexpAccessor, error) {
	ra := RegexpAccessor{RegexpString: regexpString}
	var err error
	ra.Regexp, err = regexp.Compile(regexpString)
	if err != nil {
		return nil, err
	}
	ra.SubexpNames = ra.Regexp.SubexpNames()
	if len(ra.SubexpNames) <= 1 {
		return nil, fmt.Errorf("no capturing subexpressions found in %s", regexpString)
	}
	return &ra, nil
}

func (ra *RegexpAccessor) ExistsP(scalarYamlDoc *gaby.YamlDoc, path string) bool {
	i := ra.Regexp.SubexpIndex(path)
	if i < 0 {
		return false
	}
	value, found, err := YamlSafePathGetValue[string](scalarYamlDoc, "", true)
	if !found || err != nil {
		return false
	}
	submatches := ra.Regexp.FindStringSubmatch(value)
	if submatches == nil {
		return false
	}
	return true
}

func (ra *RegexpAccessor) Replace(currentFieldValue string, value any, path string) (string, error) {
	// TODO: does it make sense to support other data types?
	stringValue, ok := value.(string)
	if !ok {
		return currentFieldValue, fmt.Errorf("only string values supported currently")
	}
	i := ra.Regexp.SubexpIndex(path)
	if i < 0 || i >= len(ra.SubexpNames) {
		return currentFieldValue, fmt.Errorf("subexp %s not found", path) // TODO: create an error type
	}
	submatchIndices := ra.Regexp.FindStringSubmatchIndex(currentFieldValue)
	if submatchIndices == nil {
		return currentFieldValue, fmt.Errorf("subexp %s not found", path)
	}
	submatchStart := submatchIndices[2*i]
	submatchEnd := submatchIndices[2*i+1]
	runes := []rune(currentFieldValue)
	beginning := runes[:submatchStart]
	end := runes[submatchEnd:]
	newFieldValue := string(beginning) + stringValue + string(end)
	return newFieldValue, nil
}

func (ra *RegexpAccessor) SetP(scalarYamlDoc *gaby.YamlDoc, value any, path string) error {
	currentFieldValue, found, err := YamlSafePathGetValue[string](scalarYamlDoc, "", true)
	if !found || err != nil {
		return fmt.Errorf("subexp %s not found", path)
	}
	newFieldValue, err := ra.Replace(currentFieldValue, value, path)
	if err != nil {
		return err
	}
	if newFieldValue == currentFieldValue {
		return nil // nothing to do
	}
	_, err = scalarYamlDoc.Set(newFieldValue)
	return err
}

func (ra *RegexpAccessor) Extract(currentFieldValue, path string) any {
	i := ra.Regexp.SubexpIndex(path)
	if i < 0 || i >= len(ra.SubexpNames) {
		return ""
	}
	submatches := ra.Regexp.FindStringSubmatch(currentFieldValue)
	if submatches == nil {
		return ""
	}
	return submatches[i]
}

func (ra *RegexpAccessor) Data(scalarYamlDoc *gaby.YamlDoc, path string) any {
	// TODO: does it make sense to support other data types?
	value, found, err := YamlSafePathGetValue[string](scalarYamlDoc, "", true)
	if !found || err != nil {
		return ""
	}
	return ra.Extract(value, path)
}
