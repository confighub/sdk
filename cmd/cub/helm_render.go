// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/bridge-impl/helmutils"
)

// helmTemplateCmd renders a Helm chart locally, previewing what
// 'cub helm install' would generate, without a server connection.
var helmTemplateCmd = &cobra.Command{
	Use:   "template <release-name> <chart-ref>",
	Short: "Render a Helm chart locally, previewing the generated units",
	Long: getCommandHelp(`Render a Helm chart client-side and show the units 'cub helm install'
would generate, without requiring a ConfigHub server connection.

By default the rendered units are written to stdout, each preceded by a
"# Unit: <slug>" comment. Use --output-dir to write one <slug>.yaml file per
unit instead.

Examples:
`+"```"+`
  # Preview to stdout
  cub helm template cubbychat oci://ghcr.io/confighub/charts/cubbychat

  # Write one file per unit
  cub helm template cubbychat ./charts/cubbychat --output-dir ./out
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
	prefix          string
	namespace       string
	createNamespace bool
	valuesFiles     []string
	set             []string
	version         string
	repo            string
	includeHooks    bool
	skipCRDs        bool
	outputDir       string
}

func init() {
	helmTemplateCmd.Flags().StringVar(&helmTemplateArgs.prefix, "prefix", "", "prefix for generated unit slugs")
	helmTemplateCmd.Flags().StringVar(&helmTemplateArgs.namespace, "namespace", "", "release namespace; when omitted the placeholder namespace is rendered")
	helmTemplateCmd.Flags().BoolVar(&helmTemplateArgs.createNamespace, "create-namespace", false, "synthesize a Namespace unit for the release namespace")
	helmTemplateCmd.Flags().StringArrayVarP(&helmTemplateArgs.valuesFiles, "values", "f", []string{}, "specify values in a YAML file (can specify multiple)")
	helmTemplateCmd.Flags().StringArrayVar(&helmTemplateArgs.set, "set", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	helmTemplateCmd.Flags().StringVar(&helmTemplateArgs.version, "version", "", "chart version constraint: a specific version (e.g. 1.1.1) or a range (e.g. ^2.0.0)")
	helmTemplateCmd.Flags().StringVar(&helmTemplateArgs.repo, "repo", "", "chart repository URL to resolve a bare chart name against")
	helmTemplateCmd.Flags().BoolVar(&helmTemplateArgs.includeHooks, "include-hooks", false, "keep helm.sh/hook manifests as plain resources instead of dropping them")
	helmTemplateCmd.Flags().BoolVar(&helmTemplateArgs.skipCRDs, "skip-crds", false, "do not generate units from the chart's crds/ directories")
	helmTemplateCmd.Flags().StringVar(&helmTemplateArgs.outputDir, "output-dir", "", "write one <slug>.yaml file per unit to this directory instead of stdout")

	helmCmd.AddCommand(helmTemplateCmd)
}

func helmTemplateCmdRun(cmd *cobra.Command, args []string) error {
	releaseName := args[0]
	chartRef := args[1]

	values, err := helmutils.MergeValues(helmTemplateArgs.valuesFiles, helmTemplateArgs.set)
	if err != nil {
		return err
	}

	src := &helmutils.HelmSource{
		APIVersion: helmutils.HelmSourceAPIVersion,
		Kind:       helmutils.HelmSourceKind,
		Metadata:   helmutils.HelmSourceMetadata{Name: releaseName},
		Spec: helmutils.HelmSourceSpec{
			Chart: helmutils.HelmSourceChart{
				Ref:     chartRef,
				Repo:    helmTemplateArgs.repo,
				Version: helmTemplateArgs.version,
			},
			Release: helmutils.HelmSourceRelease{
				Name:      releaseName,
				Namespace: helmTemplateArgs.namespace,
			},
			CreateNamespace: helmTemplateArgs.createNamespace,
			UnitPrefix:      helmTemplateArgs.prefix,
			IncludeHooks:    helmTemplateArgs.includeHooks,
			SkipCRDs:        helmTemplateArgs.skipCRDs,
			Values:          values,
		},
	}

	chrt, err := helmutils.LoadChart(src)
	if err != nil {
		return err
	}
	result, err := helmutils.Generate(chrt, src, makeSlug(releaseName))
	if err != nil {
		return err
	}

	for _, dropped := range result.DroppedHooks {
		fmt.Fprintf(os.Stderr, "Dropped hook manifest: %s\n", dropped)
	}

	if helmTemplateArgs.outputDir != "" {
		if err := os.MkdirAll(helmTemplateArgs.outputDir, 0o755); err != nil {
			return fmt.Errorf("failed to create output directory %s: %w", helmTemplateArgs.outputDir, err)
		}
		for _, u := range result.Units {
			path := filepath.Join(helmTemplateArgs.outputDir, u.Slug+".yaml")
			if err := os.WriteFile(path, []byte(u.Content), 0o644); err != nil {
				return fmt.Errorf("failed to write %s: %w", path, err)
			}
			fmt.Fprintf(os.Stderr, "Wrote %s\n", path)
		}
		return nil
	}

	for i, u := range result.Units {
		if i > 0 {
			fmt.Print("---\n")
		}
		fmt.Printf("# Unit: %s\n", u.Slug)
		fmt.Print(u.Content)
	}
	return nil
}
