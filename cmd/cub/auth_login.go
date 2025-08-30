// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/skratchdot/open-golang/open"
	"github.com/spf13/cobra"
)

var (
	asWorker               bool
	newContext             bool
	serverURL              string
	organizationSearchTerm string
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
  cub auth login --as-worker`,
	Args: cobra.NoArgs,
	RunE: authLoginCmdRun,
}

func init() {
	authLoginCmd.Flags().BoolVar(&asWorker, "as-worker", false, "Authenticate as a worker using CONFIGHUB_WORKER_ID and CONFIGHUB_WORKER_SECRET environment variables")
	authLoginCmd.Flags().StringVar(&serverURL, "server", "", "Server URL to authenticate to (e.g., https://hub.confighub.com)")
	authLoginCmd.Flags().StringVar(&organizationSearchTerm, "organization", "", "Organization partial name or ID to authenticate to (e.g., Fuz or org_1234567890)")
	authLoginCmd.Flags().BoolVar(&newContext, "new-context", false, "Create a new context during login")
	authCmd.AddCommand(authLoginCmd)
}

func authLoginCmdRun(cmd *cobra.Command, args []string) error {
	if organizationSearchTerm != "" {
		return switchToOrganization(organizationSearchTerm)
	}

	coordinate := Coordinate{}
	coordinate.OrganizationID = contextManager.ActiveContext().Coordinate.OrganizationID

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
		if err != nil {
			return err
		}
	} else {
		session, err = performUserAuth(coordinate)
		if err != nil {
			return err
		}
	}

	// Update context with authentication results
	if err := updateContextFromSession(coordinate, session); err != nil {
		return fmt.Errorf("failed to update context: %w", err)
	}

	// Preload builtin functions
	if _, _, err := listAndSaveFunctions("", "", ""); err != nil {
		return err
	}

	return nil
}

// performUserAuth handles the OAuth2 authentication flow
func performUserAuth(coordinate Coordinate) (*AuthSession, error) {

	// Get the authorization URL from ConfigHub
	authURL, err := getAuthorizationURL(LoginURL(coordinate))
	if err != nil {
		return nil, fmt.Errorf("failed to get authorization URL: %w", err)
	}

	// Set up OAuth callback handler channels
	sessionReceived := make(chan *AuthSession, 1)
	callbackError := make(chan error, 1)

	server := &http.Server{Addr: ":3000"}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Get the authorization code
		code := r.URL.Query().Get("code")
		if code == "" {
			callbackError <- fmt.Errorf("no authorization code received")
			io.WriteString(w, "Error: No authorization code received\n")
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		// Exchange code for tokens
		session, err := exchangeCodeForTokens(coordinate, code)
		userMessage := ""

		if err != nil {
			userMessage = fmt.Sprintf("error: Failed to exchange authorization code for tokens: %s", err)
		} else {
			userMessage = "Login successful!"
		}
		io.WriteString(w, `
		<html>
			<body>
				<p>`+userMessage+`</p>
				<p>You can close this window and return to the CLI.</p>
			</body>
		</html>`)

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		if err != nil {
			callbackError <- fmt.Errorf("failed to exchange code for tokens: %w", err)
		} else {
			sessionReceived <- session
		}

		// Shutdown server after response
		// At some point I added this to solve a timing issue, but trying to see if we can live without it.
		// go func() {
		// 	time.Sleep(100 * time.Millisecond)
		// 	server.Close()
		// }()
	})

	// Start the callback server
	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			if err != nil {
				callbackError <- fmt.Errorf("error starting server: %w", err)
			}
		}
	}()
	defer server.Close()

	// Open browser
	tprint("Authenticating to %s", coordinate.ServerURL)
	tprint("Opening browser. If the browser doesn't open automatically, visit this URL:")
	tprint("%s", authURL)

	if err := open.Start(authURL); err != nil {
		tprint("Warning: Could not open browser automatically")
	}

	// Wait for callback with timeout
	select {
	case session := <-sessionReceived:
		tprint("Authentication successful")
		// Save session
		session.AuthType = AuthTypeJWT
		return session, nil

	case err := <-callbackError:
		return nil, err

	case <-time.After(1 * time.Minute):
		return nil, fmt.Errorf("authentication timeout")
	}
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
	displayContextDetails(ctx)
	return nil
}

// Helper functions
// ================

// getAuthorizationURL gets the authorization URL from ConfigHub's login endpoint
func getAuthorizationURL(loginURL string) (string, error) {
	// Create a client that doesn't follow redirects
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(loginURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Check for redirect
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusTemporaryRedirect {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("expected redirect, got status %d: %s", resp.StatusCode, string(body))
	}

	// Get the Location header
	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("no Location header in redirect response")
	}

	return location, nil
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

// exchangeCodeForTokens exchanges the authorization code for tokens
func exchangeCodeForTokens(coordinate Coordinate, code string) (*AuthSession, error) {
	// Parse the server URL
	parsedURL, err := url.Parse(coordinate.ServerURL)
	if err != nil {
		// Fallback to default if parsing fails
		parsedURL = &url.URL{
			Scheme: "https",
			Host:   "hub.confighub.com",
		}
	}

	callbackURL := fmt.Sprintf("%s://%s/auth/callback", parsedURL.Scheme, parsedURL.Host)

	// Build callback URL with code and api flag
	params := url.Values{}
	params.Set("code", code)
	params.Set("client_type", "api")

	fullURL := callbackURL + "?" + params.Encode()

	resp, err := http.Get(fullURL)
	if err != nil {
		return nil, fmt.Errorf("failed to call callback endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("callback failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the JSON response
	var tokenResp struct {
		AccessToken    string `json:"access_token"`
		RefreshToken   string `json:"refresh_token"`
		OrganizationID string `json:"organization_id"`
		User           User   `json:"user"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
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
