// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Queries (query.go) are typed, org-level read helpers over the ConfigHub API.
//
// Two deliberate choices keep callers consistent:
//
//   - Org-level endpoints only. Reads always use the cross-space "ListAll*"
//     endpoints (and ListSpaces, which is already org-scoped), scoped with a
//     [Where] clause. There is no per-space-vs-"*" branching to get wrong.
//   - No globals. Every call takes an explicit [Client] and filter.
//
// The generated list endpoints return "Extended" envelopes — e.g. ListAllUnits
// returns []ExtendedUnit, where the core record is in the .Unit field and
// related entities (.Space, .Target, …) are populated only when requested via
// [ListOpts.Include].
package cubapi

import (
	"context"
	"fmt"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Where builds a SQL-inspired filter expression by AND-ing clauses, matching the
// syntax the ConfigHub API accepts for its "where" parameters. The zero value is
// an empty (match-all) filter. Where is immutable; each method returns a new
// value.
type Where struct {
	clauses []string
}

// NewWhere starts a filter from a raw clause. An empty clause yields an empty
// filter.
func NewWhere(clause string) Where {
	return Where{}.And(clause)
}

// And appends a raw clause, AND-ed with the existing ones. An empty clause is
// ignored, so And is safe to call with optional predicates.
func (w Where) And(clause string) Where {
	clause = strings.TrimSpace(clause)
	if clause == "" {
		return w
	}
	next := make([]string, len(w.clauses), len(w.clauses)+1)
	copy(next, w.clauses)
	return Where{clauses: append(next, clause)}
}

// Slug appends a `Slug = '<slug>'` predicate.
func (w Where) Slug(slug string) Where {
	return w.And(fmt.Sprintf("Slug = '%s'", slug))
}

// SpaceID appends a `SpaceID = '<id>'` predicate, scoping a query to one space
// while still using an org-level endpoint.
func (w Where) SpaceID(id goclientnew.UUID) Where {
	return w.And(fmt.Sprintf("SpaceID = '%s'", id.String()))
}

// In appends a `<field> IN ('a','b',…)` predicate over UUIDs. Calling In with no
// ids is a no-op (it returns the filter unchanged); callers that need "match
// nothing" semantics for an empty set should short-circuit before querying.
func (w Where) In(field string, ids []goclientnew.UUID) Where {
	if len(ids) == 0 {
		return w
	}
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = "'" + id.String() + "'"
	}
	return w.And(fmt.Sprintf("%s IN (%s)", field, strings.Join(quoted, ",")))
}

// Empty reports whether the filter has no clauses.
func (w Where) Empty() bool { return len(w.clauses) == 0 }

// String renders the filter as the API "where" expression.
func (w Where) String() string { return strings.Join(w.clauses, " AND ") }

// ListOpts carries the query parameters common to every list helper. Zero
// values are omitted from the request. Entity-specific parameters are set via
// each helper's mutator functions.
type ListOpts struct {
	// Select is a comma-separated, PascalCase list of fields to return.
	Select string
	// Include expands related entities into the Extended envelope (e.g.
	// "SpaceID,TargetID" to populate .Space and .Target on a unit).
	Include string
	// Filter names a stored Filter to apply, as "space/filter" or "filter".
	Filter string
	// Contains is a free-text search.
	Contains string
}

func ptrIf(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// SelectFields normalizes a select value for use in [ListOpts.Select]: the "*"
// wildcard (meaning "all fields") becomes "", which the API treats the same way
// and which the list helpers omit from the request. Callers translating a CLI
// --select value should pass it through this.
func SelectFields(value string) string {
	if value == "*" {
		return ""
	}
	return value
}

// ListUnits returns units across the organization matching where, using the
// org-level ListAllUnits endpoint. Results are ExtendedUnit envelopes; the core
// record is in each element's .Unit field. Unit-specific options (ResourceType,
// WhereData, WhereTrigger, TriggerFilter, TriggersPassed, View) are set via the
// with mutators, which run after the common where/opts are applied.
func ListUnits(ctx context.Context, c *Client, where Where, opts ListOpts, with ...func(*goclientnew.ListAllUnitsParams)) ([]*goclientnew.ExtendedUnit, error) {
	params := &goclientnew.ListAllUnitsParams{
		Where:    ptrIf(where.String()),
		Select:   ptrIf(opts.Select),
		Include:  ptrIf(opts.Include),
		Filter:   ptrIf(opts.Filter),
		Contains: ptrIf(opts.Contains),
	}
	for _, fn := range with {
		fn(params)
	}
	res, err := c.API.ListAllUnitsWithResponse(ctx, params)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return derefPtrs(res.JSON200), nil
}

// ListSpaces returns spaces across the organization matching where. ListSpaces
// is already org-scoped, so there is no "ListAll" variant. Space-specific options
// (Summary) are set via the with mutators, which run after the common where/opts
// are applied.
func ListSpaces(ctx context.Context, c *Client, where Where, opts ListOpts, with ...func(*goclientnew.ListSpacesParams)) ([]*goclientnew.ExtendedSpace, error) {
	params := &goclientnew.ListSpacesParams{
		Where:    ptrIf(where.String()),
		Select:   ptrIf(opts.Select),
		Include:  ptrIf(opts.Include),
		Filter:   ptrIf(opts.Filter),
		Contains: ptrIf(opts.Contains),
	}
	for _, fn := range with {
		fn(params)
	}
	res, err := c.API.ListSpacesWithResponse(ctx, params)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return derefPtrs(res.JSON200), nil
}

// ListTargets returns targets across the organization matching where.
func ListTargets(ctx context.Context, c *Client, where Where, opts ListOpts) ([]*goclientnew.ExtendedTarget, error) {
	params := &goclientnew.ListAllTargetsParams{
		Where:    ptrIf(where.String()),
		Select:   ptrIf(opts.Select),
		Include:  ptrIf(opts.Include),
		Filter:   ptrIf(opts.Filter),
		Contains: ptrIf(opts.Contains),
	}
	res, err := c.API.ListAllTargetsWithResponse(ctx, params)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return derefPtrs(res.JSON200), nil
}

// ListTriggers returns triggers across the organization matching where.
func ListTriggers(ctx context.Context, c *Client, where Where, opts ListOpts) ([]*goclientnew.ExtendedTrigger, error) {
	params := &goclientnew.ListAllTriggersParams{
		Where:    ptrIf(where.String()),
		Select:   ptrIf(opts.Select),
		Include:  ptrIf(opts.Include),
		Filter:   ptrIf(opts.Filter),
		Contains: ptrIf(opts.Contains),
	}
	res, err := c.API.ListAllTriggersWithResponse(ctx, params)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return derefPtrs(res.JSON200), nil
}

// ListFilters returns filters across the organization matching where. Filter-
// specific options (Entity/Id) are set via the with mutators, which run after
// the common where/opts are applied.
func ListFilters(ctx context.Context, c *Client, where Where, opts ListOpts, with ...func(*goclientnew.ListAllFiltersParams)) ([]*goclientnew.ExtendedFilter, error) {
	params := &goclientnew.ListAllFiltersParams{
		Where:    ptrIf(where.String()),
		Select:   ptrIf(opts.Select),
		Include:  ptrIf(opts.Include),
		Filter:   ptrIf(opts.Filter),
		Contains: ptrIf(opts.Contains),
	}
	for _, fn := range with {
		fn(params)
	}
	res, err := c.API.ListAllFiltersWithResponse(ctx, params)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return derefPtrs(res.JSON200), nil
}

// ListInvocations returns stored invocations across the organization matching
// where.
func ListInvocations(ctx context.Context, c *Client, where Where, opts ListOpts) ([]*goclientnew.ExtendedInvocation, error) {
	params := &goclientnew.ListAllInvocationsParams{
		Where:    ptrIf(where.String()),
		Select:   ptrIf(opts.Select),
		Include:  ptrIf(opts.Include),
		Filter:   ptrIf(opts.Filter),
		Contains: ptrIf(opts.Contains),
	}
	res, err := c.API.ListAllInvocationsWithResponse(ctx, params)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return derefPtrs(res.JSON200), nil
}

// ListChangeSets returns change sets across the organization matching where.
func ListChangeSets(ctx context.Context, c *Client, where Where, opts ListOpts) ([]*goclientnew.ExtendedChangeSet, error) {
	params := &goclientnew.ListAllChangeSetsParams{
		Where:    ptrIf(where.String()),
		Select:   ptrIf(opts.Select),
		Include:  ptrIf(opts.Include),
		Filter:   ptrIf(opts.Filter),
		Contains: ptrIf(opts.Contains),
	}
	res, err := c.API.ListAllChangeSetsWithResponse(ctx, params)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return derefPtrs(res.JSON200), nil
}

// ListChangeOrders returns change orders across the organization matching where.
func ListChangeOrders(ctx context.Context, c *Client, where Where, opts ListOpts) ([]*goclientnew.ExtendedChangeOrder, error) {
	params := &goclientnew.ListAllChangeOrdersParams{
		Where:    ptrIf(where.String()),
		Select:   ptrIf(opts.Select),
		Include:  ptrIf(opts.Include),
		Filter:   ptrIf(opts.Filter),
		Contains: ptrIf(opts.Contains),
	}
	res, err := c.API.ListAllChangeOrdersWithResponse(ctx, params)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return derefPtrs(res.JSON200), nil
}

// ListTags returns tags across the organization matching where.
func ListTags(ctx context.Context, c *Client, where Where, opts ListOpts) ([]*goclientnew.ExtendedTag, error) {
	params := &goclientnew.ListAllTagsParams{
		Where:    ptrIf(where.String()),
		Select:   ptrIf(opts.Select),
		Include:  ptrIf(opts.Include),
		Filter:   ptrIf(opts.Filter),
		Contains: ptrIf(opts.Contains),
	}
	res, err := c.API.ListAllTagsWithResponse(ctx, params)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return derefPtrs(res.JSON200), nil
}

// ListViews returns views across the organization matching where.
func ListViews(ctx context.Context, c *Client, where Where, opts ListOpts) ([]*goclientnew.ExtendedView, error) {
	params := &goclientnew.ListAllViewsParams{
		Where:    ptrIf(where.String()),
		Select:   ptrIf(opts.Select),
		Include:  ptrIf(opts.Include),
		Filter:   ptrIf(opts.Filter),
		Contains: ptrIf(opts.Contains),
	}
	res, err := c.API.ListAllViewsWithResponse(ctx, params)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return derefPtrs(res.JSON200), nil
}

// ListResources returns the resources of units across the organization matching
// where. Resources are derived from unit configuration data and are read-only;
// there is no per-space "ListAll" endpoint, so the org-level search endpoint is
// used.
// Resource-specific options (RawData) are set via the with mutators, which run after the
// common where/opts are applied.
func ListResources(ctx context.Context, c *Client, where Where, opts ListOpts, with ...func(*goclientnew.ListAllResourcesParams)) ([]*goclientnew.ExtendedResource, error) {
	params := &goclientnew.ListAllResourcesParams{
		Where:    ptrIf(where.String()),
		Select:   ptrIf(opts.Select),
		Include:  ptrIf(opts.Include),
		Filter:   ptrIf(opts.Filter),
		Contains: ptrIf(opts.Contains),
	}
	for _, fn := range with {
		fn(params)
	}
	res, err := c.API.ListAllResourcesWithResponse(ctx, params)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return derefPtrs(res.JSON200), nil
}

// ListAttributes returns attributes across the organization matching where.
func ListAttributes(ctx context.Context, c *Client, where Where, opts ListOpts) ([]*goclientnew.ExtendedAttribute, error) {
	params := &goclientnew.ListAllAttributesParams{
		Where:    ptrIf(where.String()),
		Select:   ptrIf(opts.Select),
		Include:  ptrIf(opts.Include),
		Filter:   ptrIf(opts.Filter),
		Contains: ptrIf(opts.Contains),
	}
	res, err := c.API.ListAllAttributesWithResponse(ctx, params)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return derefPtrs(res.JSON200), nil
}

// ListLinks returns links across the organization matching where. Links have no
// per-space "ListAll" endpoint; the org-level search endpoint is used.
func ListLinks(ctx context.Context, c *Client, where Where, opts ListOpts) ([]*goclientnew.ExtendedLink, error) {
	params := &goclientnew.SearchListLinksParams{
		Where:    ptrIf(where.String()),
		Select:   ptrIf(opts.Select),
		Include:  ptrIf(opts.Include),
		Filter:   ptrIf(opts.Filter),
		Contains: ptrIf(opts.Contains),
	}
	res, err := c.API.SearchListLinksWithResponse(ctx, params)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return derefPtrs(res.JSON200), nil
}

// ListBridgeWorkers returns bridge workers across the organization matching
// where. Bridge-worker-specific options (Summary) are set via the with mutators,
// which run after the common where/opts are applied.
func ListBridgeWorkers(ctx context.Context, c *Client, where Where, opts ListOpts, with ...func(*goclientnew.ListAllBridgeWorkersParams)) ([]*goclientnew.ExtendedBridgeWorker, error) {
	params := &goclientnew.ListAllBridgeWorkersParams{
		Where:    ptrIf(where.String()),
		Select:   ptrIf(opts.Select),
		Include:  ptrIf(opts.Include),
		Filter:   ptrIf(opts.Filter),
		Contains: ptrIf(opts.Contains),
	}
	for _, fn := range with {
		fn(params)
	}
	res, err := c.API.ListAllBridgeWorkersWithResponse(ctx, params)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return derefPtrs(res.JSON200), nil
}

// ResolveSpace looks up a single space by slug and returns its core record. It
// errors if no space (or more than one) matches.
func ResolveSpace(ctx context.Context, c *Client, slug string) (*goclientnew.Space, error) {
	spaces, err := ListSpaces(ctx, c, NewWhere("").Slug(slug), ListOpts{})
	if err != nil {
		return nil, err
	}
	matches := make([]*goclientnew.Space, 0, 1)
	for _, es := range spaces {
		if es.Space != nil && es.Space.Slug == slug {
			matches = append(matches, es.Space)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("space %q not found", slug)
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("ambiguous: %d spaces named %q", len(matches), slug)
	}
}

// ResolveFilter finds a single Filter by slug within a space.
func ResolveFilter(ctx context.Context, c *Client, spaceID goclientnew.UUID, slug string) (*goclientnew.Filter, error) {
	filters, err := ListFilters(ctx, c, Where{}.SpaceID(spaceID).Slug(slug), ListOpts{})
	if err != nil {
		return nil, err
	}
	for _, ef := range filters {
		if ef.Filter != nil && ef.Filter.Slug == slug {
			return ef.Filter, nil
		}
	}
	return nil, fmt.Errorf("filter %q not found in space %s", slug, spaceID.String())
}

// ResolveInvocation finds a single stored Invocation by slug within a space.
func ResolveInvocation(ctx context.Context, c *Client, spaceID goclientnew.UUID, slug string) (*goclientnew.Invocation, error) {
	invs, err := ListInvocations(ctx, c, Where{}.SpaceID(spaceID).Slug(slug), ListOpts{})
	if err != nil {
		return nil, err
	}
	for _, ei := range invs {
		if ei.Invocation != nil && ei.Invocation.Slug == slug {
			return ei.Invocation, nil
		}
	}
	return nil, fmt.Errorf("invocation %q not found in space %s", slug, spaceID.String())
}

// SpaceSlugByID returns a map from space UUID to slug for the whole organization,
// for resolving the SpaceID fields on units and other entities.
func SpaceSlugByID(ctx context.Context, c *Client) (map[goclientnew.UUID]string, error) {
	spaces, err := ListSpaces(ctx, c, Where{}, ListOpts{Select: "SpaceID,Slug"})
	if err != nil {
		return nil, err
	}
	out := make(map[goclientnew.UUID]string, len(spaces))
	for _, es := range spaces {
		if es.Space != nil {
			out[es.Space.SpaceID] = es.Space.Slug
		}
	}
	return out, nil
}

// derefPtrs converts a *[]T response body into a []*T, tolerating a nil body.
func derefPtrs[T any](items *[]T) []*T {
	if items == nil {
		return nil
	}
	out := make([]*T, 0, len(*items))
	for i := range *items {
		out = append(out, &(*items)[i])
	}
	return out
}
