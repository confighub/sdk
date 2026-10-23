// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
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
	organizations, err := cubapi.ListOrganizations(context.Background(), cubClient, cubapi.Where{}, cubapi.ListOpts{})
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
func findBestMatchingOrganization(extendedOrganizations []*goclientnew.ExtendedOrganization, searchTerm string) *goclientnew.Organization {
	searchLower := strings.ToLower(searchTerm)
	organizations := make([]*goclientnew.Organization, 0, len(extendedOrganizations))
	for _, extendedOrganization := range extendedOrganizations {
		if extendedOrganization.Organization != nil {
			organizations = append(organizations, extendedOrganization.Organization)
		}
	}

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

// callSwitchOrganizationAPI mints a session for another organization through
// POST /auth/refresh. The refresh token proves the identity provider session;
// the server verifies the user's membership in the organization itself, because
// a refresh_token grant keeps the organization Keycloak selected at login.
func callSwitchOrganizationAPI(accessToken, refreshToken, organizationID string) (*cubapi.AuthSession, error) {
	session, err := cubapi.RefreshSession(context.Background(), nil,
		contextManager.ActiveContext().Coordinate.ServerURL, refreshToken, accessToken, organizationID)
	if err != nil {
		return nil, err
	}
	if session.RefreshToken == "" {
		return nil, fmt.Errorf("the server returned no session for that organization")
	}
	return session, nil
}
