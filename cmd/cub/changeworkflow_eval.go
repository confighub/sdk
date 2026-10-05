// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

// Reading a ChangeWorkflow on the client: the prerequisites a Stage may declare. Promotion and how far a ChangeOrder has got through its
// workflow are the server's.

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// The prerequisites a Stage may declare. What each one checks is decided by the
// server's promotion; authoring refuses anything else before a definition is stored,
// so the two cannot come to name different sets.
const (
	prerequisiteValidated = "Validated"
	prerequisiteReleased  = "Released"
	prerequisiteHealthy   = "Healthy"
)

// knownPrerequisites is what a definition may name, in the order they are offered
// to someone writing one.
var knownPrerequisites = []string{prerequisiteValidated, prerequisiteReleased, prerequisiteHealthy}

// changeOrderByRef resolves a change order identifier -- a bare slug, space/slug
// or UUID -- against whatever space is selected when it runs.
func changeOrderByRef(identifier string) (*goclientnew.ChangeOrder, error) {
	changeOrder, err := resolveChangeOrder(identifier, defaultSpaceID(), "*")
	if err != nil {
		return nil, fmt.Errorf("failed to get change order: %w", err)
	}
	return changeOrder.ChangeOrder, nil
}

// bulkUnitResponses normalizes a 200/207 bulk unit response pair.
func bulkUnitResponses(json200, json207 *[]goclientnew.UnitCreateOrUpdateResponse) (*[]goclientnew.UnitCreateOrUpdateResponse, int) {
	if json200 != nil {
		return json200, 200
	}
	if json207 != nil {
		return json207, 207
	}
	return nil, 0
}
