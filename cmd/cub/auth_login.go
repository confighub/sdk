// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	cv "github.com/nirasan/go-oauth-pkce-code-verifier"
	"github.com/skratchdot/open-golang/open"
)

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log into ConfigHub",
	Long:  `Authenticate the CLI to ConfigHub via Browser Login`,
	Args:  cobra.ExactArgs(0),
	RunE:  authLoginCmdRun,
}

func init() {
	authCmd.AddCommand(authLoginCmd)
}

func authLoginCmdRun(cmd *cobra.Command, args []string) error {
	return AuthorizeUser()
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

	// construct the authorization URL (with Auth0 as the authorization provider)
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
		tprint("Successfully logged into " + cubContext.ConfigHubURL)
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
