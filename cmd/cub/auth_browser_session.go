// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

// Signing a browser in from here.
//
// On an instance with no identity provider there is nothing for a browser to log
// in against: no realm, and no stored password to type. The CLI is the thing
// holding a credential, so it hands the browser a session rather than sending it
// somewhere to earn one.

var authBrowserSessionCmd = &cobra.Command{
	Use:   "browser-session",
	Short: "Open the web UI signed in as the current context",
	Long: getCommandHelp(`Sign a browser in using this CLI's session.

Asks the server for a single-use link and opens it. The link carries no
credential of its own -- it is exchanged for a session cookie once and then
stops working, which is why it is safe to appear in a URL and why it expires
quickly.

Useful on an instance with no identity provider, where the browser has no
other way in.`, ""),
	Args: cobra.NoArgs,
	RunE: authBrowserSessionCmdRun,
}

var browserSessionNoBrowser bool

func init() {
	authBrowserSessionCmd.Flags().BoolVar(&browserSessionNoBrowser, "no-browser", false,
		"print the link instead of opening it")
	authCmd.AddCommand(authBrowserSessionCmd)
}

type browserSessionResponse struct {
	URL       string `json:"URL"`
	Ticket    string `json:"Ticket"`
	ExpiresIn int    `json:"ExpiresIn"`
}

func authBrowserSessionCmdRun(cmd *cobra.Command, args []string) error {
	ctx := contextManager.ActiveContext()
	if ctx == nil {
		return fmt.Errorf("no active context: run 'cub auth login' first")
	}
	tokenData, err := contextManager.LoadTokenData(ctx)
	if err != nil || tokenData == nil || tokenData.AccessToken == "" {
		return fmt.Errorf("not logged in: run 'cub auth login' first")
	}

	serverURL := ctx.Coordinate.ServerURL
	req, err := http.NewRequest(http.MethodPost, serverURL+"/api/me/browser-session", bytes.NewReader(nil))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tokenData.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to request a login link: %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read the response: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to request a login link: %s", string(body))
	}

	var ticket browserSessionResponse
	if err := json.Unmarshal(body, &ticket); err != nil {
		return fmt.Errorf("failed to parse the response: %w", err)
	}
	if ticket.URL == "" {
		return fmt.Errorf("the server returned no login link")
	}

	if browserSessionNoBrowser {
		tprint("Open this link to sign in (valid for %d seconds, once):\n\n  %s\n",
			ticket.ExpiresIn, ticket.URL)
		return nil
	}

	tprint("Opening the web UI signed in as %s.\nIf the browser does not open, use this link (valid for %d seconds, once):\n\n  %s\n",
		ctx.Coordinate.ServerURL, ticket.ExpiresIn, ticket.URL)

	// A failure to launch a browser is not a failure of the command: the link
	// was printed above and works either way. Headless hosts are the normal
	// case for a self-hosted install reached over SSH.
	if err := openBrowser(ticket.URL); err != nil {
		tprint("\nCould not open a browser automatically (%v). Use the link above.", err)
	}
	return nil
}

// openBrowser hands a URL to the platform's default handler.
func openBrowser(url string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		name = "xdg-open"
	}
	return exec.Command(name, append(args, url)...).Start()
}
