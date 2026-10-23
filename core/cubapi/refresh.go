// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// RefreshRequest is the body of POST /auth/refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
	// OrganizationID is the identity provider's ID for the organization to mint
	// the session for. A client that records its organization names it, so a
	// renewal stays where the session was; empty lets the server choose. Either
	// way the server mints only for an organization the user belongs to.
	OrganizationID string `json:"organization_id,omitempty"`
}

// RefreshError is a refresh the server refused, as opposed to one that could not
// be sent.
type RefreshError struct {
	StatusCode int
	Message    string
}

func (e *RefreshError) Error() string {
	return fmt.Sprintf("the server refused to renew the session (HTTP %d): %s", e.StatusCode, e.Message)
}

// RefreshSession renews a session with the identity provider's refresh token, which
// a browser or device login returns alongside the access token, through POST
// /auth/refresh.
//
// The refresh token is the credential; the access token need not be current. It is
// sent when there is one, since a server can read from it which organization the
// session was in, and an older server requires it. The returned session carries the
// new access token, the refresh token to use next time (the identity provider may
// rotate it, so the one passed in may no longer work), and the organization it was
// minted for, which an older server does not report.
func RefreshSession(ctx context.Context, httpClient *http.Client, serverURL, refreshToken, accessToken, organizationID string) (*AuthSession, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("no refresh token to renew the session with")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	body, err := json.Marshal(RefreshRequest{RefreshToken: refreshToken, OrganizationID: organizationID})
	if err != nil {
		return nil, fmt.Errorf("failed to build the refresh request: %w", err)
	}

	server := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(serverURL), "/"), "/api")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server+"/auth/refresh", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create the refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	res, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send the refresh request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return nil, &RefreshError{StatusCode: res.StatusCode, Message: refreshErrorMessage(detail)}
	}

	var tokens struct {
		AccessToken    string `json:"access_token"`
		RefreshToken   string `json:"refresh_token"`
		OrganizationID string `json:"organization_id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&tokens); err != nil {
		return nil, fmt.Errorf("failed to read the renewed session: %w", err)
	}
	if tokens.AccessToken == "" {
		return nil, fmt.Errorf("the server returned no access token")
	}
	return &AuthSession{
		AccessToken:    tokens.AccessToken,
		RefreshToken:   tokens.RefreshToken,
		OrganizationID: tokens.OrganizationID,
		AuthType:       AuthTypeJWT,
	}, nil
}

// refreshErrorMessage returns the message of the server's error body, or the body
// itself when it is not the {"message": ...} the auth endpoints answer with.
func refreshErrorMessage(body []byte) string {
	var parsed struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Message != "" {
		return parsed.Message
	}
	return strings.TrimSpace(string(body))
}
