// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

func TestUploadSourceDescription(t *testing.T) {
	tests := []struct {
		name   string
		inputs []string
		want   string
	}{
		{
			name:   "oci ref is kept verbatim",
			inputs: []string{"oci://ghcr.io/confighub/configs/cubbychat"},
			want:   "oci://ghcr.io/confighub/configs/cubbychat",
		},
		{
			name:   "stdin is named rather than dashed",
			inputs: []string{"-"},
			want:   "stdin",
		},
		{
			name:   "file and directory paths are kept as given",
			inputs: []string{"./rendered/", "extra.yaml"},
			want:   "./rendered/, extra.yaml",
		},
		{
			name:   "mixed inputs are joined",
			inputs: []string{"oci://ghcr.io/org/bundle", "-"},
			want:   "oci://ghcr.io/org/bundle, stdin",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := uploadSourceDescription(tt.inputs); got != tt.want {
				t.Errorf("uploadSourceDescription(%q) = %q, want %q", tt.inputs, got, tt.want)
			}
		})
	}
}

func TestUploadSourceDescriptionTruncates(t *testing.T) {
	inputs := make([]string, 40)
	for i := range inputs {
		inputs[i] = "oci://ghcr.io/confighub/configs/a-rather-long-bundle-name"
	}
	got := uploadSourceDescription(inputs)
	if len(got) > maxUploadSourceDescription {
		t.Errorf("length = %d, want at most %d", len(got), maxUploadSourceDescription)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncated description %q should end in an ellipsis", got)
	}
}
