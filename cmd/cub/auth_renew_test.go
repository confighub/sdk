// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/confighub/sdk/core/cubapi"
)

// testJWT is a token whose exp claim reads as given. Only the payload matters
// to cub, which reads the expiry and leaves the signature to the server.
func testJWT(name string, exp time.Time) string {
	enc := base64.RawURLEncoding
	payload, _ := json.Marshal(map[string]any{"sub": name, "exp": exp.Unix()})
	return enc.EncodeToString([]byte(`{"alg":"none"}`)) + "." + enc.EncodeToString(payload) + ".sig-" + name
}

// fakeHub is a server that accepts one access token at a time on /api/me and
// renews sessions on /auth/refresh and /auth/worker.
type fakeHub struct {
	mu           sync.Mutex
	valid        string // the access token /api/me accepts
	refreshToken string // the refresh token /auth/refresh accepts
	orgID        string // the organization the server mints for
	user         string // the user /auth/worker answers with
	refreshes    []map[string]string
	keyLogins    int
	meTokens     []string
}

func (h *fakeHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/me":
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		h.meTokens = append(h.meTokens, token)
		if token != h.valid {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"Code":"Authentication","Message":"token is expired"}`))
			return
		}
		_, _ = w.Write([]byte(`{"UserID":"00000000-0000-0000-0000-000000000001"}`))
	case "/auth/refresh":
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["authorization"] = r.Header.Get("Authorization")
		h.refreshes = append(h.refreshes, body)
		if body["refresh_token"] != h.refreshToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Invalid or expired refresh token"}`))
			return
		}
		h.valid = testJWT(fmt.Sprintf("refreshed-%d", len(h.refreshes)), time.Now().Add(24*time.Hour))
		h.refreshToken = fmt.Sprintf("rotated-%d", len(h.refreshes))
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token":    h.valid,
			"refresh_token":   h.refreshToken,
			"organization_id": h.orgID,
		})
	case "/auth/worker":
		h.keyLogins++
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["client_assertion"] == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		h.valid = testJWT(fmt.Sprintf("key-%d", h.keyLogins), time.Now().Add(24*time.Hour))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":    h.valid,
			"organization_id": h.orgID,
			"user":            map[string]string{"email": h.user},
		})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// renewTestContext points the context manager at a throwaway directory whose
// active context names the fake server, holding the given credentials.
func renewTestContext(t *testing.T, hub *fakeHub, token *TokenData, privateKey string) *Context {
	t.Helper()
	newKeyTestContextManager(t)
	srv := httptest.NewServer(hub)
	t.Cleanup(srv.Close)

	savedNew, saved, savedRenewal := cubClientNew, cubClient, lastRenewal
	t.Cleanup(func() { cubClientNew, cubClient, lastRenewal = savedNew, saved, savedRenewal })
	lastRenewal = ""

	ctx := contextManager.ActiveContext()
	ctx.Coordinate = Coordinate{ServerURL: srv.URL, OrganizationID: hub.orgID, User: hub.user}
	ctx.Metadata.PrivateKey = privateKey
	if err := contextManager.SaveTokenData(ctx, token); err != nil {
		t.Fatalf("SaveTokenData: %v", err)
	}
	return ctx
}

func getMe(t *testing.T, ctx *Context) (int, error) {
	t.Helper()
	client, err := InitializeClient(ctx)
	if err != nil {
		t.Fatalf("InitializeClient: %v", err)
	}
	res, err := client.GetMeWithResponse(context.Background())
	if err != nil {
		return 0, err
	}
	return res.StatusCode(), nil
}

func savedToken(t *testing.T, ctx *Context) *TokenData {
	t.Helper()
	token, err := contextManager.LoadTokenData(ctx)
	if err != nil {
		t.Fatalf("LoadTokenData: %v", err)
	}
	return token
}

// An expired session renews from its refresh token before the first request, so
// the server never sees the expired token, and the renewed pair is saved for the
// next command. The context's organization is asked for, so the session stays
// in it.
func TestExpiredSessionRenewsFromRefreshToken(t *testing.T) {
	hub := &fakeHub{refreshToken: "refresh-0", orgID: "org-ext-1", user: "u@example.com"}
	expired := testJWT("expired", time.Now().Add(-time.Hour))
	ctx := renewTestContext(t, hub, &TokenData{AccessToken: expired, RefreshToken: "refresh-0"}, "")

	status, err := getMe(t, ctx)
	if err != nil || status != http.StatusOK {
		t.Fatalf("GET /me = %d, %v; want 200", status, err)
	}
	if len(hub.refreshes) != 1 {
		t.Fatalf("refreshed %d times, want 1", len(hub.refreshes))
	}
	if got := hub.refreshes[0]["organization_id"]; got != "org-ext-1" {
		t.Errorf("refresh asked for organization %q, want the context's", got)
	}
	if got := hub.refreshes[0]["authorization"]; got != "Bearer "+expired {
		t.Errorf("refresh sent Authorization %q, want the expired token", got)
	}
	for _, sent := range hub.meTokens {
		if sent == expired {
			t.Error("the expired token was sent to /api/me")
		}
	}
	saved := savedToken(t, ctx)
	if saved.AccessToken != hub.valid || saved.RefreshToken != "rotated-1" {
		t.Errorf("saved %+v, want the renewed pair", saved)
	}
	if lastRenewal != "from the refresh token" {
		t.Errorf("lastRenewal = %q", lastRenewal)
	}
}

// A token the server rejects before its own expiry is renewed and the request
// retried.
func TestRejectedSessionRenewsAndRetries(t *testing.T) {
	hub := &fakeHub{refreshToken: "refresh-0", orgID: "org-ext-1"}
	revoked := testJWT("revoked", time.Now().Add(time.Hour))
	ctx := renewTestContext(t, hub, &TokenData{AccessToken: revoked, RefreshToken: "refresh-0"}, "")

	status, err := getMe(t, ctx)
	if err != nil || status != http.StatusOK {
		t.Fatalf("GET /me = %d, %v; want 200", status, err)
	}
	if len(hub.meTokens) != 2 || hub.meTokens[0] != revoked || hub.meTokens[1] != hub.valid {
		t.Errorf("/api/me saw %v, want the rejected token and then the renewed one", hub.meTokens)
	}
}

// A refresh the server refuses fails the request with the reason and the
// instructions, and leaves the stored session as it was.
func TestFailedRenewalReportsInstructions(t *testing.T) {
	hub := &fakeHub{refreshToken: "the-real-one", orgID: "org-ext-1"}
	expired := testJWT("expired", time.Now().Add(-time.Hour))
	ctx := renewTestContext(t, hub, &TokenData{AccessToken: expired, RefreshToken: "stale"}, "")

	_, err := getMe(t, ctx)
	var renewalErr *sessionRenewalError
	if !errors.As(err, &renewalErr) {
		t.Fatalf("GET /me error = %v, want a sessionRenewalError", err)
	}
	for _, want := range []string{"Invalid or expired refresh token", "cub auth login", ctx.Name} {
		if !strings.Contains(renewalErr.Error(), want) {
			t.Errorf("error %q does not mention %q", renewalErr.Error(), want)
		}
	}
	var refreshErr *cubapi.RefreshError
	if !errors.As(err, &refreshErr) || refreshErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("error does not carry the server's 401: %v", err)
	}
	if saved := savedToken(t, ctx); saved.AccessToken != expired || saved.RefreshToken != "stale" {
		t.Errorf("a failed renewal changed the stored session: %+v", saved)
	}
}

// A context with nothing to renew from sends what it has, and the server's 401
// comes back as it always did.
func TestSessionWithoutRefreshTokenIsNotRenewed(t *testing.T) {
	hub := &fakeHub{orgID: "org-ext-1"}
	expired := testJWT("expired", time.Now().Add(-time.Hour))
	ctx := renewTestContext(t, hub, &TokenData{AccessToken: expired}, "")

	status, err := getMe(t, ctx)
	if err != nil || status != http.StatusUnauthorized {
		t.Fatalf("GET /me = %d, %v; want the server's 401", status, err)
	}
	if len(hub.refreshes) != 0 {
		t.Errorf("refreshed %d times with no refresh token", len(hub.refreshes))
	}
}

// A context that was never signed in is not signed in by a renewal.
func TestContextWithoutSessionIsNotRenewed(t *testing.T) {
	if canRenewSession(&Context{Metadata: Metadata{PrivateKey: "ci"}}, &TokenData{}) {
		t.Error("a context with a key but no session would be signed in by a renewal")
	}
	if canRenewSession(&Context{}, &TokenData{RefreshToken: "r"}) {
		t.Error("a context with a refresh token but no session would be signed in by a renewal")
	}
}

// writeTestKey writes an Ed25519 private JWK for the identity, as
// `cub user key add --generate` does, and returns its path.
func writeTestKey(t *testing.T, externalID string) string {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	enc := base64.RawURLEncoding
	data, _ := json.Marshal(map[string]string{
		"kty": "OKP", "crv": "Ed25519",
		"x": enc.EncodeToString(pub), "d": enc.EncodeToString(priv.Seed()),
		"confighub_user_external_id": externalID,
	})
	path := filepath.Join(t.TempDir(), "agent.jwk")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path
}

// A context that authenticates with a key renews by signing in with it again.
func TestExpiredSessionRenewsFromKey(t *testing.T) {
	t.Setenv(privateKeyEnvVar, "")
	hub := &fakeHub{orgID: "org-ext-1", user: "agent@example.com"}
	expired := testJWT("expired", time.Now().Add(-time.Hour))
	ctx := renewTestContext(t, hub, &TokenData{AccessToken: expired}, writeTestKey(t, "sa-ext-1"))

	status, err := getMe(t, ctx)
	if err != nil || status != http.StatusOK {
		t.Fatalf("GET /me = %d, %v; want 200", status, err)
	}
	if hub.keyLogins != 1 {
		t.Errorf("signed in with the key %d times, want 1", hub.keyLogins)
	}
	if saved := savedToken(t, ctx); saved.AccessToken != hub.valid {
		t.Errorf("saved %q, want the renewed token", saved.AccessToken)
	}
	if !strings.HasPrefix(lastRenewal, "from key ") {
		t.Errorf("lastRenewal = %q", lastRenewal)
	}
}

// A key that now signs in as someone other than the context's identity is not
// saved over it.
func TestKeyRenewalRefusesAnotherIdentity(t *testing.T) {
	t.Setenv(privateKeyEnvVar, "")
	hub := &fakeHub{orgID: "org-ext-1", user: "agent@example.com"}
	expired := testJWT("expired", time.Now().Add(-time.Hour))
	ctx := renewTestContext(t, hub, &TokenData{AccessToken: expired}, writeTestKey(t, "sa-ext-1"))
	hub.orgID = "org-ext-2"

	_, err := getMe(t, ctx)
	var renewalErr *sessionRenewalError
	if !errors.As(err, &renewalErr) || !strings.Contains(err.Error(), "org-ext-2") {
		t.Fatalf("GET /me error = %v, want a renewal refused for the other organization", err)
	}
	if saved := savedToken(t, ctx); saved.AccessToken != expired {
		t.Errorf("the other identity's session was saved: %q", saved.AccessToken)
	}
}

// When another cub renewed the session since this one loaded it, its token is
// used rather than renewing again from a refresh token it may have rotated.
func TestRenewalAdoptsAnotherProcesssRenewal(t *testing.T) {
	hub := &fakeHub{refreshToken: "refresh-0", orgID: "org-ext-1"}
	expired := testJWT("expired", time.Now().Add(-time.Hour))
	ctx := renewTestContext(t, hub, &TokenData{AccessToken: expired, RefreshToken: "refresh-0"}, "")
	client, err := InitializeClient(ctx)
	if err != nil {
		t.Fatalf("InitializeClient: %v", err)
	}

	theirs := testJWT("theirs", time.Now().Add(24*time.Hour))
	hub.valid = theirs
	if err := contextManager.SaveTokenData(ctx, &TokenData{AccessToken: theirs, RefreshToken: "rotated-elsewhere"}); err != nil {
		t.Fatalf("SaveTokenData: %v", err)
	}

	res, err := client.GetMeWithResponse(context.Background())
	if err != nil || res.StatusCode() != http.StatusOK {
		t.Fatalf("GET /me = %v, %v; want 200", res, err)
	}
	if len(hub.refreshes) != 0 {
		t.Errorf("refreshed %d times; the other process's session should have been used", len(hub.refreshes))
	}
}

// A command that hands the token to something else gets a renewed one.
func TestCurrentTokenDataRenewsExpiredSession(t *testing.T) {
	hub := &fakeHub{refreshToken: "refresh-0", orgID: "org-ext-1"}
	expired := testJWT("expired", time.Now().Add(-time.Hour))
	ctx := renewTestContext(t, hub, &TokenData{AccessToken: expired, RefreshToken: "refresh-0"}, "")

	token, err := currentTokenData(ctx)
	if err != nil {
		t.Fatalf("currentTokenData: %v", err)
	}
	if token.AccessToken != hub.valid {
		t.Errorf("got %q, want the renewed token", token.AccessToken)
	}

	// A current token is returned as it is.
	if _, err := currentTokenData(ctx); err != nil || len(hub.refreshes) != 1 {
		t.Errorf("a current token was renewed again: %d refreshes, %v", len(hub.refreshes), err)
	}
}
