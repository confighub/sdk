// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Config writing (config_write.go) is the mutation half of the kubeconfig-style
// [Store]: creating, renaming, and deleting contexts, persisting the config and
// token files, and first-run initialization. It complements the read/resolve
// methods in config.go. Friendly context-name generation is intentionally left
// to the caller (the cub CLI); [Store.CreateContext] takes an explicit name.
package cubapi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultServerURL is the ConfigHub server used for a context when none is given.
const DefaultServerURL = "https://hub.confighub.com"

// SaveConfig writes the configuration to the config file (creating the config
// directory if needed).
func (s *Store) SaveConfig() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveConfigLocked()
}

func (s *Store) saveConfigLocked() error {
	data, err := yaml.Marshal(s.config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.configPath), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(s.configPath, data, 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", s.configPath, err)
	}
	return nil
}

// CreateContext adds a new context with the given (required, valid, unique) name.
// serverURL, organization, and defaultSpace fall back to defaults when empty. The
// first context created becomes the current one. It mutates the in-memory config
// only; call [Store.SaveConfig] to persist.
func (s *Store) CreateContext(name, serverURL, organization, defaultSpace string) (*Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if name == "" {
		return nil, fmt.Errorf("context name is required")
	}
	if !isValidContextName(name) {
		return nil, fmt.Errorf("invalid context name: %s (must be 32 characters or less and match ^[a-zA-Z0-9][a-zA-Z0-9_-]*$)", name)
	}
	if _, err := s.contextLocked(name); err == nil {
		return nil, fmt.Errorf("context %q already exists", name)
	}

	if defaultSpace == "" {
		defaultSpace = "default"
	}
	if serverURL == "" {
		serverURL = DefaultServerURL
	}
	tokenPath := filepath.Join(s.tokenDir, name+".json")
	ctx := &Context{
		Name:       name,
		Coordinate: Coordinate{ServerURL: serverURL, OrganizationID: organization},
		Settings:   Settings{DefaultSpace: defaultSpace},
		Metadata:   Metadata{TokenFile: toRelativeHomePath(tokenPath), Created: time.Now()},
	}
	s.config.Contexts = append(s.config.Contexts, ctx)
	if len(s.config.Contexts) == 1 {
		s.config.CurrentContext = name
	}
	return ctx, nil
}

// SetCurrentContext makes name the current context and stamps its LastUsed. It
// mutates in-memory config only; call [Store.SaveConfig] to persist.
func (s *Store) SetCurrentContext(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setCurrentContextLocked(name)
}

func (s *Store) setCurrentContextLocked(name string) error {
	ctx, err := s.contextLocked(name)
	if err != nil {
		return err
	}
	s.config.CurrentContext = name
	ctx.Metadata.LastUsed = time.Now()
	return nil
}

// GetAllContextNames returns the names of all configured contexts.
func (s *Store) GetAllContextNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.config.Contexts))
	for _, ctx := range s.config.Contexts {
		if ctx.Name != "" {
			names = append(names, ctx.Name)
		}
	}
	return names
}

// FindContextByCoordinate returns the context whose coordinate matches.
func (s *Store) FindContextByCoordinate(coordinate Coordinate) (*Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ctx := range s.config.Contexts {
		if ctx.Coordinate.Equals(coordinate) {
			return ctx, nil
		}
	}
	return nil, fmt.Errorf("context not found")
}

// DeleteContext removes a context (and its token file) and persists the config.
// The last remaining context cannot be deleted; deleting the current context
// switches to another.
func (s *Store) DeleteContext(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, err := s.contextLocked(name)
	if err != nil {
		return err
	}

	if s.config.CurrentContext == name {
		var remaining []string
		for _, c := range s.config.Contexts {
			if c.Name != name {
				remaining = append(remaining, c.Name)
			}
		}
		if len(remaining) == 0 {
			return fmt.Errorf("cannot delete last context")
		}
		if err := s.setCurrentContextLocked(remaining[0]); err != nil {
			return err
		}
	}

	tokenPath := s.TokenPath(ctx.Metadata.TokenFile)
	if err := os.Remove(tokenPath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: failed to delete token file: %v\n", err)
	}

	s.config.Contexts = slices.DeleteFunc(s.config.Contexts, func(c *Context) bool {
		return c.Name == name
	})
	return s.saveConfigLocked()
}

// RenameContext renames a context, moving its token file and updating the current
// context if it was the one renamed. It mutates in-memory config (and the token
// file on disk) only; call [Store.SaveConfig] to persist the config.
func (s *Store) RenameContext(oldName, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !isValidContextName(newName) {
		return fmt.Errorf("invalid context name: %s (must be 32 characters or less and match ^[a-zA-Z0-9][a-zA-Z0-9_-]*$)", newName)
	}
	if _, err := s.contextLocked(newName); err == nil {
		return fmt.Errorf("context %q already exists", newName)
	}
	ctx, err := s.contextLocked(oldName)
	if err != nil {
		return err
	}

	ctx.Name = newName
	oldTokenPath := s.TokenPath(ctx.Metadata.TokenFile)
	newTokenRel := filepath.Join(s.tokenDir, newName+".json")
	newTokenFull := s.TokenPath(toRelativeHomePath(newTokenRel))
	if _, err := os.Stat(oldTokenPath); err == nil {
		if err := os.Rename(oldTokenPath, newTokenFull); err != nil {
			return fmt.Errorf("rename token file: %w", err)
		}
	}
	ctx.Metadata.TokenFile = toRelativeHomePath(newTokenRel)

	if s.config.CurrentContext == oldName {
		s.config.CurrentContext = newName
	}
	return nil
}

// SaveTokenData writes a context's credentials to its token file (creating the
// token directory with secure permissions if needed).
func (s *Store) SaveTokenData(ctx *Context, token *TokenData) error {
	tokenPath := s.TokenPath(ctx.Metadata.TokenFile)
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		return fmt.Errorf("create token directory: %w", err)
	}
	if err := os.WriteFile(tokenPath, data, 0o600); err != nil {
		return fmt.Errorf("write token file %s: %w", tokenPath, err)
	}
	return nil
}

// DeleteTokenData removes a context's token file (a missing file is not an error).
func (s *Store) DeleteTokenData(ctx *Context) error {
	tokenPath := s.TokenPath(ctx.Metadata.TokenFile)
	if err := os.Remove(tokenPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete token file %s: %w", tokenPath, err)
	}
	return nil
}

// LoadOrInit loads the config at path, or — when no config file exists yet —
// creates a fresh one with a single default context named defaultContextName
// (server and space defaulted), makes it current, and persists it. It is the
// first-run initialization a CLI runs on startup. If path is empty,
// [DefaultConfigPath] is used.
func LoadOrInit(path, defaultContextName string) (*Store, error) {
	abs, err := resolveConfigPath(path)
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(abs); statErr == nil {
		return LoadConfig(abs)
	} else if !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("stat config %s: %w", abs, statErr)
	}

	store := newStore(abs)
	if _, err := store.CreateContext(defaultContextName, "", "", ""); err != nil {
		return nil, err
	}
	if err := store.SaveConfig(); err != nil {
		return nil, err
	}
	return store, nil
}

// isValidContextName reports whether name is a valid context name: 1–32 chars,
// starting alphanumeric, rest alphanumeric/dash/underscore.
func isValidContextName(name string) bool {
	if name == "" || len(name) > 32 {
		return false
	}
	if !isAlphaNum(name[0]) {
		return false
	}
	for _, c := range name[1:] {
		if !isAlphaNum(byte(c)) && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

func isAlphaNum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// toRelativeHomePath rewrites an absolute path under $HOME to a ~-prefixed path,
// so token file references in the config are portable.
func toRelativeHomePath(absolutePath string) string {
	home := os.Getenv("HOME")
	if home == "" || !strings.HasPrefix(absolutePath, home) {
		return absolutePath
	}
	return "~" + strings.TrimPrefix(absolutePath, home)
}
