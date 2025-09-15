// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var applyCmd = &cobra.Command{
	Use:   "apply <filename> <last-applied-file> <inventory-file>",
	Short: "Apply configuration from a YAML file",
	Args:  cobra.ExactArgs(3),
	Long: `Apply creates or updates entities from a YAML configuration file.

The command will:
- Create entities that don't exist
- Update entities that already exist using JSON merge patch
- Use SpaceSlug from the entity data or --space flag
- Track inventory to enable pruning of removed entities
- Process all entities in the file even if some fail

Examples:
  # Apply configuration from a file with last-applied and inventory
  cub apply config.yaml last-applied.yaml inventory.yaml

  # Apply with a specific space
  cub apply --space my-space config.yaml last-applied.yaml inventory.yaml
`,
	RunE:        applyCmdRun,
	PreRunE:     spacePreRunE,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	addSpaceFlags(applyCmd)
	addStandardDisplayFlags(applyCmd)
	rootCmd.AddCommand(applyCmd)
}

func applyCmdRun(cmd *cobra.Command, args []string) error {
	// Read the configuration file
	flagFilename = args[0]
	data, err := getBytesFromFlags()
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Validate and read last-applied file
	lastAppliedFile := args[1]
	if err := validateLastAppliedFile(lastAppliedFile); err != nil {
		return err
	}
	lastAppliedData, err := loadLastApplied(lastAppliedFile)
	if err != nil {
		return fmt.Errorf("failed to load last-applied file: %w", err)
	}

	// Validate inventory file path
	inventoryFile := args[2]
	if err := validateInventoryFile(inventoryFile); err != nil {
		return err
	}

	// Load existing inventory
	oldInventory, err := loadInventory(inventoryFile)
	if err != nil {
		return fmt.Errorf("failed to load inventory: %w", err)
	}

	// Apply the configuration
	results, newInventory, err := cubapi.Apply(ctx, cubClientNew, data, lastAppliedData, oldInventory, selectedSpaceSlug)
	if err != nil {
		return err
	}

	// Save updated inventory
	if err := saveInventory(inventoryFile, newInventory); err != nil {
		return fmt.Errorf("failed to save inventory: %w", err)
	}

	// Save the applied configuration as last-applied
	if err := saveLastApplied(lastAppliedFile, data); err != nil {
		return fmt.Errorf("failed to save last-applied file: %w", err)
	}

	// Report results
	reportApplyResults(results)

	// Return error if any entities failed
	for _, result := range results {
		if result.Error != nil {
			return fmt.Errorf("some entities failed to apply")
		}
	}

	return nil
}

func reportApplyResults(results []cubapi.ApplyResult) {
	var created, updated, unchanged, deleted, failed int
	var failures []string

	for _, result := range results {
		if result.Error != nil {
			failed++
			entityDesc := result.EntityType
			if result.EntityName != "" {
				entityDesc += " " + result.EntityName
			}
			if result.SpaceSlug != "" {
				entityDesc += " in space " + result.SpaceSlug
			}
			failures = append(failures, fmt.Sprintf("  - %s: %s", entityDesc, result.Error))
		} else if result.Action == "created" {
			created++
			displayEntityResult(result, "created")
		} else if result.Action == "updated" {
			updated++
			displayEntityResult(result, "updated")
		} else if result.Action == "unchanged" {
			unchanged++
			// Optionally display unchanged entities if verbose
		} else if result.Action == "deleted" {
			deleted++
			displayDeletedEntityResult(result)
		}
	}

	// Report summary (only if not using alternative output format)
	hasAlternativeOutput := hasAlternativeOutput()
	if !quiet && !hasAlternativeOutput {
		if created > 0 || updated > 0 || unchanged > 0 || deleted > 0 {
			tprint("Successfully applied configuration:")
			if created > 0 {
				tprint("  Created: %d entities", created)
			}
			if updated > 0 {
				tprint("  Updated: %d entities", updated)
			}
			if unchanged > 0 {
				tprint("  Unchanged: %d entities", unchanged)
			}
			if deleted > 0 {
				tprint("  Deleted: %d entities", deleted)
			}
		}

		if failed > 0 {
			tprintErr("Failed to apply %d entities:", failed)
			for _, failure := range failures {
				tprintErr(failure)
			}
		}
	}
}

func validateInventoryFile(inventoryFile string) error {
	if inventoryFile == "-" {
		return fmt.Errorf("inventory file cannot be stdin")
	}
	if strings.HasPrefix(inventoryFile, "https://") {
		return fmt.Errorf("inventory file cannot be a remote file")
	}
	return nil
}

func loadInventory(inventoryFile string) (*cubapi.Inventory, error) {
	// Check if file exists
	if _, err := os.Stat(inventoryFile); os.IsNotExist(err) {
		// Return empty inventory if file doesn't exist
		return &cubapi.Inventory{
			EntityType: "Inventory",
			Resources:  []cubapi.InventoryResource{},
		}, nil
	}

	data, err := os.ReadFile(inventoryFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read inventory file: %w", err)
	}

	var inventory cubapi.Inventory
	err = yaml.Unmarshal(data, &inventory)
	if err != nil {
		return nil, fmt.Errorf("failed to parse inventory file: %w", err)
	}

	return &inventory, nil
}

func saveInventory(inventoryFile string, inventory *cubapi.Inventory) error {
	data, err := yaml.Marshal(inventory)
	if err != nil {
		return fmt.Errorf("failed to marshal inventory: %w", err)
	}

	err = os.WriteFile(inventoryFile, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write inventory file: %w", err)
	}

	return nil
}

func displayEntityResult(result cubapi.ApplyResult, action string) {
	entityName := strings.ToLower(result.EntityType)

	switch result.EntityType {
	case EntityTypeSpace:
		if space, ok := result.Entity.(*goclientnew.Space); ok {
			if action == "created" {
				displayCreateResults(space, entityName, result.EntityName, result.EntityID, displaySpaceDetails)
			} else {
				displayUpdateResults(space, entityName, result.EntityName, result.EntityID, displaySpaceDetails)
			}
		}
	case EntityTypeUnit:
		if unit, ok := result.Entity.(*goclientnew.Unit); ok {
			if action == "created" {
				displayCreateResults(unit, entityName, result.EntityName, result.EntityID, displayUnitDetails)
			} else {
				displayUpdateResults(unit, entityName, result.EntityName, result.EntityID, displayUnitDetails)
			}
		}
	case EntityTypeLink:
		if link, ok := result.Entity.(*goclientnew.Link); ok {
			if action == "created" {
				displayCreateResults(link, entityName, result.EntityName, result.EntityID, displayLinkDetails)
			} else {
				displayUpdateResults(link, entityName, result.EntityName, result.EntityID, displayLinkDetails)
			}
		}
	case EntityTypeView:
		if view, ok := result.Entity.(*goclientnew.View); ok {
			if action == "created" {
				displayCreateResults(view, entityName, result.EntityName, result.EntityID, displayViewDetails)
			} else {
				displayUpdateResults(view, entityName, result.EntityName, result.EntityID, displayViewDetails)
			}
		}
	case EntityTypeFilter:
		if filter, ok := result.Entity.(*goclientnew.Filter); ok {
			if action == "created" {
				displayCreateResults(filter, entityName, result.EntityName, result.EntityID, displayFilterDetails)
			} else {
				displayUpdateResults(filter, entityName, result.EntityName, result.EntityID, displayFilterDetails)
			}
		}
	case EntityTypeTrigger:
		if trigger, ok := result.Entity.(*goclientnew.Trigger); ok {
			if action == "created" {
				displayCreateResults(trigger, entityName, result.EntityName, result.EntityID, displayTriggerDetails)
			} else {
				displayUpdateResults(trigger, entityName, result.EntityName, result.EntityID, displayTriggerDetails)
			}
		}
	case EntityTypeInvocation:
		if invocation, ok := result.Entity.(*goclientnew.Invocation); ok {
			if action == "created" {
				displayCreateResults(invocation, entityName, result.EntityName, result.EntityID, func(i *goclientnew.Invocation) {
					extendedInvocation := &goclientnew.ExtendedInvocation{Invocation: i}
					displayExtendedInvocationDetails(extendedInvocation)
				})
			} else {
				displayUpdateResults(invocation, entityName, result.EntityName, result.EntityID, func(i *goclientnew.Invocation) {
					extendedInvocation := &goclientnew.ExtendedInvocation{Invocation: i}
					displayExtendedInvocationDetails(extendedInvocation)
				})
			}
		}
	case EntityTypeChangeSet:
		if changeSet, ok := result.Entity.(*goclientnew.ChangeSet); ok {
			if action == "created" {
				displayCreateResults(changeSet, entityName, result.EntityName, result.EntityID, func(c *goclientnew.ChangeSet) {
					extendedChangeSet := &goclientnew.ExtendedChangeSet{ChangeSet: c}
					displayExtendedChangeSetDetails(extendedChangeSet)
				})
			} else {
				displayUpdateResults(changeSet, entityName, result.EntityName, result.EntityID, func(c *goclientnew.ChangeSet) {
					extendedChangeSet := &goclientnew.ExtendedChangeSet{ChangeSet: c}
					displayExtendedChangeSetDetails(extendedChangeSet)
				})
			}
		}
	case EntityTypeTag:
		if tag, ok := result.Entity.(*goclientnew.Tag); ok {
			if action == "created" {
				displayCreateResults(tag, entityName, result.EntityName, result.EntityID, func(t *goclientnew.Tag) {
					extendedTag := &goclientnew.ExtendedTag{Tag: t}
					displayExtendedTagDetails(extendedTag)
				})
			} else {
				displayUpdateResults(tag, entityName, result.EntityName, result.EntityID, func(t *goclientnew.Tag) {
					extendedTag := &goclientnew.ExtendedTag{Tag: t}
					displayExtendedTagDetails(extendedTag)
				})
			}
		}
	}
}

func displayDeletedEntityResult(result cubapi.ApplyResult) {
	entityName := strings.ToLower(result.EntityType)
	entityDisplayName := result.EntityName
	if result.SpaceSlug != "" && result.EntityType != EntityTypeSpace {
		entityDisplayName = result.SpaceSlug + "/" + result.EntityName
	}

	// Call displayDeleteResults with the stored response from result.Entity
	if result.Entity != nil {
		displayDeleteResults(entityName, result.EntityName, result.EntityID, result.Entity)
	} else {
		// Fallback for entities that were already deleted (not found)
		if !quiet {
			tprint("Deleted %s: %s", entityName, entityDisplayName)
		}
	}
}

// validateLastAppliedFile validates that the last-applied file path is valid
func validateLastAppliedFile(lastAppliedFile string) error {
	if lastAppliedFile == "-" {
		return fmt.Errorf("last-applied file cannot be stdin")
	}
	if strings.HasPrefix(lastAppliedFile, "https://") {
		return fmt.Errorf("last-applied file cannot be a remote file")
	}
	return nil
}

// loadLastApplied loads the last-applied configuration file
func loadLastApplied(lastAppliedFile string) ([]byte, error) {
	// Check if file exists
	if _, err := os.Stat(lastAppliedFile); os.IsNotExist(err) {
		// Return empty byte slice if file doesn't exist
		return []byte{}, nil
	}

	return os.ReadFile(lastAppliedFile)
}

// saveLastApplied saves the applied configuration to the last-applied file
func saveLastApplied(lastAppliedFile string, data []byte) error {
	return os.WriteFile(lastAppliedFile, data, 0644)
}
