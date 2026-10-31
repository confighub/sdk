// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func testAssertionSigner(t *testing.T) *AssertionSigner {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateJWK, err := json.Marshal(map[string]string{
		"kty": "OKP",
		"crv": "Ed25519",
		"x":   base64.RawURLEncoding.EncodeToString(public),
		"d":   base64.RawURLEncoding.EncodeToString(private.Seed()),
	})
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewAssertionSigner(privateJWK, "test-subject", "")
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func TestPerformAssertionAuthEndpoint(t *testing.T) {
	session := func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"token"}`))
	}
	for _, tc := range []struct {
		name       string
		token      http.HandlerFunc // what /auth/token does
		wantCalled []string
		wantErr    string
	}{
		{"the server accepts the assertion", session, []string{"/auth/token"}, ""},
		{"a server without the grant", http.NotFound, []string{"/auth/token"}, "404 page not found"},
		{"the server refuses the assertion", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Invalid credentials"}`))
		}, []string{"/auth/token"}, "invalid_grant: Invalid credentials"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var called []string
			var form url.Values
			mux := http.NewServeMux()
			mux.HandleFunc("/auth/token", func(w http.ResponseWriter, r *http.Request) {
				called = append(called, r.URL.Path)
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				form = r.PostForm
				tc.token(w, r)
			})
			mux.HandleFunc("/auth/worker", func(w http.ResponseWriter, r *http.Request) {
				called = append(called, r.URL.Path)
				session(w, r)
			})
			server := httptest.NewServer(mux)
			defer server.Close()

			got, err := PerformAssertionAuthContext(context.Background(), server.Client(), server.URL, testAssertionSigner(t))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %v, want one containing %q", err, tc.wantErr)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if got.AccessToken != "token" {
					t.Errorf("access token %q, want %q", got.AccessToken, "token")
				}
			}
			if strings.Join(called, ",") != strings.Join(tc.wantCalled, ",") {
				t.Fatalf("called %v, want %v", called, tc.wantCalled)
			}
			if form.Get("grant_type") != GrantTypeJWTBearer {
				t.Errorf("grant_type %q", form.Get("grant_type"))
			}
			if aud := assertionAudience(t, form.Get("assertion")); aud != server.URL+"/auth/token" {
				t.Errorf("aud %q, want the token endpoint", aud)
			}
		})
	}
}

// assertionAudience reads aud from an assertion without verifying it.
func assertionAudience(t *testing.T, assertion string) string {
	t.Helper()
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		t.Fatalf("assertion has %d parts", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Aud string `json:"aud"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	return claims.Aud
}
