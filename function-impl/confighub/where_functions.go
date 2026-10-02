// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package confighub

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/entityfilter"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// Issue identifiers of vet-where-expressions.
const (
	issueInvalidWhereExpression = "invalid-where-expression"
	issueLiteralUUID            = "literal-uuid"
)

func registerWhereFunctions(fh handler.FunctionRegistry, rp *cubkit.ConfigHubResourceProviderType) {
	if err := fh.RegisterFunction("get-where-expressions", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-where-expressions",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attribute-list",
				Description: "Where expressions, each with the entity type it selects",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &api.AttributeValueListSchema,
			},
			Mutating:   false,
			Validating: false,
			Hermetic:   true,
			Idempotent: true,
			Description: "Returns each where expression in entity documents, such as a Space's WhereTrigger or a Filter's Where, with its path. " +
				"Details.NeededRequired.ResourceType is the entity type it selects, as for a reference, read from the sibling field that names it where there is one, such as a Filter's From.",
			FunctionType:          api.FunctionTypeCustom,
			AttributeName:         cubkit.AttributeNameWhereExpression,
			AffectedResourceTypes: yamlkit.ResourceTypesForAttribute(cubkit.AttributeNameWhereExpression, rp),
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			values, err := getWhereExpressions(rp, fArgs.ParsedData, fArgs.Options)
			return fArgs.ParsedData, values, err
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}

	if err := fh.RegisterFunction("vet-where-expressions", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-where-expressions",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if every where expression parses against the attributes of the entity type it selects",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:   false,
			Validating: true,
			Hermetic:   true,
			Idempotent: true,
			Description: "Checks each where expression in entity documents, such as a Space's WhereTrigger or a Filter's Where, against the attributes of the entity type it selects, " +
				"as the server does when the entity is written: its syntax, the names it uses, and the types of its literals. " +
				"A literal UUID compared with an ID is scored Low without failing: it names one entity in one environment, so promoting the document carries it into another.",
			FunctionType:          api.FunctionTypeCustom,
			AttributeName:         cubkit.AttributeNameWhereExpression,
			AffectedResourceTypes: yamlkit.ResourceTypesForAttribute(cubkit.AttributeNameWhereExpression, rp),
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			result, err := vetWhereExpressions(rp, fArgs.ParsedData, fArgs.Options)
			return fArgs.ParsedData, result, err
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// whereExpression is one where expression in a document: where it is, and what it selects.
type whereExpression struct {
	value    api.AttributeValue
	selects  string
	prefixes bool
}

// whereExpressions finds every where expression in the documents, with the entity type each
// selects. A path whose selected type is named by a sibling, as a Filter's Where is by its From,
// reads the sibling from the document.
func whereExpressions(rp *cubkit.ConfigHubResourceProviderType, parsedData gaby.Container, options *api.FunctionOptions) ([]whereExpression, error) {
	var expressions []whereExpression
	for _, entityType := range cubkit.DocumentSchemaTypes() {
		for _, declared := range cubkit.DeclaredWhereExpressions(entityType) {
			visitorMap := yamlkit.GetVisitorMapForPath(rp, entityType, api.UnresolvedPath(declared.Path))
			values, err := yamlkit.GetPathsAnyType(parsedData, visitorMap, []any{}, rp, api.DataTypeString, false, false, options)
			if err != nil {
				return nil, err
			}
			for _, value := range values {
				text, ok := value.Value.(string)
				if !ok || text == "" || yamlkit.IsStringPlaceHolderValue(text) {
					continue
				}
				value.AttributeName = cubkit.AttributeNameWhereExpression
				selects := declared.Selects
				if sibling, named := strings.CutPrefix(selects, "@"); named {
					selects = siblingValue(parsedData, rp, value, sibling)
				}
				expressions = append(expressions, whereExpression{value: value, selects: selects, prefixes: declared.Prefixes})
			}
		}
	}
	return expressions, nil
}

// siblingValue reads the field beside a path, in the object holding it, or "" if there is none.
func siblingValue(parsedData gaby.Container, rp *cubkit.ConfigHubResourceProviderType, value api.AttributeValue, field string) string {
	doc, _ := yamlkit.FindResourceDoc(parsedData, rp, &value.ResourceInfo)
	if doc == nil {
		return ""
	}
	path := field
	if dot := strings.LastIndex(string(value.Path), "."); dot >= 0 {
		path = string(value.Path)[:dot+1] + field
	}
	sibling, _, _ := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(path), true)
	return sibling
}

// getWhereExpressions returns each where expression with the entity type it selects, in
// Details.NeededRequired under the ResourceType property, as a reference states the type it names.
func getWhereExpressions(rp *cubkit.ConfigHubResourceProviderType, parsedData gaby.Container, options *api.FunctionOptions) (api.AttributeValueList, error) {
	expressions, err := whereExpressions(rp, parsedData, options)
	if err != nil {
		return nil, err
	}
	values := api.AttributeValueList{}
	for _, expression := range expressions {
		value := expression.value
		details := api.AttributeDetails{}
		if value.Details != nil {
			details = *value.Details
		}
		details.NeededRequired = map[string]string{api.PropertyKeyResourceType: expression.selects}
		value.Details = &details
		values = append(values, value)
	}
	return values, nil
}

var uuidRegexp = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// literalUUID returns a UUID a term compares an ID with literally, or "". The IDs are what an
// attribute of type uuid, []uuid, or map[string]uuid holds, and the keys of a map[uuid]string.
// Permissions are left out: they name Users, which are the same in every environment.
func literalUUID(term *entityfilter.Expression) string {
	switch term.Attribute.DataType {
	case api.DataTypeUUID, api.DataTypeUUIDArray, api.DataTypeStringUUIDMap:
		if term.OperandEntityPrefix == "" {
			return uuidRegexp.FindString(term.Literal)
		}
	case api.DataTypeUUIDStringMap:
		return uuidRegexp.FindString(term.MapKey)
	}
	return ""
}

func vetWhereExpressions(rp *cubkit.ConfigHubResourceProviderType, parsedData gaby.Container, options *api.FunctionOptions) (api.ValidationResult, error) {
	expressions, err := whereExpressions(rp, parsedData, options)
	if err != nil {
		return api.ValidationResultFalse, err
	}
	table := cubkit.WhereAttributes()
	result := api.ValidationResult{Passed: true}
	fail := func(value api.AttributeValue, score api.Score, identifier, message string) {
		value.Score = score
		value.Issues = []api.Issue{{Identifier: identifier, Message: message}}
		result.FailedAttributes = append(result.FailedAttributes, value)
		result.MaxScore = api.ScoreMax(result.MaxScore, score)
	}
	for _, expression := range expressions {
		text := expression.value.Value.(string)
		if !table.HasEntityType(expression.selects) {
			result.Passed = false
			fail(expression.value, api.ScoreHigh, issueInvalidWhereExpression,
				fmt.Sprintf("%s selects %q, which is not an entity type a where expression can select", expression.value.Path, expression.selects))
			continue
		}
		var included []string
		if expression.prefixes {
			included = table.ExpandableFields(expression.selects)
		}
		terms, err := entityfilter.NewParser(table, expression.selects, included, nil).Parse(text)
		if err != nil {
			result.Passed = false
			fail(expression.value, api.ScoreHigh, issueInvalidWhereExpression,
				fmt.Sprintf("%s is not a valid where expression over %ss: %v", expression.value.Path, expression.selects, err))
			continue
		}
		for _, term := range terms {
			if id := literalUUID(term); id != "" {
				fail(expression.value, api.ScoreLow, issueLiteralUUID,
					fmt.Sprintf("%s names %s by its ID, %s, which is a different entity, or none, in every other environment", expression.value.Path, term.Path, id))
				break
			}
		}
	}
	return result, nil
}
