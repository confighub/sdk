// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// NeededPath is one of the unit's needed paths that is a field, with the Link that bound it
// named, since the record names it only by ID.
type NeededPath struct {
	goclientnew.AttributeValue
	// BoundLink is the slug of the Link that bound the path, or its ID when it cannot be read.
	BoundLink string `json:",omitempty"`
}

// blameAttributesForPath returns the unit's needed and provided paths that are one field.
//
// A needed path stays on the unit after a resolve fills it in, so a field that is needed is
// one whose value is to come, or came, from an upstream unit, whether or not a Link has bound
// it yet. Both lists name an array element by merge key, as blame does.
func blameAttributesForPath(identifier api.ResourceIdentifier, unit *goclientnew.Unit,
	resourceType, resourceName, resourceNameCore, path string) ([]NeededPath, []goclientnew.AttributeInfo) {
	matches := func(attributeType, attributeName, attributeNameCore, attributePath string) bool {
		if AuthoredPath(attributePath) != path {
			return false
		}
		_, found := matchBlameResourceInfo(identifier, []*goclientnew.ResourceInfo{{
			ResourceType:             attributeType,
			ResourceName:             attributeName,
			ResourceNameWithoutScope: attributeNameCore,
		}}, resourceType, resourceName, resourceNameCore)
		return found
	}
	var needed []NeededPath
	for _, attribute := range unit.NeededPaths {
		if matches(attribute.ResourceType, attribute.ResourceName, attribute.ResourceNameWithoutScope, attribute.Path) {
			needed = append(needed, NeededPath{AttributeValue: attribute})
		}
	}
	var provided []goclientnew.AttributeInfo
	for _, attribute := range unit.ProvidedPaths {
		if matches(attribute.ResourceType, attribute.ResourceName, attribute.ResourceNameWithoutScope, attribute.Path) {
			provided = append(provided, attribute)
		}
	}
	return needed, provided
}

// FormatNeeded renders one of a field's needed paths on one line: the attribute, the Link that
// bound it, and what it asks of the value that fills it.
func FormatNeeded(needed NeededPath) string {
	text := attributeName(needed.AttributeName)
	details := needed.Details
	if needed.BoundLink != "" {
		text += ", bound by link " + needed.BoundLink
	} else {
		text += ", not bound by a link"
	}
	var parts []string
	if details != nil {
		if details.Description != "" {
			parts = append(parts, details.Description)
		}
		if len(details.NeededRequired) > 0 {
			parts = append(parts, "requires "+formatAttributeProperties(details.NeededRequired))
		}
		if len(details.NeededPreferred) > 0 {
			parts = append(parts, "prefers "+formatAttributeProperties(details.NeededPreferred))
		}
	}
	// The value is the placeholder the unit had at the path when it was first needed, which
	// is what is left to say what it asked for once a resolve has replaced it.
	if needed.Value != nil {
		parts = append(parts, fmt.Sprintf("placeholder %q", fmt.Sprint(needed.Value)))
	}
	return joinAttributeParts(text, parts)
}

// FormatProvided renders one of a field's provided paths on one line: the attribute, and what
// it offers a needed path to match on.
func FormatProvided(attribute goclientnew.AttributeInfo) string {
	var parts []string
	if details := attribute.Details; details != nil {
		if details.Description != "" {
			parts = append(parts, details.Description)
		}
		if len(details.ProvidedProperties) > 0 {
			parts = append(parts, "offers "+formatAttributeProperties(details.ProvidedProperties))
		}
	}
	return joinAttributeParts(attributeName(attribute.AttributeName), parts)
}

// nameBoundLinks names the Links that bound the fields' needed paths, reading the ones not
// already read in one request. A needed path's Link belongs to the unit that needs it, so a
// unit's fields name a handful of Links at most, and most units name none.
func (r *Resolver) nameBoundLinks(ctx context.Context, fields []*Field) {
	var unread []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, f := range fields {
		for _, needed := range f.Needed {
			if id := boundLinkID(needed); id != uuid.Nil && !seen[id] {
				seen[id] = true
				if _, ok := r.links[id]; !ok {
					unread = append(unread, id)
				}
			}
		}
	}
	if len(unread) > 0 {
		// A Link that is not listed has been deleted or is not readable, and is named by ID.
		for _, id := range unread {
			r.links[id] = id.String()
		}
		if links, err := cubapi.ListLinks(ctx, r.client, cubapi.Where{}.In("LinkID", unread),
			cubapi.ListOpts{Select: "LinkID,Slug"}); err == nil {
			for _, link := range links {
				if link != nil && link.Link != nil {
					r.links[link.Link.LinkID] = link.Link.Slug
				}
			}
		}
	}
	for _, f := range fields {
		for i := range f.Needed {
			if id := boundLinkID(f.Needed[i]); id != uuid.Nil {
				f.Needed[i].BoundLink = r.links[id]
			}
		}
	}
}

func boundLinkID(needed NeededPath) uuid.UUID {
	if needed.Details == nil {
		return uuid.Nil
	}
	return needed.Details.BoundLinkID
}

func attributeName(name string) string {
	if name == "" {
		return "(unnamed attribute)"
	}
	return name
}

func joinAttributeParts(text string, parts []string) string {
	if len(parts) == 0 {
		return text
	}
	return text + ": " + strings.Join(parts, "; ")
}

// formatAttributeProperties renders matching properties as k=v pairs, in key order.
func formatAttributeProperties(properties map[string]string) string {
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+properties[key])
	}
	return strings.Join(pairs, ",")
}
