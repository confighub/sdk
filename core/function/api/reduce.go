// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"github.com/google/uuid"
)

// A reduce runs functions on many Units and then summarizes what they returned. Each Unit's
// functions are the map: functions that read configuration and return an AttributeValueList. Each
// value they return becomes a row, tagged with the Unit it came from. A key expression puts each
// row in one or more groups, and a reducer, a function whose signature's InputType is
// AttributeValueList, is run once per group on the group's rows instead of on configuration data.

// RowOrigin says where a row came from: the Unit whose configuration a function returned it
// from, the Unit's Space, and its Target. Every field is present, so a key expression or a reducer
// can read one without first asking whether it is there: the label maps are empty when there are
// none, and TargetID is null and the Target's slugs empty for a Unit with no Target.
type RowOrigin struct {
	UnitID      uuid.UUID         `description:"The Unit the row was returned from"`
	UnitSlug    string            `description:"The Unit's slug"`
	UnitLabels  map[string]string `description:"The Unit's labels, empty if it has none"`
	SpaceID     uuid.UUID         `description:"The Unit's Space"`
	SpaceSlug   string            `description:"The slug of the Unit's Space"`
	SpaceLabels map[string]string `description:"The labels of the Unit's Space, empty if it has none"`
	// A Target's slug is unique only within its Space, so a key naming a cluster by slug names it
	// with TargetSpaceSlug too, or uses TargetID.
	TargetID        *uuid.UUID `description:"The Target the Unit is released to; null if it has none"`
	TargetSlug      string     `description:"The slug of the Target the Unit is released to, unique only within its Space; empty if it has none"`
	TargetSpaceSlug string     `description:"The slug of the Space of the Target the Unit is released to; empty if it has none"`
	RevisionNum     int64      `description:"The revision of the Unit the row was read from, its head"`
}

// Row is a value a function returned, with where it came from.
type Row struct {
	AttributeValue
	Origin RowOrigin `description:"Where the row came from"`
}

// RowList is the input of a reducer: the rows of one group.
type RowList []Row

// TakesRows reports whether a function is a reducer, whose input is the rows of a group rather
// than configuration data.
func (fs *FunctionSignature) TakesRows() bool {
	return fs.InputType == DataTypeAttributeValueList
}
