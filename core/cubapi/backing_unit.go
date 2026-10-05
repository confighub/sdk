// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// BackingUnitEntityTypeLabel is the label the server puts on a backing Unit it creates, naming the
// type of entity the Unit holds the configuration of.
const BackingUnitEntityTypeLabel = "confighub.com/EntityType"

// backedEntityFinder finds the entity of one type that a Unit backs, and names it.
type backedEntityFinder struct {
	entityType string
	find       func(ctx context.Context, c *Client, where Where) (name string, found bool, err error)
}

// qualifiedName names an entity as <space>/<slug>, or by its slug alone when its Space is not known.
func qualifiedName(space *goclientnew.Space, slug string) string {
	if space == nil {
		return slug
	}
	return space.Slug + "/" + slug
}

var bySpace = ListOpts{Include: "SpaceID"}

// backedEntityFinders are the types of entity that can have a backing Unit.
var backedEntityFinders = []backedEntityFinder{
	{"Attribute", func(ctx context.Context, c *Client, where Where) (string, bool, error) {
		found, err := ListAttributes(ctx, c, where, bySpace)
		if err != nil || len(found) == 0 || found[0].Attribute == nil {
			return "", false, err
		}
		return qualifiedName(found[0].Space, found[0].Attribute.Slug), true, nil
	}},
	{"ChangeWorkflow", func(ctx context.Context, c *Client, where Where) (string, bool, error) {
		found, err := ListChangeWorkflows(ctx, c, where, bySpace)
		if err != nil || len(found) == 0 || found[0].ChangeWorkflow == nil {
			return "", false, err
		}
		return qualifiedName(found[0].Space, found[0].ChangeWorkflow.Slug), true, nil
	}},
	{"Component", func(ctx context.Context, c *Client, where Where) (string, bool, error) {
		found, err := ListComponents(ctx, c, where, ListOpts{})
		if err != nil || len(found) == 0 || found[0].Component == nil {
			return "", false, err
		}
		return found[0].Component.Slug, true, nil
	}},
	{"Filter", func(ctx context.Context, c *Client, where Where) (string, bool, error) {
		found, err := ListFilters(ctx, c, where, bySpace)
		if err != nil || len(found) == 0 || found[0].Filter == nil {
			return "", false, err
		}
		return qualifiedName(found[0].Space, found[0].Filter.Slug), true, nil
	}},
	{"Invocation", func(ctx context.Context, c *Client, where Where) (string, bool, error) {
		found, err := ListInvocations(ctx, c, where, bySpace)
		if err != nil || len(found) == 0 || found[0].Invocation == nil {
			return "", false, err
		}
		return qualifiedName(found[0].Space, found[0].Invocation.Slug), true, nil
	}},
	{"Link", func(ctx context.Context, c *Client, where Where) (string, bool, error) {
		found, err := ListLinks(ctx, c, where, bySpace)
		if err != nil || len(found) == 0 || found[0].Link == nil {
			return "", false, err
		}
		return qualifiedName(found[0].Space, found[0].Link.Slug), true, nil
	}},
	{"Space", func(ctx context.Context, c *Client, where Where) (string, bool, error) {
		found, err := ListSpaces(ctx, c, where, ListOpts{})
		if err != nil || len(found) == 0 || found[0].Space == nil {
			return "", false, err
		}
		return found[0].Space.Slug, true, nil
	}},
	{"Target", func(ctx context.Context, c *Client, where Where) (string, bool, error) {
		found, err := ListTargets(ctx, c, where, bySpace)
		if err != nil || len(found) == 0 || found[0].Target == nil {
			return "", false, err
		}
		return qualifiedName(found[0].Space, found[0].Target.Slug), true, nil
	}},
	{"Trigger", func(ctx context.Context, c *Client, where Where) (string, bool, error) {
		found, err := ListTriggers(ctx, c, where, bySpace)
		if err != nil || len(found) == 0 || found[0].Trigger == nil {
			return "", false, err
		}
		return qualifiedName(found[0].Space, found[0].Trigger.Slug), true, nil
	}},
	{"View", func(ctx context.Context, c *Client, where Where) (string, bool, error) {
		found, err := ListViews(ctx, c, where, bySpace)
		if err != nil || len(found) == 0 || found[0].View == nil {
			return "", false, err
		}
		return qualifiedName(found[0].Space, found[0].View.Slug), true, nil
	}},
}

// BackedEntity finds the entity a Unit holds the configuration of, if it backs one: its type,
// and its name, as <space>/<slug> or, for an entity in no Space, its slug. entityType is the
// type the Unit is labeled with, which is the only one asked when it is given; each type that
// can have a backing Unit is asked otherwise. found is false for a Unit that backs nothing.
func BackedEntity(ctx context.Context, c *Client, unitID goclientnew.UUID, entityType string) (foundType, name string, found bool, err error) {
	where := NewWhere("").Eq("BackingUnitID", unitID.String())
	for _, finder := range backedEntityFinders {
		if entityType != "" && finder.entityType != entityType {
			continue
		}
		name, found, err := finder.find(ctx, c, where)
		if err != nil {
			return "", "", false, err
		}
		if found {
			return finder.entityType, name, true, nil
		}
	}
	return "", "", false, nil
}
