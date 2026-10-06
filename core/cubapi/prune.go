// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// A prune deletes the entities whose backing Units are empty, and keeps the Units: an empty backing
// Unit says its entity should not exist, as an emptied Kubernetes Unit says its object should not.
// It is a bulk delete with from_backing_units, which passes over the entities it selects whose
// backing Units hold a document.

// pruneRequest sends one type's bulk delete with from_backing_units over the entities where
// selects.
type pruneRequest func(ctx context.Context, c *Client, where string) ([]goclientnew.DeleteResponse, error)

// pruneResults returns the per-entity results of a bulk delete, which the server answers with 200
// when every delete succeeded and 207 otherwise.
func pruneResults(json200, json207 *[]goclientnew.DeleteResponse) []goclientnew.DeleteResponse {
	if json200 != nil {
		return *json200
	}
	if json207 != nil {
		return *json207
	}
	return nil
}

var prune = true

var pruneRequests = map[string]pruneRequest{
	"Attribute": func(ctx context.Context, c *Client, where string) ([]goclientnew.DeleteResponse, error) {
		res, err := c.API.BulkDeleteAttributesWithResponse(ctx,
			&goclientnew.BulkDeleteAttributesParams{Where: &where, FromBackingUnits: &prune})
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return pruneResults(res.JSON200, res.JSON207), nil
	},
	"ChangeWorkflow": func(ctx context.Context, c *Client, where string) ([]goclientnew.DeleteResponse, error) {
		res, err := c.API.BulkDeleteChangeWorkflowsWithResponse(ctx,
			&goclientnew.BulkDeleteChangeWorkflowsParams{Where: &where, FromBackingUnits: &prune})
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return pruneResults(res.JSON200, res.JSON207), nil
	},
	"Component": func(ctx context.Context, c *Client, where string) ([]goclientnew.DeleteResponse, error) {
		res, err := c.API.BulkDeleteComponentsWithResponse(ctx,
			&goclientnew.BulkDeleteComponentsParams{Where: &where, FromBackingUnits: &prune})
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return pruneResults(res.JSON200, res.JSON207), nil
	},
	"Filter": func(ctx context.Context, c *Client, where string) ([]goclientnew.DeleteResponse, error) {
		res, err := c.API.BulkDeleteFiltersWithResponse(ctx,
			&goclientnew.BulkDeleteFiltersParams{Where: &where, FromBackingUnits: &prune})
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return pruneResults(res.JSON200, res.JSON207), nil
	},
	"Invocation": func(ctx context.Context, c *Client, where string) ([]goclientnew.DeleteResponse, error) {
		res, err := c.API.BulkDeleteInvocationsWithResponse(ctx,
			&goclientnew.BulkDeleteInvocationsParams{Where: &where, FromBackingUnits: &prune})
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return pruneResults(res.JSON200, res.JSON207), nil
	},
	"Link": func(ctx context.Context, c *Client, where string) ([]goclientnew.DeleteResponse, error) {
		res, err := c.API.BulkDeleteLinksWithResponse(ctx,
			&goclientnew.BulkDeleteLinksParams{Where: &where, FromBackingUnits: &prune})
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return pruneResults(res.JSON200, res.JSON207), nil
	},
	"Space": func(ctx context.Context, c *Client, where string) ([]goclientnew.DeleteResponse, error) {
		res, err := c.API.BulkDeleteSpacesWithResponse(ctx,
			&goclientnew.BulkDeleteSpacesParams{Where: &where, FromBackingUnits: &prune})
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return pruneResults(res.JSON200, res.JSON207), nil
	},
	"Target": func(ctx context.Context, c *Client, where string) ([]goclientnew.DeleteResponse, error) {
		res, err := c.API.BulkDeleteTargetsWithResponse(ctx,
			&goclientnew.BulkDeleteTargetsParams{Where: &where, FromBackingUnits: &prune})
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return pruneResults(res.JSON200, res.JSON207), nil
	},
	"Trigger": func(ctx context.Context, c *Client, where string) ([]goclientnew.DeleteResponse, error) {
		res, err := c.API.BulkDeleteTriggersWithResponse(ctx,
			&goclientnew.BulkDeleteTriggersParams{Where: &where, FromBackingUnits: &prune})
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return pruneResults(res.JSON200, res.JSON207), nil
	},
	"View": func(ctx context.Context, c *Client, where string) ([]goclientnew.DeleteResponse, error) {
		res, err := c.API.BulkDeleteViewsWithResponse(ctx,
			&goclientnew.BulkDeleteViewsParams{Where: &where, FromBackingUnits: &prune})
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return pruneResults(res.JSON200, res.JSON207), nil
	},
}

// PruneEntities prunes the entities of one type that where selects: of those, it deletes the ones
// whose backing Units are empty, keeping the Units, and leaves the rest alone. The results are of
// the deletes alone, one for each entity deleted or that failed to be; an entity left alone is not
// in them.
func PruneEntities(ctx context.Context, c *Client, entityType string, where string) ([]goclientnew.DeleteResponse, error) {
	request, ok := pruneRequests[entityType]
	if !ok {
		return nil, fmt.Errorf("a %s cannot have a backing Unit, so there is nothing to prune", entityType)
	}
	return request(ctx, c, where)
}

// WhereEntityIDs is a where expression selecting the entities of the type with the given IDs.
func WhereEntityIDs(entityType string, ids []uuid.UUID) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = "'" + id.String() + "'"
	}
	return fmt.Sprintf("%sID IN (%s)", entityType, strings.Join(quoted, ","))
}

// PruneOrder sorts entity types into the order to prune them in, the reverse of the order apply
// writes them in, so that an entity goes before those it names: a Trigger before the Filter it
// names, and a Space last.
func PruneOrder(entityTypes []string) {
	priority := func(entityType string) int {
		p, _ := cubkit.ApplyPriorityOf(api.ResourceType(entityType))
		return p
	}
	sort.SliceStable(entityTypes, func(i, j int) bool {
		pi, pj := priority(entityTypes[i]), priority(entityTypes[j])
		if pi != pj {
			return pi > pj
		}
		return entityTypes[i] < entityTypes[j]
	})
}
