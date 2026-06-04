// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// output formats shared across commands.
const (
	outputText = "text"
	outputTree = "tree"
	outputJSON = "json"
	outputYAML = "yaml"
)

func emitJSON(v interface{}) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func emitYAML(v interface{}) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Print(string(b))
	return nil
}

// emitObject prints an unstructured object as YAML (default) or JSON.
func emitObject(obj *unstructured.Unstructured, format string) error {
	if format == outputJSON {
		return emitJSON(obj.Object)
	}
	return emitYAML(obj.Object)
}

// renderCategoriesText prints the human-readable category breakdown.
func renderCategoriesText(res categoriesResult, byManager bool) {
	fmt.Printf("Resource: %s\n", res.Resource)
	if len(res.Categories) == 0 {
		fmt.Println("\nNo managed fields found.")
	}
	for _, cat := range res.Categories {
		var names []string
		for _, m := range cat.Managers {
			label := m.Manager
			if m.Display != "" && m.Display != m.Manager {
				label = fmt.Sprintf("%s (%s)", m.Manager, m.Display)
			}
			if m.Heuristic {
				label += " [guessed]"
			}
			names = append(names, label)
		}
		fmt.Printf("\n%s — %d manager(s): %s\n", strings.ToUpper(cat.Category), len(cat.Managers), strings.Join(names, ", "))
		if byManager {
			for _, m := range cat.Managers {
				sub := ""
				if m.Subresource != "" {
					sub = fmt.Sprintf(" [%s]", m.Subresource)
				}
				fmt.Printf("  %s (op=%s)%s — %d field(s)\n", m.Manager, m.Operation, sub, len(m.Paths))
				printPaths(m.Paths, "    ")
			}
		} else {
			fmt.Printf("  %d field(s):\n", len(cat.Paths))
			printPaths(cat.Paths, "    ")
		}
	}
	if len(res.CoOwnedFields) > 0 {
		fmt.Printf("\nCO-OWNED FIELDS (owned by more than one manager — likely apply conflicts):\n")
		for _, f := range res.CoOwnedFields {
			fmt.Printf("  %s  <-  %s\n", f.Path, strings.Join(f.Managers, ", "))
		}
	}
	if len(res.DefaultFields) > 0 {
		fmt.Printf("\nDEFAULT FIELDS (present on the object but owned by no manager — API-server defaults):\n")
		printPaths(res.DefaultFields, "  ")
	}
}

func printPaths(paths []string, indent string) {
	for _, p := range paths {
		fmt.Printf("%s%s\n", indent, p)
	}
}
