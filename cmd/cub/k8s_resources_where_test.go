// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "testing"

// A null test carries no literal, so the clause ends at its operator.
func TestWhereResourceToResourceClauseNullTest(t *testing.T) {
	got, err := whereResourceToResourceClause("kind = 'ClusterRole' AND metadata.annotations.team IS NULL")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Data.kind = 'ClusterRole' AND Data.metadata.annotations.team IS NULL"; got != want {
		t.Errorf("clause = %q, want %q", got, want)
	}
}
