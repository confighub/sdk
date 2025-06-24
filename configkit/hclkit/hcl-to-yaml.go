// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package hclkit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/gocty"
	"gopkg.in/yaml.v3"
)

// HCLToYAML converts HCL content to YAML
type HCLToYAML struct {
	content []byte
}

// NewHCLToYAML creates a new converter instance
func NewHCLToYAML() *HCLToYAML {
	return &HCLToYAML{}
}

// Spec is here: https://github.com/hashicorp/hcl/blob/main/hclsyntax/spec.md
// A brief overview of OpenTofu HCL is here:
// https://opentofu.org/docs/language/syntax/

// ParseHCL parses HCL
func (h *HCLToYAML) ParseHCL(content []byte, name string) ([]map[string]interface{}, error) {
	h.content = content

	// Parse the HCL content
	file, diags := hclsyntax.ParseConfig(content, name, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse HCL: %s", diags.Error())
	}

	// Convert the parsed HCL to a slice of maps
	result := make([]map[string]interface{}, 0)

	// Process the body of the HCL file
	for _, block := range file.Body.(*hclsyntax.Body).Blocks {
		// TODO: decide what block types to support.
		blockCategory := block.Type
		blockMap := make(map[string]interface{})
		result = append(result, blockMap)
		blockMetadata := map[string]string{}
		blockMetadata[BlockCategoryField] = string(convertBlockTypeToCategory(blockCategory))
		blockMap[MetadataPrefix] = blockMetadata

		// Handle block labels (e.g., resource "aws_instance" "example")
		switch len(block.Labels) {
		case 0:
			blockMetadata[BlockTypeField] = blockCategory
			blockMetadata[BlockNameField] = BlockNameSingleton
		case 1:
			blockMetadata[BlockTypeField] = blockCategory
			blockMetadata[BlockNameField] = block.Labels[0]
		case 2:
			blockMetadata[BlockTypeField] = block.Labels[0]
			blockMetadata[BlockNameField] = block.Labels[1]
		default:
			blockMetadata[BlockTypeField] = block.Labels[0]
			blockMetadata[BlockNameField] = block.Labels[1]
			labels := block.Labels[2:]
			// For blocks with more labels, create nested structure
			for i := 0; i < len(labels); i++ {
				nestedBlock := make(map[string]interface{})
				blockMap[labels[i]] = nestedBlock
				blockMap = nestedBlock
			}
		}

		// Process block attributes and nested blocks
		err := h.processBlockBody(block.Body, blockMap)
		if err != nil {
			return nil, fmt.Errorf("failed to process block body: %w", err)
		}
	}

	// TODO: Process top-level attributes
	// for name, attr := range file.Body.(*hclsyntax.Body).Attributes {
	// 	value, err := h.evaluateExpression(attr.Expr)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("failed to evaluate attribute %s: %w", name, err)
	// 	}
	// 	blockResult[name] = value
	// }

	return result, nil
}

// This only handles a subset of cases from https://github.com/hashicorp/hcl2/blob/master/cmd/hcldec/spec.go

// processBlockBody processes the body of an HCL block
func (h *HCLToYAML) processBlockBody(body hcl.Body, result map[string]interface{}) error {
	hclBody := body.(*hclsyntax.Body)

	// Process attributes
	for name, attr := range hclBody.Attributes {
		value, err := h.evaluateExpression(attr.Expr)
		if err != nil {
			return fmt.Errorf("failed to evaluate attribute %s: %w", name, err)
		}
		result[name] = value
	}

	// if len(hclBody.Blocks) > 1 {
	// 	fmt.Printf("%d blocks\n", len(hclBody.Blocks))
	// }

	// TODO: jsonencode

	// Process nested blocks
	for _, block := range hclBody.Blocks {
		blockType := block.Type
		// fmt.Printf("block type %s\n", blockType)

		// TODO: Is it possible to have labels on internal blocks?
		// We'd need a key name that wouldn't conflict with any types and the yaml-to-hcl
		// converter would need to support it.
		if len(block.Labels) > 0 {
			return errors.New("block labels not supported on internal blocks")
		}
		// Blocks are actually lists.
		// https://opentofu.org/docs/language/attr-as-blocks/
		// However, behavior of representing blocks as a list vs blocks is different
		// with respect to default values, so blocks are preferred except in the
		// case of an empty list.
		blockListItem, alreadyPresent := result[blockType]
		var blockList []map[string]interface{}
		if alreadyPresent {
			var ok bool
			blockList, ok = blockListItem.([]map[string]interface{})
			if !ok {
				return errors.New("unexpected type representing a block")
			}
		} else {
			blockList = make([]map[string]interface{}, 0)
		}
		blockContent := make(map[string]interface{})
		err := h.processBlockBody(block.Body, blockContent)
		if err != nil {
			return fmt.Errorf("failed to process nested block: %w", err)
		}
		result[blockType] = append(blockList, blockContent)
	}

	return nil
}

// evaluateExpression evaluates an HCL expression and returns its value
func (h *HCLToYAML) evaluateExpression(expr hcl.Expression) (interface{}, error) {
	// Create an empty evaluation context
	ctx := &hcl.EvalContext{}

	// Evaluate the expression
	value, diags := expr.Value(ctx)
	if diags.HasErrors() {
		// If evaluation fails, try to get the literal value
		return h.getLiteralValue(expr), nil
	}

	// Convert cty.Value to Go interface{}
	return h.ctyValueToInterface(value)
}

// getLiteralValue extracts literal values from expressions
func (h *HCLToYAML) getLiteralValue(expr hcl.Expression) interface{} {
	switch e := expr.(type) {
	case *hclsyntax.LiteralValueExpr:
		val, _ := h.ctyValueToInterface(e.Val)
		return val
	case *hclsyntax.TemplateExpr:
		if len(e.Parts) == 1 {
			if lit, ok := e.Parts[0].(*hclsyntax.LiteralValueExpr); ok {
				val, _ := h.ctyValueToInterface(lit.Val)
				return val
			}
		}
		// For templates with interpolations, reconstruct the template
		return h.reconstructTemplate(e)
	case *hclsyntax.TupleConsExpr:
		var result []interface{}
		for _, elem := range e.Exprs {
			result = append(result, h.getLiteralValue(elem))
		}
		return result
	case *hclsyntax.ObjectConsExpr:
		result := make(map[string]interface{})
		for _, item := range e.Items {
			keyExpr := item.KeyExpr
			valueExpr := item.ValueExpr

			key := h.getLiteralValue(keyExpr)
			value := h.getLiteralValue(valueExpr)

			if keyStr, ok := key.(string); ok {
				result[keyStr] = value
			}
		}
		return result
	case *hclsyntax.ScopeTraversalExpr:
		// Handle direct references like aws_instance.example.id
		// https://opentofu.org/docs/language/expressions/references/
		// FIXME: These are indistinguishable from string literals when we convert back.
		// https://opentofu.org/docs/language/syntax/json/
		return h.reconstructTraversal(e.Traversal)
	case *hclsyntax.FunctionCallExpr:
		// Handle function calls like join(", ", var.list)
		return h.reconstructFunctionCall(e)
	case *hclsyntax.ConditionalExpr:
		// Handle conditional expressions like var.enable ? "yes" : "no"
		return h.reconstructConditional(e)
	case *hclsyntax.BinaryOpExpr:
		// Handle binary operations like var.a + var.b
		return h.reconstructBinaryOp(e)
	case *hclsyntax.UnaryOpExpr:
		// Handle unary operations like !var.enabled
		return h.reconstructUnaryOp(e)
	case *hclsyntax.IndexExpr:
		// Handle indexing like var.list[0]
		return h.reconstructIndex(e)
	case *hclsyntax.SplatExpr:
		// Handle splat expressions like var.list[*].name
		return h.reconstructSplat(e)
	case *hclsyntax.ForExpr:
		// Handle for expressions
		return h.reconstructFor(e)
	default:
		// For any other expressions, try to get the source bytes
		return h.getExpressionSource(expr)
	}
}

// ctyValueToInterface converts a cty.Value to a Go interface{}
func (h *HCLToYAML) ctyValueToInterface(val cty.Value) (interface{}, error) {
	if val.IsNull() {
		return nil, nil
	}

	switch val.Type() {
	case cty.String:
		return val.AsString(), nil
	case cty.Number:
		if val.AsBigFloat().IsInt() {
			var result int64
			err := gocty.FromCtyValue(val, &result)
			return result, err
		}
		var result float64
		err := gocty.FromCtyValue(val, &result)
		return result, err
	case cty.Bool:
		return val.True(), nil
	}

	if val.Type().IsMapType() || val.Type().IsObjectType() {
		result := make(map[string]interface{})
		for it := val.ElementIterator(); it.Next(); {
			keyVal, elemVal := it.Element()
			key := keyVal.AsString()
			elem, err := h.ctyValueToInterface(elemVal)
			if err != nil {
				return nil, err
			}
			result[key] = elem
		}
		return result, nil
	}

	if val.Type().IsListType() || val.Type().IsTupleType() {
		var result []interface{}
		for it := val.ElementIterator(); it.Next(); {
			_, elemVal := it.Element()
			elem, err := h.ctyValueToInterface(elemVal)
			if err != nil {
				return nil, err
			}
			result = append(result, elem)
		}
		return result, nil
	}

	return val.GoString(), nil
}

// reconstructTraversal reconstructs a traversal expression like aws_instance.example.id
func (h *HCLToYAML) reconstructTraversal(traversal hcl.Traversal) string {
	var parts []string
	for _, step := range traversal {
		switch s := step.(type) {
		case hcl.TraverseRoot:
			parts = append(parts, s.Name)
		case hcl.TraverseAttr:
			parts = append(parts, s.Name)
		case hcl.TraverseIndex:
			if s.Key.Type() == cty.String {
				parts = append(parts, fmt.Sprintf(`["%s"]`, s.Key.AsString()))
			} else if s.Key.Type() == cty.Number {
				if s.Key.AsBigFloat().IsInt() {
					val, _ := s.Key.AsBigFloat().Int64()
					parts = append(parts, fmt.Sprintf(`[%d]`, val))
				} else {
					val, _ := s.Key.AsBigFloat().Float64()
					parts = append(parts, fmt.Sprintf(`[%g]`, val))
				}
			}
		}
	}

	result := ""
	for i, part := range parts {
		if i == 0 {
			result = part
		} else if part[0] == '[' {
			result += part
		} else {
			result += "." + part
		}
	}
	return result
}

// reconstructTemplate reconstructs template expressions
func (h *HCLToYAML) reconstructTemplate(template *hclsyntax.TemplateExpr) string {
	if template.IsStringLiteral() {
		// Simple string literal
		if len(template.Parts) == 1 {
			if lit, ok := template.Parts[0].(*hclsyntax.LiteralValueExpr); ok {
				return lit.Val.AsString()
			}
		}
	}

	// Complex template with interpolations
	var result string
	for _, part := range template.Parts {
		switch p := part.(type) {
		case *hclsyntax.LiteralValueExpr:
			result += p.Val.AsString()
		default:
			result += "${" + h.getExpressionSource(p) + "}"
		}
	}
	return result
}

// reconstructFunctionCall reconstructs function call expressions
func (h *HCLToYAML) reconstructFunctionCall(funcCall *hclsyntax.FunctionCallExpr) string {
	var args []string
	for _, arg := range funcCall.Args {
		args = append(args, h.expressionToString(arg))
	}
	return fmt.Sprintf("%s(%s)", funcCall.Name, strings.Join(args, ", "))
}

// reconstructConditional reconstructs conditional expressions
func (h *HCLToYAML) reconstructConditional(cond *hclsyntax.ConditionalExpr) string {
	condition := h.expressionToString(cond.Condition)
	trueResult := h.expressionToString(cond.TrueResult)
	falseResult := h.expressionToString(cond.FalseResult)
	return fmt.Sprintf("%s ? %s : %s", condition, trueResult, falseResult)
}

// reconstructBinaryOp reconstructs binary operation expressions
func (h *HCLToYAML) reconstructBinaryOp(binOp *hclsyntax.BinaryOpExpr) string {
	lhs := h.expressionToString(binOp.LHS)
	rhs := h.expressionToString(binOp.RHS)
	var op string
	switch binOp.Op {
	case hclsyntax.OpLogicalAnd:
		op = "&&"
	case hclsyntax.OpLogicalOr:
		op = "||"
	case hclsyntax.OpEqual:
		op = "=="
	case hclsyntax.OpNotEqual:
		op = "!="
	case hclsyntax.OpGreaterThan:
		op = ">"
	case hclsyntax.OpGreaterThanOrEqual:
		op = ">="
	case hclsyntax.OpLessThan:
		op = "<"
	case hclsyntax.OpLessThanOrEqual:
		op = "<="
	case hclsyntax.OpAdd:
		op = "+"
	case hclsyntax.OpSubtract:
		op = "-"
	case hclsyntax.OpMultiply:
		op = "*"
	case hclsyntax.OpDivide:
		op = "/"
	case hclsyntax.OpModulo:
		op = "%"
	default:
		op = "?"
	}
	return fmt.Sprintf("%s %s %s", lhs, op, rhs)
}

// reconstructUnaryOp reconstructs unary operation expressions
func (h *HCLToYAML) reconstructUnaryOp(unaryOp *hclsyntax.UnaryOpExpr) string {
	operand := h.expressionToString(unaryOp.Val)
	var op string
	switch unaryOp.Op {
	case hclsyntax.OpLogicalNot:
		op = "!"
	case hclsyntax.OpNegate:
		op = "-"
	default:
		op = "?"
	}
	return fmt.Sprintf("%s%s", op, operand)
}

// reconstructIndex reconstructs index expressions
func (h *HCLToYAML) reconstructIndex(index *hclsyntax.IndexExpr) string {
	collection := h.expressionToString(index.Collection)
	key := h.expressionToString(index.Key)
	return fmt.Sprintf("%s[%s]", collection, key)
}

// reconstructSplat reconstructs splat expressions
func (h *HCLToYAML) reconstructSplat(splat *hclsyntax.SplatExpr) string {
	source := h.expressionToString(splat.Source)
	each := h.expressionToString(splat.Each)
	return fmt.Sprintf("%s[*].%s", source, each)
}

// reconstructFor reconstructs for expressions
func (h *HCLToYAML) reconstructFor(forExpr *hclsyntax.ForExpr) string {
	// This is a simplified reconstruction of for expressions
	return fmt.Sprintf("[for %s in %s : %s]",
		forExpr.KeyVar,
		h.expressionToString(forExpr.CollExpr),
		h.expressionToString(forExpr.ValExpr))
}

// expressionToString converts any expression to its string representation
func (h *HCLToYAML) expressionToString(expr hcl.Expression) string {
	switch e := expr.(type) {
	case *hclsyntax.LiteralValueExpr:
		val, _ := h.ctyValueToInterface(e.Val)
		switch v := val.(type) {
		case string:
			return fmt.Sprintf(`"%s"`, v)
		case bool:
			return fmt.Sprintf("%t", v)
		default:
			return fmt.Sprintf("%v", v)
		}
	case *hclsyntax.ScopeTraversalExpr:
		return h.reconstructTraversal(e.Traversal)
	case *hclsyntax.TemplateExpr:
		return fmt.Sprintf(`"%s"`, h.reconstructTemplate(e))
	default:
		if result := h.getLiteralValue(expr); result != nil {
			if str, ok := result.(string); ok {
				return str
			}
			return fmt.Sprintf("%v", result)
		}
		return h.getExpressionSource(expr)
	}
}

// getExpressionSource tries to get the original source text of an expression
func (h *HCLToYAML) getExpressionSource(expr hcl.Expression) string {
	rng := expr.Range()
	if rng.Start.Byte >= 0 && rng.End.Byte <= len(h.content) && rng.End.Byte > rng.Start.Byte {
		return string(h.content[rng.Start.Byte:rng.End.Byte])
	}

	return "unknown"
}

// TODO: Preserve comments

// ConvertToYAML converts the parsed HCL to YAML format
func (h *HCLToYAML) ConvertToYAML(content []byte, name string) ([]byte, error) {
	hclData, err := h.ParseHCL(content, name)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HCL file: %w", err)
	}

	var fullYamlData []byte
	for i, doc := range hclData {
		if i != 0 {
			fullYamlData = append(fullYamlData, []byte("---\n")...)
		}
		yamlData, err := yaml.Marshal(doc)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal to YAML: %w", err)
		}
		fullYamlData = append(fullYamlData, yamlData...)
	}

	return fullYamlData, nil
}

func (*HclResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	h := NewHCLToYAML()
	yaml, err := h.ConvertToYAML(data, "HCL")
	// if err == nil {
	// 	fmt.Printf("HCL to YAML:\n%s\n", string(yaml))
	// }
	return yaml, err
}
