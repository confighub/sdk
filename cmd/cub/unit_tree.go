// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var unitTreeArgs struct {
	edgeType string
	nodeType string
}

var unitTreeCmd = &cobra.Command{
	Use:         "tree",
	Short:       "Display units in a tree view",
	Long:        getUnitTreeHelp(),
	Args:        cobra.ExactArgs(0),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        unitTreeCmdRun,
}

func init() {
	unitTreeCmd.Flags().StringVar(&unitTreeArgs.edgeType, "edge", "clone", "Edge type for tree relationships (clone, link)")
	unitTreeCmd.Flags().StringVar(&unitTreeArgs.nodeType, "node", "unit", "Node type for tree display (unit, space)")

	// Add standard list flags
	addSpaceFlags(unitTreeCmd)
	addStandardListFlags(unitTreeCmd)
	unitTreeCmd.Flags().StringVar(&resourceType, "resource-type", "", "resource-type filter")
	unitTreeCmd.Flags().StringVar(&whereData, "where-data", "", "where data filter")
	unitCmd.AddCommand(unitTreeCmd)
}

func getUnitTreeHelp() string {
	baseHelp := `Display units in a tree view showing relationships between units.

The tree view shows the hierarchical relationships between units based on the specified edge type.
Supports two types of relationships:

- 'clone': Units connected via UpstreamUnit (configuration inheritance)
- 'link': Units connected via Link relationships (dependency/producer-consumer)

The tree can display either unit names or space names as nodes:

- 'unit' (default): Shows unit names as tree nodes, with space names in a second column
- 'space': Shows space names as tree nodes, with unit names in a second column

By default, shows a simple tree structure. When --columns is specified, displays a hybrid
tree + tabular view with the tree on the left, complementary info in the second column,
and specified columns on the right.

Default columns for hybrid view: Status, UpgradeNeeded, UnreleasedChanges, ApplyGates

Examples:
` + "```" + `
  # Show simple clone tree for all units in current space (clone is default)
  cub unit tree

  # Show link-based tree showing dependency relationships
  cub unit tree --edge link

  # Show tree with space names as nodes
  cub unit tree --node space

  # Show hybrid tree + tabular view with default columns
  cub unit tree --columns

  # Show hybrid view with custom columns for link relationships
  cub unit tree --edge link --columns UpgradeNeeded,UnreleasedChanges,Unit.Labels.Environment

  # Show clone tree for specific units
  cub unit tree --where "Labels.environment = 'production'"

  # Show link tree for specific units
  cub unit tree --edge link --where "Labels.tier = 'web'"

  # Show tree across all spaces (requires --space "*")
  cub unit tree --space "*"

  # Show link tree across all spaces with custom columns
  cub unit tree --edge link --space "*" --columns Space.Slug,UnreleasedChanges

  # Show space-based tree across all spaces
  cub unit tree --node space --space "*" --columns UnreleasedChanges

  # Explicitly specify clone edge type
  cub unit tree --edge clone
 ` + "```" + `
 `

	agentContext := `Tree view is essential for understanding unit relationships and dependencies in ConfigHub.

Key use cases for agents:
- Discover unit hierarchies and clone relationships
- Understand configuration inheritance patterns
- Identify root units and their downstream dependencies
- Analyze dependency relationships via links
- Cross-space analysis with --space "*" wildcard

Tree structure insights:
- Root units have no upstream dependencies
- Clone relationships show configuration inheritance
- Link relationships show dependency/producer-consumer patterns
- Tree depth indicates dependency chain length
- Use --columns to add status and metadata context

Edge type differences:
- Clone trees: Show configuration inheritance (UpstreamUnit relationships)
- Link trees: Show dependency relationships (Link relationships)
- Both support cross-space operations when using --space "*"

Cross-space operations:
- Use --space "*" to analyze units across all accessible spaces
- Combine with --where filters for targeted analysis
- Add Space.Slug column to identify space context
- Link trees can show cross-space dependencies
- Use --node space to group by space names for cross-space analysis
- Use --node unit (default) to show unit relationships with space context`

	return getCommandHelp(baseHelp, agentContext)
}

// formatNodeName formats the node name based on the node type
func formatNodeName(unit *goclientnew.ExtendedUnit) string {
	if unitTreeArgs.nodeType == "space" {
		// Show space name as the node
		if unit.Space != nil {
			return unit.Space.Slug
		}
		return "unknown-space"
	} else {
		// Show unit name as the node (default)
		unitSlug := unit.Unit.Slug
		displayName := unitSlug
		if unit.Unit.DisplayName != "" && unit.Unit.DisplayName != unitSlug {
			displayName = fmt.Sprintf("%s (%s)", unitSlug, unit.Unit.DisplayName)
		}
		return displayName
	}
}

// formatComplementaryInfo formats the complementary information for the second column
func formatComplementaryInfo(unit *goclientnew.ExtendedUnit) string {
	if unitTreeArgs.nodeType == "space" {
		// When node is space, show unit name in second column
		unitSlug := unit.Unit.Slug
		displayName := unitSlug
		if unit.Unit.DisplayName != "" && unit.Unit.DisplayName != unitSlug {
			displayName = fmt.Sprintf("%s (%s)", unitSlug, unit.Unit.DisplayName)
		}
		return displayName
	} else {
		// When node is unit, show space name in second column
		if unit.Space != nil {
			return unit.Space.Slug
		}
		return "unknown-space"
	}
}

func unitTreeCmdRun(cmd *cobra.Command, args []string) error {
	// Validate edge type
	if unitTreeArgs.edgeType != "clone" && unitTreeArgs.edgeType != "link" {
		return fmt.Errorf("invalid edge type '%s'. Supported values: clone, link", unitTreeArgs.edgeType)
	}

	// Validate node type
	if unitTreeArgs.nodeType != "unit" && unitTreeArgs.nodeType != "space" {
		return fmt.Errorf("invalid node type '%s'. Supported values: unit, space", unitTreeArgs.nodeType)
	}

	// Handle both clone and link edge types

	// Get units using the same logic as unit list
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	var extendedUnits []*goclientnew.ExtendedUnit
	if selectedSpaceID == "*" {
		extendedUnits, err = apiListAllUnits(cubapi.NewWhere(where), resourceType, whereData, "", "", false, selectFields, filterID, "")
		if err != nil {
			return err
		}
	} else {
		extendedUnits, err = apiListExtendedUnits(selectedSpaceID, where, resourceType, whereData, "", "", false, selectFields, filterID, "")
		if err != nil {
			return err
		}
	}

	// Build and display the tree based on edge type
	if unitTreeArgs.edgeType == "clone" {
		return displayUnitTree(extendedUnits, "clone")
	} else {
		return displayUnitTree(extendedUnits, "link")
	}
}

// UnitTreeNode represents a node in the unit tree
type UnitTreeNode struct {
	Unit     *goclientnew.ExtendedUnit
	Children []*UnitTreeNode
	Parent   *UnitTreeNode
}

// displayUnitTree builds and displays the unit tree
func displayUnitTree(units []*goclientnew.ExtendedUnit, edgeType string) error {
	// Build tree structure based on edge type
	var tree []*UnitTreeNode
	var err error

	if edgeType == "clone" {
		tree, err = buildUnitTree(units)
	} else if edgeType == "link" {
		tree, err = buildLinkTree(units)
	} else {
		return fmt.Errorf("unsupported edge type: %s", edgeType)
	}

	if err != nil {
		return err
	}

	// Display the tree
	displayTree(tree)
	return nil
}

// buildUnitTree constructs the tree structure from units
func buildUnitTree(units []*goclientnew.ExtendedUnit) ([]*UnitTreeNode, error) {
	// Create a map of unit ID to node for quick lookup
	unitMap := make(map[string]*UnitTreeNode)
	var rootNodes []*UnitTreeNode

	// First pass: create all nodes
	for _, unit := range units {
		if unit.Unit == nil {
			continue
		}

		node := &UnitTreeNode{
			Unit:     unit,
			Children: make([]*UnitTreeNode, 0),
		}
		unitMap[unit.Unit.UnitID.String()] = node
	}

	// Second pass: establish parent-child relationships
	for _, unit := range units {
		if unit.Unit == nil {
			continue
		}

		node := unitMap[unit.Unit.UnitID.String()]

		// Check if this unit has an upstream unit (parent)
		if unit.Unit.UpstreamUnitID != nil {
			parentID := unit.Unit.UpstreamUnitID.String()
			if parentNode, exists := unitMap[parentID]; exists {
				// This unit has a parent in our dataset
				node.Parent = parentNode
				parentNode.Children = append(parentNode.Children, node)
			} else {
				// This unit has a parent but it's not in our dataset, so it's a root
				rootNodes = append(rootNodes, node)
			}
		} else {
			// This unit has no upstream unit, so it's a root
			rootNodes = append(rootNodes, node)
		}
	}

	return rootNodes, nil
}

// buildLinkTree constructs the tree structure from units based on link relationships
func buildLinkTree(units []*goclientnew.ExtendedUnit) ([]*UnitTreeNode, error) {
	// First, we need to get all the links for the units we're displaying
	// We'll need to get links where either FromUnitID or ToUnitID matches our units
	unitIDs := make([]string, 0, len(units))
	unitMap := make(map[string]*UnitTreeNode)

	// Create a map of unit ID to node for quick lookup
	for _, unit := range units {
		if unit.Unit == nil {
			continue
		}
		unitID := unit.Unit.UnitID.String()
		unitIDs = append(unitIDs, unitID)

		node := &UnitTreeNode{
			Unit:     unit,
			Children: make([]*UnitTreeNode, 0),
		}
		unitMap[unitID] = node
	}

	// Get all links that involve our units
	// Since ConfigHub where clauses don't support OR, we need to make separate calls
	// for FromUnitID and ToUnitID, then merge the results
	var allLinks []*goclientnew.ExtendedLink
	linkMap := make(map[string]*goclientnew.ExtendedLink) // Use map to deduplicate

	// Build where clauses for FromUnitID and ToUnitID separately
	fromWhereClause := "FromUnitID IN ('" + strings.Join(unitIDs, "','") + "')"
	toWhereClause := "ToUnitID IN ('" + strings.Join(unitIDs, "','") + "')"

	// Fetch links where our units are the FromUnit
	var fromLinks []*goclientnew.ExtendedLink
	var err error

	if selectedSpaceID == "*" {
		fromLinks, err = apiListAllLinks(cubapi.NewWhere(fromWhereClause), "", "")
	} else {
		fromLinks, err = apiListLinks(selectedSpaceID, fromWhereClause, "", "")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to fetch links where units are FromUnit: %w", err)
	}

	// Add fromLinks to our map
	for _, link := range fromLinks {
		if link.Link != nil {
			linkMap[link.Link.LinkID.String()] = link
		}
	}

	// Fetch links where our units are the ToUnit
	var toLinks []*goclientnew.ExtendedLink

	if selectedSpaceID == "*" {
		toLinks, err = apiListAllLinks(cubapi.NewWhere(toWhereClause), "", "")
	} else {
		toLinks, err = apiListLinks(selectedSpaceID, toWhereClause, "", "")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to fetch links where units are ToUnit: %w", err)
	}

	// Add toLinks to our map (deduplication happens automatically)
	for _, link := range toLinks {
		if link.Link != nil {
			linkMap[link.Link.LinkID.String()] = link
		}
	}

	// Convert map back to slice
	for _, link := range linkMap {
		allLinks = append(allLinks, link)
	}

	// Build parent-child relationships based on links
	// In link relationships, FromUnit is the parent and ToUnit is the child
	var rootNodes []*UnitTreeNode

	for _, link := range allLinks {
		if link.Link == nil {
			continue
		}

		fromUnitID := link.Link.FromUnitID.String()
		toUnitID := link.Link.ToUnitID.String()

		// Check if both units are in our dataset
		parentNode, parentExists := unitMap[fromUnitID]
		childNode, childExists := unitMap[toUnitID]

		if parentExists && childExists {
			// Both units are in our dataset, establish parent-child relationship
			childNode.Parent = parentNode
			parentNode.Children = append(parentNode.Children, childNode)
		}
	}

	// Find root nodes (units that have no parents in our dataset)
	for _, node := range unitMap {
		if node.Parent == nil {
			rootNodes = append(rootNodes, node)
		}
	}

	return rootNodes, nil
}

// displayTree displays the tree structure
func displayTree(nodes []*UnitTreeNode) {
	// Check if we should display columns alongside the tree
	displayColumns := shouldDisplayColumns()

	if displayColumns {
		displayTreeWithTableRenderer(nodes)
	} else {
		displayTreeOnly(nodes)
	}
}

// shouldDisplayColumns determines if we should show columns based on flags.
// Any alternative output (json, yaml, jq, yq, name) would interfere with the
// column display, so suppress the columns view in those cases.
func shouldDisplayColumns() bool {
	return !quiet && !isAlternativeOutput()
}

// displayTreeOnly displays just the tree structure
func displayTreeOnly(nodes []*UnitTreeNode) {
	for _, node := range nodes {
		displayTreeNode(node, "", true, false)
	}
}

// displayTreeWithTableRenderer displays tree using the existing table renderer
func displayTreeWithTableRenderer(nodes []*UnitTreeNode) {
	// Get columns to display
	columnsToShow := getTreeColumns()

	// Create a custom column provider using the existing unit system
	provider := NewDynamicColumnProvider(new(goclientnew.ExtendedUnit))
	provider.WithAliases(unitAliases)
	provider.WithCustomColumns(unitCustomColumns)

	// Create table
	table := tableView()

	// Set headers using the existing column system
	if !noheader {
		headers := make([]string, len(columnsToShow)+2) // +2 for the tree column and complementary column
		headers[0] = "Node"
		if unitTreeArgs.nodeType == "space" {
			headers[1] = "Unit"
		} else {
			headers[1] = "Space"
		}
		for i, col := range columnsToShow {
			headers[i+2] = columnToHeader(provider, col)
		}
		table.SetHeader(headers)
	}

	// Add rows by traversing the tree in order
	for _, node := range nodes {
		addTreeNodeToTableWithProvider(table, node, "", true, columnsToShow, provider)
	}

	table.Render()
}

// addTreeNodeToTableWithProvider adds a tree node and its children to the table using the column provider
func addTreeNodeToTableWithProvider(table *tablewriter.Table, node *UnitTreeNode, prefix string, isLast bool, columnsToShow []string, provider *DynamicColumnProvider) {
	// Determine the tree characters
	var connector string
	if isLast {
		connector = "└── "
	} else {
		connector = "├── "
	}

	// Display the current node
	displayName := formatNodeName(node.Unit)

	// Create the tree line
	treeLine := fmt.Sprintf("%s%s%s", prefix, connector, displayName)

	// Build the row
	row := make([]string, len(columnsToShow)+2) // +2 for the tree column and complementary column
	row[0] = treeLine
	row[1] = formatComplementaryInfo(node.Unit)

	// Add column values using the existing column system
	for i, col := range columnsToShow {
		row[i+2] = provider.GetValue(node.Unit, col)
	}

	table.Append(row)

	// Add children
	if len(node.Children) > 0 {
		childPrefix := prefix
		if isLast {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}

		for i, child := range node.Children {
			isLastChild := i == len(node.Children)-1
			addTreeNodeToTableWithProvider(table, child, childPrefix, isLastChild, columnsToShow, provider)
		}
	}
}

// displayTreeNode recursively displays a tree node with proper indentation
func displayTreeNode(node *UnitTreeNode, prefix string, isLast bool, withColumns bool) {
	// Determine the tree characters
	var connector string
	if isLast {
		connector = "└── "
	} else {
		connector = "├── "
	}

	// Display the current node
	displayName := formatNodeName(node.Unit)
	fmt.Printf("%s%s%s\n", prefix, connector, displayName)

	// Display children
	if len(node.Children) > 0 {
		childPrefix := prefix
		if isLast {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}

		for i, child := range node.Children {
			isLastChild := i == len(node.Children)-1
			displayTreeNode(child, childPrefix, isLastChild, withColumns)
		}
	}
}

// getTreeColumns returns the columns to display for tree view
func getTreeColumns() []string {
	// Default columns for tree view (using same as unit list)
	defaultTreeColumns := []string{"UpgradeNeeded", "UnreleasedChanges", "Unit.ApplyGates"}

	if cols := effectiveColumns(); len(cols) > 0 {
		custom := make([]string, len(cols))
		for i, col := range cols {
			custom[i] = strings.TrimSpace(col)
		}
		return custom
	}

	return defaultTreeColumns
}

// printTreeHeader prints the header for tree with columns
func printTreeHeader(columnsToShow []string) {
	// Use a fixed width for the tree + unit name column
	// This should be wide enough to accommodate most tree structures
	treeColumnWidth := 70

	// Print header
	fmt.Printf("%-*s", treeColumnWidth, "Unit")
	for _, col := range columnsToShow {
		fmt.Printf("  %-15s", col)
	}
	fmt.Println()

	// Print separator line
	fmt.Printf("%s", strings.Repeat("-", treeColumnWidth))
	for range columnsToShow {
		fmt.Printf("  %s", strings.Repeat("-", 15))
	}
	fmt.Println()
}

// flattenTreeToUnits flattens the tree structure into a list of units in tree order
func flattenTreeToUnits(nodes []*UnitTreeNode) []*goclientnew.ExtendedUnit {
	var units []*goclientnew.ExtendedUnit
	for _, node := range nodes {
		units = append(units, flattenNodeToUnits(node)...)
	}
	return units
}

// flattenNodeToUnits recursively flattens a node and its children
func flattenNodeToUnits(node *UnitTreeNode) []*goclientnew.ExtendedUnit {
	var units []*goclientnew.ExtendedUnit
	units = append(units, node.Unit)
	for _, child := range node.Children {
		units = append(units, flattenNodeToUnits(child)...)
	}
	return units
}

// findNodeByUnit finds a node by its unit
func findNodeByUnit(nodes []*UnitTreeNode, unit *goclientnew.ExtendedUnit) *UnitTreeNode {
	for _, node := range nodes {
		if found := findNodeByUnitRecursive(node, unit); found != nil {
			return found
		}
	}
	return nil
}

// findNodeByUnitRecursive recursively searches for a node by unit
func findNodeByUnitRecursive(node *UnitTreeNode, unit *goclientnew.ExtendedUnit) *UnitTreeNode {
	if node.Unit.Unit.UnitID == unit.Unit.UnitID {
		return node
	}
	for _, child := range node.Children {
		if found := findNodeByUnitRecursive(child, unit); found != nil {
			return found
		}
	}
	return nil
}

// getTreeDisplayStringForUnit gets the tree display string for a specific unit
func getTreeDisplayStringForUnit(nodes []*UnitTreeNode, unit *goclientnew.ExtendedUnit) string {
	node := findNodeByUnit(nodes, unit)
	if node == nil {
		return formatNodeName(unit)
	}
	return getTreeDisplayString(node, "", true)
}

// getTreeDisplayString generates the tree display string for a node
func getTreeDisplayString(node *UnitTreeNode, prefix string, isLast bool) string {
	// Determine the tree characters
	var connector string
	if isLast {
		connector = "└── "
	} else {
		connector = "├── "
	}

	// Display the current node
	displayName := formatNodeName(node.Unit)

	return fmt.Sprintf("%s%s%s", prefix, connector, displayName)
}
