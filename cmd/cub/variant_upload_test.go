// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

// TestUploadSourceRef covers the one input that is not recorded verbatim. The
// ref is what the upload records as its source, so it has to name what the user
// named rather than the temp directory an oci:// bundle was extracted into.
func TestUploadSourceRef(t *testing.T) {
	tests := map[string]string{
		"-":                        "stdin",
		"oci://ghcr.io/org/bundle": "oci://ghcr.io/org/bundle",
		"./rendered/":              "./rendered/",
		"extra.yaml":               "extra.yaml",
	}
	for input, want := range tests {
		if got := uploadSourceRef(input); got != want {
			t.Errorf("uploadSourceRef(%q) = %q, want %q", input, got, want)
		}
	}
}

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

func TestUploadServerPull(t *testing.T) {
	tests := []struct {
		name       string
		inputs     []string
		clientPull bool
		want       bool
		wantErr    string
	}{
		{name: "files are sent", inputs: []string{"a.yaml", "dir"}},
		{name: "a reference is pulled by the server", inputs: []string{"oci://ghcr.io/confighub/configs/cubbychat"}, want: true},
		{name: "--client-pull pulls it here", inputs: []string{"oci://ghcr.io/confighub/configs/cubbychat"}, clientPull: true},
		{name: "--client-pull combines it with files", inputs: []string{"oci://ghcr.io/x/y", "a.yaml"}, clientPull: true},
		{name: "a server pull cannot be combined", inputs: []string{"oci://ghcr.io/x/y", "a.yaml"}, wantErr: "must be the only input"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := uploadServerPull(tt.inputs, tt.clientPull)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("uploadServerPull = %v, want %v", got, tt.want)
			}
		})
	}
}

// A directory upload sends its configuration files and its README, and nothing else.
func TestIsUploadFile(t *testing.T) {
	for path, want := range map[string]bool{
		"web.yaml":          true,
		"web.yml":           true,
		"config/app.env":    true,
		"README.md":         true,
		"docs/README.md":    true,
		"NOTES.md":          false,
		"notes.txt":         false,
		"chart/.helmignore": false,
		"config/app.toml":   true,
		"config/readme.md":  false,
	} {
		if got := isUploadFile(path); got != want {
			t.Errorf("isUploadFile(%q) = %v, want %v", path, got, want)
		}
	}
}
