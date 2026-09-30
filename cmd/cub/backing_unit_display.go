// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/olekukonko/tablewriter"
)

// appendBackingUnitRow adds an entity's backing Unit to its details, by name where it can be read.
func appendBackingUnitRow(view *tablewriter.Table, unitID *uuid.UUID) {
	if unitID == nil || *unitID == uuid.Nil {
		return
	}
	view.Append([]string{"Backing Unit", backingUnitName(*unitID)})
}

// backingUnitName names a backing Unit as <space>/<slug>, which is how --space and the commands that
// take a unit name it, or by its ID when it cannot be read.
func backingUnitName(unitID uuid.UUID) string {
	unit, err := resolveUnit(unitID.String(), "", "Slug,SpaceID")
	if err != nil || unit == nil || unit.Unit == nil {
		return unitID.String()
	}
	if unit.Space != nil {
		return unit.Space.Slug + "/" + unit.Unit.Slug
	}
	return unit.Unit.Slug
}

// appendBackedEntityRow adds the entity a ConfigHub/YAML unit holds the configuration of to the
// unit's details, when it backs one.
func appendBackedEntityRow(view *tablewriter.Table, unit *goclientnew.Unit) {
	if unit == nil || unit.ToolchainType != "ConfigHub/YAML" {
		return
	}
	entityType, name, found, err := cubapi.BackedEntity(ctx, cubClient, unit.UnitID, unit.Labels[cubapi.BackingUnitEntityTypeLabel])
	if err != nil || !found {
		return
	}
	view.Append([]string{"Backs", entityType + " " + name})
}
