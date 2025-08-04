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
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/gosimple/slug"
	"github.com/itchyny/gojq"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

const (
	CONFIGHUB_DIR = ".confighub"
)

//go:embed cub-overview.md
var overviewFS embed.FS

//go:embed cub-agent-overview.md
var agentsFS embed.FS

var ctx = context.Background()
var cubClientNew *goclientnew.ClientWithResponses
var authHeader goclientnew.RequestEditorFn
var authSession AuthSession

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

	// Add an authentication check to all commands
	var err error
	authSession, err = LoadSession()
	if err != nil {
		tprint("No session. Only unauthenticated commands will work")
	} else {
		authHeader = setAuthHeader(&authSession)
	}

	// Require authentication except for "login"
	if !slices.Contains([]string{"login", "test-login"}, cmd.Name()) && authSession.BasicAuthPassword == "" && authSession.AccessToken == "" {
		return errors.New("you must be authenticated to execute this command. Log in with the command: cub auth login")
	}

	cubClientNew, err = initializeClient()
	if err != nil {
		return err
	}

	return nil
}

func getEnvURL() *url.URL {
	baseURL := &url.URL{
		Scheme: "https",
		Host:   "hub.confighub.com",
		Path:   "/api",
	}
	if os.Getenv("CONFIGHUB_URL") != "" {
		cubContext.ConfigHubURL = os.Getenv("CONFIGHUB_URL")
		splitHost := strings.Split(os.Getenv("CONFIGHUB_URL"), "://")
		baseURL = &url.URL{
			Scheme: splitHost[0],
			Host:   splitHost[1],
			Path:   "/api",
		}
	} else {
		if cubContext.ConfigHubURLSaved != "" {
			// Part of experimental multi-context
			// If "ConfigHubURLSaved" exists in context.json and CONFIGHUB_URL is not set, then we use it.
			// This should get cleaned up later.
			cubContext.ConfigHubURL = cubContext.ConfigHubURLSaved
			var err error
			baseURL, err = url.Parse(cubContext.ConfigHubURL)
			if err != nil {
				failOnError(err)
			}
			baseURL.Path = "/api"
		} else {
			// Use default URL
			cubContext.ConfigHubURL = baseURL.Scheme + "://" + baseURL.Host
		}
	}
	// default to https if no scheme is provided
	if baseURL.Scheme == "" {
		baseURL.Scheme = "https"
	}
	return baseURL
}

func main() {
	LoadCubContext()
	_ = getEnvURL()
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Debug output")

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

	err := rootCmd.Execute()
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

func initializeClient() (*goclientnew.ClientWithResponses, error) {
	ct := &CubTransport{
		RoundTripper: http.DefaultTransport,
		Agent:        "cub",
		Debug:        debug,
	}
	baseURL := getEnvURL()

	return goclientnew.NewClientWithResponses(baseURL.String(), func(c *goclientnew.Client) error {
		c.Client = &http.Client{Transport: ct}
		if authHeader != nil {
			c.RequestEditors = append(c.RequestEditors, authHeader)
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

var flagPopulateModelFromStdin = false
var flagReplaceModelFromStdin = false
var where = ""
var contains = ""
var verbose = false
var quiet = false
var jsonOutput = false
var jq = ""
var names = false
var debug = false
var noheader = false
var wait = true
var timeout = "2m"
var label []string

func enableLabelFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&label, "label", []string{}, "labels in key=value format; can separate by commas and/or use multiple instances of the flag")
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
				(*labelMap)[keyValue[0]] = keyValue[1]
			default:
				return fmt.Errorf("invalid label; expected key=value: %s", labelString)
			}
		}
	}
	return nil
}

func enableFromStdinFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&flagPopulateModelFromStdin, "from-stdin", false, "Read the ConfigHub entity JSON (e.g., retrieved with cub <entity> get --quiet --json) from stdin; merged with command arguments on create, and merged with command arguments and existing entity on update")
}

func enableReplaceFromStdinFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&flagReplaceModelFromStdin, "replace-from-stdin", false, "Read the ConfigHub entity JSON (e.g., retrieved with cub <entity> get --quiet --json) from stdin; merged with command arguments on create, and merged with command arguments and replaces existing entity on update")
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

func enableNamesFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&names, "names", false, "Only output names, suppressing default output")
}

func enableJqFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&jq, "jq", "", "jq expression, suppressing default output")
}

func enableWhereFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&where, "where", "", "Filter expression using SQL-inspired syntax. Supports conjunctions with AND. String operators: =, !=, <, >, <=, >=, LIKE, ILIKE, ~~, !~~, ~, ~*, !~, !~*. Pattern matching with LIKE/ILIKE uses % and _ wildcards. Regex operators (~, ~*, !~, !~*) support POSIX regular expressions. Examples: \"Slug LIKE 'app-%'\", \"DisplayName ILIKE '%backend%'\", \"Slug ~ '^[a-z]+-[0-9]+$'\"")
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
	enableContainsFlag(cmd)
	enableNamesFlag(cmd)
	enableQuietFlag(cmd)
	enableJsonFlag(cmd)
	enableJqFlag(cmd)
	enableNoheaderFlag(cmd)
}

func addStandardCreateFlags(cmd *cobra.Command) {
	enableLabelFlag(cmd)
	enableFromStdinFlag(cmd)
	enableVerboseFlag(cmd)
	enableQuietFlag(cmd)
	enableJsonFlag(cmd)
	enableJqFlag(cmd)
}

func addStandardGetFlags(cmd *cobra.Command) {
	enableQuietFlag(cmd)
	enableJsonFlag(cmd)
	enableJqFlag(cmd)
}

func addStandardUpdateFlags(cmd *cobra.Command) {
	enableLabelFlag(cmd)
	enableFromStdinFlag(cmd)
	enableReplaceFromStdinFlag(cmd)
	enableVerboseFlag(cmd)
	enableQuietFlag(cmd)
	enableJsonFlag(cmd)
	enableJqFlag(cmd)
}

func addStandardDeleteFlags(cmd *cobra.Command) {
	enableQuietFlag(cmd)
}

func makeSlug(name string) string {
	return slug.Make(name)
}

// Functionality for populating entities from stdin.

func populateNewModelFromStdin(v interface{}) error {
	jsonBytes, err := readStdin()
	if err != nil {
		return err
	}
	err = json.Unmarshal(jsonBytes, v)
	if err != nil {
		return err
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
		goclientnew.Unit |
		goclientnew.UnitEvent |
		goclientnew.ExtendedUnit
}

func displayCreateResults[Entity ModelConstraint](entity *Entity, entityName, slug, id string, display func(entity *Entity)) {
	// Check if any alternative output format is specified
	hasAlternativeOutput := jsonOutput || jq != ""

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
}

func displayUpdateResults[Entity ModelConstraint](entity *Entity, entityName, slug, id string, display func(entity *Entity)) {
	// Check if any alternative output format is specified
	hasAlternativeOutput := jsonOutput || jq != ""

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
}

func displayListResults[Entity ModelConstraint](entities []*Entity, getSlug func(entity *Entity) string, display func(entities []*Entity)) {
	// Check if any alternative output format is specified
	hasAlternativeOutput := names || jsonOutput || jq != ""

	if !quiet && !hasAlternativeOutput {
		display(entities)
	}
	if verbose && !hasAlternativeOutput {
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
}

func displayGetResults[Entity ModelConstraint](entity *Entity, display func(entity *Entity)) {
	// Check if any alternative output format is specified
	hasAlternativeOutput := jsonOutput || jq != ""

	if !quiet && !hasAlternativeOutput {
		display(entity)
	}
	if jsonOutput {
		displayJSON(entity)
	}
	if jq != "" {
		displayJQ(entity)
	}
}

func displayDeleteResults(entityName, slug, id string) {
	if !quiet {
		tprint("Successfully deleted %s %s (%s)", entityName, slug, id)
	}
}
