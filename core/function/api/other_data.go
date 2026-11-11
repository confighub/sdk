// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
)

// A function that reads configuration besides the Unit it runs on is passed it as OtherData, by
// key. The function lists the keys it reads in its signature's OtherDataExpected, and the caller
// says where the data under each key comes from:
//
//   - Another revision of the same Unit, named by a revision specifier such as
//     LastReleasedRevisionNum or Before:HeadRevisionNum. The specifier is the key.
//   - Another Unit, named by its ID under a key the function reads, written
//     Unit:<key>:<unit-id> where a single string names it, as in the other_data_source
//     parameter of the function invocation APIs. vet-jsonschema-from-unit reads its schema
//     from the Unit passed as schema, for example.

// MaxOtherDataKeyLength is the longest key a function may list in OtherDataExpected, or a caller
// may pass another Unit under.
const MaxOtherDataKeyLength = 64

// OtherDataUnitPrefix starts the name of another Unit as OtherData: Unit:<key>:<unit-id>.
const OtherDataUnitPrefix = "Unit:"

var otherDataKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidateOtherDataKey checks a key a function lists in OtherDataExpected, or that a caller passes
// another Unit under: letters, digits, '-' and '_', at most MaxOtherDataKeyLength of them.
func ValidateOtherDataKey(key string) error {
	if key == "" {
		return errors.New("an OtherData key cannot be empty")
	}
	if len(key) > MaxOtherDataKeyLength {
		return errors.Newf("OtherData key %q is longer than %d characters", key, MaxOtherDataKeyLength)
	}
	if !otherDataKeyPattern.MatchString(key) {
		return errors.Newf("OtherData key %q may contain only letters, digits, '-' and '_'", key)
	}
	return nil
}

// FormatOtherDataUnit names another Unit to pass as OtherData under key: Unit:<key>:<unit-id>.
func FormatOtherDataUnit(key string, unitID uuid.UUID) string {
	return fmt.Sprintf("%s%s:%s", OtherDataUnitPrefix, key, unitID)
}

// ParseOtherDataUnit reads a name of another Unit to pass as OtherData, Unit:<key>:<unit-id>. It
// returns ok false for a source that does not start with Unit:, such as a revision specifier, and
// an error for one that does but is not of that form.
func ParseOtherDataUnit(source string) (key string, unitID uuid.UUID, ok bool, err error) {
	rest, isUnit := strings.CutPrefix(source, OtherDataUnitPrefix)
	if !isUnit {
		return "", uuid.Nil, false, nil
	}
	key, id, found := strings.Cut(rest, ":")
	if !found {
		return "", uuid.Nil, true, errors.Newf("%q names another Unit as Unit:<key>:<unit-id>, and has no key", source)
	}
	if err := ValidateOtherDataKey(key); err != nil {
		return "", uuid.Nil, true, err
	}
	unitID, err = uuid.Parse(id)
	if err != nil || unitID == uuid.Nil {
		return "", uuid.Nil, true, errors.Newf("%q names another Unit as Unit:<key>:<unit-id>, and %q is not a Unit ID", source, id)
	}
	return key, unitID, true, nil
}
