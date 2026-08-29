// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Config loading (config.go) is a kubeconfig-style loader for ConfigHub CLI
// configuration. It reads the on-disk format that `cub` writes — a config file
// (`~/.confighub/config.yaml`) holding named contexts, plus per-context token
// files (`~/.confighub/tokens/<name>.json`) — and resolves the active context
// and its credentials for use by other Go tools and `cub` plugins.
//
// It is the analog of client-go's clientcmd: a reusable way to obtain "client
// info" (server URL, organization, default space, access token) without
// shelling out to `cub`. It has no dependency on cobra and holds no global
// state; construct a [Store] with [LoadConfig] and call methods on it.
//
// This is the read/resolve half of `cub`'s ContextManager. Write operations
// (creating, renaming, and deleting contexts; persisting tokens) live with the
// `cub` CLI, which owns the login flow that produces them.
package cubapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// ConfigAPIVersion and ConfigKind identify the config file format. They
	// match the values `cub` writes.
	ConfigAPIVersion = "v1"
	ConfigKind       = "Config"

	// ConfigHubDir is the per-user ConfigHub directory under $HOME.
	ConfigHubDir = ".confighub"

	// ConfigFileName is the default config file name within ConfigHubDir.
	ConfigFileName = "config.yaml"
)

// Config is the root of the config file: a kubeconfig-like document with a list
// of named contexts and a pointer to the current one.
type Config struct {
	APIVersion     string     `yaml:"apiVersion" json:"apiVersion"`
	Kind           string     `yaml:"kind" json:"kind"`
	CurrentContext string     `yaml:"currentContext,omitempty" json:"currentContext,omitempty"`
	Contexts       []*Context `yaml:"contexts" json:"contexts"`
}

// Context is a named configuration: a coordinate identifying the server, a few
// settings, and metadata including which token file holds its credentials.
type Context struct {
	Name       string     `yaml:"name" json:"name"`
	Coordinate Coordinate `yaml:"coordinate" json:"coordinate"`
	Settings   Settings   `yaml:"settings" json:"settings"`
	Metadata   Metadata   `yaml:"metadata" json:"metadata"`
}

// Coordinate uniquely identifies a context: server URL, organization, and user.
type Coordinate struct {
	ServerURL      string `yaml:"serverURL" json:"serverURL"`
	OrganizationID string `yaml:"organizationID" json:"organizationID"`
	User           string `yaml:"user" json:"user"`
}

// Settings holds per-context preferences.
type Settings struct {
	DefaultSpace string `yaml:"defaultSpace" json:"defaultSpace"`
}

// Metadata holds optional, non-identifying context data.
type Metadata struct {
	TokenFile string `yaml:"tokenFile" json:"tokenFile"`

	// PrivateKey names the key this context authenticates with, in the form it
	// was given: an alias in the key directory, a path, or empty for a context
	// that authenticates some other way.
	//
	// It is a reference, never key material. Recording it is what lets a session
	// be renewed without being told again which key to use -- with a credential
	// that needs no human, an expired token is a detail the caller should not
	// have to handle.
	PrivateKey string `yaml:"privateKey,omitempty" json:"privateKey,omitempty"`

	OrganizationName string    `yaml:"organizationName,omitempty" json:"organizationName,omitempty"`
	Created          time.Time `yaml:"created,omitempty" json:"created,omitempty"`
	LastUsed         time.Time `yaml:"lastUsed,omitempty" json:"lastUsed,omitempty"`
	Description      string    `yaml:"description,omitempty" json:"description,omitempty"`
}

// TokenData is the contents of a token file.
type TokenData struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
}

// Equals reports whether two coordinates identify the same context.
func (c Coordinate) Equals(other Coordinate) bool {
	return c.User == other.User &&
		c.OrganizationID == other.OrganizationID &&
		SameServer(c.ServerURL, other.ServerURL)
}

// SameServer reports whether two URLs name the same ConfigHub.
//
// A trailing slash and the case of the scheme or host do not make a different
// server, but a string comparison says they do -- which splits one instance
// across two contexts, so a login updates one and a later command reads the
// other. Anything it cannot parse is compared as written.
func SameServer(a, b string) bool {
	return normalizeServerURL(a) == normalizeServerURL(b)
}

func normalizeServerURL(raw string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return trimmed
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
}

// Store loads and resolves ConfigHub CLI configuration from a config file plus a
// sibling token directory. Use [Load] to construct one. Methods are safe for
// concurrent use.
//
// Unlike `cub`'s ContextManager, a Store is read-only and never panics: a
// missing config file yields an empty but usable Store (no contexts), and
// resolving the active context returns an error rather than panicking when none
// is set.
type Store struct {
	mu         sync.Mutex
	configPath string
	tokenDir   string
	keyDir     string
	config     *Config
	override   string // in-memory active-context override; not persisted
}

// DefaultConfigPath returns the config file path to use when none is given:
// config.yaml inside $CUB_CONFIG if set, otherwise $HOME/.confighub/config.yaml.
//
// CUB_CONFIG names the config *directory*, not the file. That is what the
// published documentation says, what cub exports to plugins
// (`CUB_CONFIG=<dir>` in cmd/cub/plugin_exec.go), and what plugins already
// assume -- cub-che's installer builds "$CUB_CONFIG/plugins" from it. Returning
// the value verbatim treated it as the file, which put the sibling "tokens" and
// "keys" directories one level too high and split the store in two.
func DefaultConfigPath() (string, error) {
	if p := os.Getenv("CUB_CONFIG"); p != "" {
		// A path that exists and is not a directory is the one mistake worth
		// naming: it means CUB_CONFIG was set to config.yaml itself, which used
		// to work. Left alone it surfaces further down as "open
		// .../config.yaml/config.yaml: not a directory", which describes the
		// symptom rather than the cause. A path that does not exist yet is
		// fine -- the store creates it.
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return "", fmt.Errorf(
				"CUB_CONFIG must name the config directory, but %s is a file; did you mean %s?",
				p, filepath.Dir(p))
		}
		return filepath.Join(p, ConfigFileName), nil
	}
	home := os.Getenv("HOME")
	if home == "" {
		return "", errors.New("HOME environment variable not set; pass an explicit config path")
	}
	return filepath.Join(home, ConfigHubDir, ConfigFileName), nil
}

// LoadConfig reads the config at path and returns a Store. If path is empty,
// [DefaultConfigPath] is used. The token directory is the sibling "tokens"
// directory next to the config file.
//
// A missing config file is not an error: the returned Store has no contexts.
// This lets callers fall back to other credential sources (e.g. plugin
// environment variables) without special-casing a first run.
func LoadConfig(path string) (*Store, error) {
	abs, err := resolveConfigPath(path)
	if err != nil {
		return nil, err
	}
	s := newStore(abs)

	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil // empty but usable
		}
		return nil, fmt.Errorf("read config %s: %w", abs, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", abs, err)
	}
	if err := validateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", abs, err)
	}
	s.config = &cfg
	return s, nil
}

// resolveConfigPath turns a (possibly empty or ~-prefixed) config path into an
// absolute path, defaulting to [DefaultConfigPath] when empty.
func resolveConfigPath(path string) (string, error) {
	if path == "" {
		var err error
		if path, err = DefaultConfigPath(); err != nil {
			return "", err
		}
	}
	if strings.HasPrefix(path, "~") {
		home := os.Getenv("HOME")
		if home == "" {
			return "", errors.New("HOME environment variable not set; cannot expand ~ in config path")
		}
		path = filepath.Join(home, path[1:])
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve config path: %w", err)
	}
	return abs, nil
}

// newStore builds an empty Store for the given absolute config path, with the
// token and key directories as the sibling "tokens" and "keys" directories.
func newStore(absPath string) *Store {
	return &Store{
		configPath: absPath,
		tokenDir:   filepath.Join(filepath.Dir(absPath), "tokens"),
		keyDir:     filepath.Join(filepath.Dir(absPath), "keys"),
		config:     &Config{APIVersion: ConfigAPIVersion, Kind: ConfigKind},
	}
}

func validateConfig(cfg *Config) error {
	if cfg.APIVersion != ConfigAPIVersion {
		return fmt.Errorf("unsupported apiVersion %q", cfg.APIVersion)
	}
	if cfg.Kind != ConfigKind {
		return fmt.Errorf("unsupported kind %q", cfg.Kind)
	}
	names := make(map[string]bool, len(cfg.Contexts))
	for _, ctx := range cfg.Contexts {
		if names[ctx.Name] {
			return fmt.Errorf("duplicate context name %q", ctx.Name)
		}
		names[ctx.Name] = true
		if ctx.Coordinate.ServerURL == "" {
			return fmt.Errorf("context %q missing serverURL", ctx.Name)
		}
		if ctx.Metadata.TokenFile == "" {
			return fmt.Errorf("context %q missing tokenFile", ctx.Name)
		}
	}
	if cfg.CurrentContext != "" && !names[cfg.CurrentContext] {
		return fmt.Errorf("currentContext %q does not exist", cfg.CurrentContext)
	}
	return nil
}

// ConfigPath returns the absolute path of the loaded config file.
func (s *Store) ConfigPath() string { return s.configPath }

// Contexts returns all configured contexts.
func (s *Store) Contexts() []*Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config.Contexts
}

// Context returns the context with the given name.
func (s *Store) Context(name string) (*Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.contextLocked(name)
}

func (s *Store) contextLocked(name string) (*Context, error) {
	for _, ctx := range s.config.Contexts {
		if ctx.Name == name {
			return ctx, nil
		}
	}
	return nil, fmt.Errorf("context %q not found", name)
}

// CurrentContextName returns the name of the current context as recorded in the
// config file (ignoring any in-memory override). It is empty when no context is
// configured.
func (s *Store) CurrentContextName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config.CurrentContext
}

// Use sets an in-memory active-context override for the lifetime of this Store.
// It does not modify the config file. Passing "" clears the override.
func (s *Store) Use(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "" {
		s.override = ""
		return nil
	}
	if _, err := s.contextLocked(name); err != nil {
		return err
	}
	s.override = name
	return nil
}

// ActiveContext returns the context to use: the [Use] override if set, otherwise
// the current context from the config file. It returns an error when no context
// is configured.
func (s *Store) ActiveContext() (*Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := s.override
	if name == "" {
		name = s.config.CurrentContext
	}
	if name == "" {
		return nil, errors.New("no active context configured; run `cub auth login` or `cub context`")
	}
	return s.contextLocked(name)
}

// TokenPath resolves a context's token file to an absolute path, matching `cub`:
// absolute paths are used as-is, "~"-prefixed paths expand against $HOME, and
// bare names resolve within the Store's token directory.
func (s *Store) TokenPath(tokenFile string) string {
	if filepath.IsAbs(tokenFile) {
		return tokenFile
	}
	if strings.HasPrefix(tokenFile, "~") {
		return filepath.Join(os.Getenv("HOME"), tokenFile[1:])
	}
	return filepath.Join(s.tokenDir, filepath.Base(tokenFile))
}

// KeyDir is the directory bare key aliases resolve within.
func (s *Store) KeyDir() string { return s.keyDir }

// KeyPath resolves a private-key reference to an absolute path, by the same
// three rules as TokenPath: absolute paths are used as-is, "~"-prefixed paths
// expand against $HOME, and a bare name is an *alias* resolving within the
// Store's key directory.
//
// An alias gains the ".jwk" suffix when it has no extension, so `--private-key
// ci` and `--private-key ci.jwk` name the same key. Only a bare name is treated
// as an alias; anything containing a separator is a path, so "./ci" is a file
// in the working directory and never a lookup in the key directory.
func (s *Store) KeyPath(key string) string {
	if filepath.IsAbs(key) {
		return key
	}
	if strings.HasPrefix(key, "~") {
		return filepath.Join(os.Getenv("HOME"), key[1:])
	}
	if strings.ContainsRune(key, filepath.Separator) {
		return key
	}
	if filepath.Ext(key) == "" {
		key += ".jwk"
	}
	return filepath.Join(s.keyDir, key)
}

// TokenData loads the credentials for a context from its token file.
func (s *Store) TokenData(ctx *Context) (*TokenData, error) {
	path := s.TokenPath(ctx.Metadata.TokenFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read token file %s: %w", path, err)
	}
	var token TokenData
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("parse token file %s: %w", path, err)
	}
	return &token, nil
}
