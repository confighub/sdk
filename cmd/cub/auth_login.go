// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	cv "github.com/nirasan/go-oauth-pkce-code-verifier"
	"github.com/skratchdot/open-golang/open"
)

var (
	asWorker bool
)

var authLoginCmd = &cobra.Command{
	Use:   "login [organization]",
	Short: "Log into ConfigHub",
	Long: `Authenticate the CLI to ConfigHub via Browser Login

The organization argument is optional and can be a partial match of:
- Organization Display Name (e.g., "ConfigHub")
- Organization Slug (e.g., "org_01jsqq70m483a3b1fk3zfs9z1a")
- Organization ID (ConfigHub UUID, e.g., "2af2356f-8587-4816-8619-77dfa85fb524")
- External ID (WorkOS ID, e.g., "org_01JSQQ70M483A3B1FK3ZFS9Z1A")

Examples:
  # Login without specifying organization
  cub auth login
  
  # Login to specific organization by display name
  cub auth login "ConfigHub"
  
  # Login to specific organization by slug
  cub auth login "org_01jsqq70m483a3b1fk3zfs9z1a"
  
  # Login as a worker using environment variables
  cub auth login --as-worker`,
	Args: cobra.MaximumNArgs(1),
	RunE: authLoginCmdRun,
}

func init() {
	authLoginCmd.Flags().BoolVar(&asWorker, "as-worker", false, "Authenticate as a worker using CONFIGHUB_WORKER_ID and CONFIGHUB_WORKER_SECRET environment variables")
	authCmd.AddCommand(authLoginCmd)
}

func authLoginCmdRun(cmd *cobra.Command, args []string) error {
	var desiredOrg string
	if len(args) > 0 {
		desiredOrg = args[0]
	}

	// Check if worker authentication is requested
	if asWorker {
		// Worker authentication doesn't support organization switching
		if desiredOrg != "" {
			return fmt.Errorf("organization switching is not supported with --as-worker flag")
		}
		return AuthorizeWorker()
	}

	// First, authenticate normally
	err := AuthorizeUser()
	if err != nil {
		return err
	}

	// If a specific organization was requested, switch to it
	if desiredOrg != "" {
		return switchToOrganization(desiredOrg)
	}

	return nil
}

// AuthorizeUser implements the PKCE OAuth2 flow.
func AuthorizeUser() error {

	// used for PKCE
	var codeVerifier, err = cv.CreateCodeVerifier()
	if err != nil {
		return err
	}

	// Create code_challenge with S256 method
	codeChallenge := codeVerifier.CodeChallengeS256()

	// We get Client ID from the server. The code for this is in info.go
	clientID := GetApiInfo().ClientID

	// TODO: Need to fail with a good error message if something already listens on 3000
	redirectURL := "http://127.0.0.1:3000/"

	// construct the authorization URL (with WorkOS as the authorization provider)
	authorizationURL := fmt.Sprintf(
		"https://api.workos.com/user_management/authorize?"+
			"provider=authkit&response_type=code&client_id=%s"+
			"&code_challenge=%s"+
			"&code_challenge_method=S256&redirect_uri=%s",
		clientID, codeChallenge, url.QueryEscape(redirectURL))

	// Need this to cancel the wait once the steps have completed.
	ctx, cancel := context.WithCancel(context.Background())

	// Create a WaitGroup to manage server shutdown
	var wg sync.WaitGroup

	// Increment the WaitGroup counter
	wg.Add(1)

	//nolint:gosec // not relevant
	server := &http.Server{Addr: ":3000"}

	// define a handler that will get she authorization code, call the token endpoint, and close the HTTP server
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done() // Decrement the WaitGroup counter when the handler finishes

		// get the authorization code
		code := r.URL.Query().Get("code")
		if code == "" {
			tprint("url Param 'code' is missing in callback from auth server")
			io.WriteString(w, "Error: could not find 'code' URL parameter\n") //nolint:errcheck // not needed
			return
		}

		// trade the authorization code and the code verifier for an access token
		codeVerifier := codeVerifier.String()
		session, err := getSession(clientID, codeVerifier, code, redirectURL)
		if err != nil {
			tprint("could not get access token")
			io.WriteString(w, "Error: could not retrieve access token\n") //nolint:errcheck // not needed
			return
		}
		session.AuthType = "Bearer"
		err = SaveSession(*session)
		if err != nil {
			tprint("snap: could not save session")
			io.WriteString(w, "Error: could not save session\n") //nolint:errcheck // not needed
			return
		}
		authSession = *session
		authHeader = setAuthHeader(&authSession)
		cubClientNew, err = initializeClient()
		if err != nil {
			tprint("error initializing client: %v", err)
			return
		}
		// return an indication of success to the caller
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		_, err = io.WriteString(w, `
		<html>
			<body>
				Login successful! You can close this window and return to the CLI.
			</body>
		</html>`)
		if err != nil {
			tprint("Error writing browser response: %v", err)
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Close the server after the first response
		go func() {
			// Shut down the server and cancel the context
			// to ensure the response is sent to the browser
			// before closing the server
			server.Close()
			cancel()
		}()
	})

	go func() {
		log.Println("Starting HTTP server")
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// open a browser window to the authorizationURL
	tprint("Opening browser on the URL below. If the browser doesn't open automatically then copy the URL manually and open it in your browser:")
	tprint("%s", authorizationURL)
	err = open.Start(authorizationURL)
	if err != nil {
		tprint("Can't open browser %s.\nPlease do it manually.", err)
		// DO NOT return err, give an option for user to copy & paste the URL.
	}
	// Wait for the handler to finish
	wg.Wait()

	select {
	case <-ctx.Done():
		tprint("Successfully logged into %s (Organization: %s)", cubContext.ConfigHubURL, authSession.OrganizationID)
	case <-time.After(2 * time.Minute):
		tprint("Timed out waiting for authentication")
	}
	err = setSpaceContext()
	if err != nil {
		return err
	}
	// Preload builtin functions
	_, _, err = listAndSaveFunctions("", "", "")
	if err != nil {
		return err
	}
	// Disable writing of the agents file for now.
	if false {
		// Save agents file for AI agents
		err = saveAgentsFile()
	}
	return err
}

func setSpaceContext() error {
	currentSpace, err := apiGetSpaceFromSlug(cubContext.Space)
	if err != nil {
		spaceList, err := apiListSpaces("")
		if err != nil {
			return err
		}
		if len(spaceList) == 0 {
			return fmt.Errorf("no spaces found. Current space could not be set")
		}
		// Just pick the first one
		setCubContextFromSpace(spaceList[0])
	} else {
		// current space in context.json exists but it might have the wrong IDs
		setCubContextFromSpace(currentSpace)
	}
	SaveCubContext(cubContext)
	selectedSpaceID = cubContext.SpaceID
	selectedSpaceSlug = cubContext.Space
	tprint("Current space set to %s (%s)", cubContext.Space, cubContext.SpaceID)
	return nil
}

// getSession trades the authorization code retrieved from the first OAuth2 leg for an access token
func getSession(clientID string, codeVerifier string, authorizationCode string, callbackURL string) (*AuthSession, error) {
	// set the url and form-encoded data for the POST to the access token endpoint
	endpoint := "https://api.workos.com/user_management/authenticate"
	data := fmt.Sprintf(
		"grant_type=authorization_code&client_id=%s"+
			"&code_verifier=%s"+
			"&code=%s"+
			"&redirect_uri=%s",
		clientID, codeVerifier, authorizationCode, url.QueryEscape(callbackURL))
	payload := strings.NewReader(data)

	// create the request and execute it
	req, err := http.NewRequest("POST", endpoint, payload)
	if err != nil {
		return nil, err
	}
	req.Header.Add("content-type", "application/x-www-form-urlencoded")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		tprint("HTTP error: %s", err)
		return nil, err
	}

	// process the response
	defer res.Body.Close()
	session := &AuthSession{}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	// unmarshal the json into a string map
	err = json.Unmarshal(body, session)
	if err != nil {
		tprint("JSON error: %s", err)
		return nil, err
	}
	return session, nil
}

// AuthorizeWorker implements worker authentication using JWT
func AuthorizeWorker() error {
	// Get worker credentials from environment variables
	workerID := os.Getenv("CONFIGHUB_WORKER_ID")
	workerSecret := os.Getenv("CONFIGHUB_WORKER_SECRET")

	if workerID == "" || workerSecret == "" {
		return fmt.Errorf("CONFIGHUB_WORKER_ID and CONFIGHUB_WORKER_SECRET environment variables must be set")
	}

	// Create the request body
	requestBody := map[string]string{
		"worker_id":     workerID,
		"worker_secret": workerSecret,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Make the authentication request
	endpoint := fmt.Sprintf("%s/auth/worker", cubContext.ConfigHubURL)
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make authentication request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("authentication failed: %s", string(body))
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	session := AuthSession{}
	session.AuthType = "Bearer"

	err = json.Unmarshal(body, &session)
	if err != nil {
		return fmt.Errorf("failed to parse authentication response: %w", err)
	}

	// Save the session
	err = SaveSession(session)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	authSession = session
	authHeader = setAuthHeader(&authSession)
	cubClientNew, err = initializeClient()
	if err != nil {
		return fmt.Errorf("error initializing client: %w", err)
	}

	tprint("Successfully logged in as worker %s (Organization: %s)", workerID, session.OrganizationID)

	// Set space context
	err = setSpaceContext()
	if err != nil {
		return err
	}

	// Preload builtin functions
	_, _, err = listAndSaveFunctions("", "", "")
	if err != nil {
		return err
	}

	return nil
}
