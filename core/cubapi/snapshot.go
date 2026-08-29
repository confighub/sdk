// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Snapshots (snapshot.go) read a fleet-wide view of ConfigHub configuration: the
// units a filter selects, the resources inside them, and the unit / space /
// target metadata that says where each one lives.
//
// It is the part of a fleet-wide tool that is not about any particular kind of
// configuration. A caller supplies the resource types it cares about and a
// function that turns one resource into its own model; everything between --
// scoping, the two queries, the join, the cluster key -- is here.
package cubapi

import (
	"context"
	"fmt"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// ClusterNone is the cluster key for units a snapshot cannot attribute to a
// cluster: their space has no release target, so there is nothing to name. They
// group under one bucket rather than each space standing in for a cluster of its
// own, which would count things that are not clusters.
const ClusterNone = "None"

// snapshotUnitInclude expands the two related entities a snapshot cannot read off
// the unit row. Space is included for its Labels, which mark a canonical
// base/policy space -- not for its slug, which the unit carries as SpaceSlug.
// Target is included for its slug, the cluster key, which the unit has no field
// for.
//
// Fetching spaces and targets separately instead was measured and is slower at
// this shape: two more round trips cost more than the joins do over hundreds of
// unit rows.
const snapshotUnitInclude = "SpaceID,TargetID"

// snapshotUnitSelect are the unit fields [UnitMeta] carries. Naming them keeps a
// fleet-wide list from serializing every column of every unit, which is most of
// what a snapshot costs.
const snapshotUnitSelect = "UnitID,SpaceID,SpaceSlug,Slug,TargetID,Labels,ApplyGates,ApplyWarnings," +
	"HeadRevisionNum,LastReleasedRevisionNum,UpstreamRevisionNum,LastChangeDescription"

// resourceOrderBy makes the fetch reproducible. An unordered query comes back in
// "the database's default order", which the API documents as no promise at all,
// and a caller's own sorts tie-break on the order they were handed. ResourceID
// is the primary key, so ordering by it alone is a total order.
var resourceOrderBy = "ResourceID"

// Origin is where one resource came from: the ConfigHub entities that hold it
// and the cluster a snapshot attributes it to.
type Origin struct {
	// Cluster is the key resources are grouped by, [ClusterNone] when the unit's
	// space has no release target.
	Cluster string
	// Target is the unit's target slug, empty when it has none.
	Target       string
	Space        string
	SpaceID      string
	SpaceLabels  map[string]string
	UnitID       string
	UnitSlug     string
	ResourceName string
	ResourceType string
	// Canonical marks a definition in a base or policy space: shown in
	// inventories, kept out of cluster analysis.
	Canonical bool
}

// UnitMeta is the per-unit metadata a snapshot joins onto resources. It also
// stands on its own: a unit holding none of the resource types a caller asked
// for still appears here, which is what lets a fleet inventory count units the
// resource query cannot see.
type UnitMeta struct {
	UnitID                  string            `json:"unitId"`
	Slug                    string            `json:"slug"`
	SpaceID                 string            `json:"spaceId"`
	SpaceSlug               string            `json:"spaceSlug"`
	SpaceLabels             map[string]string `json:"spaceLabels,omitempty"`
	TargetID                string            `json:"targetId,omitempty"`
	TargetSlug              string            `json:"targetSlug,omitempty"`
	Labels                  map[string]string `json:"labels,omitempty"`
	GateCount               int               `json:"gateCount"`
	WarningCount            int               `json:"warningCount,omitempty"`
	HeadRevisionNum         int64             `json:"headRevisionNum"`
	LastReleasedRevisionNum int64             `json:"lastReleasedRevisionNum,omitempty"`
	UpstreamRevisionNum     int64             `json:"upstreamRevisionNum,omitempty"`
	LastChangeDescription   string            `json:"lastChangeDescription,omitempty"`
}

// Gated reports whether the unit has any ApplyGates attached.
func (u UnitMeta) Gated() bool { return u.GateCount > 0 }

// Unreleased reports whether the unit's head revision has not been captured by a
// release. Publishing a release advances LastReleasedRevisionNum, so it is the
// field that answers "is what is authored here the thing that was published".
func (u UnitMeta) Unreleased() bool {
	return u.LastReleasedRevisionNum == 0 || u.LastReleasedRevisionNum < u.HeadRevisionNum
}

// Snapshot is one fleet-wide read: every resource a caller asked for, and the
// metadata of every unit in scope.
type Snapshot[R any] struct {
	// Resources is every resource that was read, canonical ones included.
	Resources []R
	// Units is in-scope unit metadata by UnitID.
	Units map[string]UnitMeta
	// Filter is the unit `where` predicate the snapshot was scoped by; empty
	// means the whole fleet the caller can view.
	Filter string `json:"filter,omitempty"`
}

// IsCanonicalSpace reports whether a space holds definitions rather than
// deployed configuration. The standard Variant=base label marks a base or
// template space; a `role` label of base or policy is treated the same way.
func IsCanonicalSpace(labels map[string]string) bool {
	switch labels["Variant"] {
	case "base":
		return true
	}
	switch labels["role"] {
	case "base", "policy":
		return true
	}
	return false
}

// DefaultClusterKey names the cluster a unit's resources belong to: its target
// slug, or [ClusterNone] when its space has no release target.
func DefaultClusterKey(meta UnitMeta) string {
	if meta.TargetSlug != "" {
		return meta.TargetSlug
	}
	return ClusterNone
}

// SnapshotLoader reads a fleet snapshot. The zero value is not usable: New is
// required, and so is one of ResourceTypes or ResourceWhere.
type SnapshotLoader[R any] struct {
	// ResourceTypes are the exact ResourceTypes to read, e.g.
	// "apps/v1/Deployment". They become one IN clause, which is how a union of
	// values is written in a filter language that has no OR. Naming versions
	// exactly means a new API version has to be added here.
	ResourceTypes []string

	// ResourceWhere replaces ResourceTypes with a raw clause, for a set that
	// cannot be enumerated -- an API group whose kinds are open-ended, say.
	// Prefer ResourceTypes: an exact list needs no pattern and no re-matching.
	ResourceWhere string

	// Toolchain scopes both queries to one toolchain type, e.g.
	// "Kubernetes/YAML". Defaults to [DefaultToolchainType].
	//
	// It is a field rather than a clause in UnitWhere because ToolchainType is a
	// column on the resource as well as on the unit, so naming it once puts the
	// term in both queries, where each is narrowed by it in SQL.
	Toolchain string

	// UnitWhere further scopes which units are in scope at all, ANDed with the
	// caller's predicate. Empty means the toolchain alone decides.
	UnitWhere string

	// ClusterKey names the cluster a unit's resources belong to. Defaults to
	// [DefaultClusterKey]. Override it where the cluster a caller reports is not
	// the target the configuration is delivered to.
	ClusterKey func(UnitMeta) string

	// Canonical reports whether a space holds definitions rather than deployed
	// configuration. Defaults to [IsCanonicalSpace].
	Canonical func(spaceLabels map[string]string) bool

	// Keep, when set, decides whether a resource that came back is kept. Use it
	// where ResourceWhere is broader than the set actually wanted: a filter
	// literal cannot carry a backslash, so a pattern's dots match any character
	// and the clause can only be written broader than intended, never narrower.
	Keep func(Origin) bool

	// New builds the caller's own resource from its origin and its
	// configuration, which arrives as already-parsed JSON. It is called once per
	// resource read, in the order the query returned them.
	New func(Origin, map[string]any) R
}

func (l SnapshotLoader[R]) toolchain() string {
	if l.Toolchain != "" {
		return l.Toolchain
	}
	return DefaultToolchainType
}

func (l SnapshotLoader[R]) clusterKey(meta UnitMeta) string {
	if l.ClusterKey != nil {
		return l.ClusterKey(meta)
	}
	return DefaultClusterKey(meta)
}

func (l SnapshotLoader[R]) canonical(labels map[string]string) bool {
	if l.Canonical != nil {
		return l.Canonical(labels)
	}
	return IsCanonicalSpace(labels)
}

// resourceWhere is the clause selecting the resource types to read. A type name
// that cannot appear in a filter literal is reported through the returned
// filter's [Where.Err].
func (l SnapshotLoader[R]) resourceWhere() (Where, error) {
	if l.ResourceWhere != "" {
		return NewWhere(l.ResourceWhere), nil
	}
	if len(l.ResourceTypes) == 0 {
		return Where{}, fmt.Errorf("cubapi: snapshot loader needs ResourceTypes or ResourceWhere")
	}
	w := Where{}.InStrings("ResourceType", l.ResourceTypes)
	return w, w.Err()
}

// Load reads the snapshot, scoped by a single ConfigHub unit `where` predicate;
// empty means everything the caller can view. The predicate may reference unit,
// space and target metadata, and the server applies it.
//
// Where it applies it is worth knowing when a scope can be written more than one
// way. A term naming the unit's own columns -- Slug, Labels, ToolchainType,
// SpaceID -- is compiled into the SQL query. A term prefixed with another entity
// -- Space.Labels.Environment, Target.ProviderType -- cannot be, so the server
// expands that entity for every row the SQL query returned and evaluates the
// term afterwards. Both answer the same question; only the first narrows the
// read. Prefer a unit-level term where the caller has the choice.
func (l SnapshotLoader[R]) Load(ctx context.Context, c *Client, where string) (*Snapshot[R], error) {
	if l.New == nil {
		return nil, fmt.Errorf("cubapi: snapshot loader needs New")
	}
	resourceWhere, err := l.resourceWhere()
	if err != nil {
		return nil, err
	}

	units, err := ListUnits(ctx, c, Where{}.Eq("ToolchainType", l.toolchain()).And(l.UnitWhere).And(where),
		ListOpts{Include: snapshotUnitInclude, Select: snapshotUnitSelect})
	if err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}

	inScope := make(map[string]UnitMeta, len(units))
	unitIDs := make([]goclientnew.UUID, 0, len(units))
	for _, eu := range units {
		if eu.Unit == nil || eu.Unit.UnitID == (goclientnew.UUID{}) {
			continue
		}
		meta := newUnitMeta(eu)
		inScope[meta.UnitID] = meta
		unitIDs = append(unitIDs, eu.Unit.UnitID)
	}

	extended, err := listSnapshotResources(ctx, c,
		resourceWhere.Eq("ToolchainType", l.toolchain()), unitIDs)
	if err != nil {
		return nil, fmt.Errorf("list resources: %w", err)
	}

	resources := make([]R, 0, len(extended))
	for _, er := range extended {
		if er.Resource == nil || er.Resource.Data == nil {
			continue
		}
		r := er.Resource
		meta, ok := inScope[r.UnitID.String()]
		if !ok {
			continue // out of scope
		}
		space := r.SpaceSlug
		if space == "" {
			space = meta.SpaceSlug
		}
		origin := Origin{
			Cluster:      l.clusterKey(meta),
			Target:       meta.TargetSlug,
			Space:        space,
			SpaceID:      r.SpaceID.String(),
			SpaceLabels:  meta.SpaceLabels,
			UnitID:       r.UnitID.String(),
			UnitSlug:     firstNonEmpty(r.UnitSlug, meta.Slug),
			ResourceName: r.ResourceName,
			ResourceType: r.ResourceType,
			Canonical:    l.canonical(meta.SpaceLabels),
		}
		if l.Keep != nil && !l.Keep(origin) {
			continue
		}
		resources = append(resources, l.New(origin, r.Data))
	}

	return &Snapshot[R]{Resources: resources, Units: inScope, Filter: where}, nil
}

// listSnapshotResources reads the resources inside the in-scope units from the
// Resource entity, which mirrors the configuration in each unit's data and is
// queried in SQL.
//
// The units are named by ID rather than by re-sending the caller's predicate:
// that predicate selects units and is written against unit attributes, which the
// resource query would need re-spelled with a `Unit.` prefix, and the IDs are
// already in hand from the unit list a snapshot needs anyway.
//
// Correctness comes from the join onto the in-scope unit metadata, not from this
// query: a resource whose unit is out of scope is dropped when the two are
// joined. The clauses here are what keep the server from sending rows that would
// only be discarded. ResourceType, ToolchainType and UnitID are all columns on
// the resource, so all three narrow the read in SQL.
//
// The UnitID list is the one clause that cannot always be sent: it grows with
// the fleet, and going over [MaxFilterLength] is a 400, not a truncated answer.
// So it goes in only when the rendered filter fits without it, and a fleet-wide
// run simply reads more rows and discards them.
func listSnapshotResources(ctx context.Context, c *Client, where Where, unitIDs []goclientnew.UUID) ([]*goclientnew.ExtendedResource, error) {
	if len(unitIDs) == 0 {
		return nil, nil
	}
	if scoped := where.In("UnitID", unitIDs); len(scoped.String()) <= MaxFilterLength {
		where = scoped
	}

	// No Include: the space and unit slugs are columns on the row, and the target
	// slug comes from the unit metadata already loaded. No RawData either: Data
	// is the resource's configuration as parsed JSON, which is what a caller
	// walks.
	return ListResources(ctx, c, where, ListOpts{},
		func(p *goclientnew.ListAllResourcesParams) { p.OrderBy = &resourceOrderBy })
}

func newUnitMeta(eu *goclientnew.ExtendedUnit) UnitMeta {
	meta := UnitMeta{
		UnitID:                  eu.Unit.UnitID.String(),
		Slug:                    eu.Unit.Slug,
		SpaceID:                 eu.Unit.SpaceID.String(),
		SpaceSlug:               eu.Unit.SpaceSlug,
		Labels:                  eu.Unit.Labels,
		GateCount:               len(eu.Unit.ApplyGates),
		WarningCount:            len(eu.Unit.ApplyWarnings),
		HeadRevisionNum:         eu.Unit.HeadRevisionNum,
		LastReleasedRevisionNum: eu.Unit.LastReleasedRevisionNum,
		UpstreamRevisionNum:     eu.Unit.UpstreamRevisionNum,
		LastChangeDescription:   eu.Unit.LastChangeDescription,
	}
	if eu.Unit.TargetID != nil {
		meta.TargetID = eu.Unit.TargetID.String()
	}
	if eu.Space != nil {
		meta.SpaceLabels = eu.Space.Labels
	}
	if eu.Target != nil {
		meta.TargetSlug = eu.Target.Slug
	}
	return meta
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
