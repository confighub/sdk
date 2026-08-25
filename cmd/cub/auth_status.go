// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check whether the current session is authenticated",
	Long: getCommandHelp(`Check whether the current session is authenticated.

Unlike 'cub context get', this command contacts the server to verify that the access
token is still valid. It loads the token for the active context, checks its expiration
locally, and then calls the server's /me endpoint to confirm the token is accepted.

If the session is not authenticated or the token has expired, the command exits with a
non-zero status and prints instructions to run 'cub auth login'. Re-authentication
requires an interactive browser sign-in, so an agent cannot complete it on the user's
behalf and must ask the user to run 'cub auth login'.

It also compares the client and server versions. Pre-1.0, a change in the second version
number is not backward compatible, so a cub older than the server exits non-zero telling
you to run 'cub upgrade', and a cub newer than the server prints a warning.

Examples:
`+"```"+`
  # Check authentication status against the server in the active context
  cub auth status
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(0),
	RunE: authStatusCmdRun,
}

func init() {
	authCmd.AddCommand(authStatusCmd)
}

// reauthInstructions is the actionable guidance returned to an agent when the
// session is not usable. Re-authentication is an interactive browser flow, so the
// agent must hand the task to the user rather than attempting it itself.
const reauthInstructions = "Ask the user to run 'cub auth login' to re-authenticate. " +
	"This is an interactive browser sign-in that an agent cannot complete on the user's behalf."

func authStatusCmdRun(cmd *cobra.Command, args []string) error {
	ctx := contextManager.ActiveContext()
	tokenData, err := contextManager.LoadTokenData(ctx)
	if err != nil || tokenData.AccessToken == "" {
		return fmt.Errorf("not authenticated: no access token found for context %q. %s", ctx.Name, reauthInstructions)
	}

	// Check expiration locally first to avoid a server round trip on an obviously
	// expired token.
	if expiry, ok := tokenExpiry(tokenData.AccessToken); ok && time.Now().After(expiry) {
		return fmt.Errorf("not authenticated: access token expired at %s. %s",
			expiry.Format(time.RFC3339), reauthInstructions)
	}

	// Verify against the server. A token can be rejected (revoked, signed by a
	// different server, user removed from the org) even when its own exp claim
	// has not passed yet.
	res, err := cubClientNew.GetMeWithResponse(cmd.Context())
	if err != nil {
		return fmt.Errorf("failed to reach server %s to verify authentication: %w", ctx.Coordinate.ServerURL, err)
	}
	if res.StatusCode() == http.StatusUnauthorized {
		return fmt.Errorf("not authenticated: server %s rejected the access token (401 Unauthorized). %s",
			ctx.Coordinate.ServerURL, reauthInstructions)
	}
	if res.JSON200 == nil {
		return fmt.Errorf("unexpected response from server %s while verifying authentication: %s",
			ctx.Coordinate.ServerURL, res.Status())
	}

	// The server stamps its version on every response, so the /me call above already
	// carries it and this costs no extra round trip. It is empty against a server
	// predating that header, which checkVersionSkew treats as undecidable.
	serverVersion := cubClient.ServerVersion()

	view := detailView()
	view.Append([]string{"Status", "Authenticated"})
	view.Append([]string{"User", ctx.Coordinate.User})
	view.Append([]string{"Organization Name", ctx.Metadata.OrganizationName})
	view.Append([]string{"Server URL", ctx.Coordinate.ServerURL})
	if expiry, ok := tokenExpiry(tokenData.AccessToken); ok {
		view.Append([]string{"Token Expires", fmt.Sprintf("%s (in %s)",
			expiry.Format(time.RFC3339), time.Until(expiry).Round(time.Second))})
	}
	view.Append([]string{"Client Version", Version})
	if serverVersion != "" {
		view.Append([]string{"Server Version", serverVersion})
	}
	view.Render()

	// Reported last: this command is run as a prerequisite check, and a client too
	// old for the server is as much a reason not to proceed as an expired token.
	return checkVersionSkew(Version, serverVersion)
}

// tokenExpiry parses a JWT access token and returns its expiration time from the
// "exp" claim. ok is false when the token is not a parseable JWT or has no exp claim.
// It does not verify the token signature; that verification is the server's job.
func tokenExpiry(accessToken string) (expiry time.Time, ok bool) {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0), true
}
