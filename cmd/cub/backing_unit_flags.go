// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/spf13/cobra"
)

// backingUnitArgs are the flags for entities with backing Units: ConfigHub/YAML Units holding each
// entity's configuration, which the server then keeps in step with the entity. Only one command
// runs per invocation, so the commands that take them share them.
var backingUnitArgs struct {
	withBackingUnits bool
	backingUnitSpace string
	fromBackingUnits bool
	whereUnit        string
	filterUnit       string
	patchExisting    bool
}

// addBackingUnitFlags adds --with-backing-units to a command that creates or updates entities, and
// --backing-unit-space where the entity is in no Space of its own. A create of one entity passes
// them on; an update of one entity runs as the command's bulk patch of it alone.
func addBackingUnitFlags(cmd *cobra.Command, entityName string, organizationLevel, create bool) {
	cmd.Flags().BoolVar(&backingUnitArgs.withBackingUnits, "with-backing-units", false,
		fmt.Sprintf("give each %s the bulk operation writes a backing Unit if it has none: a ConfigHub/YAML Unit holding its configuration, kept in step with it", entityName))
	if organizationLevel {
		cmd.Flags().StringVar(&backingUnitArgs.backingUnitSpace, "backing-unit-space", "",
			fmt.Sprintf("space, by slug or UUID, for the backing Units --with-backing-units creates; required with it, since a %s is in no space of its own", entityName))
	}
	run := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if backingUnitArgs.backingUnitSpace != "" && !backingUnitArgs.withBackingUnits {
			return errors.New("--backing-unit-space is only used with --with-backing-units")
		}
		if !create && len(args) > 0 && backingUnitArgs.withBackingUnits {
			return runSingleUpdateInBulk(cmd, entityName, args, run)
		}
		return run(cmd, args)
	}
}

// addFromBackingUnitsFlags adds --from-backing-units to a command that updates or creates entities,
// which writes them from their backing Units, and to a create the --where-unit and --filter-unit
// that select the Units to create from. A create from Units is a bulk operation; an update of one
// entity runs as the command's bulk patch of it alone.
func addFromBackingUnitsFlags(cmd *cobra.Command, entityName string, create bool) {
	if create {
		cmd.Flags().BoolVar(&backingUnitArgs.fromBackingUnits, "from-backing-units", false,
			fmt.Sprintf("create a %s from each ConfigHub/YAML Unit --where-unit, --filter-unit and --space select that describes one, in the Unit's space, with the Unit as its backing Unit", entityName))
		cmd.Flags().StringVar(&backingUnitArgs.whereUnit, "where-unit", "", "where expression over Units selecting those to create from, with --from-backing-units")
		cmd.Flags().StringVar(&backingUnitArgs.filterUnit, "filter-unit", "", "filter, by slug or UUID, over Units selecting those to create from, with --from-backing-units")
		cmd.Flags().BoolVar(&backingUnitArgs.patchExisting, "patch-existing", false,
			fmt.Sprintf("with --from-backing-units, patch a %s a selected Unit already backs with what the Unit holds that it has not taken yet, rather than report that the Unit backs it", entityName))
	} else {
		cmd.Flags().BoolVar(&backingUnitArgs.fromBackingUnits, "from-backing-units", false,
			fmt.Sprintf("patch each %s selected with what its backing Unit holds that it has not taken yet; the patch the other flags make is applied after it", entityName))
	}
	run := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if create && len(args) > 0 && backingUnitArgs.fromBackingUnits {
			return errors.New("--from-backing-units creates from the units --where-unit and --filter-unit select, and takes no positional arguments")
		}
		if backingUnitArgs.withBackingUnits && backingUnitArgs.fromBackingUnits {
			return errors.New("--with-backing-units and --from-backing-units cannot be used together: an entity written from a Unit has that Unit as its backing Unit already")
		}
		if (backingUnitArgs.whereUnit != "" || backingUnitArgs.filterUnit != "") && !backingUnitArgs.fromBackingUnits {
			if entityName == "Trigger" {
				return errors.New("--where-unit and --filter-unit select backing units, with --from-backing-units; a trigger's own WhereUnit is --where-unit-field")
			}
			return errors.New("--where-unit and --filter-unit are only used with --from-backing-units")
		}
		if backingUnitArgs.patchExisting && !backingUnitArgs.fromBackingUnits {
			return errors.New("--patch-existing is only used with --from-backing-units")
		}
		if create && backingUnitArgs.fromBackingUnits && (where != "" || filter != "") {
			return errors.New("--from-backing-units selects the Units to create from with --where-unit and --filter-unit, not --where and --filter")
		}
		if !create && len(args) > 0 && backingUnitArgs.fromBackingUnits {
			return runSingleUpdateInBulk(cmd, entityName, args, run)
		}
		return run(cmd, args)
	}
}

// addPruneFlag adds --from-backing-units to a command that deletes entities, which prunes: of the
// entities selected, it deletes those whose backing Units are empty, and keeps the Units. A prune
// is a bulk delete; one entity named alone is pruned as the bulk delete of it alone.
func addPruneFlag(cmd *cobra.Command, entityName string) {
	entity := strings.ToLower(entityName)
	cmd.Flags().BoolVar(&backingUnitArgs.fromBackingUnits, "from-backing-units", false,
		fmt.Sprintf("prune: delete only the %ss selected whose backing Units are empty, which says they should not exist, and keep the Units; the rest are left alone", entity))
	run := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !backingUnitArgs.fromBackingUnits || len(args) == 0 {
			return run(cmd, args)
		}
		if len(args) != 1 {
			return fmt.Errorf("with --from-backing-units, name the %s by its slug or ID alone", entity)
		}
		if where != "" || filter != "" {
			return fmt.Errorf("name one %s, or select them with --where or --filter, not both", entity)
		}
		if id, err := uuid.Parse(args[0]); err == nil {
			where = fmt.Sprintf("%sID = '%s'", entityName, id)
		} else {
			where = fmt.Sprintf("Slug = '%s'", args[0])
		}
		return run(cmd, nil)
	}
}

// deleteOperationName names what a bulk delete did, for its summary: a prune with
// --from-backing-units.
func deleteOperationName() string {
	if backingUnitArgs.fromBackingUnits {
		return "prune"
	}
	return "delete"
}

// runSingleUpdateInBulk runs an update of one entity, named by slug or ID, with the backing-unit
// flags as the command's bulk patch of that entity alone: the server takes them on bulk patches,
// where each entity is written in a transaction of its own, as a single update would write it.
func runSingleUpdateInBulk(cmd *cobra.Command, entityName string, args []string, run func(*cobra.Command, []string) error) error {
	entity := strings.ToLower(entityName)
	if len(args) != 1 {
		return fmt.Errorf("with --with-backing-units or --from-backing-units, name the %s by its slug or ID alone", entity)
	}
	if where != "" || filter != "" {
		return fmt.Errorf("name one %s, or select them with --where or --filter, not both", entity)
	}
	if patch, err := cmd.Flags().GetBool("patch"); err != nil || !patch {
		return errors.New("--with-backing-units and --from-backing-units update with a patch; add --patch")
	}
	if id, err := uuid.Parse(args[0]); err == nil {
		where = fmt.Sprintf("%sID = '%s'", entityName, id)
	} else {
		where = fmt.Sprintf("Slug = '%s'", args[0])
	}
	return run(cmd, nil)
}

// withBackingUnitsParam is the request's with_backing_units: set only when the flag is.
func withBackingUnitsParam() *bool {
	if !backingUnitArgs.withBackingUnits {
		return nil
	}
	return &backingUnitArgs.withBackingUnits
}

// fromBackingUnitsParam is the request's from_backing_units: set only when the flag is.
func fromBackingUnitsParam() *bool {
	if !backingUnitArgs.fromBackingUnits {
		return nil
	}
	return &backingUnitArgs.fromBackingUnits
}

// patchExistingParam is the request's patch_existing: set only when the flag is.
func patchExistingParam() *bool {
	if !backingUnitArgs.patchExisting {
		return nil
	}
	return &backingUnitArgs.patchExisting
}

// fromBackingUnitsCreateParams are the where_unit and filter_unit of a create with
// --from-backing-units, which are nil without it. spaceID is the space the command selects, or ""
// for a type in no space; the Units are selected in it.
func fromBackingUnitsCreateParams(spaceID string) (whereUnit, filterUnit *string, err error) {
	if !backingUnitArgs.fromBackingUnits {
		return nil, nil, nil
	}
	whereExpression := backingUnitArgs.whereUnit
	if spaceID != "" {
		whereExpression = addSpaceIDToWhereClause(whereExpression, spaceID)
	}
	filterID, err := parseFilterFlag(backingUnitArgs.filterUnit)
	if err != nil {
		return nil, nil, err
	}
	if whereExpression == "" && filterID == "" {
		return nil, nil, errors.New("--from-backing-units needs --where-unit, --filter-unit or --space to select the Units to create from")
	}
	if whereExpression != "" {
		whereUnit = &whereExpression
	}
	if filterID != "" {
		filterUnit = &filterID
	}
	return whereUnit, filterUnit, nil
}

// backingUnitSpaceParam is the request's backing_unit_space: set only when the flag is.
func backingUnitSpaceParam() *string {
	if backingUnitArgs.backingUnitSpace == "" {
		return nil
	}
	return &backingUnitArgs.backingUnitSpace
}
