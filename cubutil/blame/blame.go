// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package blame reads what set each field of a Unit, when, and why, and everything else
// recorded about each field, through the API's blame reads, and renders it for a terminal.
//
// The server computes the answer: it flattens a Unit's configuration into fields, credits
// each to the change that wrote it, and follows each merge and Link resolve upstream to
// where the value was set. This package asks for it and words it.
package blame

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// The types are the API's.
type (
	// Field is one field and everything known about how it got its value.
	Field = goclientnew.BlameField
	// Origin is one hop of a field's history: a change, in the unit and space it was made in.
	Origin = goclientnew.BlameOrigin
	// Validation is a validating Trigger's failed check that names a field.
	Validation = goclientnew.BlameValidation
	// NeededPath is one of a unit's needed paths that is a field.
	NeededPath = goclientnew.BlameNeededPath
	// PathReport is everything recorded about one field.
	PathReport = goclientnew.UnitPathReport
	// PathChange is one change to a field, with the records it was read from.
	PathChange = goclientnew.BlamePathChange
)

// ErrNoUnits is what Variant returns when the where clause selects no units.
var ErrNoUnits = errors.New("no units selected")

// MaxUpstreamDepth is the most upstream units the server follows a value through, and how
// many it follows when not told.
const MaxUpstreamDepth = 8

// Options say what to ask the server for.
type Options struct {
	// MaxUpstreamDepth is how many upstream units to follow; 0 means MaxUpstreamDepth.
	MaxUpstreamDepth int
	// NoUpstream reports each unit's own record without following a merge or a Link to where
	// the value was set.
	NoUpstream bool
	// Path keeps only the fields whose path contains it, and Resource only the fields of a
	// resource named <type>/<name> or <name>.
	Path     string
	Resource string
	// Flags keeps only the fields with at least one of these flag codes.
	Flags []string
	// IncludeComments keeps the $comment$ pseudo-fields that carry YAML comments.
	IncludeComments bool
}

func (o Options) upstream() (*bool, *int) {
	upstream := !o.NoUpstream
	var depth *int
	if o.MaxUpstreamDepth > 0 {
		depth = &o.MaxUpstreamDepth
	}
	return &upstream, depth
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Unit reads the blame of one unit.
func Unit(ctx context.Context, client *cubapi.Client, spaceID, unitID uuid.UUID, opts Options) ([]*Field, error) {
	upstream, depth := opts.upstream()
	params := &goclientnew.GetUnitBlameParams{
		Path:            optional(opts.Path),
		Resource:        optional(opts.Resource),
		Flags:           optional(strings.Join(opts.Flags, ",")),
		Upstream:        upstream,
		UpstreamDepth:   depth,
		IncludeComments: &opts.IncludeComments,
	}
	res, err := client.API.GetUnitBlameWithResponse(ctx, spaceID, unitID, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	return pointers(res.JSON200.Fields), nil
}

// Variant reads the blame of every unit a where clause selects, reading every page, in Space
// and then unit slug order. A unit the server could not blame is an error naming it.
func Variant(ctx context.Context, client *cubapi.Client, where string, opts Options) ([]*Field, error) {
	upstream, depth := opts.upstream()
	params := &goclientnew.SearchUnitBlameParams{
		Where:           optional(where),
		Path:            optional(opts.Path),
		Resource:        optional(opts.Resource),
		Flags:           optional(strings.Join(opts.Flags, ",")),
		Upstream:        upstream,
		UpstreamDepth:   depth,
		IncludeComments: &opts.IncludeComments,
	}
	units, err := cubapi.ReadPages(cubapi.ListOpts{}, func(_ *int, token *string) (*http.Response, *[]goclientnew.UnitBlame, error) {
		// The server answers for a few dozen units a page; its own page size is the one to use.
		params.Continue = token
		res, err := client.API.SearchUnitBlameWithResponse(ctx, params)
		if cubapi.IsAPIError(err, res) {
			return nil, nil, cubapi.InterpretErrorGeneric(err, res)
		}
		return res.HTTPResponse, res.JSON200, nil
	})
	if err != nil {
		return nil, err
	}
	if len(units) == 0 {
		return nil, ErrNoUnits
	}
	var blamed [][]*Field
	for _, unit := range units {
		if unit.Error != nil {
			return nil, errors.Newf("failed to blame unit %s: %s", unit.UnitID, unit.Error.Message)
		}
		if len(unit.Fields) > 0 {
			blamed = append(blamed, pointers(unit.Fields))
		}
	}
	slices.SortStableFunc(blamed, func(a, b []*Field) int {
		return cmp.Or(strings.Compare(a[0].SpaceSlug, b[0].SpaceSlug), strings.Compare(a[0].UnitSlug, b[0].UnitSlug))
	})
	return slices.Concat(blamed...), nil
}

// Path reads everything recorded about one field of a unit, named by its REF, a prefix of
// one, or its path; resource names the resource when more than one has the path.
func Path(ctx context.Context, client *cubapi.Client, spaceID, unitID uuid.UUID, field, resource string,
	history bool, opts Options) (*PathReport, error) {
	upstream, depth := opts.upstream()
	params := &goclientnew.GetUnitPathParams{
		Field:         field,
		Resource:      optional(resource),
		History:       &history,
		Upstream:      upstream,
		UpstreamDepth: depth,
	}
	res, err := client.API.GetUnitPathWithResponse(ctx, spaceID, unitID, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, errors.New("the server returned no report")
	}
	return res.JSON200, nil
}

// OriginOf is the hop that actually set a field's value: the end of its chain.
func OriginOf(f *Field) *Origin {
	if len(f.Chain) == 0 {
		return nil
	}
	return &f.Chain[len(f.Chain)-1]
}

func pointers[T any](values []T) []*T {
	ptrs := make([]*T, len(values))
	for i := range values {
		ptrs[i] = &values[i]
	}
	return ptrs
}
