// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// --view on a list command names a View of the entity type listed. The server returns the values
// of the View's columns on each entity, having loaded whatever they read, and the table shows
// those columns, grouped and ordered as the View says, in place of the command's own.

// listViewRef is the View --view names, as a slug or UUID.
var listViewRef string

// listView is the View --view names, resolved before the command runs, or nil.
var listView *goclientnew.View

// enableViewFlag adds --view to a list command. The View is resolved before the command runs,
// against the Space the command was given, so that a command that widens its search to every
// Space still finds a View named by a bare slug.
func enableViewFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&listViewRef, "view", "",
		"View, by slug or UUID, whose columns to display. The View's Filter, if it has one, selects the entities as --filter does")
	preRun := cmd.PreRunE
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if preRun != nil {
			if err := preRun(cmd, args); err != nil {
				return err
			}
		}
		return resolveListView()
	}
}

// resolveListView resolves --view.
func resolveListView() error {
	listView = nil
	if listViewRef == "" {
		return nil
	}
	// The fields are named, since an empty select would be the command's own --select, which
	// names the listed entity's fields.
	view, err := resolveView(listViewRef, defaultSpaceID(), "ViewID,Slug,SpaceID,Columns,GroupBy,OrderBy,OrderByDirection")
	if err != nil {
		return err
	}
	if view.View == nil {
		return errors.Newf("view %s not found", listViewRef)
	}
	listView = view.View
	return nil
}

// listViewFor returns the UUID of the View --view names when commandPath is the command being
// run, and "" otherwise. The list API helpers pass it as the view parameter, and they are also
// called by other commands, whose View is of some other entity type.
func listViewFor(commandPath string) string {
	if listView == nil || runningCommandPath != commandPath {
		return ""
	}
	return listView.ViewID.String()
}

// viewColumnValues returns the values of the View's columns on an Extended envelope, by column
// name. A column that selected several values, such as one with a wildcard over containers, has
// them joined, so that one entity stays one row.
func viewColumnValues(entity any) map[string]string {
	field := reflect.ValueOf(entity)
	if field.Kind() == reflect.Ptr {
		field = field.Elem()
	}
	if field.Kind() != reflect.Struct {
		return nil
	}
	viewColumns := field.FieldByName("ViewColumns")
	if !viewColumns.IsValid() {
		return nil
	}
	columns, _ := viewColumns.Interface().([]goclientnew.ViewColumn)
	byName := map[string][]string{}
	for _, col := range columns {
		if col.Value != "" {
			byName[col.Name] = append(byName[col.Name], col.Value)
		}
	}
	values := make(map[string]string, len(byName))
	for name, joined := range byName {
		values[name] = strings.Join(joined, ", ")
	}
	return values
}

// displayViewColumns prints a table of the View's columns, one row per entity.
func displayViewColumns[Entity any](entities []*Entity, view *goclientnew.View) {
	rows := make([]map[string]string, len(entities))
	for i, entity := range entities {
		rows[i] = viewColumnValues(entity)
	}
	sortViewRows(rows, view)

	table := tableView()
	if !noheader {
		headers := make([]string, len(view.Columns))
		for i, col := range view.Columns {
			headers[i] = col.Name
		}
		table.SetHeader(headers)
	}
	for _, values := range rows {
		row := make([]string, len(view.Columns))
		for i, col := range view.Columns {
			row[i] = values[col.Name]
		}
		table.Append(row)
	}
	table.Render()
}

// sortViewRows orders the rows by the View's ordering columns. The View's GroupBy and OrderBy come
// first, then the columns' own GroupBy and OrderByDirection, in column order. GroupBy orders
// ascending unless a direction is given, and int columns are ordered numerically.
func sortViewRows(rows []map[string]string, view *goclientnew.View) {
	type orderCol struct {
		name      string
		ascending bool
		isInt     bool
	}
	var orderCols []orderCol

	dataType := func(name string) string {
		for _, c := range view.Columns {
			if c.Name == name {
				return c.DataType
			}
		}
		return ""
	}
	if view.GroupBy != "" {
		orderCols = append(orderCols, orderCol{
			name: view.GroupBy, ascending: view.OrderByDirection != "DESC", isInt: dataType(view.GroupBy) == "int",
		})
	}
	if view.OrderBy != "" && view.OrderBy != view.GroupBy {
		orderCols = append(orderCols, orderCol{
			name: view.OrderBy, ascending: view.OrderByDirection != "DESC", isInt: dataType(view.OrderBy) == "int",
		})
	}
	for _, col := range view.Columns {
		if col.Name == view.GroupBy || col.Name == view.OrderBy {
			continue
		}
		dir := col.OrderByDirection
		if col.GroupBy && dir == "" {
			dir = "ASC"
		}
		if dir == "" {
			continue
		}
		orderCols = append(orderCols, orderCol{name: col.Name, ascending: dir != "DESC", isInt: col.DataType == "int"})
	}
	if len(orderCols) == 0 {
		return
	}

	sort.SliceStable(rows, func(i, j int) bool {
		for _, oc := range orderCols {
			vi, vj := rows[i][oc.name], rows[j][oc.name]
			if vi == vj {
				continue
			}
			less := vi < vj
			if oc.isInt {
				ni, ei := strconv.ParseInt(vi, 10, 64)
				nj, ej := strconv.ParseInt(vj, 10, 64)
				if ei == nil && ej == nil {
					less = ni < nj
				}
			}
			if oc.ascending {
				return less
			}
			return !less
		}
		return false
	})
}
