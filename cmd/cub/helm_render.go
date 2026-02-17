// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// helmTemplateCmd renders a Helm chart's templates locally without requiring a server.
var helmTemplateCmd = &cobra.Command{
	Use:   "template <release-name> <repo>/<chartname>",
	Short: "Render a Helm chart's templates locally",
	Long: getCommandHelp(`Render a Helm chart's templates to stdout or to a local directory.
This command does not require a ConfigHub server connection.

It loads a chart from configured Helm repositories, renders templates with
the provided values, splits CRDs from regular resources, and outputs the result.

By default, rendered YAML is written to stdout. Use --output-dir to write
separate files for CRDs and resources.

Examples:
`+"```"+`
  # Render nginx chart to stdout
  cub helm template my-nginx bitnami/nginx --version 15.5.2

  # Render with custom values to stdout
  cub helm template my-nginx bitnami/nginx --version 15.5.2 -f values.yaml --set image.tag=latest

  # Render to a directory (creates my-nginx.yaml and my-nginx-crds.yaml)
  cub helm template my-nginx bitnami/nginx --version 15.5.2 --output-dir ./out

  # Render cert-manager with namespace
  cub helm template cert-manager jetstack/cert-manager --version v1.17.1 --namespace cert-manager --output-dir ./out
`+"```"+`
`, ""),
	Args:          cobra.ExactArgs(2),
	RunE:          helmTemplateCmdRun,
	SilenceUsage:  true,
	SilenceErrors: true,
	// Override PersistentPreRunE from helmCmd to skip server authentication
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
}

var helmTemplateArgs struct {
	valuesFiles    []string
	set            []string
	version        string
	repo           string
	namespace      string
	usePlaceholder bool
	skipCRDs       bool
	outputDir      string
}

func init() {
	helmTemplateCmd.Flags().StringArrayVarP(&helmTemplateArgs.valuesFiles, "values", "f", []string{}, "specify values in a YAML file or a URL (can specify multiple)")
	helmTemplateCmd.Flags().StringArrayVar(&helmTemplateArgs.set, "set", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	helmTemplateCmd.Flags().StringVar(&helmTemplateArgs.version, "version", "", "specify a version constraint for the chart version to use. This constraint can be a specific tag (e.g. 1.1.1) or range (e.g. ^2.0.0)")
	helmTemplateCmd.Flags().StringVar(&helmTemplateArgs.repo, "repo", "", "specify the chart repository URL where to locate the requested chart")
	helmTemplateCmd.Flags().StringVar(&helmTemplateArgs.namespace, "namespace", "default", "namespace for the release (used during template rendering)")
	helmTemplateCmd.Flags().BoolVar(&helmTemplateArgs.usePlaceholder, "use-placeholder", false, "use confighubplaceholder placeholder for rendering")
	helmTemplateCmd.Flags().BoolVar(&helmTemplateArgs.skipCRDs, "skip-crds", false, "if set, no CRDs from the chart's crds/ directory will be rendered (does not affect templated CRDs)")
	helmTemplateCmd.Flags().StringVar(&helmTemplateArgs.outputDir, "output-dir", "", "write rendered YAML files to this directory instead of stdout")

	helmCmd.AddCommand(helmTemplateCmd)
}

func helmTemplateCmdRun(cmd *cobra.Command, args []string) error {
	releaseName := args[0]
	chartName := args[1]

	result, err := renderHelmChart(helmRenderInput{
		releaseName:    releaseName,
		chartName:      chartName,
		valuesFiles:    helmTemplateArgs.valuesFiles,
		set:            helmTemplateArgs.set,
		version:        helmTemplateArgs.version,
		repo:           helmTemplateArgs.repo,
		namespace:      helmTemplateArgs.namespace,
		usePlaceholder: helmTemplateArgs.usePlaceholder,
		skipCRDs:       helmTemplateArgs.skipCRDs,
	})
	if err != nil {
		return err
	}

	// Prepend namespace resource to regular resources if needed
	resources := prependNamespaceResource(result.Resources, helmTemplateArgs.namespace)

	if helmTemplateArgs.outputDir != "" {
		return writeTemplateOutput(helmTemplateArgs.outputDir, releaseName, result.CRDs, resources)
	}

	return writeTemplateStdout(result.CRDs, resources)
}

// writeTemplateStdout writes rendered YAML to stdout (CRDs first, then resources).
func writeTemplateStdout(crds, resources string) error {
	if crds != "" {
		fmt.Print(crds)
		if resources != "" {
			fmt.Print("---\n")
		}
	}
	if resources != "" {
		fmt.Print(resources)
	}
	return nil
}

// writeTemplateOutput writes rendered YAML to separate files in the output directory.
func writeTemplateOutput(outputDir, releaseName, crds, resources string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", outputDir, err)
	}

	if crds != "" {
		crdPath := filepath.Join(outputDir, releaseName+"-crds.yaml")
		if err := os.WriteFile(crdPath, []byte(crds), 0o644); err != nil {
			return fmt.Errorf("failed to write CRDs file %s: %w", crdPath, err)
		}
		fmt.Fprintf(os.Stderr, "Wrote %s\n", crdPath)
	}

	if resources != "" {
		resourcePath := filepath.Join(outputDir, releaseName+".yaml")
		if err := os.WriteFile(resourcePath, []byte(resources), 0o644); err != nil {
			return fmt.Errorf("failed to write resources file %s: %w", resourcePath, err)
		}
		fmt.Fprintf(os.Stderr, "Wrote %s\n", resourcePath)
	}

	return nil
}
