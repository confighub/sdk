// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"fmt"
	"regexp"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
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
	Data(scalarYamlDoc *gaby.YamlDoc, path string) (any, error)

	// Replace replaces the value of the specified attribute or subpart within
	// the provided string.
	Replace(currentFieldValue string, value any, path string) (string, error)

	// Extract returns the value of the specified attribute or subpart within the
	// provided string.
	Extract(currentFieldValue, path string) (any, error)
}

const EmbeddedAccessorSeparator = "#"

// RegexpAccessor is an EmbeddedAccessor that uses regular expressions to extract
// and insert subparts of a structured string value.
type RegexpAccessor struct {
	RegexpString string
	Regexp       *regexp.Regexp
	SubexpNames  []string
}

var embeddedAccessorMap = map[string]EmbeddedAccessor{}
var embeddedAccessorMutex sync.Mutex

var UnsupportedAccessorType = errors.New("accessor type not supported")
var NoSubexpressions = errors.New("no capturing subexpressions")
var UnsupportedValueType = errors.New("only string values supported currently")
var EmbeddedPathNotFound = errors.New("embedded path not found")

func newEmbeddedAccessor(embeddedAccessorType api.EmbeddedAccessorType, config string) (EmbeddedAccessor, error) {
	switch embeddedAccessorType {
	case api.EmbeddedAccessorRegexp:
		a, err := newRegexpAccessor(config)
		return a, err
	case api.EmbeddedAccessorLine:
		a, err := newLineAccessor(config)
		return a, err
	case api.EmbeddedAccessorJSON:
		a, err := newJSONAccessor(config)
		return a, err
	case api.EmbeddedAccessorYAML:
		a, err := newYAMLAccessor(config)
		return a, err
	default:
		return nil, UnsupportedAccessorType
	}
}

func GetEmbeddedAccessor(embeddedAccessorType api.EmbeddedAccessorType, config string) (EmbeddedAccessor, error) {
	memokey := string(embeddedAccessorType) + "/" + config
	embeddedAccessorMutex.Lock()
	defer embeddedAccessorMutex.Unlock()
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
		return nil, errors.Mark(fmt.Errorf("no capturing subexpressions found in %s", regexpString), NoSubexpressions)
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
		return currentFieldValue, UnsupportedValueType
	}
	i := ra.Regexp.SubexpIndex(path)
	if i < 0 || i >= len(ra.SubexpNames) {
		return currentFieldValue, errors.Mark(fmt.Errorf("subexp %s not found", path), EmbeddedPathNotFound)
	}
	submatchIndices := ra.Regexp.FindStringSubmatchIndex(currentFieldValue)
	if submatchIndices == nil {
		return currentFieldValue, errors.Mark(fmt.Errorf("subexp %s not found", path), EmbeddedPathNotFound)
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
		return errors.Mark(fmt.Errorf("subexp %s not found", path), EmbeddedPathNotFound)
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

func (ra *RegexpAccessor) Extract(currentFieldValue, path string) (any, error) {
	i := ra.Regexp.SubexpIndex(path)
	if i < 0 || i >= len(ra.SubexpNames) {
		return "", EmbeddedPathNotFound
	}
	submatches := ra.Regexp.FindStringSubmatch(currentFieldValue)
	if submatches == nil {
		return "", EmbeddedPathNotFound
	}
	return submatches[i], nil
}

func (ra *RegexpAccessor) Data(scalarYamlDoc *gaby.YamlDoc, path string) (any, error) {
	// TODO: does it make sense to support other data types?
	value, found, err := YamlSafePathGetValue[string](scalarYamlDoc, "", true)
	if !found || err != nil {
		return "", EmbeddedPathNotFound
	}
	return ra.Extract(value, path)
}
