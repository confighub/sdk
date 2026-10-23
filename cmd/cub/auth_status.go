// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
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

An expired session is renewed when the context can renew it without a person: a context
that authenticates with a private key, such as a service account's, signs in again with the
key, and one signed in through the browser uses the refresh token stored with it. Every
command does the same when the server rejects an expired token, so this one only reports it.
The renewed credentials are saved to the context, as 'cub auth login' saves them.

If the session is not authenticated and cannot be renewed, the command exits with a non-zero
status and prints instructions to run 'cub auth login'. For a context that authenticates
with a private key, 'cub auth login' signs in from the key with no browser, so an agent can
run it. Otherwise re-authentication is an interactive browser sign-in, which an agent cannot
complete on the user's behalf, so it must ask the user to run 'cub auth login'.

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
// session is not usable. A context that authenticates with a private key renews
// from it without a person, so the agent can do that itself; any other context
// needs an interactive browser sign-in, so the agent must hand it to the user.
func reauthInstructions(ctx *Context) string {
	if ctx != nil && ctx.Metadata.PrivateKey != "" {
		return "Run 'cub auth login' to re-authenticate: this context authenticates with a private key, " +
			"so it renews the session from the key without a browser."
	}
	return "Ask the user to run 'cub auth login' to re-authenticate. " +
		"This is an interactive browser sign-in that an agent cannot complete on the user's behalf."
}

func authStatusCmdRun(cmd *cobra.Command, args []string) error {
	ctx := contextManager.ActiveContext()
	tokenData, err := contextManager.LoadTokenData(ctx)
	if err != nil || tokenData.AccessToken == "" {
		return fmt.Errorf("not authenticated: no access token found for context %q. %s", ctx.Name, reauthInstructions(ctx))
	}

	// Check expiration locally first to avoid a server round trip on an obviously
	// expired token, and to renew it, when the context can, before asking the
	// server about it.
	if expiry, ok := tokenExpiry(tokenData.AccessToken); ok && time.Now().After(expiry) {
		if !canRenewSession(ctx, tokenData) {
			return fmt.Errorf("not authenticated: access token expired at %s. %s",
				expiry.Format(time.RFC3339), reauthInstructions(ctx))
		}
		if _, err := renewSession(cmd.Context(), ctx, tokenData.AccessToken); err != nil {
			// %v, not %w: this message already says what the renewal error
			// would, and failOnError would otherwise print only the latter.
			return fmt.Errorf("not authenticated: access token expired at %s, and %v",
				expiry.Format(time.RFC3339), err)
		}
		if cubClientNew, err = InitializeClient(ctx); err != nil {
			return fmt.Errorf("error initializing client: %w", err)
		}
	}

	// Verify against the server. A token can be rejected (revoked, signed by a
	// different server, user removed from the org) even when its own exp claim
	// has not passed yet. The client renews a session the server rejects when
	// it can, so a 401 here is one that could not be renewed.
	res, err := cubClientNew.GetMeWithResponse(cmd.Context())
	var renewalErr *sessionRenewalError
	if errors.As(err, &renewalErr) {
		return fmt.Errorf("not authenticated: server %s rejected the access token (401 Unauthorized), and %v",
			ctx.Coordinate.ServerURL, renewalErr)
	}
	if err != nil {
		return fmt.Errorf("failed to reach server %s to verify authentication: %w", ctx.Coordinate.ServerURL, err)
	}
	if res.StatusCode() == http.StatusUnauthorized {
		return fmt.Errorf("not authenticated: server %s rejected the access token (401 Unauthorized). %s",
			ctx.Coordinate.ServerURL, reauthInstructions(ctx))
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
	if lastRenewal != "" {
		view.Append([]string{"Session Renewed", lastRenewal})
	}
	// Read again: a renewal replaced the token this command started with.
	if renewed, err := contextManager.LoadTokenData(ctx); err == nil && renewed.AccessToken != "" {
		tokenData = renewed
	}
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
