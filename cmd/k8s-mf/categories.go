// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCategoriesCommand() *cobra.Command {
	var (
		output    string
		file      string
		byManager bool
		category  string
	)
	cmd := &cobra.Command{
		Use:   "categories [TYPE NAME]",
		Short: "Show which fields each category of field manager owns",
		Long: `Group a resource's managed fields by the kind of actor that owns them —
appliers (kubectl, ConfigHub, ArgoCD, Flux, Sveltos, Helm, …), admission
controllers, and asynchronous controllers — rather than by raw manager string.

Also reports CO-OWNED fields (owned by more than one manager, a frequent source
of apply conflicts) and DEFAULT fields (present on the object but owned by no
manager — values the API server defaulted).

The object is read from the live cluster (TYPE NAME) or from a file (-f), e.g.
the output of "kubectl get ... -o yaml --show-managed-fields".

Examples:
  k8s-mf categories deployment my-app -n prod
  kubectl get deploy my-app -o yaml --show-managed-fields | k8s-mf categories -f -`,
		Args: cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			obj, proj, err := inputObject(cmd.Context(), file, args)
			if err != nil {
				return err
			}
			a, err := analyze(obj, proj)
			if err != nil {
				return err
			}
			res := categoriesResult{
				Resource:      resourceRef(obj),
				Categories:    a.byCategory(),
				DefaultFields: a.defaultFields(),
				CoOwnedFields: a.coOwnedFields(),
			}
			if category != "" {
				res.Categories = filterCategories(res.Categories, category)
			}
			switch output {
			case outputJSON:
				return emitJSON(res)
			case outputYAML:
				return emitYAML(res)
			case outputText, outputTree, "":
				renderCategoriesText(res, byManager)
				return nil
			default:
				return fmt.Errorf("unknown output format %q (text|json|yaml)", output)
			}
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", outputText, "Output format: text|json|yaml")
	cmd.Flags().StringVarP(&file, "file", "f", "", "Read the object from a YAML/JSON file (\"-\" for stdin) instead of the cluster")
	cmd.Flags().BoolVar(&byManager, "by-manager", false, "Break each category down by individual field manager")
	cmd.Flags().StringVar(&category, "category", "", "Show only this category (Applier|AsyncController|Unknown)")
	return cmd
}

func filterCategories(cats []categoryReport, want string) []categoryReport {
	var out []categoryReport
	for _, c := range cats {
		if c.Category == want {
			out = append(out, c)
		}
	}
	return out
}
