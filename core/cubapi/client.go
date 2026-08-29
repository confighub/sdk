// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Client construction (client.go) builds an authenticated ConfigHub API client
// for Go tools and `cub` plugins. It turns resolved credentials — a server URL
// plus a bearer token (or a full [AuthSession] for Basic auth) — into a ready
// *goclientnew.ClientWithResponses, and provides a server-verifying auth check.
//
// Two credential sources are supported:
//
//   - Plugin environment: the CUB_SERVER / CUB_TOKEN / CUB_SPACE variables that
//     `cub` passes to plugins. Use [NewClientFromEnvironment].
//   - Local config: the active context of a [Store] (the ~/.confighub session a
//     user creates with `cub auth login`). Use [NewClientFromConfig].
//
// [ResolveClient] tries the environment first and falls back to local config, so
// the same binary works both as a `cub` plugin and as a standalone CLI without
// code changes.
package cubapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"sync/atomic"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/sethvargo/go-envconfig"
)

// DefaultUserAgent is sent when [ClientOptions.UserAgent] is empty.
const DefaultUserAgent = "cub-sdk"

// ServerVersionHeader is the response header the ConfigHub server stamps its own
// version onto, on every /api response. A client that reads it learns the server
// version from a call it was already making, rather than spending a round trip on
// /api/info. The value is what /api/info reports in Version: a release version such
// as "v0.2.34", or "v0.2-dev" for a build from a working tree.
const ServerVersionHeader = "ConfigHub-Version"

// ClientOptions configures client construction. ServerURL is required; the rest
// are optional. Provide either Token (bearer) or Session (which additionally
// supports Basic auth); Session takes precedence when both are set.
type ClientOptions struct {
	ServerURL string
	Token     string
	Session   *AuthSession

	// Space is the default space slug, when known (e.g. from CUB_SPACE or the
	// active context's settings). It is carried on the returned Client for
	// callers' convenience and does not affect requests.
	Space string

	UserAgent  string
	Debug      bool         // dump requests and responses to stdout
	HTTPClient *http.Client // base client; defaults to a client using http.DefaultTransport
}

// Client wraps the generated API client together with the resolved server URL
// and default space, so tools don't have to thread them separately.
type Client struct {
	API    *goclientnew.ClientWithResponses
	Server string // server URL without the "/api" suffix
	Space  string // default space slug, if known

	transport *clientTransport
}

// ServerVersion returns the version the server reported on the most recent
// response, or "" if no request has completed yet or the server does not stamp
// [ServerVersionHeader] (a server older than the header). Callers that need the
// version regardless can fall back to /api/info.
func (c *Client) ServerVersion() string {
	if c.transport == nil {
		return ""
	}
	return c.transport.serverVersion()
}

// NewClient constructs a Client from explicit options. It returns an error if
// ServerURL is empty.
func NewClient(opts ClientOptions) (*Client, error) {
	server := strings.TrimRight(strings.TrimSpace(opts.ServerURL), "/")
	if server == "" {
		return nil, fmt.Errorf("cubapi: ServerURL is required")
	}
	agent := opts.UserAgent
	if agent == "" {
		agent = DefaultUserAgent
	}

	base := opts.HTTPClient
	if base == nil {
		base = &http.Client{Transport: http.DefaultTransport}
	}
	rt := base.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	transport := &clientTransport{base: rt, agent: agent, debug: opts.Debug}
	httpClient := &http.Client{
		Transport:     transport,
		CheckRedirect: base.CheckRedirect,
		Jar:           base.Jar,
		Timeout:       base.Timeout,
	}

	authHeader := authHeaderValue(opts)

	api, err := goclientnew.NewClientWithResponses(server+"/api", func(c *goclientnew.Client) error {
		c.Client = httpClient
		if authHeader != "" {
			c.RequestEditors = append(c.RequestEditors, func(_ context.Context, req *http.Request) error {
				req.Header.Set("Authorization", authHeader)
				return nil
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cubapi: build API client: %w", err)
	}

	return &Client{API: api, Server: server, Space: opts.Space, transport: transport}, nil
}

// authHeaderValue derives the Authorization header value from the options,
// preferring an explicit Session (which may be Basic) over a bare bearer token.
func authHeaderValue(opts ClientOptions) string {
	if s := opts.Session; s != nil {
		if s.AuthType == AuthTypeBasic {
			encoded := base64.StdEncoding.EncodeToString([]byte(s.User.Email + ":" + s.BasicAuthPassword))
			return "Basic " + encoded
		}
		if s.AccessToken != "" {
			return "Bearer " + s.AccessToken
		}
		return ""
	}
	if opts.Token != "" {
		return "Bearer " + opts.Token
	}
	return ""
}

// VerifyAuth round-trips GET /me to confirm the credentials are accepted by the
// server. It is the direct-API equivalent of `cub auth status`: unlike a local
// token check, it proves the server honors the token right now. On success it
// returns the authenticated organization member.
func (c *Client) VerifyAuth(ctx context.Context) (*goclientnew.OrganizationMember, error) {
	res, err := c.API.GetMeWithResponse(ctx)
	if IsAPIError(err, res) {
		return nil, fmt.Errorf("not authenticated to ConfigHub (%s): %w", c.Server, InterpretErrorGeneric(err, res))
	}
	if res.JSON200 == nil {
		return nil, fmt.Errorf("not authenticated to ConfigHub (%s): unexpected empty response from GET /me", c.Server)
	}
	return res.JSON200, nil
}

// Environment holds the credentials `cub` passes to plugins via environment
// variables (see core/plugin and cub's plugin execution). It is populated with
// go-envconfig.
type Environment struct {
	Server  string `env:"CUB_SERVER"`
	Token   string `env:"CUB_TOKEN"`
	Space   string `env:"CUB_SPACE"`
	Config  string `env:"CUB_CONFIG"`
	Context string `env:"CUB_CONTEXT"`
}

// HasCredentials reports whether the environment carries enough to build a
// client directly (server URL and token), i.e. the binary is running as a
// `cub` plugin.
func (e Environment) HasCredentials() bool {
	return e.Server != "" && e.Token != ""
}

// LoadEnvironment reads the CUB_* variables from the process environment.
func LoadEnvironment(ctx context.Context) (Environment, error) {
	var env Environment
	if err := envconfig.Process(ctx, &env); err != nil {
		return Environment{}, fmt.Errorf("cubapi: read environment: %w", err)
	}
	return env, nil
}

// NewClientFromEnvironment builds a Client from the CUB_SERVER / CUB_TOKEN /
// CUB_SPACE variables (plugin mode). Fields set on opts (UserAgent, Debug,
// HTTPClient) are honored; opts.ServerURL/Token/Space are overridden by the
// environment. It returns an error when the environment lacks credentials.
func NewClientFromEnvironment(ctx context.Context, opts ClientOptions) (*Client, error) {
	env, err := LoadEnvironment(ctx)
	if err != nil {
		return nil, err
	}
	if !env.HasCredentials() {
		return nil, fmt.Errorf("cubapi: CUB_SERVER and CUB_TOKEN must be set to build a client from the environment")
	}
	opts.ServerURL = env.Server
	opts.Token = env.Token
	opts.Session = nil
	opts.Space = env.Space
	return NewClient(opts)
}

// NewClientFromConfig builds a Client from a Store's active context, loading the
// bearer token from the context's token file. The context's default space
// becomes the client's default space unless opts.Space is already set.
func NewClientFromConfig(ctx context.Context, store *Store, opts ClientOptions) (*Client, error) {
	active, err := store.ActiveContext()
	if err != nil {
		return nil, err
	}
	token, err := store.TokenData(active)
	if err != nil {
		return nil, fmt.Errorf("cubapi: load token for context %q: %w", active.Name, err)
	}
	opts.ServerURL = active.Coordinate.ServerURL
	opts.Token = token.AccessToken
	opts.Session = nil
	if opts.Space == "" {
		opts.Space = active.Settings.DefaultSpace
	}
	return NewClient(opts)
}

// ResolveClient builds a Client using whichever credential source is available,
// preferring the plugin environment (CUB_SERVER/CUB_TOKEN) and falling back to
// the local ~/.confighub config (honoring CUB_CONFIG / CUB_CONTEXT). This is the
// one-call entry point for tools that should work both as a `cub` plugin and as
// a standalone CLI.
func ResolveClient(ctx context.Context, opts ClientOptions) (*Client, error) {
	env, err := LoadEnvironment(ctx)
	if err != nil {
		return nil, err
	}
	if env.HasCredentials() {
		o := opts
		o.ServerURL = env.Server
		o.Token = env.Token
		o.Session = nil
		o.Space = env.Space
		return NewClient(o)
	}

	store, err := LoadConfig(env.Config)
	if err != nil {
		return nil, err
	}
	if env.Context != "" {
		if err := store.Use(env.Context); err != nil {
			return nil, err
		}
	}
	return NewClientFromConfig(ctx, store, opts)
}

// MemoizedClient is one tool's connection to ConfigHub, built on first use by
// [ResolveClient] -- from the cub plugin environment (CUB_SERVER / CUB_TOKEN) or
// the local ~/.confighub session -- and reused thereafter.
//
// The zero value is usable; set UserAgent so the server can tell tools apart. A
// MemoizedClient must not be copied after first use.
type MemoizedClient struct {
	// UserAgent identifies the tool to the server, e.g. "cub-netpol". Empty uses
	// [DefaultUserAgent].
	UserAgent string

	once   sync.Once
	client *Client
	err    error
}

// Client returns the memoized client. Building it performs no network I/O; use
// [MemoizedClient.Preflight] to verify the session against the server.
func (m *MemoizedClient) Client(ctx context.Context) (*Client, error) {
	m.once.Do(func() {
		m.client, m.err = ResolveClient(ctx, ClientOptions{UserAgent: m.UserAgent})
	})
	return m.client, m.err
}

// Preflight is the standard gate for any ConfigHub-touching command: it builds
// the client and verifies the session against the server, rather than only
// reading local state, so an expired token is reported here instead of surfacing
// as an unrelated failure further in. It returns the ready client, or an error
// carrying the remediation.
func (m *MemoizedClient) Preflight(ctx context.Context) (*Client, error) {
	c, err := m.Client(ctx)
	if err != nil {
		return nil, notAuthenticated(err)
	}
	if _, err := c.VerifyAuth(ctx); err != nil {
		return nil, notAuthenticated(err)
	}
	return c, nil
}

func notAuthenticated(err error) error {
	return fmt.Errorf("not authenticated to ConfigHub — run `cub auth login` (interactive) and retry: %w", err)
}

// clientTransport adds a User-Agent header and optional request/response dumping
// around a base RoundTripper, and remembers the server version each response
// carried. Requests can be in flight on several goroutines, so the recorded
// version is guarded rather than a plain field.
type clientTransport struct {
	base    http.RoundTripper
	agent   string
	debug   bool
	version atomic.Pointer[string]
}

func (t *clientTransport) serverVersion() string {
	if v := t.version.Load(); v != nil {
		return *v
	}
	return ""
}

func (t *clientTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("User-Agent", t.agent)
	if t.debug {
		if dump, err := httputil.DumpRequestOut(r, true); err == nil {
			fmt.Println(string(dump))
		}
	}
	res, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if v := res.Header.Get(ServerVersionHeader); v != "" {
		t.version.Store(&v)
	}
	if t.debug {
		if dump, derr := httputil.DumpResponse(res, true); derr == nil {
			fmt.Println(string(dump))
		}
	}
	return res, nil
}
