// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"embed"
	"os"
	"path/filepath"
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
	return content
}

// getFormattedOverview returns what --help-overview prints. An agent gets the agent overview
// followed by the CLI overview: the first says what an agent needs to know about ConfigHub
// and how to act, and the second holds the conventions every command shares, which people
// read too, so each fact is written in one place.
func getFormattedOverview() string {
	content, err := overviewFS.ReadFile("cub-overview.md")
	if err != nil {
		// Fallback to simple text if markdown can't be read
		return `Command line tool for using ConfigHub`
	}

	if IsAgent {
		agentContent, err := agentsFS.ReadFile("cub-agent-overview.md")
		if err != nil {
			return string(content)
		}
		return string(agentContent) + "\n" + string(content)
	}

	return getFormattedContent(string(content))
}

func getSimpleHelp() string {
	return `Command line tool for using ConfigHub

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
