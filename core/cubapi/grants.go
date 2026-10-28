// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Grant is an entity whose Permissions name a User, directly or through one of
// its Groups.
type Grant struct {
	// EntityType is the entity's type, as the API names it: Space, Unit, Link, ...
	EntityType string
	ID         goclientnew.UUID
	// SpaceID is the Space the entity is in, and zero for one that is in none.
	SpaceID goclientnew.UUID
	// Name is the entity's Slug, or for a Release, which has none, "release <number>".
	Name        string
	DisplayName string
	CreatedAt   time.Time
	// Actions are the actions granted, in order of name.
	Actions []GrantedAction
}

// GrantedAction is one action a Grant gives, and the subjects it is given through.
type GrantedAction struct {
	Action string
	// Direct is set when the action names the User itself.
	Direct bool
	// GroupIDs are the User's Groups the action names.
	GroupIDs []goclientnew.UUID
}

// GrantReadError is an entity type whose entities could not be searched.
type GrantReadError struct {
	EntityType string
	Err        error
}

// Grants is the result of [ListGrantsTo].
type Grants struct {
	// Grants are ordered by entity type, then by Space and Name.
	Grants []Grant
	// Unreadable are the entity types that could not be searched, so that a
	// Grant missing from Grants is not taken for one that does not exist.
	Unreadable []GrantReadError
}

// grantedEntity is what a Grant reads of an entity of any type.
type grantedEntity struct {
	id, spaceID       goclientnew.UUID
	name, displayName string
	permissions       *goclientnew.Permissions
	createdAt         time.Time
}

type grantSource struct {
	entityType string
	list       func(ctx context.Context, c *Client, where Where) ([]grantedEntity, error)
}

// grantSourceOf adapts a list helper: it selects only the fields a Grant reads,
// and of maps each envelope to them, reporting false for one with no entity.
func grantSourceOf[E any](entityType, fields string, list func(context.Context, *Client, Where, ListOpts) ([]*E, error), of func(*E) (grantedEntity, bool)) grantSource {
	return grantSource{
		entityType: entityType,
		list: func(ctx context.Context, c *Client, where Where) ([]grantedEntity, error) {
			items, err := list(ctx, c, where, ListOpts{Select: fields + ",CreatedAt,Permissions"})
			if err != nil {
				return nil, err
			}
			entities := make([]grantedEntity, 0, len(items))
			for _, item := range items {
				if entity, ok := of(item); ok {
					entities = append(entities, entity)
				}
			}
			return entities, nil
		},
	}
}

// grantSources are the entity types that have Permissions, in the order a
// listing shows them. Attestations are left out: whoever records one is granted
// Manage on it, so an account that records them would list every one.
var grantSources = []grantSource{
	grantSourceOf("Space", "SpaceID,Slug,DisplayName",
		func(ctx context.Context, c *Client, w Where, o ListOpts) ([]*goclientnew.ExtendedSpace, error) {
			return ListSpaces(ctx, c, w, o)
		},
		func(e *goclientnew.ExtendedSpace) (grantedEntity, bool) {
			if e.Space == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.Space.SpaceID, goclientnew.UUID{}, e.Space.Slug, e.Space.DisplayName, e.Space.Permissions, e.Space.CreatedAt}, true
		}),
	grantSourceOf("Component", "ComponentID,Slug,DisplayName", ListComponents,
		func(e *goclientnew.ExtendedComponent) (grantedEntity, bool) {
			if e.Component == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.Component.ComponentID, goclientnew.UUID{}, e.Component.Slug, e.Component.DisplayName, e.Component.Permissions, e.Component.CreatedAt}, true
		}),
	grantSourceOf("ServiceAccount", "ServiceAccountID,Slug,DisplayName", ListServiceAccounts,
		func(e *goclientnew.ExtendedServiceAccount) (grantedEntity, bool) {
			if e.ServiceAccount == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.ServiceAccount.ServiceAccountID, goclientnew.UUID{}, e.ServiceAccount.Slug, e.ServiceAccount.DisplayName, e.ServiceAccount.Permissions, e.ServiceAccount.CreatedAt}, true
		}),
	grantSourceOf("Unit", "UnitID,SpaceID,Slug,DisplayName",
		func(ctx context.Context, c *Client, w Where, o ListOpts) ([]*goclientnew.ExtendedUnit, error) {
			return ListUnits(ctx, c, w, o)
		},
		func(e *goclientnew.ExtendedUnit) (grantedEntity, bool) {
			if e.Unit == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.Unit.UnitID, e.Unit.SpaceID, e.Unit.Slug, e.Unit.DisplayName, e.Unit.Permissions, e.Unit.CreatedAt}, true
		}),
	grantSourceOf("Link", "LinkID,SpaceID,Slug,DisplayName", ListLinks,
		func(e *goclientnew.ExtendedLink) (grantedEntity, bool) {
			if e.Link == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.Link.LinkID, e.Link.SpaceID, e.Link.Slug, e.Link.DisplayName, e.Link.Permissions, e.Link.CreatedAt}, true
		}),
	grantSourceOf("Target", "TargetID,SpaceID,Slug,DisplayName", ListTargets,
		func(e *goclientnew.ExtendedTarget) (grantedEntity, bool) {
			if e.Target == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.Target.TargetID, e.Target.SpaceID, e.Target.Slug, e.Target.DisplayName, e.Target.Permissions, e.Target.CreatedAt}, true
		}),
	grantSourceOf("Channel", "ChannelID,SpaceID,Slug,DisplayName",
		func(ctx context.Context, c *Client, w Where, o ListOpts) ([]*goclientnew.ExtendedChannel, error) {
			return ListChannels(ctx, c, w, o)
		},
		func(e *goclientnew.ExtendedChannel) (grantedEntity, bool) {
			if e.Channel == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.Channel.ChannelID, e.Channel.SpaceID, e.Channel.Slug, e.Channel.DisplayName, e.Channel.Permissions, e.Channel.CreatedAt}, true
		}),
	grantSourceOf("ChangeWorkflow", "ChangeWorkflowID,SpaceID,Slug,DisplayName", ListChangeWorkflows,
		func(e *goclientnew.ExtendedChangeWorkflow) (grantedEntity, bool) {
			if e.ChangeWorkflow == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.ChangeWorkflow.ChangeWorkflowID, e.ChangeWorkflow.SpaceID, e.ChangeWorkflow.Slug, e.ChangeWorkflow.DisplayName, e.ChangeWorkflow.Permissions, e.ChangeWorkflow.CreatedAt}, true
		}),
	grantSourceOf("ChangeSet", "ChangeSetID,SpaceID,Slug,DisplayName", ListChangeSets,
		func(e *goclientnew.ExtendedChangeSet) (grantedEntity, bool) {
			if e.ChangeSet == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.ChangeSet.ChangeSetID, e.ChangeSet.SpaceID, e.ChangeSet.Slug, e.ChangeSet.DisplayName, e.ChangeSet.Permissions, e.ChangeSet.CreatedAt}, true
		}),
	grantSourceOf("ChangeOrder", "ChangeOrderID,SpaceID,Slug,DisplayName", ListChangeOrders,
		func(e *goclientnew.ExtendedChangeOrder) (grantedEntity, bool) {
			if e.ChangeOrder == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.ChangeOrder.ChangeOrderID, e.ChangeOrder.SpaceID, e.ChangeOrder.Slug, e.ChangeOrder.DisplayName, e.ChangeOrder.Permissions, e.ChangeOrder.CreatedAt}, true
		}),
	// The org-wide list keeps only the newest Release of each Space, which is wanted here: whoever
	// publishes is granted Manage on every Release, and listing each would drown out the rest.
	grantSourceOf("Release", "ReleaseID,SpaceID,ReleaseNum",
		func(ctx context.Context, c *Client, w Where, o ListOpts) ([]*goclientnew.ExtendedRelease, error) {
			return ListReleases(ctx, c, w, o)
		},
		func(e *goclientnew.ExtendedRelease) (grantedEntity, bool) {
			if e.Release == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.Release.ReleaseID, e.Release.SpaceID, fmt.Sprintf("release %d", e.Release.ReleaseNum), "", e.Release.Permissions, e.Release.CreatedAt}, true
		}),
	grantSourceOf("View", "ViewID,SpaceID,Slug,DisplayName", ListViews,
		func(e *goclientnew.ExtendedView) (grantedEntity, bool) {
			if e.View == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.View.ViewID, e.View.SpaceID, e.View.Slug, e.View.DisplayName, e.View.Permissions, e.View.CreatedAt}, true
		}),
	grantSourceOf("Filter", "FilterID,SpaceID,Slug,DisplayName",
		func(ctx context.Context, c *Client, w Where, o ListOpts) ([]*goclientnew.ExtendedFilter, error) {
			return ListFilters(ctx, c, w, o)
		},
		func(e *goclientnew.ExtendedFilter) (grantedEntity, bool) {
			if e.Filter == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.Filter.FilterID, e.Filter.SpaceID, e.Filter.Slug, e.Filter.DisplayName, e.Filter.Permissions, e.Filter.CreatedAt}, true
		}),
	grantSourceOf("Trigger", "TriggerID,SpaceID,Slug,DisplayName", ListTriggers,
		func(e *goclientnew.ExtendedTrigger) (grantedEntity, bool) {
			if e.Trigger == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.Trigger.TriggerID, e.Trigger.SpaceID, e.Trigger.Slug, e.Trigger.DisplayName, e.Trigger.Permissions, e.Trigger.CreatedAt}, true
		}),
	grantSourceOf("Invocation", "InvocationID,SpaceID,Slug,DisplayName", ListInvocations,
		func(e *goclientnew.ExtendedInvocation) (grantedEntity, bool) {
			if e.Invocation == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.Invocation.InvocationID, e.Invocation.SpaceID, e.Invocation.Slug, e.Invocation.DisplayName, e.Invocation.Permissions, e.Invocation.CreatedAt}, true
		}),
	grantSourceOf("Tag", "TagID,SpaceID,Slug,DisplayName", ListTags,
		func(e *goclientnew.ExtendedTag) (grantedEntity, bool) {
			if e.Tag == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.Tag.TagID, e.Tag.SpaceID, e.Tag.Slug, e.Tag.DisplayName, e.Tag.Permissions, e.Tag.CreatedAt}, true
		}),
	grantSourceOf("Attribute", "AttributeID,SpaceID,Slug,DisplayName", ListAttributes,
		func(e *goclientnew.ExtendedAttribute) (grantedEntity, bool) {
			if e.Attribute == nil {
				return grantedEntity{}, false
			}
			return grantedEntity{e.Attribute.AttributeID, e.Attribute.SpaceID, e.Attribute.Slug, e.Attribute.DisplayName, e.Attribute.Permissions, e.Attribute.CreatedAt}, true
		}),
}

// grantReadConcurrency bounds the list requests ListGrantsTo has in flight.
const grantReadConcurrency = 6

// ListGrantsTo returns every entity whose Permissions name userID, or one of
// groupIDs, under any action: what a User, such as a ServiceAccount's, has been
// granted. groupIDs are the User's Groups. It sees only what the caller may see.
//
// A where expression has no OR, so each entity type is searched once per
// subject, with `Permissions.*.UserIDs ? '<id>'` or `Permissions.*.GroupIDs ? '<id>'`.
// An entity type that cannot be searched is reported in Unreadable rather than
// failing the whole search.
func ListGrantsTo(ctx context.Context, c *Client, userID goclientnew.UUID, groupIDs []goclientnew.UUID) *Grants {
	type subject struct {
		field string
		id    goclientnew.UUID
	}
	subjects := []subject{{"UserIDs", userID}}
	for _, groupID := range groupIDs {
		subjects = append(subjects, subject{"GroupIDs", groupID})
	}

	type read struct {
		source   int
		entities []grantedEntity
		err      error
	}
	reads := make([]read, 0, len(grantSources)*len(subjects))
	for i := range grantSources {
		for range subjects {
			reads = append(reads, read{source: i})
		}
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, grantReadConcurrency)
	for i := range reads {
		s := subjects[i%len(subjects)]
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			where := NewWhere(fmt.Sprintf("Permissions.*.%s ? '%s'", s.field, s.id))
			reads[i].entities, reads[i].err = grantSources[reads[i].source].list(ctx, c, where)
		})
	}
	wg.Wait()

	result := &Grants{Grants: []Grant{}}
	failed := map[int]bool{}
	seen := map[string]bool{}
	order := map[string]int{}
	for i, source := range grantSources {
		order[source.entityType] = i
	}
	for _, r := range reads {
		source := grantSources[r.source]
		if r.err != nil {
			if !failed[r.source] {
				failed[r.source] = true
				result.Unreadable = append(result.Unreadable, GrantReadError{EntityType: source.entityType, Err: r.err})
			}
			continue
		}
		for _, entity := range r.entities {
			key := source.entityType + ":" + entity.id.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			if actions := grantedActions(entity.permissions, userID, groupIDs); len(actions) > 0 {
				result.Grants = append(result.Grants, Grant{
					EntityType:  source.entityType,
					ID:          entity.id,
					SpaceID:     entity.spaceID,
					Name:        entity.name,
					DisplayName: entity.displayName,
					CreatedAt:   entity.createdAt,
					Actions:     actions,
				})
			}
		}
	}
	sort.SliceStable(result.Grants, func(i, j int) bool {
		a, b := result.Grants[i], result.Grants[j]
		if a.EntityType != b.EntityType {
			return order[a.EntityType] < order[b.EntityType]
		}
		if a.SpaceID != b.SpaceID {
			return a.SpaceID.String() < b.SpaceID.String()
		}
		return naturalLess(a.Name, b.Name)
	})
	return result
}

// naturalLess orders names with the numbers in them compared as numbers, so that
// release-9 comes before release-10.
func naturalLess(a, b string) bool {
	for a != "" && b != "" {
		da, db := digitRun(a), digitRun(b)
		if da > 0 && db > 0 {
			na, nb := strings.TrimLeft(a[:da], "0"), strings.TrimLeft(b[:db], "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			a, b = a[da:], b[db:]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

// digitRun is the length of the run of digits s starts with.
func digitRun(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n
}

// grantedActions are the actions of permissions that name userID or one of
// groupIDs, in order of name.
func grantedActions(permissions *goclientnew.Permissions, userID goclientnew.UUID, groupIDs []goclientnew.UUID) []GrantedAction {
	if permissions == nil {
		return nil
	}
	var actions []GrantedAction
	for action, subjects := range *permissions {
		granted := GrantedAction{Action: action, Direct: subjects.UserIDs[userID.String()]}
		for _, groupID := range groupIDs {
			if subjects.GroupIDs[groupID.String()] {
				granted.GroupIDs = append(granted.GroupIDs, groupID)
			}
		}
		if granted.Direct || len(granted.GroupIDs) > 0 {
			actions = append(actions, granted)
		}
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].Action < actions[j].Action })
	return actions
}
