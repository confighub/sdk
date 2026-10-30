// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

func TestReleaseTargetName(t *testing.T) {
	spaceID, otherSpaceID, targetID := uuid.New(), uuid.New(), uuid.New()
	space := &goclientnew.Space{SpaceID: spaceID, ReleaseTargetID: &targetID}

	for _, tc := range []struct {
		name   string
		space  *goclientnew.Space
		target *goclientnew.Target
		want   string
	}{
		{"no release target", &goclientnew.Space{SpaceID: spaceID}, nil, ""},
		{"in this space", space, &goclientnew.Target{Slug: "oci", SpaceID: spaceID, SpaceSlug: "app"}, "oci"},
		{"in another space", space, &goclientnew.Target{Slug: "oci", SpaceID: otherSpaceID, SpaceSlug: "cluster"}, "cluster/oci"},
		{"not expanded", space, nil, targetID.String()},
	} {
		if got := releaseTargetName(tc.space, tc.target); got != tc.want {
			t.Errorf("%s: releaseTargetName = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestComponentName(t *testing.T) {
	componentID := uuid.New()
	space := &goclientnew.Space{ComponentID: &componentID}
	for _, tc := range []struct {
		name      string
		space     *goclientnew.Space
		component *goclientnew.Component
		want      string
	}{
		{"no component", &goclientnew.Space{}, nil, ""},
		{"expanded", space, &goclientnew.Component{Slug: "web"}, "web"},
		{"not expanded", space, nil, componentID.String()},
	} {
		if got := componentName(tc.space, tc.component); got != tc.want {
			t.Errorf("%s: componentName = %q, want %q", tc.name, got, tc.want)
		}
	}
}
