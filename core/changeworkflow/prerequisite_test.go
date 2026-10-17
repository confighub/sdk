// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package changeworkflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluateCELChangeSetPrerequisite(t *testing.T) {
	space := map[string]any{"Slug": "dev"}
	changeSet := map[string]any{"Description": "blocked"}

	satisfied, err := EvaluateCELChangeSetPrerequisite(`ChangeSet.Description != "blocked"`, space, changeSet)
	require.NoError(t, err)
	assert.False(t, satisfied)

	satisfied, err = EvaluateCELChangeSetPrerequisite(`Space.Slug == "dev" && ChangeOrder == null && Release == null`, space, changeSet)
	require.NoError(t, err)
	assert.True(t, satisfied)

	// An expression written for a promotion reads a ChangeOrder, which closing a ChangeSet has none
	// of, so it cannot answer rather than answering for the ChangeSet.
	_, err = EvaluateCELChangeSetPrerequisite(`ChangeOrder.Slug != ""`, space, changeSet)
	require.Error(t, err)
}

func TestEvaluateCELPrerequisiteEntitiesBindsNoChangeSet(t *testing.T) {
	satisfied, err := EvaluateCELPrerequisiteEntities(`ChangeSet == null && ChangeOrder.Slug == "five"`,
		map[string]any{"Slug": "dev"}, map[string]any{"Slug": "five"}, nil)
	require.NoError(t, err)
	assert.True(t, satisfied)

	require.NoError(t, ValidateCELPrerequisite(`ChangeSet.Description != "blocked"`),
		"one expression may be named by a ChangeOrder's gates and a ChangeSet's")
}
