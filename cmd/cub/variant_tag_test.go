// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestVariantTagRequest(t *testing.T) {
	tagID := uuid.New()
	changeOrderID := uuid.New()
	spaceID := uuid.New().String()
	tests := []struct {
		name          string
		revision      string
		move          bool
		spaceID       string
		whereSpace    string
		changeOrderID *uuid.UUID
		stage         string
		wantWhere     string
		wantErr       string
	}{
		{
			name:      "a variant selects its space",
			revision:  "HeadRevisionNum",
			spaceID:   spaceID,
			wantWhere: "SpaceID = '" + spaceID + "'",
		},
		{
			name:       "--where-space narrows the variant's space rather than replacing it",
			revision:   "HeadRevisionNum",
			spaceID:    spaceID,
			whereSpace: "Labels.Environment = 'prod'",
			wantWhere:  "SpaceID = '" + spaceID + "' AND Labels.Environment = 'prod'",
		},
		{
			name:          "a change order alone selects spaces",
			revision:      "HeadRevisionNum",
			move:          true,
			changeOrderID: &changeOrderID,
			stage:         "staging",
		},
		{
			name:     "nothing selects no spaces",
			revision: "HeadRevisionNum",
			wantErr:  "tag needs the spaces",
		},
		{
			name:     "--stage needs a change order",
			revision: "HeadRevisionNum",
			spaceID:  spaceID,
			stage:    "staging",
			wantErr:  "--stage names a stage",
		},
		{
			name:     "a move names where to, so it cannot remove",
			revision: "Remove",
			move:     true,
			spaceID:  spaceID,
			wantErr:  "--move names the revision",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, err := variantTagRequest(tagID, tt.revision, tt.move, tt.spaceID, tt.whereSpace, tt.changeOrderID, tt.stage)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("variantTagRequest error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("variantTagRequest: %v", err)
			}
			if request.WhereSpace != tt.wantWhere {
				t.Errorf("WhereSpace = %q, want %q", request.WhereSpace, tt.wantWhere)
			}
			if request.TagID != tagID || request.Revision != tt.revision || request.Move != tt.move ||
				request.ChangeOrderID != tt.changeOrderID || request.TargetStage != tt.stage {
				t.Errorf("request = %+v, want the tag, revision, move, change order and stage passed through", request)
			}
		})
	}
}

func TestVariantTagUnitWhere(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	spaceID := uuid.New().String()
	tests := []struct {
		name                string
		userWhere           string
		spaceID             string
		whereSpaceIDs       []uuid.UUID
		changeOrderSpaceIDs []uuid.UUID
		want                string
	}{
		{
			name:    "a variant alone",
			spaceID: spaceID,
			want:    "SpaceID = '" + spaceID + "'",
		},
		{
			name:      "--where is narrowed by the variant, not replaced",
			userWhere: "Slug = 'web'",
			spaceID:   spaceID,
			want:      "Slug = 'web' AND SpaceID = '" + spaceID + "'",
		},
		{
			name:                "--where-space and a change order each narrow",
			whereSpaceIDs:       []uuid.UUID{a, b},
			changeOrderSpaceIDs: []uuid.UUID{b, c},
			want:                "SpaceID IN ('" + a.String() + "', '" + b.String() + "') AND SpaceID IN ('" + b.String() + "', '" + c.String() + "')",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := variantTagUnitWhere(tt.userWhere, tt.spaceID, tt.whereSpaceIDs, tt.changeOrderSpaceIDs); got != tt.want {
				t.Errorf("variantTagUnitWhere = %q, want %q", got, tt.want)
			}
		})
	}
}
