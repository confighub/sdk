// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/skratchdot/open-golang/open"
	"github.com/spf13/cobra"
)

var (
	asWorker               bool
	newContext             bool
	serverURL              string
	organizationSearchTerm string
	noBrowser              bool
)

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log into ConfigHub",
	Long: `Authenticate the CLI to ConfigHub via Browser Login

Examples:
  # Login (creates or updates current context)
  cub auth login
  
  # Login to a specific server
  cub auth login --server https://hub.confighub.com
  
  # Login to a specific existing context
  cub --context prod2 auth login
  
  # Create a new context and login with a specific server
  cub auth login --new-context --server https://hub.confighub.com
  
  # Login as a worker using environment variables
  cub auth login --as-worker
  
  # Login without automatically opening browser
  cub auth login --no-browser`,
	Args: cobra.NoArgs,
	RunE: authLoginCmdRun,
}

func init() {
	authLoginCmd.Flags().BoolVar(&asWorker, "as-worker", false, "Authenticate as a worker using CONFIGHUB_WORKER_ID and CONFIGHUB_WORKER_SECRET environment variables")
	authLoginCmd.Flags().StringVar(&serverURL, "server", "", "Server URL to authenticate to (e.g., https://hub.confighub.com)")
	authLoginCmd.Flags().StringVar(&organizationSearchTerm, "organization", "", "Organization partial name or ID to authenticate to (e.g., Fuz or org_1234567890)")
	authLoginCmd.Flags().BoolVar(&newContext, "new-context", false, "Create a new context during login")
	authLoginCmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not automatically open browser for authentication")
	authCmd.AddCommand(authLoginCmd)
}

func authLoginCmdRun(cmd *cobra.Command, args []string) error {
	coordinate := Coordinate{}
	coordinate.OrganizationID = contextManager.ActiveContext().Coordinate.OrganizationID
	// check against this after login to see if we need to switch organization
	// Strictly speaking we can just use coordinate.OrganizationID here because
	// we pass a copy of it to the updateContextFromSession function and this one is not updated
	// but that might be misunderstood by future code readers.
	oldOrganizationID := coordinate.OrganizationID

	if os.Getenv("CONFIGHUB_URL") != "" {
		if serverURL != "" {
			return fmt.Errorf("cannot use both --server and CONFIGHUB_URL environment variable")
		}
		coordinate.ServerURL = os.Getenv("CONFIGHUB_URL")
	} else if serverURL != "" {
		coordinate.ServerURL = serverURL
	} else {
		coordinate.ServerURL = contextManager.ActiveContext().Coordinate.ServerURL
	}

	var err error
	var session *AuthSession
	if asWorker {
		// Handle worker authentication
		session, err = performWorkerAuth(coordinate)
	} else {
		session, err = performUserAuth(coordinate, noBrowser)
	}
	if err != nil {
		return err
	}

	// Update context with authentication results
	if err := updateContextFromSession(coordinate, session); err != nil {
		return fmt.Errorf("failed to update context: %w", err)
	}

	if organizationSearchTerm != "" {
		switchToOrganization(organizationSearchTerm)
	} else {
		if oldOrganizationID != "" && oldOrganizationID != contextManager.ActiveContext().Coordinate.OrganizationID {
			switchToOrganization(oldOrganizationID)
		}
	}
	displayContextDetails(contextManager.ActiveContext())

	// Preload builtin functions
	if _, _, err := listAndSaveFunctions("", "", ""); err != nil {
		return err
	}

	return nil
}

// performUserAuth handles the PKCE device login authentication flow
func performUserAuth(coordinate Coordinate, noBrowser bool) (*AuthSession, error) {
	tprint("Authenticating to %s", coordinate.ServerURL)
	tprint("Starting device login flow...")

	// Get client ID and device URLs from server
	apiInfo, err := getApiInfo(coordinate)
	if err != nil {
		return nil, fmt.Errorf("failed to get API info: %w", err)
	}

	// Generate PKCE code verifier and challenge
	codeVerifier := generateCodeVerifier()
	codeChallenge := generateCodeChallenge(codeVerifier)

	// Get device login information directly from WorkOS
	deviceInfo, err := getDeviceLoginInfoFromWorkOS(apiInfo.ClientID, codeChallenge, apiInfo.DeviceAuthURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get device login info: %w", err)
	}

	// Display the verification code to the user
	tprint("")
	if noBrowser {
		tprint("Visit this URL to authenticate:")
		tprint("%s", deviceInfo.VerificationURLComplete)
		tprint("")
		tprint("Or manually enter this code: %s", deviceInfo.UserCode)
		tprint("")
		tprint("Waiting for authentication...")
	} else {
		tprint("Opening browser for authentication...")
		tprint("If the browser doesn't open automatically, visit this URL:")
		tprint("%s", deviceInfo.VerificationURLComplete)
		tprint("")
		tprint("Or manually enter this code: %s", deviceInfo.UserCode)
		tprint("")
		tprint("Waiting for authentication...")

		// Open browser automatically
		if err := open.Start(deviceInfo.VerificationURLComplete); err != nil {
			tprint("Warning: Could not open browser automatically")
		}
	}

	// Poll for token exchange directly with WorkOS
	session, err := pollForDeviceTokenWithWorkOS(apiInfo.ClientID, deviceInfo.DeviceCode, codeVerifier, apiInfo.DeviceTokenURL, deviceInfo.Interval, deviceInfo.ExpiresIn)
	if err != nil {
		return nil, fmt.Errorf("device authentication failed: %w", err)
	}

	tprint("Authentication successful")
	session.AuthType = AuthTypeJWT

	return session, nil
}

// performWorkerAuth handles worker authentication
func performWorkerAuth(coordinate Coordinate) (*AuthSession, error) {
	workerID := os.Getenv("CONFIGHUB_WORKER_ID")
	workerSecret := os.Getenv("CONFIGHUB_WORKER_SECRET")

	if workerID == "" || workerSecret == "" {
		return nil, fmt.Errorf("CONFIGHUB_WORKER_ID and CONFIGHUB_WORKER_SECRET environment variables must be set")
	}

	// Create request body
	requestBody := map[string]string{
		"worker_id":     workerID,
		"worker_secret": workerSecret,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Make authentication request
	endpoint := fmt.Sprintf("%s/auth/worker", coordinate.ServerURL)
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make authentication request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("authentication failed: %s", string(body))
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	session := AuthSession{}
	session.AuthType = "Bearer"

	if err := json.Unmarshal(body, &session); err != nil {
		return nil, fmt.Errorf("failed to parse authentication response: %w", err)
	}

	tprint("Successfully logged in as worker %s (Organization: %s)", workerID, session.OrganizationID)
	return &session, nil
}

func updateContextFromSession(coordinate Coordinate, session *AuthSession) error {
	coordinate.User = session.User.Email
	coordinate.OrganizationID = session.OrganizationID

	ctx := contextManager.ActiveContext()
	// Only consider other contexts if active context doesn't have a user and org.
	if !(ctx.Coordinate.User == "" && ctx.Coordinate.OrganizationID == "") {
		// Did the user log in to a different coordinate?
		if !ctx.Coordinate.Equals(coordinate) {
			// Check if we have a context for this coordinate and then use it.
			var err error
			ctx, err = contextManager.FindContextByCoordinate(coordinate)
			if err != nil {
				// If no context is found, create a new context and set the coordinate.
				ctx = contextManager.NewContext()
			}
		}
	}
	ctx.Coordinate = coordinate
	contextManager.SetCurrentContext(ctx.Name)

	// Save tokens
	token := &TokenData{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
	}

	if err := contextManager.SaveTokenData(ctx, token); err != nil {
		return err
	}

	// Reinitialize the API client in case any API calls are made after this point
	var err error
	cubClientNew, err = InitializeClient(ctx)
	if err != nil {
		return fmt.Errorf("error initializing client: %w", err)
	}

	// Get or refresh organization name
	org, err := apiGetOrganizationFromExternalID(ctx.Coordinate.OrganizationID)
	if err != nil {
		tprint("Warning: Failed to get organization display name for org %s: %s", ctx.Coordinate.OrganizationID, err)
		ctx.Metadata.OrganizationName = OrgNameLookupFailure
	} else {
		ctx.Metadata.OrganizationName = org.DisplayName
	}

	// Sets selectedSpaceID and selectedSpaceSlug to valid values and updates default space in context if it doesn't exist
	err = setSpaceContext()
	if err != nil {
		return err
	}
	err = contextManager.SaveConfig()
	if err != nil {
		return fmt.Errorf("failed to save context: %w", err)
	}
	return nil
}

// Helper functions
// ================

// getApiInfo gets the API info from the server
func getApiInfo(coordinate Coordinate) (*goclientnew.ApiInfo, error) {
	// creating new api client because the endpoint may be new
	apiclient, err := goclientnew.NewClientWithResponses(coordinate.ServerURL + "/api")
	if err != nil {
		return nil, err
	}
	apiinfo, err := apiclient.ApiInfoWithResponse(ctx)
	if IsAPIError(err, apiinfo) {
		return nil, InterpretErrorGeneric(err, apiinfo)
	}
	if apiinfo.JSON200 == nil {
		return nil, fmt.Errorf("API info not available from server")
	}
	return apiinfo.JSON200, nil
}

// generateCodeVerifier generates a PKCE code verifier
func generateCodeVerifier() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	result := make([]byte, 128)
	for i := range result {
		result[i] = chars[time.Now().UnixNano()%int64(len(chars))]
	}
	return string(result)
}

// generateCodeChallenge generates a PKCE code challenge from the verifier
func generateCodeChallenge(codeVerifier string) string {
	// For simplicity, we'll use the plain method (S256 would require crypto/sha256)
	// In production, you should use S256 method
	return codeVerifier
}

// DeviceLoginInfo represents the response from WorkOS device login
type DeviceLoginInfo struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURL         string `json:"verification_uri"`
	VerificationURLComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// getDeviceLoginInfoFromWorkOS gets device login information directly from WorkOS
func getDeviceLoginInfoFromWorkOS(clientID, codeChallenge, deviceAuthURL string) (*DeviceLoginInfo, error) {
	// Call WorkOS device authorization endpoint
	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "plain") // Using plain for simplicity
	// Make POST request with form data
	req, err := http.NewRequest("POST", deviceAuthURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create device authorization request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call WorkOS device authorization: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("WorkOS device authorization failed: %s", string(body))
	}

	var deviceInfo DeviceLoginInfo
	if err := json.NewDecoder(resp.Body).Decode(&deviceInfo); err != nil {
		return nil, fmt.Errorf("failed to decode WorkOS response: %w", err)
	}

	return &deviceInfo, nil
}

// pollForDeviceTokenWithWorkOS polls WorkOS directly for token exchange
func pollForDeviceTokenWithWorkOS(clientID, deviceCode, codeVerifier, tokenURL string, interval, expiresIn int) (*AuthSession, error) {
	startTime := time.Now()
	expiryTime := startTime.Add(time.Duration(expiresIn) * time.Second)
	pollInterval := time.Duration(interval) * time.Second

	for time.Now().Before(expiryTime) {
		// Try to exchange device code for tokens directly with WorkOS
		session, err := exchangeDeviceCodeForTokensWithWorkOS(clientID, deviceCode, codeVerifier, tokenURL)
		if err == nil {
			return session, nil
		}

		// Handle different error types
		switch {
		case strings.Contains(err.Error(), "authorization_pending"):
			// Continue polling
		case strings.Contains(err.Error(), "slow_down"):
			// Increase polling interval (double it)
			pollInterval = pollInterval * 2
			tprint("Rate limited, slowing down polling...")
		case strings.Contains(err.Error(), "access_denied"):
			return nil, fmt.Errorf("authentication was denied: %w", err)
		case strings.Contains(err.Error(), "expired_token"):
			return nil, fmt.Errorf("authentication token expired: %w", err)
		default:
			return nil, err
		}

		// Wait before next poll
		time.Sleep(pollInterval)
	}

	return nil, fmt.Errorf("device authentication timed out")
}

// exchangeDeviceCodeForTokensWithWorkOS exchanges the device code for tokens directly with WorkOS
func exchangeDeviceCodeForTokensWithWorkOS(clientID, deviceCode, codeVerifier, tokenURL string) (*AuthSession, error) {
	// Call WorkOS token endpoint
	params := url.Values{}
	params.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	params.Set("device_code", deviceCode)
	params.Set("client_id", clientID)
	params.Set("code_verifier", codeVerifier)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call WorkOS token endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		// Parse error response
		var errorResp struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		if err := json.Unmarshal(body, &errorResp); err == nil {
			switch errorResp.Error {
			case "authorization_pending":
				return nil, fmt.Errorf("authorization_pending")
			case "slow_down":
				return nil, fmt.Errorf("slow_down")
			case "access_denied":
				return nil, fmt.Errorf("access_denied: %s", errorResp.Description)
			case "expired_token":
				return nil, fmt.Errorf("expired_token: %s", errorResp.Description)
			default:
				return nil, fmt.Errorf("token exchange failed: %s - %s", errorResp.Error, errorResp.Description)
			}
		}

		return nil, fmt.Errorf("token exchange failed: %s", string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	// tprint("Token exchange response: %s", string(body))
	// Parse WorkOS token response
	var tokenResp struct {
		AccessToken    string `json:"access_token"`
		RefreshToken   string `json:"refresh_token"`
		OrganizationID string `json:"organization_id"`
		User           User   `json:"user"`
	}

	err = json.Unmarshal(body, &tokenResp)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal token response: %w", err)
	}

	// Convert to AuthSession
	session := &AuthSession{
		AccessToken:    tokenResp.AccessToken,
		RefreshToken:   tokenResp.RefreshToken,
		OrganizationID: tokenResp.OrganizationID,
		User:           tokenResp.User,
		AuthType:       AuthTypeJWT,
	}

	return session, nil
}

// parseJWTToken parses a JWT token and returns the payload as JSON
func parseJWTToken(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid JWT token format")
	}

	// Decode the payload (second part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	// Pretty print the JSON
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, payload, "", "  "); err != nil {
		return "", fmt.Errorf("failed to format JSON: %w", err)
	}

	return prettyJSON.String(), nil
}

func performLogout(logoutURL string, accessToken string) error {
	client := &http.Client{}
	// Set the Authorization header
	req, err := http.NewRequest("GET", logoutURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tprint("Warning: Failed to log out. Status: %d", resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		tprint("Response: %v", string(body))
	}
	return nil
}

// setSpaceContext ensures the space context is properly set
func setSpaceContext() error {
	ctx := contextManager.ActiveContext()

	currentSpace, err := apiGetSpaceFromSlug(ctx.Settings.DefaultSpace, "")
	if err != nil {
		spaceList, err := apiListSpaces("", "")
		if err != nil {
			return err
		}
		if len(spaceList) == 0 {
			return fmt.Errorf("no spaces found. Current space could not be set")
		}
		// Just pick the first one
		tprint("Default space from context, %s not found in org. Using %s instead", ctx.Settings.DefaultSpace, spaceList[0].Slug)
		currentSpace = spaceList[0]
		// Update context. This will not be persisted until SaveConfig is called.
		ctx.Settings.DefaultSpace = currentSpace.Slug
	}
	selectedSpaceID = currentSpace.SpaceID.String()
	selectedSpaceSlug = currentSpace.Slug
	return nil
}
