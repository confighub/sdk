// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCubBinaryPathIgnoresPATH is the regression test for runCub resolving its
// child binary by name. It used to fall back to a "cub" on $PATH whenever the
// running binary's basename was not exactly "cub", so a build at bin/cub-dev
// would do its own work but hand every delegated step to whatever release was
// installed. This test runs in precisely that shape: under `go test` the running
// binary is cub.test.
func TestCubBinaryPathIgnoresPATH(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skip("os.Executable unavailable on this platform")
	}
	if filepath.Base(self) == "cub" {
		t.Skip("running binary is itself named cub; the $PATH fallback is unobservable here")
	}

	// A decoy cub, first on $PATH. The old resolution would pick this over the
	// running binary; the current one must not look for it at all.
	dir := t.TempDir()
	decoy := filepath.Join(dir, "cub")
	if err := os.WriteFile(decoy, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got := cubBinaryPath()
	if got != self {
		t.Errorf("cubBinaryPath() = %q, want the running binary %q", got, self)
	}
	if got == "cub" || got == decoy {
		t.Errorf("cubBinaryPath() = %q: resolved via $PATH, so delegated work would run a different build", got)
	}
}

// TestExternalSourceRecordJSON pins the wire shape of the annotation a re-upload
// reads back. The digest is omitted for a non-oci:// input rather than emitted
// empty, but granularity is always present: it decides the shape of the entire
// Unit set, so a re-upload that could not recover it would silently restructure
// the Space instead of updating it.
func TestExternalSourceRecordJSON(t *testing.T) {
	tests := []struct {
		name   string
		record externalSourceRecord
		want   string
	}{
		{
			name: "oci input carries its resolved digest",
			record: externalSourceRecord{
				Ref:         "oci://ghcr.io/org/bundle",
				Digest:      "sha256:abc",
				Granularity: "per-file",
			},
			want: `{"ref":"oci://ghcr.io/org/bundle","digest":"sha256:abc","granularity":"per-file"}`,
		},
		{
			name: "directory input records granularity with no digest",
			record: externalSourceRecord{
				Ref:         "./rendered/",
				Granularity: "minimal",
			},
			want: `{"ref":"./rendered/","granularity":"minimal"}`,
		},
		{
			name: "stdin input records the namespace it ran with",
			record: externalSourceRecord{
				Ref:         "stdin",
				Granularity: "per-resource",
				Namespace:   "myapp",
			},
			want: `{"ref":"stdin","granularity":"per-resource","namespace":"myapp"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.record)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("json.Marshal() = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestRecordedSource covers reading the upload options back off a Space. The
// not-found results all mean "nothing to check against, let the upload proceed",
// so a Space seeded before the annotation was written for every input, or one
// whose annotation is unreadable, must never be mistaken for a mismatch.
func TestRecordedSource(t *testing.T) {
	tests := []struct {
		name       string
		annotation string
		wantFound  bool
		wantGran   string
		wantNS     string
	}{
		{
			name:       "absent annotation is not a record",
			annotation: "",
			wantFound:  false,
		},
		{
			name:       "empty array is not a record",
			annotation: `[]`,
			wantFound:  false,
		},
		{
			name:       "malformed JSON is not a record",
			annotation: `not json`,
			wantFound:  false,
		},
		{
			name:       "single record",
			annotation: `[{"ref":"stdin","granularity":"per-file","namespace":"myapp"}]`,
			wantFound:  true,
			wantGran:   "per-file",
			wantNS:     "myapp",
		},
		{
			name:       "multiple inputs share one set of options",
			annotation: `[{"ref":"a.yaml","granularity":"minimal"},{"ref":"b.yaml","granularity":"minimal"}]`,
			wantFound:  true,
			wantGran:   "minimal",
			wantNS:     "",
		},
		{
			name:       "oci record with a digest",
			annotation: `[{"ref":"oci://ghcr.io/org/b","digest":"sha256:abc","granularity":"per-resource"}]`,
			wantFound:  true,
			wantGran:   "per-resource",
			wantNS:     "",
		},
		{
			// An omitted namespace is a recorded value, not a missing one: it means
			// the upload ran without --namespace, so passing one later is a mismatch.
			name:       "omitted namespace is found as empty",
			annotation: `[{"ref":"stdin","granularity":"minimal"}]`,
			wantFound:  true,
			wantGran:   "minimal",
			wantNS:     "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := recordedSource(tt.annotation)
			if found != tt.wantFound {
				t.Fatalf("recordedSource(%q) found = %v, want %v", tt.annotation, found, tt.wantFound)
			}
			if !found {
				return
			}
			if got.Granularity != tt.wantGran {
				t.Errorf("granularity = %q, want %q", got.Granularity, tt.wantGran)
			}
			if got.Namespace != tt.wantNS {
				t.Errorf("namespace = %q, want %q", got.Namespace, tt.wantNS)
			}
		})
	}
}

// TestUploadNamespaceDesc checks that an absent --namespace is named rather than
// rendered as an empty string, since it appears in both halves of the mismatch
// message and "specifies " reads as a truncation.
func TestUploadNamespaceDesc(t *testing.T) {
	if got := uploadNamespaceDesc(""); got != "no --namespace" {
		t.Errorf("uploadNamespaceDesc(\"\") = %q, want %q", got, "no --namespace")
	}
	if got := uploadNamespaceDesc("myapp"); got != "--namespace myapp" {
		t.Errorf("uploadNamespaceDesc(%q) = %q, want %q", "myapp", got, "--namespace myapp")
	}
}

// TestUploadSourceRef covers the one input that is not recorded verbatim.
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
