// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Resolution (resolve.go) turns an entity reference as a person writes it -- a
// slug, a "space/slug", or a UUID -- into the entity.
//
// Every resolver goes through the organization-level list endpoint with one of
// two filters, never both:
//
//   - a UUID becomes "<Entity>ID = '<id>'", with no space clause. A UUID
//     identifies an entity outright, so scoping it by the caller's space can
//     only turn a correct reference into a "not found" for a reason the caller
//     never asked about.
//   - a slug becomes "Slug = '<slug>'", AND-ed with the space when one is
//     known. A slug is unique only within a space, so an unscoped slug lookup
//     that matches more than once is reported as ambiguous rather than
//     answered with whichever row came back first.
package cubapi

import (
	"context"
	"fmt"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// Ref is an entity reference as written on a command line. Exactly one of the
// slug or the id forms is in effect, reported by [Ref.IsID].
//
// Accepted spellings:
//
//	my-unit                  a slug in the caller's space
//	my-space/my-unit         a slug in a named space
//	7c61…d308                a UUID, which names the entity in any space
//	my-space/7c61…d308       a UUID; the space is redundant and ignored
//	*/my-unit                a slug, searched across the organization
type Ref struct {
	// Space is the space part of a qualified reference: a slug, a UUID, "*"
	// for the whole organization, or "" when the reference named no space and
	// the caller's default applies.
	Space string
	// Name is the entity part: a slug, or the string form of a UUID.
	Name string
	// ID is the parsed UUID when Name is one, and the zero UUID otherwise.
	ID goclientnew.UUID

	isID bool
}

// ParseRef splits a reference into its space and entity parts and notes
// whether the entity part is a UUID. It performs no lookups and cannot fail:
// a reference with more than one "/" keeps the first segment as the space and
// the rest as the name, so a malformed reference is reported by the resolver
// that fails to find it rather than by a parse error that cannot say what was
// being looked for.
func ParseRef(s string) Ref {
	r := Ref{Name: strings.TrimSpace(s)}
	if space, name, found := strings.Cut(r.Name, "/"); found {
		r.Space, r.Name = strings.TrimSpace(space), strings.TrimSpace(name)
	}
	if id, err := uuid.Parse(r.Name); err == nil {
		r.ID, r.isID = goclientnew.UUID(id), true
	}
	return r
}

// NewRef builds a reference to a named entity in a named space, for a caller
// that already has the two parts separately and should not have to join them
// with a "/" just to have ParseRef split them again. An empty space means the
// caller's default.
func NewRef(space, name string) Ref {
	r := Ref{Space: strings.TrimSpace(space), Name: strings.TrimSpace(name)}
	if id, err := uuid.Parse(r.Name); err == nil {
		r.ID, r.isID = goclientnew.UUID(id), true
	}
	return r
}

// RefFromID builds a reference to an entity by UUID.
func RefFromID(id goclientnew.UUID) Ref {
	return Ref{Name: id.String(), ID: id, isID: true}
}

// IsID reports whether the reference names an entity by UUID rather than by
// slug. A UUID reference is not scoped by space.
func (r Ref) IsID() bool { return r.isID }

// Empty reports whether the reference names nothing.
func (r Ref) Empty() bool { return r.Name == "" }

// String renders the reference the way it would be written.
func (r Ref) String() string {
	if r.Space == "" || r.isID {
		return r.Name
	}
	return r.Space + "/" + r.Name
}

// ResolveOpts says where to look and what to fetch.
type ResolveOpts struct {
	// Space scopes a slug lookup. The zero UUID means "search the whole
	// organization". It is ignored when the reference is a UUID, and
	// overridden when the reference carries its own space.
	Space goclientnew.UUID
	// Select is a comma-separated list of fields to return. Empty means every
	// field, which is what a single-entity read normally wants.
	Select string
	// Include expands related entities into the returned envelope. Empty uses
	// the entity's default expansion, which is what the corresponding
	// "cub <entity> get" displays.
	Include string
}

// spaceIDForRef decides which space a slug lookup is scoped to: the
// reference's own space when it names one, otherwise the caller's. A "*"
// space, like the zero UUID, means the whole organization.
func spaceIDForRef(ctx context.Context, c *Client, ref Ref, opts ResolveOpts) (goclientnew.UUID, error) {
	switch {
	case ref.Space == "":
		return opts.Space, nil
	case ref.Space == "*":
		return goclientnew.UUID{}, nil
	}
	if id, err := uuid.Parse(ref.Space); err == nil {
		return goclientnew.UUID(id), nil
	}
	space, err := ResolveSpace(ctx, c, Ref{Name: ref.Space}, ResolveOpts{Select: "SpaceID,Slug"})
	if err != nil {
		return goclientnew.UUID{}, err
	}
	return space.Space.SpaceID, nil
}

// resolver holds the per-entity parts of a lookup. Everything else about
// resolving is the same for every entity, which is why there is one
// implementation and a table rather than one function per entity.
type resolver[E any] struct {
	// entity names the type in errors, lower case ("change set").
	entity string
	// idField is the filter column holding the entity's own id ("TargetID").
	idField string
	// include is the default expansion, matching what "cub <entity> get"
	// displays. ResolveOpts.Include overrides it.
	include string
	list    func(context.Context, *Client, Where, ListOpts) ([]*E, error)
	slugOf  func(*E) string
	idOf    func(*E) goclientnew.UUID
}

func (r resolver[E]) resolve(ctx context.Context, c *Client, ref Ref, opts ResolveOpts) (*E, error) {
	if ref.Empty() {
		return nil, fmt.Errorf("no %s named", r.entity)
	}

	where := Where{}
	var spaceID goclientnew.UUID
	if ref.IsID() {
		where = where.Eq(r.idField, ref.Name)
	} else {
		var err error
		if spaceID, err = spaceIDForRef(ctx, c, ref, opts); err != nil {
			return nil, err
		}
		where = where.Slug(ref.Name)
		if spaceID != (goclientnew.UUID{}) {
			where = where.SpaceID(spaceID)
		}
	}

	include := opts.Include
	if include == "" {
		include = r.include
	}
	found, err := r.list(ctx, c, where, ListOpts{Select: opts.Select, Include: include})
	if err != nil {
		return nil, err
	}

	// The filter is exact, so this only drops a row the server would not have
	// matched. It is kept so that a change in filter semantics cannot turn a
	// wrong row into a confidently returned answer.
	matches := make([]*E, 0, 1)
	for _, e := range found {
		if e == nil {
			continue
		}
		if ref.IsID() {
			if r.idOf(e) == ref.ID {
				matches = append(matches, e)
			}
			continue
		}
		if r.slugOf(e) == ref.Name {
			matches = append(matches, e)
		}
	}

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, r.notFound(ref, spaceID)
	default:
		return nil, fmt.Errorf("ambiguous %s reference %q: %d match across spaces; qualify it as space/%s",
			r.entity, ref.String(), len(matches), ref.Name)
	}
}

func (r resolver[E]) notFound(ref Ref, spaceID goclientnew.UUID) error {
	switch {
	case ref.IsID():
		return fmt.Errorf("%s %s not found", r.entity, ref.Name)
	case spaceID == goclientnew.UUID{}:
		return fmt.Errorf("%s %q not found in any space", r.entity, ref.Name)
	default:
		return fmt.Errorf("%s %q not found in space %s", r.entity, ref.Name, spaceID.String())
	}
}

// The default expansions below match what each "cub <entity> get" displayed
// before resolution was shared, so that the slug and the UUID spelling of a
// reference return the same envelope.
const (
	spaceGetInclude        = "TriggerFilterID,TriggerIDs"
	unitGetInclude         = "UnitEventID,TargetID,UpstreamUnitID,SpaceID,FromLinkID,BridgeWorkerID,ChangeSetID,UpstreamSpaceID,ApprovedBy"
	targetGetInclude       = "SpaceID,BridgeWorkerID,TriggerFilterID,TriggerIDs"
	triggerGetInclude      = "SpaceID,BridgeWorkerID,InvocationID,UnitFilterID"
	filterGetInclude       = "SpaceID,FromSpaceID"
	invocationGetInclude   = "SpaceID,BridgeWorkerID"
	changeSetGetInclude    = "SpaceID,StartTagID,EndTagID"
	changeOrderGetInclude  = "SpaceID,StartTagID,EndTagID,RestoreTagID,InvocationID,UnitFilterID"
	tagGetInclude          = "SpaceID,ChangeSetID"
	viewGetInclude         = "SpaceID,FilterID"
	attributeGetInclude    = "SpaceID"
	linkGetInclude         = "SpaceID,ToSpaceID,FromUnitID,ToUnitID"
	bridgeWorkerGetInclude = "SpaceID"
)

// ResolveSpace looks up one space by slug or UUID. A space is not itself
// space-resident, so ResolveOpts.Space is ignored. The with mutators run after
// the common parameters are applied, for the space-specific ones (Summary).
func ResolveSpace(ctx context.Context, c *Client, ref Ref, opts ResolveOpts,
	with ...func(*goclientnew.ListSpacesParams)) (*goclientnew.ExtendedSpace, error) {
	r := resolver[goclientnew.ExtendedSpace]{
		entity: "space", idField: "SpaceID", include: spaceGetInclude,
		list: func(ctx context.Context, c *Client, w Where, o ListOpts) ([]*goclientnew.ExtendedSpace, error) {
			return ListSpaces(ctx, c, w, o, with...)
		},
		slugOf: func(e *goclientnew.ExtendedSpace) string {
			if e.Space == nil {
				return ""
			}
			return e.Space.Slug
		},
		idOf: func(e *goclientnew.ExtendedSpace) goclientnew.UUID {
			if e.Space == nil {
				return goclientnew.UUID{}
			}
			return e.Space.SpaceID
		},
	}
	// A space reference never carries a space of its own.
	return r.resolve(ctx, c, Ref{Name: ref.Name, ID: ref.ID, isID: ref.isID}, ResolveOpts{Select: opts.Select, Include: opts.Include})
}

// ResolveUnit looks up one unit by slug, space/slug, or UUID.
func ResolveUnit(ctx context.Context, c *Client, ref Ref, opts ResolveOpts) (*goclientnew.ExtendedUnit, error) {
	return resolver[goclientnew.ExtendedUnit]{
		entity: "unit", idField: "UnitID", include: unitGetInclude,
		list: func(ctx context.Context, c *Client, w Where, o ListOpts) ([]*goclientnew.ExtendedUnit, error) {
			return ListUnits(ctx, c, w, o)
		},
		slugOf: func(e *goclientnew.ExtendedUnit) string {
			if e.Unit == nil {
				return ""
			}
			return e.Unit.Slug
		},
		idOf: func(e *goclientnew.ExtendedUnit) goclientnew.UUID {
			if e.Unit == nil {
				return goclientnew.UUID{}
			}
			return e.Unit.UnitID
		},
	}.resolve(ctx, c, ref, opts)
}

// ResolveTarget looks up one target by slug, space/slug, or UUID.
func ResolveTarget(ctx context.Context, c *Client, ref Ref, opts ResolveOpts) (*goclientnew.ExtendedTarget, error) {
	return resolver[goclientnew.ExtendedTarget]{
		entity: "target", idField: "TargetID", include: targetGetInclude,
		list: ListTargets,
		slugOf: func(e *goclientnew.ExtendedTarget) string {
			if e.Target == nil {
				return ""
			}
			return e.Target.Slug
		},
		idOf: func(e *goclientnew.ExtendedTarget) goclientnew.UUID {
			if e.Target == nil {
				return goclientnew.UUID{}
			}
			return e.Target.TargetID
		},
	}.resolve(ctx, c, ref, opts)
}

// ResolveTrigger looks up one trigger by slug, space/slug, or UUID.
func ResolveTrigger(ctx context.Context, c *Client, ref Ref, opts ResolveOpts) (*goclientnew.ExtendedTrigger, error) {
	return resolver[goclientnew.ExtendedTrigger]{
		entity: "trigger", idField: "TriggerID", include: triggerGetInclude,
		list: ListTriggers,
		slugOf: func(e *goclientnew.ExtendedTrigger) string {
			if e.Trigger == nil {
				return ""
			}
			return e.Trigger.Slug
		},
		idOf: func(e *goclientnew.ExtendedTrigger) goclientnew.UUID {
			if e.Trigger == nil {
				return goclientnew.UUID{}
			}
			return e.Trigger.TriggerID
		},
	}.resolve(ctx, c, ref, opts)
}

// ResolveFilter looks up one filter by slug, space/slug, or UUID.
func ResolveFilter(ctx context.Context, c *Client, ref Ref, opts ResolveOpts) (*goclientnew.ExtendedFilter, error) {
	return resolver[goclientnew.ExtendedFilter]{
		entity: "filter", idField: "FilterID", include: filterGetInclude,
		list: func(ctx context.Context, c *Client, w Where, o ListOpts) ([]*goclientnew.ExtendedFilter, error) {
			return ListFilters(ctx, c, w, o)
		},
		slugOf: func(e *goclientnew.ExtendedFilter) string {
			if e.Filter == nil {
				return ""
			}
			return e.Filter.Slug
		},
		idOf: func(e *goclientnew.ExtendedFilter) goclientnew.UUID {
			if e.Filter == nil {
				return goclientnew.UUID{}
			}
			return e.Filter.FilterID
		},
	}.resolve(ctx, c, ref, opts)
}

// ResolveInvocation looks up one stored invocation by slug, space/slug, or UUID.
func ResolveInvocation(ctx context.Context, c *Client, ref Ref, opts ResolveOpts) (*goclientnew.ExtendedInvocation, error) {
	return resolver[goclientnew.ExtendedInvocation]{
		entity: "invocation", idField: "InvocationID", include: invocationGetInclude,
		list: ListInvocations,
		slugOf: func(e *goclientnew.ExtendedInvocation) string {
			if e.Invocation == nil {
				return ""
			}
			return e.Invocation.Slug
		},
		idOf: func(e *goclientnew.ExtendedInvocation) goclientnew.UUID {
			if e.Invocation == nil {
				return goclientnew.UUID{}
			}
			return e.Invocation.InvocationID
		},
	}.resolve(ctx, c, ref, opts)
}

// ResolveChangeSet looks up one change set by slug, space/slug, or UUID.
func ResolveChangeSet(ctx context.Context, c *Client, ref Ref, opts ResolveOpts) (*goclientnew.ExtendedChangeSet, error) {
	return resolver[goclientnew.ExtendedChangeSet]{
		entity: "change set", idField: "ChangeSetID", include: changeSetGetInclude,
		list: ListChangeSets,
		slugOf: func(e *goclientnew.ExtendedChangeSet) string {
			if e.ChangeSet == nil {
				return ""
			}
			return e.ChangeSet.Slug
		},
		idOf: func(e *goclientnew.ExtendedChangeSet) goclientnew.UUID {
			if e.ChangeSet == nil {
				return goclientnew.UUID{}
			}
			return e.ChangeSet.ChangeSetID
		},
	}.resolve(ctx, c, ref, opts)
}

// ResolveChangeOrder looks up one change order by slug, space/slug, or UUID.
func ResolveChangeOrder(ctx context.Context, c *Client, ref Ref, opts ResolveOpts) (*goclientnew.ExtendedChangeOrder, error) {
	return resolver[goclientnew.ExtendedChangeOrder]{
		entity: "change order", idField: "ChangeOrderID", include: changeOrderGetInclude,
		list: ListChangeOrders,
		slugOf: func(e *goclientnew.ExtendedChangeOrder) string {
			if e.ChangeOrder == nil {
				return ""
			}
			return e.ChangeOrder.Slug
		},
		idOf: func(e *goclientnew.ExtendedChangeOrder) goclientnew.UUID {
			if e.ChangeOrder == nil {
				return goclientnew.UUID{}
			}
			return e.ChangeOrder.ChangeOrderID
		},
	}.resolve(ctx, c, ref, opts)
}

// ResolveTag looks up one tag by slug, space/slug, or UUID.
func ResolveTag(ctx context.Context, c *Client, ref Ref, opts ResolveOpts) (*goclientnew.ExtendedTag, error) {
	return resolver[goclientnew.ExtendedTag]{
		entity: "tag", idField: "TagID", include: tagGetInclude,
		list: ListTags,
		slugOf: func(e *goclientnew.ExtendedTag) string {
			if e.Tag == nil {
				return ""
			}
			return e.Tag.Slug
		},
		idOf: func(e *goclientnew.ExtendedTag) goclientnew.UUID {
			if e.Tag == nil {
				return goclientnew.UUID{}
			}
			return e.Tag.TagID
		},
	}.resolve(ctx, c, ref, opts)
}

// ResolveView looks up one view by slug, space/slug, or UUID.
func ResolveView(ctx context.Context, c *Client, ref Ref, opts ResolveOpts) (*goclientnew.ExtendedView, error) {
	return resolver[goclientnew.ExtendedView]{
		entity: "view", idField: "ViewID", include: viewGetInclude,
		list: ListViews,
		slugOf: func(e *goclientnew.ExtendedView) string {
			if e.View == nil {
				return ""
			}
			return e.View.Slug
		},
		idOf: func(e *goclientnew.ExtendedView) goclientnew.UUID {
			if e.View == nil {
				return goclientnew.UUID{}
			}
			return e.View.ViewID
		},
	}.resolve(ctx, c, ref, opts)
}

// ResolveAttribute looks up one attribute by slug, space/slug, or UUID.
func ResolveAttribute(ctx context.Context, c *Client, ref Ref, opts ResolveOpts) (*goclientnew.ExtendedAttribute, error) {
	return resolver[goclientnew.ExtendedAttribute]{
		entity: "attribute", idField: "AttributeID", include: attributeGetInclude,
		list: ListAttributes,
		slugOf: func(e *goclientnew.ExtendedAttribute) string {
			if e.Attribute == nil {
				return ""
			}
			return e.Attribute.Slug
		},
		idOf: func(e *goclientnew.ExtendedAttribute) goclientnew.UUID {
			if e.Attribute == nil {
				return goclientnew.UUID{}
			}
			return e.Attribute.AttributeID
		},
	}.resolve(ctx, c, ref, opts)
}

// ResolveLink looks up one link by slug, space/slug, or UUID.
func ResolveLink(ctx context.Context, c *Client, ref Ref, opts ResolveOpts) (*goclientnew.ExtendedLink, error) {
	return resolver[goclientnew.ExtendedLink]{
		entity: "link", idField: "LinkID", include: linkGetInclude,
		list: ListLinks,
		slugOf: func(e *goclientnew.ExtendedLink) string {
			if e.Link == nil {
				return ""
			}
			return e.Link.Slug
		},
		idOf: func(e *goclientnew.ExtendedLink) goclientnew.UUID {
			if e.Link == nil {
				return goclientnew.UUID{}
			}
			return e.Link.LinkID
		},
	}.resolve(ctx, c, ref, opts)
}

// ResolveBridgeWorker looks up one bridge worker by slug, space/slug, or UUID.
func ResolveBridgeWorker(ctx context.Context, c *Client, ref Ref, opts ResolveOpts) (*goclientnew.ExtendedBridgeWorker, error) {
	return resolver[goclientnew.ExtendedBridgeWorker]{
		entity: "worker", idField: "BridgeWorkerID", include: bridgeWorkerGetInclude,
		list: func(ctx context.Context, c *Client, w Where, o ListOpts) ([]*goclientnew.ExtendedBridgeWorker, error) {
			return ListBridgeWorkers(ctx, c, w, o)
		},
		slugOf: func(e *goclientnew.ExtendedBridgeWorker) string {
			if e.BridgeWorker == nil {
				return ""
			}
			return e.BridgeWorker.Slug
		},
		idOf: func(e *goclientnew.ExtendedBridgeWorker) goclientnew.UUID {
			if e.BridgeWorker == nil {
				return goclientnew.UUID{}
			}
			return e.BridgeWorker.BridgeWorkerID
		},
	}.resolve(ctx, c, ref, opts)
}
