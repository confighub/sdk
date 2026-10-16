// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"github.com/google/uuid"
)

// LinkFindingReason says what a Link's resolve could not do.
type LinkFindingReason string

const (
	// LinkFindingNeededNotFound: a resource or path the Link names in the downstream Unit -- a
	// binding's needed path, a DownstreamPath -- is not there, or cannot be written.
	LinkFindingNeededNotFound LinkFindingReason = "NeededNotFound"
	// LinkFindingProvidedNotFound: a resource or path the Link names in the upstream Unit -- a
	// binding's provided path, an UpstreamPath -- is not there, or does not hold a value of the
	// binding's type. Typically the upstream resource was renamed after the Link was written.
	LinkFindingProvidedNotFound LinkFindingReason = "ProvidedNotFound"
	// LinkFindingResolveFailed: the resolve failed, and wrote nothing. Details says why.
	LinkFindingResolveFailed LinkFindingReason = "ResolveFailed"
)

// LinkFinding records something a Link's resolve could not do in the Unit the Link is from. A
// resolve that cannot find what the Link names, or fails outright, leaves the Unit as it was,
// so without a record the Unit would look current while holding a value that is not.
//
// Findings are outstanding until the Link resolves cleanly or is deleted: each resolve of a
// Link replaces the findings that carry its LinkID and leaves the others' alone.
type LinkFinding struct {
	LinkID   uuid.UUID         `json:",omitempty,omitzero" description:"The Link whose resolve recorded the finding"`
	Reason   LinkFindingReason `description:"What the resolve could not do: NeededNotFound, ProvidedNotFound, or ResolveFailed"`
	Resource ResourceInfo      `json:",omitzero" description:"The resource the finding is about, in the Unit Reason names; empty for a finding about the resolve as a whole"`
	Path     ResolvedPath      `json:",omitempty" description:"The path the finding is about; empty for a resource-level finding"`
	Key      string            `json:",omitempty" description:"The Key of the Link's entry the finding is about, when the entry has one"`
	Entry    string            `json:",omitempty" description:"The Link's entry the finding is about, as its list and position, such as ManualBindings[0]"`
	UnitID   uuid.UUID         `json:",omitzero,omitempty" description:"The other Unit involved: the Link's upstream Unit"`
	Details  string            `json:",omitempty" description:"Explanation the Reason alone cannot carry, such as the error a failed resolve reported"`
}

type LinkFindingList []LinkFinding

// ReplaceLinkFindings returns the findings with those of one Link replaced by what its latest
// resolve found. The other Links' findings keep their order, and the Link's new ones follow.
func ReplaceLinkFindings(findings LinkFindingList, linkID uuid.UUID, replacement LinkFindingList) LinkFindingList {
	var result LinkFindingList
	for _, finding := range findings {
		if finding.LinkID != linkID {
			result = append(result, finding)
		}
	}
	return append(result, replacement...)
}
