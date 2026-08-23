// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"

	"github.com/confighub/sdk/core/cubapi"
	cubbyname "github.com/confighub/sdk/core/cubbyname"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

const OrgNameLookupFailure = "Org Name Lookup Failure"

// The config types are defined in github.com/confighub/sdk/core/cubapi and
// aliased here so cub's context handling shares one on-disk contract with the
// SDK Store. (The methods on these types — e.g. Coordinate.Equals — live in
// cubapi.)
type (
	Config     = cubapi.Config
	Context    = cubapi.Context
	Coordinate = cubapi.Coordinate
	Settings   = cubapi.Settings
	Metadata   = cubapi.Metadata
	TokenData  = cubapi.TokenData
)

// ContextManager handles all context operations. It is a thin adapter over
// cubapi.Store, which owns the on-disk config/token format; this type preserves
// cub's historical method surface (panicking accessors, friendly context-name
// generation, the OverrideCurrentContext name) over the Store's API.
type ContextManager struct {
	store *cubapi.Store
}

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Context commands",
	Long: getCommandHelp(
		`Commands for managing locally stored ConfigHub authentication contexts.`,
		"Commands operate on local state. Server and authentication not required.",
	),
}

func init() {
	rootCmd.AddCommand(contextCmd)
}

// NewContextManager creates a context manager at the default config location.
func NewContextManager() (*ContextManager, error) {
	return NewContextManagerWithPath("")
}

// NewContextManagerWithPath creates a context manager for the given config path
// (empty means the default ~/.confighub/config.yaml). On first run — when no
// config file exists yet — it creates a default context.
func NewContextManagerWithPath(configPath string) (*ContextManager, error) {
	store, err := cubapi.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	cm := &ContextManager{store: store}

	// Ensure the config and token directories exist.
	configDir := filepath.Dir(store.ConfigPath())
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "tokens"), 0o700); err != nil {
		return nil, fmt.Errorf("failed to create token directory: %w", err)
	}
	// Private keys live beside tokens and are owner-only for the same reason:
	// anything that can read one can authenticate as the identity it belongs to.
	if err := os.MkdirAll(filepath.Join(configDir, "keys"), 0o700); err != nil {
		return nil, fmt.Errorf("failed to create key directory: %w", err)
	}

	// First run: create a default context (which becomes current) and persist.
	if len(store.Contexts()) == 0 {
		ctx, err := store.CreateContext(pickContextName(nil), "", "", "")
		if err != nil {
			return nil, err
		}
		ctx.Metadata.Description = "Default context created by cub CLI on first run"
		if err := store.SetCurrentContext(ctx.Name); err != nil {
			return nil, err
		}
		if err := store.SaveConfig(); err != nil {
			return nil, err
		}
	}
	return cm, nil
}

// --- adapter methods over cubapi.Store ---

// SaveConfig persists the configuration to disk.
func (cm *ContextManager) SaveConfig() error { return cm.store.SaveConfig() }

// ConfigPath returns the absolute path of the config file.
func (cm *ContextManager) ConfigPath() string { return cm.store.ConfigPath() }

// GetContext retrieves a context by name.
func (cm *ContextManager) GetContext(name string) (*Context, error) {
	return cm.store.Context(name)
}

// FindContextByCoordinate finds a context by coordinate.
func (cm *ContextManager) FindContextByCoordinate(coordinate Coordinate) (*Context, error) {
	return cm.store.FindContextByCoordinate(coordinate)
}

// GetAllContextNames returns all context names.
func (cm *ContextManager) GetAllContextNames() []string {
	return cm.store.GetAllContextNames()
}

// ListContexts returns all contexts.
func (cm *ContextManager) ListContexts() []*Context { return cm.store.Contexts() }

// CurrentContextName returns the name of the current context (ignoring any
// override).
func (cm *ContextManager) CurrentContextName() string { return cm.store.CurrentContextName() }

// CurrentContext returns the current context as recorded in the config file. It
// panics if there is no current context (there always should be after init).
func (cm *ContextManager) CurrentContext() *Context {
	ctx, err := cm.store.Context(cm.store.CurrentContextName())
	if err != nil {
		panic(err)
	}
	return ctx
}

// ActiveContext returns the active context: the override if one is set (see
// OverrideCurrentContext), otherwise the current context. It panics if there is
// no active context.
func (cm *ContextManager) ActiveContext() *Context {
	ctx, err := cm.store.ActiveContext()
	if err != nil {
		panic(err)
	}
	return ctx
}

// ActiveContextName returns the name of the active context — the override if one
// is set, otherwise the current context — or "" when there is none. Unlike
// ActiveContext it does not panic, so display code can mark the active row
// without turning a hand-edited config into a stack trace.
func (cm *ContextManager) ActiveContextName() string {
	ctx, err := cm.store.ActiveContext()
	if err != nil {
		return ""
	}
	return ctx.Name
}

// SetCurrentContext sets the current context (stamping its LastUsed). Call
// SaveConfig to persist.
func (cm *ContextManager) SetCurrentContext(name string) error {
	return cm.store.SetCurrentContext(name)
}

// OverrideCurrentContext sets the active context for the lifetime of the process
// without changing the persisted current context.
func (cm *ContextManager) OverrideCurrentContext(name string) error {
	return cm.store.Use(name)
}

// NewContext creates a new context with a generated friendly name.
func (cm *ContextManager) NewContext() *Context {
	ctx, _ := cm.store.CreateContext(pickContextName(cm.store.GetAllContextNames()), "", "", "")
	return ctx
}

// CreateContext creates a new context; an empty name gets a generated friendly
// name. Call SaveConfig to persist.
func (cm *ContextManager) CreateContext(name, serverURL, organization, defaultSpace string) (*Context, error) {
	if name == "" {
		name = pickContextName(cm.store.GetAllContextNames())
	}
	return cm.store.CreateContext(name, serverURL, organization, defaultSpace)
}

// DeleteContext deletes a context (and its token file) and persists the config.
func (cm *ContextManager) DeleteContext(name string) error {
	return cm.store.DeleteContext(name)
}

// RenameContext renames a context (moving its token file). Call SaveConfig to
// persist the config change.
func (cm *ContextManager) RenameContext(oldName, newName string) error {
	return cm.store.RenameContext(oldName, newName)
}

// TokenPath resolves a context's token file to an absolute path.
func (cm *ContextManager) TokenPath(tokenFile string) string {
	return cm.store.TokenPath(tokenFile)
}

// KeyPath resolves a private-key reference to an absolute path. A bare name is
// an alias within the key directory; anything with a separator is a path.
func (cm *ContextManager) KeyPath(key string) string {
	return cm.store.KeyPath(key)
}

// KeyDir is the directory bare key aliases resolve within.
func (cm *ContextManager) KeyDir() string {
	return cm.store.KeyDir()
}

// LoadTokenData loads the token data for a context.
func (cm *ContextManager) LoadTokenData(ctx *Context) (*TokenData, error) {
	return cm.store.TokenData(ctx)
}

// SaveTokenData saves the token data for a context.
func (cm *ContextManager) SaveTokenData(ctx *Context, token *TokenData) error {
	return cm.store.SaveTokenData(ctx, token)
}

// DeleteTokenData deletes the token data for a context.
func (cm *ContextManager) DeleteTokenData(ctx *Context) error {
	return cm.store.DeleteTokenData(ctx)
}

// --- endpoints the server advertises for a context's server URL ---

// ociServerURLFromApiInfo builds the OCI registry URL for a server from what it
// advertises at /api/info. The scheme is taken from the API's own URL: the
// response carries a host and a port but nothing that says whether the registry
// speaks TLS, and an http API fronts a plaintext registry. The port is omitted
// when it is the scheme's default, so the result matches what a puller would
// write by hand.
//
// The result is empty when the server advertises no OCI host — an older server,
// or one running with the registry disabled — so callers get nothing rather than
// an endpoint that never answers.
func ociServerURLFromApiInfo(serverURL string, apiInfo *goclientnew.ApiInfo) string {
	if apiInfo == nil || apiInfo.OCIHost == "" {
		return ""
	}
	scheme := "https"
	// Anything else (including a bare "host:port", which parses as an opaque URL
	// with the host as its scheme) is not something to propagate.
	if u, err := url.Parse(serverURL); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		scheme = u.Scheme
	}
	host := apiInfo.OCIHost
	if port := apiInfo.OCIPort; port != "" && !isDefaultPort(scheme, port) {
		host = net.JoinHostPort(host, port)
	}
	return scheme + "://" + host
}

// isDefaultPort reports whether port is the one a client assumes for scheme.
func isDefaultPort(scheme, port string) bool {
	return (scheme == "https" && port == "443") || (scheme == "http" && port == "80")
}

// --- friendly context-name generation (cub UX, not part of the on-disk format) ---

// pickContextName returns a friendly context name not already in inUse: a random
// unused predefined name, or a predefined name with a random suffix when all are
// taken.
func pickContextName(inUse []string) string {
	available := difference(cubbyname.List(), inUse)
	if len(available) > 0 {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(available))))
		if err != nil {
			return available[0]
		}
		return available[idx.Int64()]
	}
	for {
		suffix := generateRandomSuffix(5)
		baseIdx, err := rand.Int(rand.Reader, big.NewInt(int64(len(predefinedContextNames))))
		if err != nil {
			baseIdx = big.NewInt(0)
		}
		candidate := predefinedContextNames[baseIdx.Int64()] + "-" + suffix
		if !slices.Contains(inUse, candidate) {
			return candidate
		}
	}
}

// difference returns elements in a that are not in b.
func difference(a, b []string) []string {
	result := make([]string, 0)
	for _, item := range a {
		if !slices.Contains(b, item) {
			result = append(result, item)
		}
	}
	return result
}

// generateRandomSuffix generates a random alphanumeric lowercase string.
func generateRandomSuffix(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			result[i] = charset[0]
		} else {
			result[i] = charset[idx.Int64()]
		}
	}
	return string(result)
}

var predefinedContextNames = []string{
	"honey-paw",
	"fuzzy-ears",
	"berry-snout",
	"little-claws",
	"paw-print",
	"cuddle-paw",
	"beary-cute",
	"honey-whiskers",
	"cubby-nest",
	"fluffy-muzzle",
	"sleepy-cub",
	"berry-paws",
	"snuggle-bear",
	"bamboo-cub",
	"chubby-paws",
	"cozy-cub",
	"sweet-growl",
	"teddy-tracks",
	"playful-cub",
	"gentle-paw",
}
