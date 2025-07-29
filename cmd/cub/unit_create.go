// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/confighub/sdk/workerapi"
)

var unitCreateCmd = &cobra.Command{
	Use:   "create <slug> [config-file]",
	Short: "Create a unit",
	Long:  getUnitCreateHelp(),
	Args:  cobra.RangeArgs(1, 2),
	RunE:  unitCreateCmdRun,
}

func getUnitCreateHelp() string {
	baseHelp := `Create a new unit in a space. A unit represents a collection of related resources that can be managed together.

Like other ConfigHub entities, Units have metadata, which can be partly set on the command line
and otherwise read from stdin using the flag --from-stdin. 

Unlike other ConfigHub entities, Units also contain configuration data, which is read from another
source.

Unit configuration data can be provided in multiple ways:
  1. From a local or remote configuration file, or from stdin (by specifying "-")
  2. By cloning an existing upstream unit (using --upstream-unit)

Examples:
  # Create a unit from a local YAML file
  cub unit create --space my-space myunit config.yaml

  # Create a unit from a file:// URL
  cub unit create --space my-space myunit file:///path/to/config.yaml

  # Create a unit from a remote HTTPS URL
  cub unit create --space my-space myunit https://example.com/config.yaml

  # Create a unit with config from stdin
  cub unit create --space my-space myunit -

  # Combine Unit JSON metadata from stdin with config data from file
  cub unit create --space my-space myunit config.yaml --from-stdin

  # Clone an existing unit
  cub unit create --space my-space --json --from-stdin myclone --upstream-unit sample-deployment`

	agentContext := `Essential for adding new configuration to ConfigHub.

Agent creation workflow:
1. Prepare configuration files locally (YAML, HCL, properties, etc.)
2. Choose appropriate unit slug (used for referencing the unit)
3. Create unit and wait for triggers to complete validation
4. Check for any validation issues or apply gates

Creation methods:

From local file:
  cub unit create --space SPACE my-unit config.yaml --wait

From stdin (useful for programmatic creation):
  cat config.yaml | cub unit create --space SPACE my-unit - --wait

Clone existing unit:
  cub unit create --space SPACE my-variant --upstream-unit SOURCE_UNIT --upstream-space SOURCE_SPACE --from-stdin < metadata.json

Key flags for agents:
- --wait: Wait for triggers and validation to complete (recommended)
- --json: Get structured response with unit ID and details
- --verbose: Show detailed creation information
- --from-stdin: Read additional metadata from stdin (for cloning)
- --label: Add labels for organization and filtering

Post-creation workflow:
1. Use 'function do get-placeholders' to check for placeholder values
2. Use 'function do' commands to modify configuration as needed
3. Use 'unit approve' if approval is required
4. Use 'unit apply' to deploy to live infrastructure

Important: Unit slugs must be unique within a space and follow naming conventions (lowercase, hyphens allowed).`

	return getCommandHelp(baseHelp, agentContext)
}

var unitCreateArgs struct {
	upstreamUnitSlug  string
	upstreamSpaceSlug string
	importUnitSlug    string
	toolchainType     string
	targetSlug        string
}

func init() {
	addStandardCreateFlags(unitCreateCmd)
	enableWaitFlag(unitCreateCmd)
	unitCreateCmd.Flags().StringVar(&unitCreateArgs.targetSlug, "target", "", "target for the unit")
	unitCreateCmd.Flags().StringVar(&unitCreateArgs.upstreamUnitSlug, "upstream-unit", "", "upstream unit slug to clone")
	unitCreateCmd.Flags().StringVar(&unitCreateArgs.upstreamSpaceSlug, "upstream-space", "", "space slug of upstream unit to clone")
	unitCreateCmd.Flags().StringVar(&unitCreateArgs.importUnitSlug, "import", "", "source unit slug")
	// default to ToolchainKubernetesYAML
	unitCreateCmd.Flags().StringVarP(&unitCreateArgs.toolchainType, "toolchain", "t", string(workerapi.ToolchainKubernetesYAML), "toolchain type")
	unitCmd.AddCommand(unitCreateCmd)
}

func unitCreateCmdRun(cmd *cobra.Command, args []string) error {
	// Validate conflicting options - if 2nd arg is "-" (stdin for config), can't also read metadata from stdin
	if len(args) > 1 && args[1] == "-" && flagPopulateModelFromStdin {
		return errors.New("can't read both entity attributes and config data from stdin")
	}

	spaceID := uuid.MustParse(selectedSpaceID)
	newUnit := &goclientnew.Unit{}
	newParams := &goclientnew.CreateUnitParams{}
	if flagPopulateModelFromStdin {
		if err := populateNewModelFromStdin(newUnit); err != nil {
			return err
		}
	}

	// Handle config data from 2nd positional argument
	if len(args) > 1 {
		if unitCreateArgs.upstreamUnitSlug != "" {
			return errors.New("shouldn't specify both an upstream to clone and config data")
		}
		content, err := fetchContent(args[1])
		if err != nil {
			return fmt.Errorf("failed to read config: %w", err)
		}
		var base64Content strfmt.Base64 = content
		newUnit.Data = base64Content.String()
	}

	err := setLabels(&newUnit.Labels)
	if err != nil {
		return err
	}
	var upstreamSpaceID, upstreamUnitID uuid.UUID
	if unitCreateArgs.upstreamSpaceSlug != "" {
		upstreamSpace, err := apiGetSpaceFromSlug(unitCreateArgs.upstreamSpaceSlug)
		if err != nil {
			return err
		}
		upstreamSpaceID = upstreamSpace.SpaceID
	}
	if unitCreateArgs.upstreamUnitSlug != "" {
		if unitCreateArgs.upstreamSpaceSlug == "" {
			upstreamSpaceID = spaceID
		}
		upstreamUnit, err := apiGetUnitFromSlugInSpace(unitCreateArgs.upstreamUnitSlug, upstreamSpaceID.String())
		if err != nil {
			return err
		}
		upstreamUnitID = upstreamUnit.UnitID
	}
	if unitCreateArgs.targetSlug != "" {
		target, err := apiGetTargetFromSlug(unitCreateArgs.targetSlug, selectedSpaceID)
		if err != nil {
			return err
		}
		newUnit.TargetID = &target.Target.TargetID
	}

	// If these were set from stdin, they will be overridden
	newUnit.SpaceID = spaceID
	newUnit.Slug = makeSlug(args[0])
	if newUnit.DisplayName == "" {
		newUnit.DisplayName = args[0]
	}
	newUnit.ToolchainType = unitCreateArgs.toolchainType

	if unitCreateArgs.upstreamUnitSlug != "" {
		newParams.UpstreamSpaceId = &upstreamSpaceID
		newParams.UpstreamUnitId = &upstreamUnitID
	}


	unitRes, err := cubClientNew.CreateUnitWithResponse(ctx, spaceID, newParams, *newUnit)
	if IsAPIError(err, unitRes) {
		return InterpretErrorGeneric(err, unitRes)
	}

	unitDetails := unitRes.JSON200
	if wait {
		err = awaitTriggersRemoval(unitDetails)
		if err != nil {
			return err
		}
	}
	displayCreateResults(unitDetails, "unit", args[0], unitDetails.UnitID.String(), displayUnitDetails)
	return nil
}

func fetchContent(source string) ([]byte, error) {
	if source == "" {
		return nil, errors.New("source cannot be empty")
	}

	// Handle stdin
	if source == "-" {
		return readStdin()
	}

	// Handle file:// URLs
	if filePath, found := strings.CutPrefix(source, "file://"); found {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
		}
		return data, nil
	}

	// Handle HTTPS URLs only
	if strings.HasPrefix(source, "https://") {
		return fetchWithHTTP(source)
	}

	// Handle local files (backward compatibility - no prefix)
	if !strings.Contains(source, "://") {
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, err
		}
		return data, nil
	}

	return nil, fmt.Errorf("unsupported URL scheme: %s (only file:// and https:// are supported)", source)
}

func fetchWithHTTP(source string) ([]byte, error) {
	// Only allow HTTPS URLs for security
	if !strings.HasPrefix(source, "https://") {
		return nil, fmt.Errorf("only HTTPS URLs are supported, got: %s", source)
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make HTTP request
	resp, err := client.Get(source)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", source, err)
	}
	defer resp.Body.Close()

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch %s, Code: %d, Status: %s", source, resp.StatusCode, resp.Status)
	}

	// Read response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response from %s: %w", source, err)
	}

	return data, nil
}
