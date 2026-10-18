// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/entityfilter"
	"github.com/confighub/sdk/core/function/api"
)

// Every entity whose list takes --where has its attributes listed by explain --where, so a
// schema whose where entity type is named differently cannot go unnoticed.
func TestExplainWhereCoversEveryListWithWhere(t *testing.T) {
	for parent, schemaName := range explainSchemas {
		for _, sub := range parent.Commands() {
			if sub.Name() != "list" || sub.Flags().Lookup("where") == nil {
				continue
			}
			if _, err := whereAttributesFor(schemaName); err != nil {
				t.Errorf("%s takes --where, but %s explain --where fails: %v", sub.CommandPath(), parent.CommandPath(), err)
			}
		}
	}
}

// operandFor returns an operator and operand an expression can compare an attribute of the type
// with, so that a form can be checked by parsing it.
func operandFor(dataType api.DataType) string {
	switch dataType {
	case api.DataTypeInt:
		return "= 1"
	case api.DataTypeBool:
		return "= true"
	case api.DataTypeTime:
		return "> '2025-01-01T00:00:00Z'"
	case api.DataTypeUUID:
		return "= '7c61626f-ddbe-41af-93f6-b69f4ab6d308'"
	default:
		return "= 'x'"
	}
}

// The prefixed forms explain --where prints are ones the parser accepts: each names, through the
// reference, an attribute of the entity it names.
func TestExplainWherePrefixedFormsParse(t *testing.T) {
	table := cubkit.WhereAttributes()
	checked := 0
	for entityType, attributes := range table {
		parser := entityfilter.NewParser(table, entityType, table.ExpandableFields(entityType), nil)
		for name, attribute := range attributes {
			if !attribute.Expandable {
				continue
			}
			referenced := table[attribute.References]
			var candidates []string
			for candidate, a := range referenced {
				switch a.DataType {
				case api.DataTypeString, api.DataTypeInt, api.DataTypeUUID, api.DataTypeTime, api.DataTypeBool:
					candidates = append(candidates, candidate)
				}
			}
			if len(candidates) == 0 {
				continue
			}
			sort.Strings(candidates)
			target := candidates[0]
			for _, form := range whereAttributeForms(name, attribute) {
				if !strings.HasSuffix(form, ".<attribute>") {
					continue
				}
				expression := fmt.Sprintf("%s %s", strings.TrimSuffix(form, "<attribute>")+target, operandFor(referenced[target].DataType))
				if _, err := parser.Parse(expression); err != nil {
					t.Errorf("%s.%s: explain --where prints %s, but %q does not parse: %v", entityType, name, form, expression, err)
				}
				checked++
			}
		}
	}
	if checked == 0 {
		t.Fatal("checked no prefixed forms")
	}
}

func TestExplainWhereNamesFormsByType(t *testing.T) {
	for _, tc := range []struct {
		name      string
		attribute entityfilter.Attribute
		want      string
	}{
		{"Slug", entityfilter.Attribute{DataType: api.DataTypeString}, "Slug"},
		{"Labels", entityfilter.Attribute{DataType: api.DataTypeStringMap}, "Labels.<key>"},
		{"Conflicts", entityfilter.Attribute{DataType: api.DataTypeJSON}, "Conflicts, Conflicts.<path>"},
		{"Permissions", entityfilter.Attribute{DataType: api.DataTypeStringStringUUIDBoolMap}, "Permissions.<action>.UserIDs"},
		{"SpaceID", entityfilter.Attribute{DataType: api.DataTypeUUID, References: "Space", Expandable: true}, "SpaceID, Space.<attribute>"},
		{"UpstreamRevisionNum", entityfilter.Attribute{DataType: api.DataTypeInt, References: "Revision"}, "UpstreamRevisionNum"},
		{"FromLinkID", entityfilter.Attribute{DataType: api.DataTypeUUIDArray, References: "Link", Expandable: true}, "FromLinkID, FromLink.*.<attribute>"},
	} {
		if got := strings.Join(whereAttributeForms(tc.name, tc.attribute), ", "); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
