// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// New context system structures
const (
	ConfigAPIVersion     = "v1"
	ConfigKind           = "Config"
	OrgNameLookupFailure = "Org Name Lookup Failure"
	DefaultServerURL     = "https://hub.confighub.com"
	ConfigHubDir         = ".confighub"
)

// Config represents the root configuration structure
type Config struct {
	APIVersion     string     `yaml:"apiVersion" json:"apiVersion"`
	Kind           string     `yaml:"kind" json:"kind"`
	CurrentContext string     `yaml:"currentContext,omitempty" json:"currentContext,omitempty"`
	Contexts       []*Context `yaml:"contexts" json:"contexts"`
}

// Context represents a named context configuration
// TODO: Naming doesn't feel ideal because we have "context.context" in various forms. This follows kubeconfig and might be ok.
type Context struct {
	Name       string     `yaml:"name" json:"name"`
	Coordinate Coordinate `yaml:"coordinate" json:"coordinate"`
	Settings   Settings   `yaml:"settings" json:"settings"`
	Metadata   Metadata   `yaml:"metadata" json:"metadata"`
}

// Coordinate represents a context coordinate
// This is a tuple of server URL, organization ID, and user.
// It is used to identify a context uniquely.
type Coordinate struct {
	ServerURL      string `yaml:"serverURL" json:"serverURL"`
	OrganizationID string `yaml:"organizationID" json:"organizationID"`
	User           string `yaml:"user" json:"user"`
}

type Settings struct {
	DefaultSpace string `yaml:"defaultSpace" json:"defaultSpace"`
}

// Metadata holds optional context metadata
type Metadata struct {
	TokenFile string `yaml:"tokenFile" json:"tokenFile"`
	// Org name is considered metadata because it's just a lookup based on the organization ID.
	OrganizationName string    `yaml:"organizationName,omitempty" json:"organizationName,omitempty"` // Display name (e.g., "ConfigHubOps")
	Created          time.Time `yaml:"created,omitempty" json:"created,omitempty"`
	LastUsed         time.Time `yaml:"lastUsed,omitempty" json:"lastUsed,omitempty"`
	Description      string    `yaml:"description,omitempty" json:"description,omitempty"`
}

// TokenData represents the token file structure
type TokenData struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
}

// ContextManager handles all context operations
type ContextManager struct {
	configPath      string
	tokenDir        string
	config          *Config
	mutex           sync.Mutex
	overrideContext *Context
}

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Context commands",
	Long:  `Context commands`,
}

func init() {
	rootCmd.AddCommand(contextCmd)
}

// NewContextManager creates a new context manager instance with the default config location
// If the config file does not exist, it creates a new one with a default context
func NewContextManager() (*ContextManager, error) {
	return NewContextManagerWithPath("")
}

// NewContextManagerWithPath creates a new context manager instance with a custom config path
// If configPath is empty it uses the default location ~/.confighub/config.yaml
func NewContextManagerWithPath(configPath string) (*ContextManager, error) {
	if configPath == "" {
		homeDir := os.Getenv("HOME")
		if homeDir == "" {
			return nil, errors.New("HOME environment variable not set. ContextManager must be initialized with a custom config path")
		}
		configPath = filepath.Join(homeDir, ConfigHubDir, "config.yaml")
	}

	// Expand ~ in the config path if present
	if strings.HasPrefix(configPath, "~") {
		homeDir := os.Getenv("HOME")
		if homeDir == "" {
			return nil, errors.New("HOME environment variable not set. ContextManager must be initialized with a custom config path")
		}
		configPath = filepath.Join(homeDir, configPath[1:])
	}

	// Make config path absolute
	var err error
	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config path: %w", err)
	}

	// Token directory is relative to config file location
	// If config is at /tmp/test/config.yaml, tokens go to /tmp/test/tokens/
	configDir := filepath.Dir(configPath)
	tokenDir := filepath.Join(configDir, "tokens")

	cm := &ContextManager{
		configPath: configPath,
		tokenDir:   tokenDir,
	}

	// Ensure directories exist
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.MkdirAll(tokenDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create token directory: %w", err)
	}

	// Load existing config or create new one
	err = cm.LoadConfig()
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// create an initial config
		cm.config = &Config{
			APIVersion: ConfigAPIVersion,
			Kind:       ConfigKind,
			Contexts:   []*Context{},
		}
		newCtx := cm.NewContext()
		newCtx.Metadata.Description = "Default context created by cub CLI on first run"
		cm.SetCurrentContext(newCtx.Name)

		cm.SaveConfig()
	}

	// The returned context manager should always have a current context. CurrentContext() will panic if there is no current context.
	// This can be made more robust over time. E.g. add more checks after loading the config file.
	// The key is that we don't want clients to have to check for errors once the context manager is initialized successfully.
	return cm, nil
}

// Equals returns true if the coordinate matches the other coordinate.
func (c *Coordinate) Equals(other Coordinate) bool {
	return (c.User == other.User) && (c.OrganizationID == other.OrganizationID) && (c.ServerURL == other.ServerURL)
}

// LoadConfig loads the configuration from disk
func (cm *ContextManager) LoadConfig() error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		return err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := cm.validateConfig(&config); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	cm.config = &config
	return nil
}

// SaveConfig saves the configuration to disk
func (cm *ContextManager) SaveConfig() error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	data, err := yaml.Marshal(cm.config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(cm.configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// validateConfig validates the configuration structure
func (cm *ContextManager) validateConfig(config *Config) error {
	if config.APIVersion != ConfigAPIVersion {
		return fmt.Errorf("unsupported API version: %s", config.APIVersion)
	}
	if config.Kind != ConfigKind {
		return fmt.Errorf("unsupported kind: %s", config.Kind)
	}

	// Validate context names are unique
	names := make(map[string]bool)
	for _, ctx := range config.Contexts {
		if names[ctx.Name] {
			return fmt.Errorf("duplicate context name: %s", ctx.Name)
		}
		names[ctx.Name] = true

		// Validate context data
		if ctx.Coordinate.ServerURL == "" {
			return fmt.Errorf("context %s missing server", ctx.Name)
		}
		// Organization can be empty initially - will be set during authentication
		if ctx.Metadata.TokenFile == "" {
			return fmt.Errorf("context %s missing tokenFile", ctx.Name)
		}
	}

	// Validate current context exists if set
	if config.CurrentContext != "" && !names[config.CurrentContext] {
		return fmt.Errorf("current context %s does not exist", config.CurrentContext)
	}

	return nil
}

// GetContext retrieves a context by name
func (cm *ContextManager) GetContext(name string) (*Context, error) {
	for _, ctx := range cm.config.Contexts {
		if ctx.Name == name {
			return ctx, nil
		}
	}
	return nil, fmt.Errorf("context %s not found", name)
}

// FindContextByCoordinate finds a context by coordinate
func (cm *ContextManager) FindContextByCoordinate(coordinate Coordinate) (*Context, error) {
	for _, ctx := range cm.config.Contexts {
		if ctx.Coordinate.Equals(coordinate) {
			return ctx, nil
		}
	}
	return nil, fmt.Errorf("context not found")
}

// GetAllContextNames returns a list of all context names
func (cm *ContextManager) GetAllContextNames() []string {
	var names []string
	if cm.config != nil {
		for _, ctx := range cm.config.Contexts {
			if ctx.Name != "" {
				names = append(names, ctx.Name)
			}
		}
	}
	return names
}

// CurrentContext returns the current context as set in the config file
func (cm *ContextManager) CurrentContext() *Context {
	ctx, err := cm.GetContext(cm.config.CurrentContext)
	if err != nil {
		// There should always be a current context.
		panic(err)
	}
	return ctx
}

// SetCurrentContext sets the current context. This does not affect the active context.
// It is typically used if you want to update the current context in the config file.
// You must call SaveConfig() to persist the change.
func (cm *ContextManager) SetCurrentContext(name string) error {
	if _, err := cm.GetContext(name); err != nil {
		return err
	}
	cm.config.CurrentContext = name

	// Update last used timestamp
	ctx, _ := cm.GetContext(name)
	// Note that it is up to the caller to save the context, so current context and last used timestamp
	// will not be updated in the config file unless the caller calls SaveConfig after this call.
	ctx.Metadata.LastUsed = time.Now()

	return nil
}

// ActiveContext returns the active context. The active context is the current context
// unless you have explicitly set a different active context with SetActiveContext().
// when saving the config, the active context is ignored. Current context is what will
// be saved to the config file.
func (cm *ContextManager) ActiveContext() *Context {
	if cm.overrideContext != nil {
		return cm.overrideContext
	}
	return cm.CurrentContext()
}

// OverrideCurrentContext sets the active context. This is a way of overriding the current context
// for the duration of the program. It is typically used when you want to use a different context
// for a single command but you don't want to change the current context in the config file.
func (cm *ContextManager) OverrideCurrentContext(name string) error {
	ctx, err := cm.GetContext(name)
	if err != nil {
		return err
	}
	cm.overrideContext = ctx
	// TODO: Consider updating last used timestamp for this context. But it would require us to
	// save the config file and right now this is up to the caller.
	return nil
}

// NewContext creates a new context with a random name. It is a convenience method for
// CreateContext with empty arguments
func (cm *ContextManager) NewContext() *Context {
	ctx, _ := cm.CreateContext("", "", "", "")
	// We can ignore errors because they only occur if the name parameter is not empty.
	return ctx
}

// CreateContext creates a new context
func (cm *ContextManager) CreateContext(name, serverURL, organization, defaultSpace string) (*Context, error) {
	// The mutex ensures that only one context can be created at a time and we avoid duplicate names
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if name == "" {
		name = cm.pickName()
	} else {
		// Check if context already exists
		if _, err := cm.GetContext(name); err == nil {
			return nil, fmt.Errorf("context %s already exists", name)
		}

		// Validate name format
		if !isValidContextName(name) {
			return nil, fmt.Errorf("invalid context name: %s (must be 32 characters or less and match pattern ^[a-zA-Z0-9][a-zA-Z0-9_-]*$)", name)
		}
	}
	// Organization should be a WorkOS ID if provided (e.g., "org_01JSQQ70M483A3B1FK3ZFS9Z1A")
	// We don't try to resolve names since we may not be authenticated yet
	// Users can either provide the exact WorkOS ID or leave it empty to select during auth

	// Default space to "default" if not provided
	if defaultSpace == "" {
		defaultSpace = "default"
	}
	if serverURL == "" {
		serverURL = DefaultServerURL
	}

	// Create token file path
	tokenPath := filepath.Join(cm.tokenDir, name+".json")

	// Create new context
	ctx := &Context{
		Name: name,
		Coordinate: Coordinate{
			ServerURL:      serverURL,
			OrganizationID: organization, // Use as-is, should be WorkOS ID or empty
			User:           "",           // Will be populated during authentication
		},
		Settings: Settings{
			DefaultSpace: defaultSpace,
		},
		Metadata: Metadata{
			TokenFile: toRelativeHomePath(tokenPath),
			Created:   time.Now(),
		},
	}

	cm.config.Contexts = append(cm.config.Contexts, ctx)

	// Set as current if it's the first context
	if len(cm.config.Contexts) == 1 {
		cm.config.CurrentContext = name
	}

	return ctx, nil
}

func (cm *ContextManager) pickName() string {
	// Get all in-use context names
	inUse := cm.GetAllContextNames()

	// Subtract in-use names from predefined names
	available := cm.difference(predefinedContextNames, inUse)

	// If we have available predefined names, pick one randomly
	if len(available) > 0 {
		// Generate random index
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(available))))
		if err != nil {
			// Fallback to first available name if random generation fails
			return available[0]
		}
		return available[idx.Int64()]
	}

	// If no predefined names available, pick a random predefined name and append random suffix
	for {
		// Generate random 5-character alphanumeric lowercase suffix
		suffix := cm.generateRandomSuffix(5)

		// Pick a random predefined name
		baseIdx, err := rand.Int(rand.Reader, big.NewInt(int64(len(predefinedContextNames))))
		if err != nil {
			// Fallback to first predefined name if random generation fails
			baseIdx = big.NewInt(0)
		}
		baseName := predefinedContextNames[baseIdx.Int64()]

		// Combine base name with suffix
		candidateName := baseName + "-" + suffix

		// Check if this name is not in use
		if !slices.Contains(inUse, candidateName) {
			return candidateName
		}
		// If in use, continue loop to try again
	}
}

// difference returns elements in a that are not in b
func (cm *ContextManager) difference(a, b []string) []string {
	result := make([]string, 0)
	for _, item := range a {
		if !slices.Contains(b, item) {
			result = append(result, item)
		}
	}
	return result
}

// generateRandomSuffix generates a random alphanumeric lowercase string of given length
func (cm *ContextManager) generateRandomSuffix(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			// Fallback to first character if random generation fails
			result[i] = charset[0]
		} else {
			result[i] = charset[idx.Int64()]
		}
	}
	return string(result)
}

// DeleteContext deletes a context. Current context cannot be deleted.
func (cm *ContextManager) DeleteContext(name string) error {
	ctx, err := cm.GetContext(name)
	if err != nil {
		return err
	}

	if cm.CurrentContext().Name == ctx.Name {
		contexts := cm.GetAllContextNames()
		// remove this context from list
		contexts = slices.DeleteFunc(contexts, func(ctx string) bool {
			return ctx == name
		})
		// if there is only one context left, use it
		if len(contexts) == 0 {
			return fmt.Errorf("cannot delete last context")
		} else {
			cm.SetCurrentContext(contexts[0])
		}
	}

	// Delete token file if it exists
	tokenPath := cm.TokenPath(ctx.Metadata.TokenFile)
	if err := os.Remove(tokenPath); err != nil && !os.IsNotExist(err) {
		// Log warning but don't fail
		fmt.Fprintf(os.Stderr, "Warning: failed to delete token file: %v\n", err)
	}

	// Remove from slice
	cm.config.Contexts = slices.DeleteFunc(cm.config.Contexts, func(ctx *Context) bool {
		return ctx.Name == name
	})

	return cm.SaveConfig()
}

// RenameContext renames a context
func (cm *ContextManager) RenameContext(oldName, newName string) error {
	if !isValidContextName(newName) {
		return fmt.Errorf("invalid context name: %s (must be 32 characters or less and match pattern ^[a-zA-Z0-9][a-zA-Z0-9_-]*$)", newName)
	}

	// Check if new name already exists
	if _, err := cm.GetContext(newName); err == nil {
		return fmt.Errorf("context %s already exists", newName)
	}

	ctx, err := cm.GetContext(oldName)
	if err != nil {
		return err
	}

	// Update context name
	ctx.Name = newName

	// Update token file path
	oldTokenPath := cm.TokenPath(ctx.Metadata.TokenFile)
	newTokenPath := filepath.Join(cm.tokenDir, newName+".json")
	newTokenFullPath := cm.TokenPath(toRelativeHomePath(newTokenPath))

	// Rename token file if it exists
	if _, err := os.Stat(oldTokenPath); err == nil {
		if err := os.Rename(oldTokenPath, newTokenFullPath); err != nil {
			return fmt.Errorf("failed to rename token file: %w", err)
		}
	}

	ctx.Metadata.TokenFile = toRelativeHomePath(newTokenPath)

	// Update current context if needed
	if cm.config.CurrentContext == oldName {
		cm.config.CurrentContext = newName
	}

	return nil
}

// ListContexts returns all contexts
func (cm *ContextManager) ListContexts() []*Context {
	return cm.config.Contexts
}

// TokenPath returns the full path for a token file
func (cm *ContextManager) TokenPath(tokenFile string) string {
	if filepath.IsAbs(tokenFile) {
		return tokenFile
	}
	// Handle relative paths from config
	if strings.HasPrefix(tokenFile, "~") {
		homeDir := os.Getenv("HOME")
		return filepath.Join(homeDir, tokenFile[1:])
	}
	// Default to token directory
	return filepath.Join(cm.tokenDir, filepath.Base(tokenFile))
}

// LoadTokenData loads the token data for a context
func (cm *ContextManager) LoadTokenData(ctx *Context) (*TokenData, error) {
	tokenPath := cm.TokenPath(ctx.Metadata.TokenFile)

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	var token TokenData
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token: %w", err)
	}

	return &token, nil
}

// SaveTokenData saves a token data for a context
func (cm *ContextManager) SaveTokenData(ctx *Context, token *TokenData) error {
	tokenPath := cm.TokenPath(ctx.Metadata.TokenFile)

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	// Ensure token directory exists
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0700); err != nil {
		return fmt.Errorf("failed to create token directory: %w", err)
	}

	// Write with secure permissions
	if err := os.WriteFile(tokenPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	// TODO: Consider if we should update LastUsed in the context metadata at this point.
	// It makes sense because saving the token implies that the user when through the auth flow.
	// However, it will require a file save of the main config file and that could be a surprising side effect.

	return nil
}

// DeleteTokenData deletes the token data for a context
func (cm *ContextManager) DeleteTokenData(ctx *Context) error {
	tokenPath := cm.TokenPath(ctx.Metadata.TokenFile)
	if err := os.Remove(tokenPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete token file: %w", err)
	}
	return nil
}

// isValidContextName validates context name format
func isValidContextName(name string) bool {
	// Main reason to keep this relatively short is to make list views easier to read.
	// If we find that we need longer names, this can be increased.
	if len(name) > 32 {
		return false
	}
	if name == "" {
		return false
	}
	// Must start with alphanumeric
	if !isAlphaNum(name[0]) {
		return false
	}
	// Rest can be alphanumeric, dash, or underscore
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

// toRelativeHomePath converts an absolute path to use ~ prefix if it's under the home directory
// For example: /Users/joe/.confighub/tokens/foo.json -> ~/.confighub/tokens/foo.json
func toRelativeHomePath(absolutePath string) string {
	homeDir := os.Getenv("HOME")
	if homeDir == "" || !strings.HasPrefix(absolutePath, homeDir) {
		return absolutePath
	}
	return "~" + strings.TrimPrefix(absolutePath, homeDir)
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
