// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// OpenChangeSetOpts says how to open a ChangeSet on Units.
type OpenChangeSetOpts struct {
	// DryRun reports what would be opened and opens nothing.
	DryRun bool
	// SpaceID is the Space the caller names things relative to: a Unit or a ChangeSet in it is
	// named by its slug alone in a message, one elsewhere by space/slug. The zero UUID names
	// everything by space/slug.
	SpaceID goclientnew.UUID
}

// ChangeSetHeldError is OpenChangeSet's refusal when another ChangeSet holds some of the
// Units: a Unit is in one ChangeSet at a time, and the ChangeSet is opened on all of the Units
// or on none.
type ChangeSetHeldError struct {
	// Held is how many of the Units other ChangeSets hold.
	Held int
	// Description names the held Units by the ChangeSet holding them, as in
	// "api, config are in capacity-1 (Open); web is in hotfix (Open)".
	Description string
}

func (e *ChangeSetHeldError) Error() string {
	return fmt.Sprintf("another changeset holds %d of them: %s", e.Held, e.Description)
}

// OpenChangeSet opens a ChangeSet on every Unit sel selects, or on none, and returns how many it
// was newly opened on. The Units are those a function invocation with sel selects: its Where,
// WhereData, ResourceType and IncludeHidden, and its ToolchainType when set. sel.Where may not be
// empty, so a ChangeSet is never opened across a whole organization by accident.
//
// A Unit the ChangeSet is already open on stays in it. Every revision made to a Unit while the
// ChangeSet is open on it is recorded in it, so the change can be reviewed, approved and restored
// as one, and none of them can be released until the ChangeSet closes.
//
// When another ChangeSet holds any of the Units it opens nothing and returns a
// *ChangeSetHeldError. That is found before anything is written: the server refuses a held Unit
// one at a time, after the Units before it have been taken, so letting it find them would open
// the ChangeSet on some of the Units and not others.
func OpenChangeSet(ctx context.Context, c *Client, changeSetID goclientnew.UUID, sel Selector, opts OpenChangeSetOpts) (int, error) {
	if strings.TrimSpace(sel.Where) == "" {
		return 0, fmt.Errorf("cubapi: opening a changeset needs a selection of Units")
	}
	where := NewWhere(sel.Where)
	if sel.ToolchainType != "" {
		where = where.Eq("ToolchainType", sel.ToolchainType)
	}
	units, err := ListUnits(ctx, c, where,
		ListOpts{Select: changeSetUnitSelect, Include: "SpaceID", IncludeHidden: sel.IncludeHidden},
		func(p *goclientnew.ListAllUnitsParams) {
			if sel.WhereData != "" {
				p.WhereData = &sel.WhereData
			}
			if sel.ResourceType != "" {
				p.ResourceType = &sel.ResourceType
			}
		})
	if err != nil {
		return 0, err
	}
	return openOnListed(ctx, c, changeSetID, units, opts)
}

// OpenChangeSetOnUnits is OpenChangeSet for Units named by ID, for a command that writes Unit by
// Unit: opening the ChangeSet on all of them first is what keeps a held Unit near the end from
// leaving the change made in it on the Units before it only.
func OpenChangeSetOnUnits(ctx context.Context, c *Client, changeSetID goclientnew.UUID, unitIDs []goclientnew.UUID, opts OpenChangeSetOpts) (int, error) {
	if len(unitIDs) == 0 {
		return 0, nil
	}
	var units []*goclientnew.ExtendedUnit
	for start := 0; start < len(unitIDs); start += changeSetChunk {
		chunk := unitIDs[start:min(start+changeSetChunk, len(unitIDs))]
		listed, err := ListUnits(ctx, c, Where{}.In("UnitID", chunk),
			ListOpts{Select: changeSetUnitSelect, Include: "SpaceID", IncludeHidden: "*"})
		if err != nil {
			return 0, err
		}
		units = append(units, listed...)
	}
	return openOnListed(ctx, c, changeSetID, units, opts)
}

const (
	changeSetUnitSelect = "UnitID,Slug,SpaceID,ChangeSetID"
	// changeSetChunk is how many Units one request names, so a filter listing them stays within
	// the server's limit on the length of a query.
	changeSetChunk = 100
)

// openOnListed opens the ChangeSet on units, all or none.
func openOnListed(ctx context.Context, c *Client, changeSetID goclientnew.UUID, units []*goclientnew.ExtendedUnit, opts OpenChangeSetOpts) (int, error) {
	already, held := unitsByChangeSet(units, changeSetID, opts.SpaceID)
	if len(held) > 0 {
		return 0, &ChangeSetHeldError{
			Held:        heldUnitCount(held),
			Description: describeHeldUnits(held, changeSetNames(ctx, c, held, opts.SpaceID)),
		}
	}
	ids := make([]goclientnew.UUID, 0, len(units))
	for _, eu := range units {
		if eu.Unit != nil {
			ids = append(ids, eu.Unit.UnitID)
		}
	}

	// Every listed Unit is patched, those already in the ChangeSet included, which the server
	// takes as no change. A Unit another ChangeSet took since the listing is refused by the
	// server by name, where leaving it out would hide it.
	body, err := json.Marshal(map[string]any{"ChangeSetID": changeSetID.String()})
	if err != nil {
		return 0, err
	}
	var patched int
	var failed []string
	for start := 0; start < len(ids); start += changeSetChunk {
		selection := Where{}.In("UnitID", ids[start:min(start+changeSetChunk, len(ids))]).String()
		params := &goclientnew.BulkPatchUnitsParams{Where: &selection, ChangeSetId: &changeSetID}
		if opts.DryRun {
			dryRun := true
			params.DryRun = &dryRun
		}
		res, err := c.API.BulkPatchUnitsWithBodyWithResponse(ctx, params, "application/merge-patch+json", bytes.NewReader(body))
		if IsAPIError(err, res) {
			return max(patched-already, 0), InterpretErrorGeneric(err, res)
		}
		responses := res.JSON200
		if responses == nil {
			responses = res.JSON207
		}
		if responses == nil {
			continue
		}
		for _, r := range *responses {
			if r.Error != nil {
				failed = append(failed, errString(r.Error))
				continue
			}
			patched++
		}
	}
	if len(failed) > 0 {
		return max(patched-already, 0), fmt.Errorf("failed to open the changeset on %d unit(s): %s", len(failed), strings.Join(failed, "; "))
	}
	return max(patched-already, 0), nil
}

// OpenChangeSet makes ch's ChangeSet ready for a write to the Units sel selects, and returns its
// ID for the write's change_set_id, or nil when ch names no ChangeSet. On a commit it opens the
// ChangeSet on those Units, all of them or none. A dry run opens nothing, but it refuses as the
// commit would -- a ChangeSet that does not exist, or Units another ChangeSet holds -- so the dry
// run is where that is found.
func (ch Change) OpenChangeSet(ctx context.Context, c *Client, sel Selector) (*goclientnew.UUID, error) {
	return ch.openChangeSet(ctx, c, func(id goclientnew.UUID, opts OpenChangeSetOpts) error {
		_, err := OpenChangeSet(ctx, c, id, sel, opts)
		return err
	})
}

// OpenChangeSetOnUnits is Change.OpenChangeSet for Units named by ID: a command that writes Unit
// by Unit calls it with all of them first, so the ChangeSet is opened on all of them or none.
func (ch Change) OpenChangeSetOnUnits(ctx context.Context, c *Client, unitIDs []goclientnew.UUID) (*goclientnew.UUID, error) {
	return ch.openChangeSet(ctx, c, func(id goclientnew.UUID, opts OpenChangeSetOpts) error {
		_, err := OpenChangeSetOnUnits(ctx, c, id, unitIDs, opts)
		return err
	})
}

func (ch Change) openChangeSet(ctx context.Context, c *Client, open func(goclientnew.UUID, OpenChangeSetOpts) error) (*goclientnew.UUID, error) {
	if ch.ChangeSet == "" {
		return nil, nil
	}
	cs, err := ResolveChangeSet(ctx, c, ParseRef(ch.ChangeSet), ResolveOpts{})
	if err != nil {
		return nil, fmt.Errorf("changeset %s: %w", ch.ChangeSet, err)
	}
	id := cs.ChangeSet.ChangeSetID
	if err := open(id, OpenChangeSetOpts{DryRun: ch.DryRun()}); err != nil {
		verb := "was not"
		if ch.DryRun() {
			verb = "could not be"
		}
		return nil, fmt.Errorf("changeset %s %s opened on the Units this change writes: %w", ch.ChangeSet, verb, err)
	}
	return &id, nil
}

// unitsByChangeSet counts the Units already in changeSetID, and groups the names of those
// another ChangeSet holds by that ChangeSet. A Unit in spaceID is named by its slug, one
// elsewhere by space/slug.
func unitsByChangeSet(units []*goclientnew.ExtendedUnit, changeSetID, spaceID goclientnew.UUID) (int, map[goclientnew.UUID][]string) {
	already := 0
	held := map[goclientnew.UUID][]string{}
	for _, eu := range units {
		unit := eu.Unit
		if unit == nil {
			continue
		}
		switch {
		case unit.ChangeSetID == nil || *unit.ChangeSetID == (goclientnew.UUID{}):
		case *unit.ChangeSetID == changeSetID:
			already++
		default:
			name := unit.Slug
			if unit.SpaceID != spaceID && eu.Space != nil {
				name = eu.Space.Slug + "/" + unit.Slug
			}
			held[*unit.ChangeSetID] = append(held[*unit.ChangeSetID], name)
		}
	}
	return already, held
}

func heldUnitCount(held map[goclientnew.UUID][]string) int {
	count := 0
	for _, names := range held {
		count += len(names)
	}
	return count
}

// changeSetNames names each ChangeSet holding Units for a message, as its slug and state, with
// its Space when that is not spaceID. One that cannot be read keeps its ID, which is what the
// caller is left to look up.
func changeSetNames(ctx context.Context, c *Client, held map[goclientnew.UUID][]string, spaceID goclientnew.UUID) map[goclientnew.UUID]string {
	names := make(map[goclientnew.UUID]string, len(held))
	ids := make([]goclientnew.UUID, 0, len(held))
	for id := range held {
		names[id] = id.String()
		ids = append(ids, id)
	}
	changeSets, err := ListChangeSets(ctx, c, Where{}.In("ChangeSetID", ids), ListOpts{Include: "SpaceID"})
	if err != nil {
		return names
	}
	for _, extended := range changeSets {
		if extended.ChangeSet == nil {
			continue
		}
		names[extended.ChangeSet.ChangeSetID] = changeSetName(extended, spaceID)
	}
	return names
}

func changeSetName(extended *goclientnew.ExtendedChangeSet, spaceID goclientnew.UUID) string {
	name := extended.ChangeSet.Slug
	if extended.ChangeSet.SpaceID != spaceID && extended.Space != nil {
		name = extended.Space.Slug + "/" + name
	}
	if extended.ChangeSet.State != "" {
		name = fmt.Sprintf("%s (%s)", name, extended.ChangeSet.State)
	}
	return name
}

// describeHeldUnits lists the held Units by the ChangeSet holding them, in a stable order:
// "api, config are in capacity-1 (Open); web is in hotfix (Open)".
func describeHeldUnits(held map[goclientnew.UUID][]string, names map[goclientnew.UUID]string) string {
	parts := make([]string, 0, len(held))
	for id, units := range held {
		slices.Sort(units)
		verb := "is"
		if len(units) > 1 {
			verb = "are"
		}
		parts = append(parts, fmt.Sprintf("%s %s in %s", strings.Join(units, ", "), verb, names[id]))
	}
	slices.Sort(parts)
	return strings.Join(parts, "; ")
}
