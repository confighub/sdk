// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var authSwitchCmd = &cobra.Command{
	Use:   "switch [organization]",
	Short: "Switch to a different organization",
	Long: getCommandHelp(`Switch to a different organization without going through the browser UI.

The organization argument can be a partial match of:

- Organization Display Name (e.g., "ConfigHub")
- Organization Slug (e.g., "id3f6c2a1e-9b4d-4e7a-8c52-1d0f7e9a6b43")
- Organization ID (ConfigHub UUID, e.g., "2af2356f-8587-4816-8619-77dfa85fb524")
- External ID (the identity provider's UUID, e.g., "3f6c2a1e-9b4d-4e7a-8c52-1d0f7e9a6b43")

Examples:
`+"```"+`
  # Switch to organization by display name
  cub auth switch "ConfigHub"

  # Switch to organization by slug
  cub auth switch "id3f6c2a1e-9b4d-4e7a-8c52-1d0f7e9a6b43"
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: authSwitchCmdRun,
}

func init() {
	authCmd.AddCommand(authSwitchCmd)
}

func authSwitchCmdRun(cmd *cobra.Command, args []string) error {
	return switchToOrganization(args[0])
}

// switchToOrganization switches to the specified organization
func switchToOrganization(searchTerm string) error {
	ctx := contextManager.ActiveContext()
	tokenData, err := contextManager.LoadTokenData(ctx)
	if err != nil {
		return fmt.Errorf("failed to load tokens for current context: %w", err)
	}

	if tokenData.RefreshToken == "" {
		return fmt.Errorf("no refresh token found. Please run 'cub auth login' first")
	}

	// Get list of organizations to find the matching one
	organizations, err := apiListOrganizations("", "*", "")
	if err != nil {
		return fmt.Errorf("failed to list organizations: %w", err)
	}

	// Find the best matching organization
	matchedOrg := findBestMatchingOrganization(organizations, searchTerm)
	if matchedOrg == nil {
		return fmt.Errorf("no organization found matching '%s'", searchTerm)
	}

	// Check if we're already in the correct organization
	if ctx.Coordinate.OrganizationID == matchedOrg.ExternalID {
		fmt.Printf("Already in organization: %s (%s)\n", matchedOrg.DisplayName, matchedOrg.ExternalID)
		return nil
	}

	// Call the switch organization API
	newTokens, err := callSwitchOrganizationAPI(tokenData.AccessToken, tokenData.RefreshToken, matchedOrg.ExternalID)
	if err != nil {
		return fmt.Errorf("failed to switch organization: %w. Try running 'cub auth login' to re-authenticate first", err)
	}

	newCoordinate := Coordinate{
		ServerURL:      ctx.Coordinate.ServerURL,
		OrganizationID: matchedOrg.ExternalID,
		User:           ctx.Coordinate.User,
	}

	// An explicit override (--context or $CUB_CONTEXT) names the context to
	// operate on, so switch it in place rather than handing off to (or creating)
	// another context, and leave the persisted current context alone.
	if activeContextOverrideSource != "" {
		ctx.Coordinate = newCoordinate
	} else {
		ctx, err = contextManager.FindContextByCoordinate(newCoordinate)
		if err != nil {
			ctx = contextManager.NewContext()
			ctx.Coordinate = newCoordinate
			tokenData = &TokenData{}
		}
		if err := contextManager.SetCurrentContext(ctx.Name); err != nil {
			return err
		}
		// Make the handed-off context the active one so the rest of the flow
		// reads and writes the context that just received the new tokens.
		if err := contextManager.OverrideCurrentContext(ctx.Name); err != nil {
			return err
		}
	}
	// We set this even if it might already been set.
	// It could have been changed on the server or failed to set previously.
	ctx.Metadata.OrganizationName = matchedOrg.DisplayName

	tokenData.AccessToken = newTokens.AccessToken
	tokenData.RefreshToken = newTokens.RefreshToken

	// Save updated tokens
	err = contextManager.SaveTokenData(ctx, tokenData)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	// Update the API client
	cubClientNew, err = InitializeClient(ctx)
	if err != nil {
		return fmt.Errorf("error initializing client: %w", err)
	}

	// Save the configuration
	if err := contextManager.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save context configuration: %w", err)
	}

	fmt.Printf("Successfully switched to organization: %s (%s)\n", matchedOrg.DisplayName, matchedOrg.ExternalID)
	return nil
}

// findBestMatchingOrganization finds the organization that best matches the search term
func findBestMatchingOrganization(organizations []*goclientnew.Organization, searchTerm string) *goclientnew.Organization {
	searchLower := strings.ToLower(searchTerm)

	// First, try exact matches
	for _, org := range organizations {
		if strings.ToLower(org.DisplayName) == searchLower ||
			strings.ToLower(org.Slug) == searchLower ||
			strings.ToLower(org.OrganizationID.String()) == searchLower ||
			strings.ToLower(org.ExternalID) == searchLower {
			return org
		}
	}

	// Then try partial matches
	for _, org := range organizations {
		if strings.Contains(strings.ToLower(org.DisplayName), searchLower) ||
			strings.Contains(strings.ToLower(org.Slug), searchLower) ||
			strings.Contains(strings.ToLower(org.OrganizationID.String()), searchLower) ||
			strings.Contains(strings.ToLower(org.ExternalID), searchLower) {
			return org
		}
	}

	return nil
}

// switchOrganizationRequest is the body of POST /auth/refresh. An organization id
// asks the server to mint for that organization instead of the current one; it
// verifies membership itself, because Keycloak does not honour an organization
// selection on a refresh_token grant before 26.6.
type switchOrganizationRequest struct {
	RefreshToken   string `json:"refresh_token"`
	OrganizationID string `json:"organization_id"`
}

// switchOrganizationResponse is the pair of tokens the refresh returns.
type switchOrganizationResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// callSwitchOrganizationAPI mints a session for another organization through
// POST /auth/refresh.
//
// The endpoint authenticates the caller twice over: the refresh token proves the
// identity provider session, and the current ConfigHub token in the Authorization
// header proves which session is asking. Both are already in the context, so this
// needs nothing the CLI does not hold.
func callSwitchOrganizationAPI(accessToken, refreshToken, organizationID string) (*switchOrganizationResponse, error) {
	body, err := json.Marshal(switchOrganizationRequest{
		RefreshToken:   refreshToken,
		OrganizationID: organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	refreshURL := strings.TrimSuffix(contextManager.ActiveContext().Coordinate.ServerURL, "/api") + "/auth/refresh"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, refreshURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// The body carries the server's reason -- not a member of that
		// organization, an expired refresh token -- which is worth more than the
		// status alone.
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	var tokens switchOrganizationResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, fmt.Errorf("failed to read the new session: %w", err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		return nil, fmt.Errorf("the server returned no session for that organization")
	}
	return &tokens, nil
}
