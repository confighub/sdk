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

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
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
	Long: getCommandHelp(`Authenticate the CLI to ConfigHub via Browser Login

By default the credentials are stored in the context matching what you logged
into, which may be a different context than the current one (or a new one) if you
authenticated as another user or organization. When a context is named explicitly
with --context or CUB_CONTEXT, that context is the one updated and it does not
become the current context.

Examples:
`+"```"+`
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
  cub auth login --no-browser
`+"```"+`
`, ""),
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
	if newContext && globalContextFlag != "" {
		return fmt.Errorf("cannot use both --new-context and --context: --new-context creates a context, --context selects an existing one")
	}

	coordinate := Coordinate{}
	// A brand-new context has no organization to preserve, so don't seed one from
	// the context that happens to be active; the login lands wherever it lands.
	if !newContext {
		coordinate.OrganizationID = contextManager.ActiveContext().Coordinate.OrganizationID
	}
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
	var session *cubapi.AuthSession
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
	if _, _, err := listAndMaybeSaveFunctions("", "", "", ""); err != nil {
		return err
	}

	return nil
}

// performUserAuth handles the PKCE device login authentication flow
func performUserAuth(coordinate Coordinate, noBrowser bool) (*cubapi.AuthSession, error) {
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

	// Get device login information from the identity provider
	deviceInfo, err := getDeviceLoginInfo(apiInfo.ClientID, codeChallenge, apiInfo.DeviceAuthURL)
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

	// Poll for token exchange
	session, err := pollForDeviceToken(apiInfo.ClientID, deviceInfo.DeviceCode, codeVerifier, apiInfo.DeviceTokenURL, deviceInfo.Interval, deviceInfo.ExpiresIn)
	if err != nil {
		return nil, fmt.Errorf("device authentication failed: %w", err)
	}

	tprint("Authentication successful")
	session.AuthType = cubapi.AuthTypeJWT

	return session, nil
}

// performWorkerAuth handles worker authentication
func performWorkerAuth(coordinate Coordinate) (*cubapi.AuthSession, error) {
	workerID := os.Getenv("CONFIGHUB_WORKER_ID")
	workerSecret := os.Getenv("CONFIGHUB_WORKER_SECRET")
	if workerID == "" || workerSecret == "" {
		return nil, fmt.Errorf("CONFIGHUB_WORKER_ID and CONFIGHUB_WORKER_SECRET environment variables must be set")
	}

	session, err := cubapi.PerformWorkerAuth(coordinate.ServerURL, workerID, workerSecret)
	if err != nil {
		return nil, err
	}
	tprint("Successfully logged in as worker %s (Organization: %s)", workerID, session.OrganizationID)
	return session, nil
}

// loginTargetContext returns the context a successful login must be written to.
//
// An explicit override (--context or $CUB_CONTEXT) names the context to log
// into, so it is used verbatim: silently re-targeting a different context would
// leave the named one without the new token, which is precisely what the user
// asked for. The persisted current context is left alone in that case, because
// an override is not supposed to mutate shared config.
//
// Without an override, login follows the coordinate: an already-authenticated
// context whose coordinate no longer matches hands off to the context for the
// new coordinate (creating one if none exists), and that context becomes
// current. --new-context always logs into a freshly created context; being an
// explicit flag it outranks $CUB_CONTEXT, and combining it with --context is
// rejected up front rather than silently ignoring one of the two.
func loginTargetContext(coordinate Coordinate) (*Context, error) {
	if activeContextOverrideSource != "" && !newContext {
		return contextManager.ActiveContext(), nil
	}

	ctx := contextManager.ActiveContext()
	switch {
	case newContext:
		ctx = contextManager.NewContext()
	// Only consider other contexts if active context doesn't have a user and org.
	case ctx.Coordinate.User != "" || ctx.Coordinate.OrganizationID != "":
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
	if err := contextManager.SetCurrentContext(ctx.Name); err != nil {
		return nil, err
	}
	return ctx, nil
}

func updateContextFromSession(coordinate Coordinate, session *cubapi.AuthSession) error {
	coordinate.User = session.User.Email
	coordinate.OrganizationID = session.OrganizationID

	ctx, err := loginTargetContext(coordinate)
	if err != nil {
		return err
	}
	ctx.Coordinate = coordinate
	// Point the active context at the login target so the rest of the flow
	// (setSpaceContext, the org switch-back in authLoginCmdRun, the summary we
	// print) reads and writes the context that just received the token, whether
	// that context came from an override or from coordinate matching.
	if err := contextManager.OverrideCurrentContext(ctx.Name); err != nil {
		return err
	}

	// Save tokens
	token := &TokenData{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
	}

	if err := contextManager.SaveTokenData(ctx, token); err != nil {
		return err
	}

	// Reinitialize the API client in case any API calls are made after this point
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
	if cubapi.IsAPIError(err, apiinfo) {
		return nil, cubapi.InterpretErrorGeneric(err, apiinfo)
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

// DeviceLoginInfo represents the response from the identity provider's device authorization endpoint
type DeviceLoginInfo struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURL         string `json:"verification_uri"`
	VerificationURLComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// getDeviceLoginInfo requests device authorization from the identity provider
func getDeviceLoginInfo(clientID, codeChallenge, deviceAuthURL string) (*DeviceLoginInfo, error) {
	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "plain")
	params.Set("scope", "openid email")

	req, err := http.NewRequest(http.MethodPost, deviceAuthURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create device authorization request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device authorization request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device authorization failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var deviceInfo DeviceLoginInfo
	if err := json.NewDecoder(resp.Body).Decode(&deviceInfo); err != nil {
		return nil, fmt.Errorf("failed to decode device authorization response: %w", err)
	}

	return &deviceInfo, nil
}

// pollForDeviceToken polls the identity provider's token endpoint until the device is authorized
func pollForDeviceToken(clientID, deviceCode, codeVerifier, tokenURL string, interval, expiresIn int) (*cubapi.AuthSession, error) {
	startTime := time.Now()
	expiryTime := startTime.Add(time.Duration(expiresIn) * time.Second)
	pollInterval := time.Duration(interval) * time.Second

	for time.Now().Before(expiryTime) {
		session, err := exchangeDeviceCodeForTokens(clientID, deviceCode, codeVerifier, tokenURL)
		if err == nil {
			return session, nil
		}

		switch {
		case strings.Contains(err.Error(), "authorization_pending"):
			// Continue polling
		case strings.Contains(err.Error(), "slow_down"):
			// Increase polling interval (add 5 seconds per RFC 8628)
			pollInterval = pollInterval + 5*time.Second
			tprint("Rate limited, slowing down polling...")
		case strings.Contains(err.Error(), "access_denied"):
			return nil, fmt.Errorf("authentication was denied: %w", err)
		case strings.Contains(err.Error(), "expired_token"):
			return nil, fmt.Errorf("authentication token expired: %w", err)
		default:
			return nil, err
		}

		time.Sleep(pollInterval)
	}

	return nil, fmt.Errorf("device authentication timed out")
}

// tokenResponse represents the token response from an OAuth2 identity provider.
// Some providers include user and organization info inline; others require JWT extraction.
type tokenResponse struct {
	AccessToken    string      `json:"access_token"`
	RefreshToken   string      `json:"refresh_token"`
	OrganizationID string      `json:"organization_id"`
	User           cubapi.User `json:"user"`
	IDToken        string      `json:"id_token"`
	TokenType      string      `json:"token_type"`
	ExpiresIn      int         `json:"expires_in"`
	Scope          string      `json:"scope"`
}

// exchangeDeviceCodeForTokens exchanges the device code for tokens
func exchangeDeviceCodeForTokens(clientID, deviceCode, codeVerifier, tokenURL string) (*cubapi.AuthSession, error) {
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
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
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

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token response: %w", err)
	}

	session := &cubapi.AuthSession{
		AccessToken:    tokenResp.AccessToken,
		RefreshToken:   tokenResp.RefreshToken,
		OrganizationID: tokenResp.OrganizationID,
		User:           tokenResp.User,
		AuthType:       cubapi.AuthTypeJWT,
	}

	// If user info was not provided inline in the response, extract from the JWT access token
	if session.User.Email == "" {
		user, orgID, err := extractUserInfoFromJWT(tokenResp.AccessToken)
		if err == nil {
			session.User = user
			if session.OrganizationID == "" {
				session.OrganizationID = orgID
			}
		}
	}

	return session, nil
}

// extractUserInfoFromJWT extracts user information from a JWT access token's claims.
// This is used as a fallback when the token response does not include inline user/org info.
func extractUserInfoFromJWT(accessToken string) (cubapi.User, string, error) {
	var (
		user  cubapi.User
		orgID string
	)

	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return user, "", fmt.Errorf("invalid JWT token format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return user, "", fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var claims struct {
		Sub               string         `json:"sub"`
		Email             string         `json:"email"`
		PreferredUsername string         `json:"preferred_username"`
		GivenName         string         `json:"given_name"`
		FamilyName        string         `json:"family_name"`
		Name              string         `json:"name"`
		OrgID             string         `json:"org_id"`
		Organization      map[string]any `json:"organization"`
	}

	if err := json.Unmarshal(payload, &claims); err != nil {
		return user, "", fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	user.Email = claims.Email
	user.FirstName = claims.GivenName
	user.LastName = claims.FamilyName
	if claims.OrgID != "" {
		orgID = claims.OrgID
	} else {
		for _, v := range claims.Organization {
			if childKV, ok := v.(map[string]any); ok {
				if idAny, ok := childKV["id"]; ok {
					orgID = idAny.(string)
					break
				}
			}
		}
	}

	return user, orgID, nil
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

	// A wildcard ("*") or empty default space selects cross-space (org-level)
	// operations by default; there is no space to resolve, so honor it directly.
	if ctx.Settings.DefaultSpace == "*" || ctx.Settings.DefaultSpace == "" {
		selectedSpaceID = ctx.Settings.DefaultSpace
		selectedSpaceSlug = ctx.Settings.DefaultSpace
		return nil
	}

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
