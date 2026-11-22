// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

func TestParseRevertRange(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    revertRange
		wantErr string
	}{
		{
			name: "one revision is the range from the one before it",
			spec: "7",
			want: revertRange{From: "Before:7", To: "7", Change: "7"},
		},
		{
			name: "a changeset is the range from before it",
			spec: "ChangeSet:web/tls",
			want: revertRange{From: "Before:ChangeSet:web/tls", To: "ChangeSet:web/tls", Change: "ChangeSet:web/tls"},
		},
		{
			name: "a range is taken as written, excluding its start, as git does",
			spec: "5..7",
			want: revertRange{From: "5", To: "7"},
		},
		{
			name: "a range of tags",
			spec: "Tag:web/pre..Tag:web/post",
			want: revertRange{From: "Tag:web/pre", To: "Tag:web/post"},
		},
		{
			name: "a range may start before a change",
			spec: "Before:ChangeSet:web/tls..HeadRevisionNum",
			want: revertRange{From: "Before:ChangeSet:web/tls", To: "HeadRevisionNum"},
		},
		{
			name: "a tag slug with a dot is one revision, not a range",
			spec: "v1.2",
			want: revertRange{From: "Before:v1.2", To: "v1.2", Change: "v1.2"},
		},
		{
			name:    "the state before a change is not a change",
			spec:    "Before:ChangeSet:web/tls",
			wantErr: "names the state before a change",
		},
		{
			name:    "a range needs both ends",
			spec:    "5..",
			wantErr: "is missing one",
		},
		{
			name:    "a range of numbers that runs backwards is empty",
			spec:    "7..5",
			wantErr: "is empty",
		},
		{
			name:    "a range from a revision to itself is empty",
			spec:    "5..5",
			wantErr: "is empty",
		},
		{
			name:    "a change order is demoted",
			spec:    "ChangeOrder:web/bump",
			wantErr: "cub variant demote",
		},
		{
			name:    "a range ending at a change order is demoted too",
			spec:    "Before:ChangeOrder:web/bump..ChangeOrder:web/bump",
			wantErr: "cub variant demote",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRevertRange(tt.spec)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseRevertRange(%q) error = %v, want one containing %q", tt.spec, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRevertRange(%q): %v", tt.spec, err)
			}
			if got != tt.want {
				t.Errorf("parseRevertRange(%q) = %+v, want %+v", tt.spec, got, tt.want)
			}
		})
	}
}

func TestRevisionSpecEntity(t *testing.T) {
	tests := []struct {
		spec, wantType, wantIdentifier string
	}{
		{"7", "", "7"},
		{"-1", "", "-1"},
		{"HeadRevisionNum", "", "HeadRevisionNum"},
		{"Before:LastReleasedRevisionNum", "", "LastReleasedRevisionNum"},
		{"release-2", "Tag", "release-2"},
		{"Tag:web/release-2", "Tag", "web/release-2"},
		{"Before:ChangeSet:web/tls", "ChangeSet", "web/tls"},
		{"Release:3", "Release", "3"},
		{"6f1c2b9e-3d4a-4f5b-8c7d-9e0a1b2c3d4e", "Revision", "6f1c2b9e-3d4a-4f5b-8c7d-9e0a1b2c3d4e"},
	}
	for _, tt := range tests {
		gotType, gotIdentifier := revisionSpecEntity(tt.spec)
		if gotType != tt.wantType || gotIdentifier != tt.wantIdentifier {
			t.Errorf("revisionSpecEntity(%q) = %q, %q, want %q, %q", tt.spec, gotType, gotIdentifier, tt.wantType, tt.wantIdentifier)
		}
	}
}

func TestChangeSetRevertDescription(t *testing.T) {
	if got := changeSetRevertDescription("tls", ""); got != "Revert changeset tls" {
		t.Errorf("without a description = %q", got)
	}
	if got := changeSetRevertDescription("tls", "Turn on TLS"); got != "Revert changeset tls: Turn on TLS" {
		t.Errorf("with a description = %q", got)
	}
}

func TestParseSelectedRevisionParameterBeforeANumber(t *testing.T) {
	tests := []struct {
		spec    string
		head    int64
		want    string
		wantErr string
	}{
		{spec: "5", want: "5"},
		{spec: "Before:5", want: "4"},
		{spec: "Before:-1", head: 10, want: "8"},
		{spec: "Before:1", wantErr: "no revision before revision 1"},
	}
	for _, tt := range tests {
		got, _, err := parseSelectedRevisionParameterIn("", tt.spec, serverResolvedRevision, tt.head)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("parseSelectedRevisionParameterIn(%q) error = %v, want one containing %q", tt.spec, err, tt.wantErr)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("parseSelectedRevisionParameterIn(%q) = %q, %v, want %q", tt.spec, got, err, tt.want)
		}
	}
}

func TestRevertMergeRange(t *testing.T) {
	revisionID := "6f1c2b9e-3d4a-4f5b-8c7d-9e0a1b2c3d4e"
	tests := []struct {
		name              string
		spec              string
		head              int64
		wantBase, wantEnd string
		wantErr           string
	}{
		{name: "one revision is merged from it back to the one before", spec: "7", wantBase: "7", wantEnd: "6"},
		{name: "a range is reversed", spec: "5..7", wantBase: "7", wantEnd: "5"},
		{name: "deltas count back from head", spec: "-3..-1", head: 10, wantBase: "9", wantEnd: "7"},
		{name: "a backwards range of deltas is empty once they are counted", spec: "-1..-3", head: 10, wantErr: "is empty"},
		{name: "named revisions are left to the server", spec: "LastReleasedRevisionNum..HeadRevisionNum", wantBase: "HeadRevisionNum", wantEnd: "LastReleasedRevisionNum"},
		{name: "a revision named by ID", spec: "Revision:" + revisionID, wantBase: "Revision:" + revisionID, wantEnd: "Before:Revision:" + revisionID},
		{name: "a bare revision ID", spec: revisionID, wantBase: "Revision:" + revisionID, wantEnd: "Before:Revision:" + revisionID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := parseRevertRange(tt.spec)
			if err == nil {
				var base, end string
				base, end, err = revertMergeRange(r, "", tt.head)
				if err == nil && (base != tt.wantBase || end != tt.wantEnd) {
					t.Errorf("revertMergeRange(%q) = %q, %q, want %q, %q", tt.spec, base, end, tt.wantBase, tt.wantEnd)
				}
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("revertMergeRange(%q) error = %v, want one containing %q", tt.spec, err, tt.wantErr)
				}
			} else if err != nil {
				t.Errorf("revertMergeRange(%q): %v", tt.spec, err)
			}
		})
	}
}
