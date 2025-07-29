// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var authSwitchCmd = &cobra.Command{
	Use:   "switch [organization]",
	Short: "Switch to a different organization",
	Long: `Switch to a different organization without going through the browser UI.
	
The organization argument can be a partial match of:
- Organization Display Name (e.g., "ConfigHub")
- Organization Slug (e.g., "org_01jsqq70m483a3b1fk3zfs9z1a")
- Organization ID (ConfigHub UUID, e.g., "2af2356f-8587-4816-8619-77dfa85fb524")
- External ID (WorkOS ID, e.g., "org_01JSQQ70M483A3B1FK3ZFS9Z1A")

Examples:
  # Switch to organization by display name
  cub auth switch "ConfigHub"
  
  # Switch to organization by slug
  cub auth switch "org_01jsqq70m483a3b1fk3zfs9z1a"`,
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
	// Load current session to get refresh token
	session, err := LoadSession()
	if err != nil {
		return fmt.Errorf("failed to load session: %w", err)
	}

	if session.RefreshToken == "" {
		return fmt.Errorf("no refresh token found. Please run 'cub auth login' first")
	}

	// Get list of organizations to find the matching one
	organizations, err := apiListOrganizations("")
	if err != nil {
		return fmt.Errorf("failed to list organizations: %w", err)
	}

	// Find the best matching organization
	matchedOrg := findBestMatchingOrganization(organizations, searchTerm)
	if matchedOrg == nil {
		return fmt.Errorf("no organization found matching '%s'", searchTerm)
	}

	// Check if we're already in the correct organization
	if session.OrganizationID == matchedOrg.ExternalID {
		fmt.Printf("Already in organization: %s (%s)\n", matchedOrg.DisplayName, matchedOrg.ExternalID)
		return nil
	}

	// Call the switch organization API
	newTokens, err := callSwitchOrganizationAPI(session.RefreshToken, matchedOrg.ExternalID)
	if err != nil {
		return fmt.Errorf("failed to switch organization: %w", err)
	}

	// Update session with new tokens
	session.AccessToken = newTokens.AccessToken
	session.RefreshToken = newTokens.RefreshToken
	session.OrganizationID = matchedOrg.ExternalID

	// Save updated session
	err = SaveSession(session)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	// Update the global context and auth header with the new session
	authSession = session
	authHeader = setAuthHeader(&authSession)
	cubClientNew, err = initializeClient()
	if err != nil {
		return fmt.Errorf("error initializing client: %w", err)
	}

	// Set the space context after switching organizations
	err = setSpaceContext()
	if err != nil {
		return fmt.Errorf("failed to set space context: %w", err)
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

// SwitchOrganizationResponse represents the response from the switch organization API
type SwitchOrganizationResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// callSwitchOrganizationAPI calls the /auth/switch-organization endpoint
func callSwitchOrganizationAPI(refreshToken, organizationID string) (*SwitchOrganizationResponse, error) {
	// Construct the URL
	switchURL := fmt.Sprintf("%s/auth/switch-organization?organization_id=%s",
		strings.TrimSuffix(cubContext.ConfigHubURL, "/api"),
		url.QueryEscape(organizationID))

	// Create the HTTP request
	req, err := http.NewRequestWithContext(context.Background(), "GET", switchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add the refresh token as a cookie
	req.AddCookie(&http.Cookie{
		Name:  "confighub_refresh_token",
		Value: refreshToken,
	})

	// Create client that doesn't follow redirects automatically
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// Check for redirect status codes (3xx)
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API request failed with status %d", resp.StatusCode)
	}

	// Extract tokens from the response cookies
	var newAccessToken, newRefreshToken string

	for _, cookie := range resp.Cookies() {
		switch cookie.Name {
		case "confighub_session":
			newAccessToken = cookie.Value
		case "confighub_refresh_token":
			newRefreshToken = cookie.Value
		}
	}

	if newAccessToken == "" || newRefreshToken == "" {
		return nil, fmt.Errorf("failed to get new tokens from response cookies")
	}

	return &SwitchOrganizationResponse{
		AccessToken:  newAccessToken,
		RefreshToken: newRefreshToken,
	}, nil
}
