// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"embed"
	"os"
	"path/filepath"

	"github.com/charmbracelet/glamour"
)

const (
	CONFIGHUB_DIR = ".confighub"
)

//go:embed cub-overview.md
var overviewFS embed.FS

//go:embed cub-agent-overview.md
var agentsFS embed.FS

var IsAgent bool = os.Getenv("CONFIGHUB_AGENT") != ""

// Helper functions for dynamic help text generation

// getCommandHelp returns help text for commands with optional agent context
func getCommandHelp(baseHelp string, agentContext string) string {
	if IsAgent {
		if agentContext != "" {
			return baseHelp + "\n\n" + agentContext
		}
		return baseHelp
	}
	// Docgen uses an env var rather than a global variable in order to pass info across
	// process boundaries.
	if os.Getenv("CONFIGHUB_DOCGEN") != "" {
		return baseHelp
	}
	return getFormattedContent(baseHelp)
}

func getFormattedContent(content string) string {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(80),
	)
	if err != nil {
		// Fallback to raw markdown if glamour fails
		return content
	}

	formatted, err := renderer.Render(content)
	if err != nil {
		// Fallback to raw markdown if rendering fails
		return content
	}

	return formatted
}

func getFormattedOverview() string {
	if IsAgent {
		content, err := agentsFS.ReadFile("cub-agent-overview.md")
		if err == nil {
			return string(content)
		}
	}

	content, err := overviewFS.ReadFile("cub-overview.md")
	if err != nil {
		// Fallback to simple text if markdown can't be read
		return `Command line tool for using ConfigHub
To change the default confighub host, set CONFIGHUB_URL environment variable.`
	}

	if IsAgent {
		return string(content)
	}

	return getFormattedContent(string(content))
}

func getSimpleHelp() string {
	return `Command line tool for using ConfigHub
To change the default confighub host, set CONFIGHUB_URL environment variable.

AI agents should set CONFIGHUB_AGENT=1 for all cub commands with --help, and execute cub --help-overview to display a detailed overview.`
}

func saveAgentsFile() error {
	content, err := agentsFS.ReadFile("cub-agent-overview.md")
	if err != nil {
		return err
	}

	configHubDir := filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR)
	err = os.MkdirAll(configHubDir, 0755)
	if err != nil {
		return err
	}

	agentsFile := filepath.Join(configHubDir, "agents.md")
	err = os.WriteFile(agentsFile, content, 0644)
	if err != nil {
		return err
	}

	return nil
}
