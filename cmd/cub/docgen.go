// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

var (
	docgenOut         string
	docgenFrontmatter bool
)

var docgenCmd = &cobra.Command{
	Use:    "docgen",
	Short:  "Generate CLI documentation",
	Long:   `Generate markdown documentation for all cub commands`,
	Hidden: true,
	Run:    docgenCmdRun,
}

func init() {
	docgenCmd.Flags().StringVar(&docgenOut, "out", "./docs/cli", "output directory")
	docgenCmd.Flags().BoolVar(&docgenFrontmatter, "frontmatter", false, "prepend simple YAML front matter to markdown")

	rootCmd.AddCommand(docgenCmd)
}

func docgenCmdRun(cmd *cobra.Command, args []string) {
	// Create output directory if it doesn't exist
	if err := os.MkdirAll(docgenOut, 0o755); err != nil {
		failOnError(err)
	}

	// Remove the docgen command from rootCmd before generating docs
	// This prevents the docgen command itself from appearing in the generated documentation
	rootCmd.RemoveCommand(cmd)

	// Configure root command for documentation generation
	rootCmd.DisableAutoGenTag = true // stable, reproducible files (no timestamp footer)

	// Generate documentation based on frontmatter flag
	if docgenFrontmatter {
		prep := func(filename string) string {
			base := filepath.Base(filename)
			name := strings.TrimSuffix(base, filepath.Ext(base))
			title := strings.ReplaceAll(name, "_", " ")
			return fmt.Sprintf("---\ntitle: %q\nslug: %q\ndescription: \"CLI reference for %s\"\n---\n\n", title, name, title)
		}
		link := func(name string) string { return strings.ToLower(name) }

		if err := doc.GenMarkdownTreeCustom(rootCmd, docgenOut, prep, link); err != nil {
			failOnError(err)
		}
	} else {
		if err := doc.GenMarkdownTree(rootCmd, docgenOut); err != nil {
			failOnError(err)
		}
	}

	// Copy overview files to output directory
	if err := copyOverviewFiles(docgenOut); err != nil {
		failOnError(err)
	}

	// Add overview links to root cub.md SEE ALSO section
	if err := addOverviewLinksToSeeAlso(docgenOut); err != nil {
		failOnError(err)
	}

	fmt.Printf("Documentation generated in %s\n", docgenOut)
}

func copyOverviewFiles(outDir string) error {
	// Copy cub-overview.md
	overviewContent, err := overviewFS.ReadFile("cub-overview.md")
	if err != nil {
		return fmt.Errorf("failed to read cub-overview.md: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "cub-overview.md"), overviewContent, 0644); err != nil {
		return fmt.Errorf("failed to write cub-overview.md: %w", err)
	}

	// Copy cub-agent-overview.md
	agentContent, err := agentsFS.ReadFile("cub-agent-overview.md")
	if err != nil {
		return fmt.Errorf("failed to read cub-agent-overview.md: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "cub-agent-overview.md"), agentContent, 0644); err != nil {
		return fmt.Errorf("failed to write cub-agent-overview.md: %w", err)
	}

	return nil
}

func addOverviewLinksToSeeAlso(outDir string) error {
	cubMdPath := filepath.Join(outDir, "cub.md")

	// Read the cub.md file
	content, err := os.ReadFile(cubMdPath)
	if err != nil {
		return fmt.Errorf("failed to read cub.md: %w", err)
	}

	contentStr := string(content)

	// Find the SEE ALSO section and add overview links
	seeAlsoMarker := "### SEE ALSO\n\n"
	seeAlsoIdx := strings.Index(contentStr, seeAlsoMarker)
	if seeAlsoIdx == -1 {
		return fmt.Errorf("SEE ALSO section not found in cub.md")
	}

	// Insert overview links at the beginning of SEE ALSO section
	overviewLinks := "* [cub overview](cub-overview.md)\t - Overview of the cub CLI\n" +
		"* [cub agent overview](cub-agent-overview.md)\t - Overview for AI agents using cub\n"

	insertPos := seeAlsoIdx + len(seeAlsoMarker)
	newContent := contentStr[:insertPos] + overviewLinks + contentStr[insertPos:]

	// Write back the modified content
	if err := os.WriteFile(cubMdPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write modified cub.md: %w", err)
	}

	return nil
}
