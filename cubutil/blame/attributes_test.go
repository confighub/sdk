// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// TestBlameAttributesForPath: a needed or provided path is the field when its resource is the
// field's, by name or, renamed, by name without scope, and its path is the field's however it
// anchors an array element. A sibling path, or the same path of another resource, is not.
func TestBlameAttributesForPath(t *testing.T) {
	identifier := mustBlameProvider(t, "Kubernetes/YAML")
	unit := &goclientnew.Unit{
		NeededPaths: []goclientnew.AttributeValue{
			{AttributeName: "image", ResourceType: "apps/v1/Deployment", ResourceName: "confighubplaceholder/web",
				ResourceNameWithoutScope: "web", Path: "spec.template.spec.containers.?name=web;@0.image"},
			{AttributeName: "replicas", ResourceType: "apps/v1/Deployment", ResourceName: "ns/web",
				ResourceNameWithoutScope: "web", Path: "spec.replicas"},
		},
		ProvidedPaths: []goclientnew.AttributeInfo{
			{AttributeName: "image", ResourceType: "apps/v1/Deployment", ResourceName: "ns/other",
				ResourceNameWithoutScope: "other", Path: "spec.template.spec.containers.?name=web.image"},
			{AttributeName: "service-name", ResourceType: "apps/v1/Deployment", ResourceName: "ns/web",
				ResourceNameWithoutScope: "web", Path: "spec.template.spec.containers.?name=web.image"},
		},
	}
	needed, provided := blameAttributesForPath(identifier, unit, "apps/v1/Deployment", "ns/web", "web",
		"spec.template.spec.containers.?name=web.image")
	if len(needed) != 1 || needed[0].AttributeName != "image" || needed[0].BoundLink != "" {
		t.Errorf("needed = %v, want the image attribute alone", needed)
	}
	if len(provided) != 1 || provided[0].AttributeName != "service-name" {
		t.Errorf("provided = %v, want web's attribute and not other's", provided)
	}

	f := &Field{Needed: needed, Provided: provided}
	if got := blameFlags(f, 1, true); got != "np" {
		t.Errorf("flags = %q, want np", got)
	}
	if got := blameFlags(&Field{}, 1, true); got != "" {
		t.Errorf("a field with no needed or provided path has neither flag, got %q", got)
	}
}

// TestNameBoundLinks: a Link already read is named without a request, and every needed path
// it bound gets its name. A path no Link bound is left unnamed.
func TestNameBoundLinks(t *testing.T) {
	linkID := uuid.New()
	r := &Resolver{links: map[uuid.UUID]string{linkID: "web-to-registry"}}
	bound := NeededPath{AttributeValue: goclientnew.AttributeValue{Details: &goclientnew.AttributeDetails{BoundLinkID: linkID}}}
	fields := []*Field{{Needed: []NeededPath{bound, {}}}, {Needed: []NeededPath{bound}}}
	r.nameBoundLinks(t.Context(), fields)
	if fields[0].Needed[0].BoundLink != "web-to-registry" || fields[1].Needed[0].BoundLink != "web-to-registry" {
		t.Errorf("bound paths named %q and %q, want web-to-registry", fields[0].Needed[0].BoundLink, fields[1].Needed[0].BoundLink)
	}
	if fields[0].Needed[1].BoundLink != "" {
		t.Errorf("an unbound path is named %q", fields[0].Needed[1].BoundLink)
	}
}

func TestFormatNeededAndProvided(t *testing.T) {
	bound := NeededPath{
		AttributeValue: goclientnew.AttributeValue{
			AttributeName: "container-image",
			Value:         "confighubplaceholder",
			Details: &goclientnew.AttributeDetails{
				BoundLinkID:     uuid.New(),
				Description:     "the image to run",
				NeededRequired:  map[string]string{"registry": "ghcr", "arch": "arm64"},
				NeededPreferred: map[string]string{"tier": "stable"},
			},
		},
		BoundLink: "web-to-registry",
	}
	want := `container-image, bound by link web-to-registry: the image to run; requires arch=arm64,registry=ghcr; ` +
		`prefers tier=stable; placeholder "confighubplaceholder"`
	if got := FormatNeeded(bound); got != want {
		t.Errorf("FormatNeeded =\n  %s\nwant\n  %s", got, want)
	}
	if got := FormatNeeded(NeededPath{AttributeValue: goclientnew.AttributeValue{AttributeName: "port"}}); got != "port, not bound by a link" {
		t.Errorf("unbound FormatNeeded = %q", got)
	}

	provided := goclientnew.AttributeInfo{
		AttributeName: "container-image",
		Details:       &goclientnew.AttributeDetails{ProvidedProperties: map[string]string{"registry": "ghcr"}},
	}
	if got := FormatProvided(provided); got != "container-image: offers registry=ghcr" {
		t.Errorf("FormatProvided = %q", got)
	}
}
