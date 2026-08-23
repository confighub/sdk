// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// PerformAssertionAuth authenticates by signing a short-lived assertion and
// exchanging it for a session, the asymmetric counterpart to
// PerformWorkerAuth.
//
// Nothing secret crosses the wire: the assertion is a signature over claims the
// server can verify with the public key it already holds, and it expires in
// minutes. That is the whole point of the exchange -- PerformWorkerAuth sends a
// credential ConfigHub stored and can replay, this one sends proof of a
// credential ConfigHub has never seen.
//
// The identity is carried inside the assertion (its iss and sub, and the kid in
// its header), so unlike PerformWorkerAuth nothing else needs naming here. The
// server resolves the credential from the kid alone.
func PerformAssertionAuth(serverURL string, signer *AssertionSigner) (*AuthSession, error) {
	if signer == nil {
		return nil, fmt.Errorf("no signing key configured")
	}

	assertion, err := signer.Sign()
	if err != nil {
		return nil, fmt.Errorf("signing assertion: %w", err)
	}

	// Field names are RFC 7523 §2.2's. The server discriminates on which
	// credential fields are present rather than on a mode flag, so the secret
	// fields are omitted entirely rather than sent empty.
	requestBody := map[string]string{
		"client_assertion_type": ClientAssertionTypeJWTBearer,
		"client_assertion":      assertion,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	endpoint := fmt.Sprintf("%s/auth/worker", serverURL)
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
		// Every server-side rejection here is one uniform 401 by design, so the
		// kid is worth printing: it is the only thing that distinguishes "this
		// key is not registered" from "this key signed something invalid", and
		// it is what `cub user key list` shows.
		return nil, fmt.Errorf("authentication failed for key %s: %s", signer.Kid(), string(body))
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	session := AuthSession{}
	session.AuthType = AuthTypeJWT
	if err := json.Unmarshal(body, &session); err != nil {
		return nil, fmt.Errorf("failed to parse authentication response: %w", err)
	}
	return &session, nil
}
