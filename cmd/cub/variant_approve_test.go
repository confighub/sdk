// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "testing"

func TestVariantApproveWhere(t *testing.T) {
	tests := []struct {
		name      string
		userWhere string
		all       bool
		want      string
	}{
		{
			name: "bare approve selects only what a release would publish",
			want: variantApproveDefaultWhere,
		},
		{
			name:      "--where is narrowed by the default, not replaced by it",
			userWhere: "Slug LIKE 'deployment-%'",
			want:      "Slug LIKE 'deployment-%' AND " + variantApproveDefaultWhere,
		},
		{
			name: "--all drops the target constraint",
			all:  true,
			want: "",
		},
		{
			name:      "--all keeps a --where of its own",
			userWhere: "Slug LIKE 'deployment-%'",
			all:       true,
			want:      "Slug LIKE 'deployment-%'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := variantApproveWhere(tt.userWhere, tt.all); got != tt.want {
				t.Errorf("variantApproveWhere(%q, %v) = %q, want %q",
					tt.userWhere, tt.all, got, tt.want)
			}
		})
	}
}
