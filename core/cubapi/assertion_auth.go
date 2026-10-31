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
	"net/url"
	"strings"
)

// PerformAssertionAuth authenticates by signing a short-lived assertion and
// exchanging it for a session.
//
// Nothing secret crosses the wire: the assertion is a signature over claims the
// server can verify with the public key it already holds, and it expires in
// minutes. It is proof of a credential ConfigHub has never seen.
//
// The identity is carried inside the assertion (its iss and sub, and the kid in
// its header), so nothing else needs naming here. The server resolves the
// credential from the kid alone.
func PerformAssertionAuth(serverURL string, signer *AssertionSigner) (*AuthSession, error) {
	return PerformAssertionAuthContext(context.Background(), nil, serverURL, signer)
}

// PerformAssertionAuthContext is PerformAssertionAuth under a context, over the given HTTP
// client, or the default one when nil. A long-lived process authenticates this way, so that
// its authentication is bounded and goes through the same client as everything else it does.
func PerformAssertionAuthContext(ctx context.Context, httpClient *http.Client, serverURL string, signer *AssertionSigner) (*AuthSession, error) {
	if signer == nil {
		return nil, fmt.Errorf("no signing key configured")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	// The JWT-bearer grant (RFC 7523 §2.1) at the OAuth token endpoint. The assertion names
	// the endpoint in aud, as RFC 7523 §3 has it.
	tokenEndpoint := strings.TrimRight(serverURL, "/") + "/auth/token"
	assertion, err := signer.SignFor(tokenEndpoint)
	if err != nil {
		return nil, fmt.Errorf("signing assertion: %w", err)
	}
	form := url.Values{"grant_type": {GrantTypeJWTBearer}, "assertion": {assertion}}
	status, body, err := postForSession(ctx, httpClient, tokenEndpoint, "application/x-www-form-urlencoded", []byte(form.Encode()))
	if err != nil {
		return nil, err
	}

	if status != http.StatusOK {
		var tokenErr struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &tokenErr)
		// Every refusal of the assertion is the same by design, so the kid is worth
		// printing: it is the only thing that distinguishes "this key is not registered"
		// from "this key signed something invalid", and it is what `cub serviceaccount key list` shows.
		reason := strings.TrimSpace(string(body))
		if tokenErr.Error != "" {
			reason = tokenErr.Error
			if tokenErr.Description != "" {
				reason += ": " + tokenErr.Description
			}
		}
		return nil, fmt.Errorf("authentication failed for key %s: %s", signer.Kid(), reason)
	}

	session := AuthSession{}
	session.AuthType = AuthTypeJWT
	if err := json.Unmarshal(body, &session); err != nil {
		return nil, fmt.Errorf("failed to parse authentication response: %w", err)
	}
	return &session, nil
}

// GrantTypeJWTBearer is RFC 7523 §2.1's grant type: an assertion presented as the grant.
const GrantTypeJWTBearer = "urn:ietf:params:oauth:grant-type:jwt-bearer"

func postForSession(ctx context.Context, httpClient *http.Client, endpoint, contentType string, payload []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	res, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to make authentication request: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return res.StatusCode, body, nil
}
