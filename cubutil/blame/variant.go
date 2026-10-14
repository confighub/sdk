// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Variant blames every unit of a space that the where clause selects, in slug order.
//
// What each unit's blame starts from -- its record and its data -- is read for the whole
// selection in one request each, rather than two per unit. The walk upstream is per unit, but
// its caches are shared, so a base that every unit merges from is read once.
func (r *Resolver) Variant(ctx context.Context, space *goclientnew.Space, selectionWhere string) ([]*Field, error) {
	listed, err := cubapi.ListUnits(ctx, r.client, cubapi.NewWhere(selectionWhere).SpaceID(space.SpaceID), cubapi.ListOpts{})
	if err != nil {
		return nil, err
	}
	units := make([]*goclientnew.Unit, 0, len(listed))
	for _, extended := range listed {
		if extended.Unit != nil {
			units = append(units, extended.Unit)
		}
	}
	if len(units) == 0 {
		return nil, errors.Newf("no units selected in space %s", space.Slug)
	}
	slices.SortFunc(units, func(a, b *goclientnew.Unit) int {
		return strings.Compare(a.Slug, b.Slug)
	})

	whereClause := fmt.Sprintf("SpaceID = '%s'", space.SpaceID)
	if selectionWhere != "" {
		whereClause = selectionWhere + " AND " + whereClause
	}
	sources, err := r.searchUnitMutationSources(ctx, whereClause)
	if err != nil {
		return nil, err
	}
	data, err := r.searchUnitData(ctx, whereClause)
	if err != nil {
		return nil, err
	}

	var fields []*Field
	for _, unit := range units {
		unitFields, err := r.UnitData(ctx, unit, sources[unit.UnitID], data[unit.UnitID])
		if err != nil {
			return nil, errors.Wrapf(err, "failed to blame unit %s", unit.Slug)
		}
		fields = append(fields, unitFields...)
	}
	return fields, nil
}
