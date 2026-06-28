// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClientRequiresServerURL(t *testing.T) {
	if _, err := NewClient(ClientOptions{}); err == nil {
		t.Fatal("NewClient() with empty ServerURL = nil error, want error")
	}
	if _, err := NewClient(ClientOptions{ServerURL: "https://hub.confighub.com"}); err != nil {
		t.Fatalf("NewClient() valid = %v, want nil", err)
	}
}

func TestAuthHeaderValue(t *testing.T) {
	tests := []struct {
		name string
		opts ClientOptions
		want string
	}{
		{"bearer token", ClientOptions{Token: "abc"}, "Bearer abc"},
		{"no creds", ClientOptions{}, ""},
		{"session bearer", ClientOptions{Session: &AuthSession{AuthType: AuthTypeJWT, AccessToken: "jwt"}}, "Bearer jwt"},
		{"session basic", ClientOptions{Session: &AuthSession{AuthType: AuthTypeBasic, BasicAuthPassword: "pw", User: User{Email: "a@b.com"}}}, "Basic YUBiLmNvbTpwdw=="},
		{"session beats token", ClientOptions{Token: "ignored", Session: &AuthSession{AuthType: AuthTypeJWT, AccessToken: "jwt"}}, "Bearer jwt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := authHeaderValue(tc.opts); got != tc.want {
				t.Fatalf("authHeaderValue = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEnvironment(t *testing.T) {
	t.Setenv("CUB_SERVER", "https://hub.confighub.com")
	t.Setenv("CUB_TOKEN", "tok")
	t.Setenv("CUB_SPACE", "my-space")
	t.Setenv("CUB_CONFIG", "")
	t.Setenv("CUB_CONTEXT", "")
	env, err := LoadEnvironment(context.Background())
	if err != nil {
		t.Fatalf("LoadEnvironment: %v", err)
	}
	if !env.HasCredentials() || env.Server != "https://hub.confighub.com" || env.Token != "tok" || env.Space != "my-space" {
		t.Fatalf("env = %+v", env)
	}
	t.Setenv("CUB_TOKEN", "")
	noCreds, _ := LoadEnvironment(context.Background())
	if noCreds.HasCredentials() {
		t.Fatal("HasCredentials() = true with empty token")
	}
}

func TestNewClientFromEnvironmentRequiresCredentials(t *testing.T) {
	t.Setenv("CUB_SERVER", "")
	t.Setenv("CUB_TOKEN", "")
	if _, err := NewClientFromEnvironment(context.Background(), ClientOptions{}); err == nil {
		t.Fatal("NewClientFromEnvironment without creds = nil, want error")
	}
}

func TestClientSendsAuthAndUserAgent(t *testing.T) {
	var gotAuth, gotAgent, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAgent = r.Header.Get("User-Agent")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"OrganizationID":"00000000-0000-0000-0000-000000000000"}`))
	}))
	defer srv.Close()

	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "secret-token", UserAgent: "cub-test"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.VerifyAuth(context.Background()); err != nil {
		t.Fatalf("VerifyAuth: %v", err)
	}
	if gotAuth != "Bearer secret-token" || gotAgent != "cub-test" || gotPath != "/api/me" {
		t.Fatalf("auth=%q agent=%q path=%q", gotAuth, gotAgent, gotPath)
	}
}

func TestVerifyAuthRejectsUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"Message":"unauthorized"}`))
	}))
	defer srv.Close()
	c, _ := NewClient(ClientOptions{ServerURL: srv.URL, Token: "bad"})
	if _, err := c.VerifyAuth(context.Background()); err == nil {
		t.Fatal("VerifyAuth against 401 = nil, want error")
	}
}
