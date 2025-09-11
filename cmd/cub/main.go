// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/itchyny/gojq"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/confighub/sdk/configkit/yamlkit"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

const (
	CONFIGHUB_DIR = ".confighub"
)

//go:embed cub-overview.md
var overviewFS embed.FS

//go:embed cub-agent-overview.md
var agentsFS embed.FS

var (
	// Global API client instance
	cubClientNew *goclientnew.ClientWithResponses

	// Global context manager instance
	contextManager *ContextManager

	ctx = context.Background()
)

type CubTransport struct {
	RoundTripper http.RoundTripper
	Agent        string
	Debug        bool
}

func (ct *CubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("User-Agent", ct.Agent)

	if ct.Debug {
		dump, err := httputil.DumpRequestOut(r, true)
		if err != nil {
			return nil, err
		}
		fmt.Println(string(dump))
	}
	res, err := ct.RoundTripper.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if ct.Debug {
		dump, err := httputil.DumpResponse(res, true)
		if err != nil {
			return res, err
		}
		fmt.Println(string(dump))
	}
	return res, nil
}

var IsAgent bool = os.Getenv("CONFIGHUB_AGENT") != ""

// Helper functions for dynamic help text generation

// getCommandHelp returns help text for commands with optional agent context
func getCommandHelp(baseHelp string, agentContext string) string {
	if IsAgent && agentContext != "" {
		return baseHelp + "\n\n" + agentContext
	}
	return baseHelp
}

func getFormattedOverview() string {
	if IsAgent {
		content, err := agentsFS.ReadFile("cub-agent-overview.md")
		if err == nil {
			return string(content)
		}
	}

	content, err := overviewFS.ReadFile("cub-overview.md")
	if err != nil {
		// Fallback to simple text if markdown can't be read
		return `Command line tool for using ConfigHub
To change the default confighub host, set CONFIGHUB_URL environment variable.`
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(80),
	)
	if err != nil || IsAgent {
		// Fallback to raw markdown if glamour fails
		return string(content)
	}

	formatted, err := renderer.Render(string(content))
	if err != nil {
		// Fallback to raw markdown if rendering fails
		return string(content)
	}

	return formatted
}

func getSimpleHelp() string {
	return `Command line tool for using ConfigHub
To change the default confighub host, set CONFIGHUB_URL environment variable.

AI agents should set CONFIGHUB_AGENT=1 for all cub commands with --help, and execute cub --help-overview to display a detailed overview.`
}

func saveAgentsFile() error {
	content, err := agentsFS.ReadFile("cub-agent-overview.md")
	if err != nil {
		return err
	}

	configHubDir := filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR)
	err = os.MkdirAll(configHubDir, 0755)
	if err != nil {
		return err
	}

	agentsFile := filepath.Join(configHubDir, "agents.md")
	err = os.WriteFile(agentsFile, content, 0644)
	if err != nil {
		return err
	}

	return nil
}

var rootCmd = &cobra.Command{
	Use:   "cub",
	Short: "ConfigHub CLI",
	Long:  getSimpleHelp(),
}

// Global context flag
var globalContextFlag string

func globalPreRun(cmd *cobra.Command, args []string) error {
	if debug {
		err := os.Setenv("CONFIGHUB_DEBUG", "1")
		if err != nil {
			return err
		}
	} else if os.Getenv("CONFIGHUB_DEBUG") == "1" {
		// Required for the new goclientnew.Client Debug mode
		fmt.Printf("cub Debug mode enabled. version: %s, buildDate: %s\n", BuildTag, BuildDate)
		debug = true
	}
	var err error
	if globalContextFlag != "" {
		err = contextManager.OverrideCurrentContext(globalContextFlag)
		if err != nil {
			return err
		}
	}

	cubClientNew, err = InitializeClient(contextManager.ActiveContext())
	if err != nil {
		return err
	}

	return nil
}

func main() {
	var err error
	// CUB_CONFIG is optional. If not set, the default path will be used.

	contextManager, err = NewContextManagerWithPath(os.Getenv("CUB_CONFIG"))
	if err != nil {
		failOnError(err)
	}
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Debug output")
	rootCmd.PersistentFlags().StringVar(&globalContextFlag, "context", "", "The context to use for this command")

	// Add --help-overview flag
	var helpOverview bool
	rootCmd.Flags().BoolVar(&helpOverview, "help-overview", false, "Show detailed overview instead of standard help")

	// Store original help function before overriding
	originalHelpFunc := rootCmd.HelpFunc()

	// Override the help function to handle --help-overview
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if helpOverview {
			fmt.Print(getFormattedOverview())
			return
		}
		// Use the original help function
		originalHelpFunc(cmd, args)
	})

	// This turns off printing Usage after an error
	rootCmd.SilenceUsage = true
	// We don't want root command to print errors. We'll do it ourselves.
	rootCmd.SilenceErrors = true

	rootCmd.PersistentPreRunE = globalPreRun

	err = rootCmd.Execute()
	failOnError(err)
}

func setAuthHeader(authSession *AuthSession) goclientnew.RequestEditorFn {
	return func(ctx context.Context, r *http.Request) error {
		authHeaderToken := setAuthHeaderToken(authSession)
		if authHeaderToken != "" {
			r.Header.Set("Authorization", authHeaderToken)
		}
		return nil
	}
}

func setAuthHeaderToken(authSession *AuthSession) string {
	var authHeaderToken string
	if authSession.AuthType == AuthTypeBasic {
		encoded := base64.StdEncoding.EncodeToString([]byte(authSession.User.Email + ":" + authSession.BasicAuthPassword))
		authHeaderToken = fmt.Sprintf("Basic %s", encoded)
	} else {
		authHeaderToken = fmt.Sprintf("Bearer %s", authSession.AccessToken)
	}
	return authHeaderToken
}

// InitializeClient initializes the API client for the given context.
// It sets the base URL and the authentication header if a token is present.
// If the context is updated during the course of execution and further API calls are made,
// then this function should be called again to update the API client.
func InitializeClient(ctx *Context) (*goclientnew.ClientWithResponses, error) {
	ct := &CubTransport{
		RoundTripper: http.DefaultTransport,
		Agent:        "cub",
		Debug:        debug,
	}
	baseURL := ctx.Coordinate.ServerURL + "/api"
	hasToken := true
	var authHeader string
	tokenData, err := contextManager.LoadTokenData(ctx)
	if err != nil {
		hasToken = false
	} else {
		authHeader = fmt.Sprintf("Bearer %s", tokenData.AccessToken)
	}

	return goclientnew.NewClientWithResponses(baseURL, func(c *goclientnew.Client) error {
		c.Client = &http.Client{Transport: ct}
		if hasToken {
			c.RequestEditors = append(c.RequestEditors, func(ctx context.Context, req *http.Request) error {
				req.Header.Set("Authorization", authHeader)
				return nil
			})
		}
		return nil
	})
}

// Do not call this directly from a command for error responses from API requests.
// Call InterpretErrorGeneric and return the result instead.
func failOnError(err error) {
	if err != nil {
		tprintErr("Failed: %s", err.Error())
		os.Exit(1)
	}
}

func tableView() *tablewriter.Table {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetTablePadding("    ")
	table.SetNoWhiteSpace(true)
	return table
}

func detailView() *tablewriter.Table {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(true)
	table.SetBorder(false)
	table.SetTablePadding("    ")
	table.SetNoWhiteSpace(true)
	return table
}

func mapToString(m map[string]string) string {
	var arr []string
	for key, value := range m {
		arr = append(arr, fmt.Sprintf("%s=%s", key, value))
	}
	return strings.Join(arr, ",")
}

func labelsToString(labels map[string]string) string {
	return mapToString(labels)
}

func deleteGatesToString(deleteGates map[string]bool) string {
	if deleteGates == nil || len(deleteGates) == 0 {
		return ""
	}
	var keys []string
	for key := range deleteGates {
		keys = append(keys, key)
	}
	return strings.Join(keys, ", ")
}

func annotationsToString(annotations map[string]string) string {
	return mapToString(annotations)
}

func uuidPtrToString(uuidPtr *goclientnew.UUID) string {
	if uuidPtr != nil && *uuidPtr != uuid.Nil {
		return uuidPtr.String()
	}
	return ""
}

// tprint stands for terminal print
func tprint(format string, args ...interface{}) {
	// Ensure there are no leading newlines and exactly one trailing newline.
	format = strings.Trim(format, "\n") + "\n"
	fmt.Printf(format, args...)
}

func tprintErr(format string, args ...interface{}) {
	red := color.New(color.FgRed).Add(color.Bold)
	redf := red.SprintFunc()
	// Ensure there are no leading newlines and exactly one trailing newline.
	format = strings.Trim(format, "\n") + "\n"
	fmt.Fprint(os.Stderr, redf(fmt.Sprintf(format, args...)))
}

func tprintRaw(output string) {
	// Ensure there are no leading newlines and exactly one trailing newline.
	output = strings.Trim(output, "\n") + "\n"
	fmt.Print(output)
}

func readFile(fileName string) []byte {
	data, err := os.ReadFile(fileName)
	failOnError(err)
	return data
}

func readStdin() ([]byte, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func fetchContent(source string) ([]byte, error) {
	if source == "" {
		return nil, errors.New("source cannot be empty")
	}

	// Handle stdin
	if source == "-" {
		return readStdin()
	}

	// Handle file:// URLs
	if filePath, found := strings.CutPrefix(source, "file://"); found {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
		}
		return data, nil
	}

	// Handle HTTPS URLs only
	if strings.HasPrefix(source, "https://") {
		return fetchWithHTTP(source)
	}

	// Handle local files (backward compatibility - no prefix)
	if !strings.Contains(source, "://") {
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, err
		}
		return data, nil
	}

	return nil, fmt.Errorf("unsupported URL scheme: %s (only file:// and https:// are supported)", source)
}

func fetchWithHTTP(source string) ([]byte, error) {
	// Only allow HTTPS URLs for security
	if !strings.HasPrefix(source, "https://") {
		return nil, fmt.Errorf("only HTTPS URLs are supported, got: %s", source)
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make HTTP request
	resp, err := client.Get(source)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", source, err)
	}
	defer resp.Body.Close()

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch %s, Code: %d, Status: %s", source, resp.StatusCode, resp.Status)
	}

	// Read response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response from %s: %w", source, err)
	}

	return data, nil
}

var flagPopulateModelFromStdin = false
var flagReplace = false
var flagFilename = ""
var where = ""
var filter = ""
var contains = ""
var verbose = false
var quiet = false
var jsonOutput = false
var yamlOutput = false
var jq = ""
var yq = ""
var names = false
var selectFields = ""
var debug = false
var noheader = false
var wait = true
var timeout = "2m"
var label []string
var deleteGate []string
var spaceIdentifiers []string
var allowExists bool

func enableLabelFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&label, "label", []string{}, "labels in key=value format; can separate by commas and/or use multiple instances of the flag")
}

func enableAllowExistsFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&allowExists, "allow-exists", false, "Allow creation of resources that already exist")
}

func enableDeleteGateFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&deleteGate, "delete-gate", []string{}, "delete gates in key[=true] format; can separate by commas and/or use multiple instances of the flag")
}

func setLabels(labelMap *map[string]string) error {
	if label != nil && len(label) != 0 {
		if *labelMap == nil {
			*labelMap = map[string]string{}
		}
		for _, labelString := range label {
			keyValue := strings.Split(labelString, "=")
			switch len(keyValue) {
			case 1:
				(*labelMap)[keyValue[0]] = ""
			case 2:
				// Note: For patch operations, value "-" indicates removal and is handled
				// by BuildPatchData and EnhancePatchData functions. This function only
				// handles non-patch (Put) operations where removal is not supported.
				(*labelMap)[keyValue[0]] = keyValue[1]
			default:
				return fmt.Errorf("invalid label; expected key=value: %s", labelString)
			}
		}
	}
	return nil
}

func setDeleteGates(deleteGateMap *map[string]bool) error {
	if deleteGate != nil && len(deleteGate) != 0 {
		if *deleteGateMap == nil {
			*deleteGateMap = map[string]bool{}
		}
		for _, deleteGateString := range deleteGate {
			keyValue := strings.Split(deleteGateString, "=")
			switch len(keyValue) {
			case 1:
				(*deleteGateMap)[keyValue[0]] = true
			case 2:
				// Note: For patch operations, value "-" indicates removal and is handled
				// by BuildPatchData and EnhancePatchData functions. This function only
				// handles non-patch (Put) operations where removal is not supported.
				if keyValue[1] != "true" {
					return fmt.Errorf("invalid delete-gate value; only 'true' is allowed: %s", deleteGateString)
				}
				(*deleteGateMap)[keyValue[0]] = true
			default:
				return fmt.Errorf("invalid delete-gate; expected key or key=true: %s", deleteGateString)
			}
		}
	}
	return nil
}

func enableFromStdinFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&flagPopulateModelFromStdin, "from-stdin", false, "Read the ConfigHub entity JSON (e.g., retrieved with cub <entity> get --quiet --json) from stdin; merged with command arguments on create, and merged with command arguments and existing entity on update")
}

func enableReplaceFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&flagReplace, "replace", false, "Replace entity instead of merging when using --from-stdin or --filename")
}

func enableFilenameFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&flagFilename, "filename", "", "Read the ConfigHub entity JSON from file, URL (https://), or stdin (-); mutually exclusive with --from-stdin")
}

func enableVerboseFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Detailed output, additive with default output")
}

func enableQuietFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&quiet, "quiet", false, "No default output. Use with --jq to get specific output")
}

func enableQuietFlagForOperation(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&quiet, "quiet", false, "No default output.")
}

func enableNoheaderFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&noheader, "no-header", false, "No header for lists")
}

func enableJsonFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "JSON output, suppressing default output")
}

func enableYamlFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&yamlOutput, "yaml", false, "YAML output, suppressing default output")
}

func enableNamesFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&names, "names", false, "Only output names, suppressing default output")
}

func enableJqFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&jq, "jq", "", "jq expression, suppressing default output")
}

func enableYqFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&yq, "yq", "", "yq expression, suppressing default output")
}

func enableSelectFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&selectFields, "select", "", "Comma-separated list of fields to retrieve and display. Entity IDs and Slug are always included. Example: \"DisplayName,CreatedAt,Labels\"")
}

func enableWhereFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&where, "where", "", "Filter expression using SQL-inspired syntax. Supports conjunctions with AND. String operators: =, !=, <, >, <=, >=, LIKE, ILIKE, ~~, !~~, ~, ~*, !~, !~*. Pattern matching with LIKE/ILIKE uses % and _ wildcards. Regex operators (~, ~*, !~, !~*) support POSIX regular expressions. Examples: \"Slug LIKE 'app-%'\", \"DisplayName ILIKE '%backend%'\", \"Slug ~ '^[a-z]+-[0-9]+$'\"")
}

func enableFilterFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&filter, "filter", "", "Filter entity to apply to the list. Specify as 'space/filter' for cross-space filters or just 'filter' for current space. Supports both slugs and UUIDs. The filter will be combined with any --where clause using AND logic. Examples: \"production-filters/security-check\", \"my-filter-uuid\", \"validation-rules\"")
}

func hasAlternativeOutput() bool {
	return jsonOutput || jq != "" || yamlOutput || yq != "" || names
}

// parseFilterFlag parses the filter flag and returns the filter ID
// Supports formats:
// - "filter-slug" (uses current space)
// - "space-slug/filter-slug" (uses specified space)
// - "filter-uuid" (direct UUID)
// - "space-uuid/filter-slug" (uses specified space)
func parseFilterFlag(filterValue string) (string, error) {
	if filterValue == "" {
		return "", nil
	}

	uuid, err := parseEntityIdentifierSingle[goclientnew.Filter](
		filterValue,
		EntityTypeFilter,
		apiGetFilterFromSlugInSpace,
		func(f *goclientnew.Filter) string { return f.FilterID.String() },
	)
	if err != nil {
		return "", err
	}
	return uuid.String(), nil
}

// Entity type constants for consistent naming across the codebase
const (
	EntityTypeFilter       = "Filter"
	EntityTypeView         = "View"
	EntityTypeInvocation   = "Invocation"
	EntityTypeTrigger      = "Trigger"
	EntityTypeTag          = "Tag"
	EntityTypeChangeSet    = "ChangeSet"
	EntityTypeTarget       = "Target"
	EntityTypeBridgeWorker = "BridgeWorker"
	EntityTypeUnit         = "Unit"
	EntityTypeLink         = "Link"
	EntityTypeSet          = "Set"
)

// EntityInSpace type constraint for all entities that reside in spaces
type EntityInSpace interface {
	goclientnew.Filter | goclientnew.View | goclientnew.Invocation |
		goclientnew.Trigger | goclientnew.Tag | goclientnew.ChangeSet |
		goclientnew.Target | goclientnew.BridgeWorker | goclientnew.Unit |
		goclientnew.Link | goclientnew.Set
}

// apiGetEntityFromSlugInSpaceFunc is a function type for getting entities by slug in a space
type apiGetEntityFromSlugInSpaceFunc[T any] func(slug string, spaceID string, selectParam string) (*T, error)

// parseEntityIdentifiersAsEntities parses entity identifiers in various formats:
// - UUID (direct entity UUID)
// - Slug (using current space)
// - space/slug or space-uuid/slug
// Returns the full entities
func parseEntityIdentifiersAsEntities[T EntityInSpace](
	identifiers []string,
	entityType string,
	selectParam string,
	apiGetFunc apiGetEntityFromSlugInSpaceFunc[T],
	getEntityID func(*T) string,
) ([]T, error) {
	var entities []T

	// Default selectParam to entityType + "ID" if empty
	if selectParam == "" {
		selectParam = entityType + "ID"
	}

	for _, identifier := range identifiers {
		// Try to parse as UUID directly first
		if id, err := uuid.Parse(identifier); err == nil {
			// For UUIDs, we need to get the entity from the current space
			entity, err := apiGetFunc(id.String(), selectedSpaceID, selectParam)
			if err != nil {
				return nil, fmt.Errorf("%s with ID '%s' not found: %w", entityType, id.String(), err)
			}
			entities = append(entities, *entity)
			continue
		}

		// Split on "/" to check for space/entity format
		parts := strings.Split(identifier, "/")

		var spaceID string
		var entitySlug string

		if len(parts) == 1 {
			// Format: "entity-slug" - determine which space to use
			entitySlug = parts[0]

			// Priority: selectedSpaceID (from --space flag) > context default space
			if selectedSpaceID != "*" && selectedSpaceID != "" {
				// Use explicitly selected space (from --space flag)
				spaceID = selectedSpaceID
			} else {
				// Fall back to default space from context
				cubContext := contextManager.CurrentContext()
				if cubContext == nil || cubContext.Settings.DefaultSpace == "" {
					return nil, fmt.Errorf("no space for %s selected, specified, or in current context", entityType)
				}
				// Look up space (handles both UUIDs and slugs)
				space, err := apiGetSpaceFromSlug(cubContext.Settings.DefaultSpace, "SpaceID")
				if err != nil {
					return nil, fmt.Errorf("space '%s' not found: %w", cubContext.Settings.DefaultSpace, err)
				}
				spaceID = space.SpaceID.String()
			}
		} else if len(parts) == 2 {
			// Format: "space/entity-slug"
			// Look up space (handles both UUIDs and slugs)
			space, err := apiGetSpaceFromSlug(parts[0], "SpaceID")
			if err != nil {
				return nil, fmt.Errorf("space '%s' not found: %w", parts[0], err)
			}
			spaceID = space.SpaceID.String()
			entitySlug = parts[1]
		} else {
			return nil, fmt.Errorf("invalid %s identifier format: %s", entityType, identifier)
		}

		// Look up the entity by slug in the specified space
		entity, err := apiGetFunc(entitySlug, spaceID, selectParam)
		if err != nil {
			return nil, fmt.Errorf("%s '%s' not found in space %s: %w", entityType, entitySlug, spaceID, err)
		}

		entities = append(entities, *entity)
	}

	return entities, nil
}

// parseEntityIdentifiers is a wrapper that returns UUIDs by extracting them from the entities
func parseEntityIdentifiers[T EntityInSpace](
	identifiers []string,
	entityType string,
	apiGetFunc apiGetEntityFromSlugInSpaceFunc[T],
	getEntityID func(*T) string,
) ([]uuid.UUID, error) {
	// Get entities with minimal select (just ID field)
	entities, err := parseEntityIdentifiersAsEntities(identifiers, entityType, "", apiGetFunc, getEntityID)
	if err != nil {
		return nil, err
	}

	// Extract UUIDs from entities
	uuids := make([]uuid.UUID, len(entities))
	for i, entity := range entities {
		entityIDStr := getEntityID(&entity)
		entityUUID, err := uuid.Parse(entityIDStr)
		if err != nil {
			return nil, fmt.Errorf("invalid UUID from entity: %w", err)
		}
		uuids[i] = entityUUID
	}

	return uuids, nil
}

// parseEntityIdentifierSingle parses a single entity identifier using parseEntityIdentifiers
// Returns the UUID directly
func parseEntityIdentifierSingle[T EntityInSpace](
	identifier string,
	entityType string,
	apiGetFunc apiGetEntityFromSlugInSpaceFunc[T],
	getEntityID func(*T) string,
) (uuid.UUID, error) {
	if identifier == "" {
		return uuid.Nil, fmt.Errorf("%s value cannot be empty", entityType)
	}

	uuids, err := parseEntityIdentifiers([]string{identifier}, entityType, apiGetFunc, getEntityID)
	if err != nil {
		return uuid.Nil, err
	}

	if len(uuids) != 1 {
		return uuid.Nil, fmt.Errorf("unexpected number of UUIDs returned for %s", entityType)
	}

	return uuids[0], nil
}

// parseEntityIdentifierSingleAsEntity parses a single entity identifier and returns the full entity
func parseEntityIdentifierSingleAsEntity[T EntityInSpace](
	identifier string,
	entityType string,
	selectParam string,
	apiGetFunc apiGetEntityFromSlugInSpaceFunc[T],
	getEntityID func(*T) string,
) (*T, error) {
	if identifier == "" {
		return nil, fmt.Errorf("%s value cannot be empty", entityType)
	}

	entities, err := parseEntityIdentifiersAsEntities([]string{identifier}, entityType, selectParam, apiGetFunc, getEntityID)
	if err != nil {
		return nil, err
	}

	if len(entities) != 1 {
		return nil, fmt.Errorf("unexpected number of entities returned for %s", entityType)
	}

	return &entities[0], nil
}

func parseTagSlug(tagValue string) (string, error) {
	uuid, err := parseEntityIdentifierSingle[goclientnew.Tag](
		tagValue,
		EntityTypeTag,
		apiGetTagFromSlugInSpace,
		func(t *goclientnew.Tag) string { return t.TagID.String() },
	)
	if err != nil {
		return "", err
	}
	return uuid.String(), nil
}

func parseChangeSetSlug(changesetValue string) (uuid.UUID, error) {
	return parseEntityIdentifierSingle[goclientnew.ChangeSet](
		changesetValue,
		EntityTypeChangeSet,
		apiGetChangeSetFromSlugInSpace,
		func(c *goclientnew.ChangeSet) string { return c.ChangeSetID.String() },
	)
}

func parseInvocationSlug(invocationValue string) (uuid.UUID, error) {
	return parseEntityIdentifierSingle[goclientnew.Invocation](
		invocationValue,
		EntityTypeInvocation,
		apiGetInvocationFromSlugInSpace,
		func(i *goclientnew.Invocation) string { return i.InvocationID.String() },
	)
}

func enableContainsFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&contains, "contains", "", "Free text search for entities containing the specified text. Searches across string fields (like Slug, DisplayName) and map fields (like Labels, Annotations). Case-insensitive matching. Can be combined with --where using AND logic. Example: \"backend\" to find entities with backend in any searchable field")
}

func enableWaitFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&wait, "wait", true, "wait for completion")
	cmd.Flags().StringVar(&timeout, "timeout", "2m", "completion timeout as a duration with units, such as 10s or 2m")
}

type Unmarshalable interface {
	UnmarshalBinary(data []byte) error
}

func addStandardListFlags(cmd *cobra.Command) {
	enableWhereFlag(cmd)
	enableFilterFlag(cmd)
	enableContainsFlag(cmd)
	enableSelectFlag(cmd)
	enableNamesFlag(cmd)
	enableQuietFlag(cmd)
	enableJsonFlag(cmd)
	enableJqFlag(cmd)
	enableYamlFlag(cmd)
	enableYqFlag(cmd)
	enableNoheaderFlag(cmd)
}

func addStandardCreateFlags(cmd *cobra.Command) {
	enableLabelFlag(cmd)
	enableDeleteGateFlag(cmd)
	enableAllowExistsFlag(cmd)
	enableFromStdinFlag(cmd)
	enableFilenameFlag(cmd)
	enableVerboseFlag(cmd)
	enableQuietFlag(cmd)
	enableJsonFlag(cmd)
	enableJqFlag(cmd)
	enableYamlFlag(cmd)
	enableYqFlag(cmd)
}

func addStandardGetFlags(cmd *cobra.Command) {
	enableQuietFlag(cmd)
	enableJsonFlag(cmd)
	enableJqFlag(cmd)
	enableYamlFlag(cmd)
	enableYqFlag(cmd)
	enableSelectFlag(cmd)
}

func addStandardUpdateFlags(cmd *cobra.Command) {
	enableLabelFlag(cmd)
	enableDeleteGateFlag(cmd)
	enableFromStdinFlag(cmd)
	enableReplaceFlag(cmd)
	enableFilenameFlag(cmd)
	enableVerboseFlag(cmd)
	enableQuietFlag(cmd)
	enableJsonFlag(cmd)
	enableJqFlag(cmd)
	enableYamlFlag(cmd)
	enableYqFlag(cmd)
}

func addStandardDeleteFlags(cmd *cobra.Command) {
	enableVerboseFlag(cmd)
	enableQuietFlag(cmd)
	enableJsonFlag(cmd)
	enableJqFlag(cmd)
	enableYamlFlag(cmd)
	enableYqFlag(cmd)
}

// TODO: Move to a reusable library

var SlugCoreChars string = "\\-_.A-Za-z0-9"
var SlugInvalidRegexpString string = "[^" + SlugCoreChars + "]"
var SlugInvalidRegexp = regexp.MustCompile(SlugInvalidRegexpString)
var multipleDashesString = "---*"
var multipleDashesRegexp = regexp.MustCompile(multipleDashesString)

func makeSlug(providedText string) string {
	// Note: Not lowercased and not converted to kabob-case
	// Also not internationalized.

	// Remove leading and trailing spaces
	slug := strings.TrimSpace(providedText)
	// Remove invalid characters
	slug = SlugInvalidRegexp.ReplaceAllString(slug, "-")
	// Collapse multiple consecutive dashes. There could be consecutive punctuation.
	slug = multipleDashesRegexp.ReplaceAllString(slug, "-")
	// Trim leading and trailing punctuation
	slug = strings.Trim(slug, "-._")
	// Don't allow UUIDs
	_, err := uuid.Parse(slug)
	if err == nil {
		slug = "id" + slug
	}
	return slug
}

// Functionality for populating entities from stdin.

func populateNewModelFromStdin(v interface{}) error {
	jsonBytes, err := readStdin()
	if err != nil {
		return err
	}
	return mergeEntityWithData(v, jsonBytes)
}

func mergeEntityWithData(v any, data []byte) error {
	// Hopefully this also works fine for JSON
	return yaml.Unmarshal(data, v)
}

func populateModelFromFile(v any, filename string) error {
	data, err := fetchContent(filename)
	if err != nil {
		return err
	}
	return mergeEntityWithData(v, data)
}

func validateSpaceFlag(bulk bool) error {
	if !bulk && selectedSpaceID == "*" {
		return errors.New("--space must not be '*' when not performing bulk operations")
	}
	return nil
}

func validateStdinFlags() error {
	if flagPopulateModelFromStdin && flagFilename != "" {
		return errors.New("--from-stdin and --filename are mutually exclusive")
	}
	return nil
}

// getBytesFromFlags returns raw bytes from --from-stdin or --filename flags
func getBytesFromFlags() ([]byte, error) {
	if flagPopulateModelFromStdin {
		return readStdin()
	} else if flagFilename != "" {
		return fetchContent(flagFilename)
	}
	return nil, nil
}

// populateModelFromFlags handles both --from-stdin and --filename flags
func populateModelFromFlags(v any) error {
	data, err := getBytesFromFlags()
	if err != nil {
		return err
	}
	if data != nil {
		return mergeEntityWithData(v, data)
	}
	return nil
}

func displayJSON(entity any) {
	outBytes, err := json.MarshalIndent(entity, "", "  ")
	failOnError(err)
	tprintRaw(string(outBytes))
}

func displayJQForBytes(outBytes []byte, jqExpr string) {
	var tree any
	err := json.Unmarshal(outBytes, &tree)
	failOnError(err)
	jqQuery, err := gojq.Parse(jqExpr)
	failOnError(err)
	iter := jqQuery.Run(tree)
	for {
		value, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := value.(error); ok {
			if err, ok := err.(*gojq.HaltError); ok && err.Value() == nil {
				break
			}
			failOnError(err)
		}
		switch v := value.(type) {
		case string, int, bool:
			tprint("%v", v)
		default:
			displayJSON(value)
		}
	}
}

func displayJQ(entity any) {
	outBytes, err := json.Marshal(entity)
	failOnError(err)
	displayJQForBytes(outBytes, jq)
}

func displayYAML(entity any) {
	outBytes, err := yaml.Marshal(entity)
	failOnError(err)
	tprintRaw(string(outBytes))
}

func displayYQ(entity any) {
	outBytes, err := yaml.Marshal(entity)
	failOnError(err)
	yqBytes, err := yamlkit.EvalYQExpression(yq, string(outBytes))
	failOnError(err)
	tprintRaw(string(yqBytes))
}

type ModelConstraint interface {
	goclientnew.Link |
		goclientnew.ExtendedLink |
		goclientnew.Organization |
		goclientnew.OrganizationMember |
		goclientnew.User |
		goclientnew.ExtendedBridgeWorker |
		goclientnew.BridgeWorker |
		goclientnew.BridgeWorkerStatus |
		goclientnew.Revision |
		goclientnew.ExtendedRevision |
		goclientnew.Mutation |
		goclientnew.ExtendedMutation |
		goclientnew.Set |
		goclientnew.ExtendedSet |
		goclientnew.Space |
		goclientnew.ExtendedSpace |
		goclientnew.Target |
		goclientnew.ExtendedTarget |
		goclientnew.Trigger |
		goclientnew.ExtendedTrigger |
		goclientnew.Filter |
		goclientnew.ExtendedFilter |
		goclientnew.View |
		goclientnew.ExtendedView |
		goclientnew.Invocation |
		goclientnew.ExtendedInvocation |
		goclientnew.Tag |
		goclientnew.ExtendedTag |
		goclientnew.ChangeSet |
		goclientnew.ExtendedChangeSet |
		goclientnew.Unit |
		goclientnew.UnitEvent |
		goclientnew.ExtendedUnit |
		Context
}

type DeleteConstraint interface {
	goclientnew.DeleteResponse |
		goclientnew.DeleteBridgeWorkerResponse |
		goclientnew.DeleteChangeSetResponse |
		goclientnew.DeleteFilterResponse |
		goclientnew.DeleteInvocationResponse |
		goclientnew.DeleteLinkResponse |
		goclientnew.DeleteOrganizationResponse |
		goclientnew.DeleteOrganizationMemberResponse |
		goclientnew.DeleteSetResponse |
		goclientnew.DeleteSpaceResponse |
		goclientnew.DeleteTagResponse |
		goclientnew.DeleteTargetResponse |
		goclientnew.DeleteTriggerResponse |
		goclientnew.DeleteUnitResponse |
		goclientnew.DeleteViewResponse
}

func displayCreateResults[Entity ModelConstraint](entity *Entity, entityName, slug, id string, display func(entity *Entity)) {
	// Check if any alternative output format is specified
	hasAlternativeOutput := hasAlternativeOutput()

	if !quiet && !hasAlternativeOutput {
		tprint("Successfully created %s %s (%s)", entityName, slug, id)
	}
	if verbose {
		display(entity)
	}
	if jsonOutput {
		displayJSON(entity)
	}
	if jq != "" {
		displayJQ(entity)
	}
	if yamlOutput {
		displayYAML(entity)
	}
	if yq != "" {
		displayYQ(entity)
	}
}

func displayUpdateResults[Entity ModelConstraint](entity *Entity, entityName, slug, id string, display func(entity *Entity)) {
	// Check if any alternative output format is specified
	hasAlternativeOutput := hasAlternativeOutput()

	if !quiet && !hasAlternativeOutput {
		tprint("Successfully updated %s %s (%s)", entityName, slug, id)
	}
	if verbose {
		display(entity)
	}
	if jsonOutput {
		displayJSON(entity)
	}
	if jq != "" {
		displayJQ(entity)
	}
	if yamlOutput {
		displayYAML(entity)
	}
	if yq != "" {
		displayYQ(entity)
	}
}

func displayListResults[Entity ModelConstraint](entities []*Entity, getSlug func(entity *Entity) string, display func(entities []*Entity)) {
	// Check if any alternative output format is specified
	hasAlternativeOutput := hasAlternativeOutput()

	if (!quiet || verbose) && !hasAlternativeOutput {
		display(entities)
	}
	if names && getSlug != nil {
		table := tableView()
		for _, entity := range entities {
			table.Append([]string{
				getSlug(entity),
			})
		}
		table.Render()
	}
	if jsonOutput {
		displayJSON(entities)
	}
	if jq != "" {
		displayJQ(entities)
	}
	if yamlOutput {
		displayYAML(entities)
	}
	if yq != "" {
		displayYQ(entities)
	}
}

func displayGetResults[Entity ModelConstraint](entity *Entity, display func(entity *Entity)) {
	// Check if any alternative output format is specified
	hasAlternativeOutput := hasAlternativeOutput()

	if !quiet && !hasAlternativeOutput {
		display(entity)
	}
	if jsonOutput {
		displayJSON(entity)
	}
	if jq != "" {
		displayJQ(entity)
	}
	if yamlOutput {
		displayYAML(entity)
	}
	if yq != "" {
		displayYQ(entity)
	}
}

func displayDeleteResults[ResponseType DeleteConstraint](entityName, slug, id string, response *ResponseType) {
	// Check if any alternative output format is specified
	hasAlternativeOutput := hasAlternativeOutput()

	if !quiet && !hasAlternativeOutput {
		tprint("Successfully deleted %s %s (%s)", entityName, slug, id)
	}
	if jsonOutput {
		displayJSON(response)
	}
	if jq != "" {
		displayJQ(response)
	}
	if yamlOutput {
		displayYAML(response)
	}
	if yq != "" {
		displayYQ(response)
	}
}

// addSpaceIDToWhereClause adds space constraint to where clause, for reuse across commands
func addSpaceIDToWhereClause(whereClause, spaceID string) string {
	if spaceID == "*" {
		return whereClause
	}
	spaceConstraint := fmt.Sprintf("SpaceID = '%s'", spaceID)
	if whereClause != "" {
		return fmt.Sprintf("%s AND %s", whereClause, spaceConstraint)
	}
	return spaceConstraint
}

// displayBulkDeleteResults handles the display of bulk delete operation results
func displayBulkDeleteResults(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, entityName, operationName, contextInfo string) error {
	var responses *[]goclientnew.DeleteResponse
	if statusCode == 200 && responses200 != nil {
		responses = responses200
	} else if statusCode == 207 && responses207 != nil {
		responses = responses207
	} else {
		return fmt.Errorf("unexpected status code %d or no response data", statusCode)
	}

	if responses == nil {
		return fmt.Errorf("no response data received")
	}

	successCount := 0
	failureCount := 0
	var failedErrors []*goclientnew.ResponseError
	var successMessages []string

	for _, resp := range *responses {
		if resp.Error == nil {
			successCount++
			if verbose && resp.Message != "" {
				successMessages = append(successMessages, resp.Message)
			}
		} else {
			failureCount++
			failedErrors = append(failedErrors, resp.Error)
		}
	}

	// Check if any alternative output format is specified
	hasAlternativeOutput := hasAlternativeOutput()

	// Display output based on format flags
	if jsonOutput {
		displayJSON(responses)
	}
	if jq != "" {
		displayJQ(responses)
	}
	if yamlOutput {
		displayYAML(responses)
	}
	if yq != "" {
		displayYQ(responses)
	}

	// Display regular output unless quiet or alternative output is specified
	if !quiet && !hasAlternativeOutput {
		// Display verbose success messages
		if verbose && len(successMessages) > 0 {
			for _, msg := range successMessages {
				fmt.Printf("Successfully %sd %s: %s\n", operationName, entityName, msg)
			}
		}

		// Display summary
		fmt.Printf("\nBulk %s operation completed:\n", operationName)
		fmt.Printf("  Success: %d %s(s)\n", successCount, entityName)
		if failureCount > 0 {
			fmt.Printf("  Failed: %d %s(s)\n", failureCount, entityName)
		}
		if contextInfo != "" {
			fmt.Printf("  Context: %s\n", contextInfo)
		}

		// Display detailed errors if verbose
		if verbose && len(failedErrors) > 0 {
			fmt.Printf("\nFailures:\n")
			displayResponseErrorTable(failedErrors)
		}
	}

	// Return error based on status code and failure count
	if statusCode == 207 || failureCount > 0 {
		return fmt.Errorf("bulk %s partially failed: %d succeeded, %d failed", operationName, successCount, failureCount)
	}

	return nil
}

// Generic display function for entity types
func displayBulkGenericCreateOrUpdateResults[T any](
	responses200 *[]T,
	responses207 *[]T,
	statusCode int,
	entityName string,
	operationName string,
	contextInfo string,
	getError func(*T) *goclientnew.ResponseError,
	getName func(*T) string,
) error {
	var responses *[]T
	if statusCode == 200 && responses200 != nil {
		responses = responses200
	} else if statusCode == 207 && responses207 != nil {
		responses = responses207
	} else {
		return fmt.Errorf("unexpected status code %d or no response data", statusCode)
	}

	if responses == nil {
		return fmt.Errorf("no response data received")
	}

	successCount := 0
	failureCount := 0
	var successNames []string
	var failedErrors []*goclientnew.ResponseError

	for i := range *responses {
		resp := &(*responses)[i]
		if err := getError(resp); err != nil {
			failureCount++
			failedErrors = append(failedErrors, err)
		} else {
			successCount++
			if verbose {
				if name := getName(resp); name != "" {
					successNames = append(successNames, name)
				}
			}
		}
	}

	// Check if any alternative output format is specified
	hasAlternativeOutput := hasAlternativeOutput()

	// Display output based on format flags
	if jsonOutput {
		displayJSON(responses)
	}
	if jq != "" {
		displayJQ(responses)
	}
	if yamlOutput {
		displayYAML(responses)
	}
	if yq != "" {
		displayYQ(responses)
	}

	// Display regular output unless quiet or alternative output is specified
	if !quiet && !hasAlternativeOutput {
		// Display verbose success messages
		if verbose && len(successNames) > 0 {
			for _, name := range successNames {
				fmt.Printf("Successfully %sd %s: %s\n", operationName, entityName, name)
			}
		}

		// Display failed entities if any
		if len(failedErrors) > 0 {
			fmt.Printf("\nFailed to %s %ss:\n", operationName, entityName)
			displayResponseErrorTable(failedErrors)
		}

		// Display summary
		fmt.Printf("\nBulk %s operation completed:\n", operationName)
		fmt.Printf("  Success: %d %s(s)\n", successCount, entityName)
		if failureCount > 0 {
			fmt.Printf("  Failed: %d %s(s)\n", failureCount, entityName)
		}
		if contextInfo != "" {
			fmt.Printf("  Context: %s\n", contextInfo)
		}
	}

	// Return error based on status code and failure count
	if statusCode == 207 || failureCount > 0 {
		return fmt.Errorf("bulk %s partially failed: %d succeeded, %d failed", operationName, successCount, failureCount)
	}

	return nil
}
