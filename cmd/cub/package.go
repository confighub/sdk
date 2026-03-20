// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// The package commands are used to create packages and load packages into an org.
// A package is a serialized collection of spaces, units, links, etc.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var packageCmdGroup = &cobra.Command{
	Use:   "package <command>",
	Short: "package commands",
	Long:  getCommandHelp(`package commands`, ""),
}

func init() {
	if os.Getenv("CONFIGHUB_EXPERIMENTAL") != "" {
		rootCmd.AddCommand(packageCmdGroup)
	}
}

type PackageManifest struct {
	Spaces      []SpaceEntry      `json:"spaces"`
	Units       []UnitEntry       `json:"units"`
	Targets     []TargetEntry     `json:"targets"`
	Workers     []WorkerEntry     `json:"workers"`
	Links       []LinkEntry       `json:"links"`
	Views       []ViewEntry       `json:"views"`
	Filters     []FilterEntry     `json:"filters"`
	Tags        []TagEntry        `json:"tags"`
	Invocations []InvocationEntry `json:"invocations"`
	Triggers    []TriggerEntry    `json:"triggers,omitempty"`
	Attributes  []AttributeEntry  `json:"attributes,omitempty"`
}

type SpaceEntry struct {
	Slug       string `json:"slug"`
	DetailsLoc string `json:"details_loc"`
}

type UnitEntry struct {
	Slug        string `json:"slug"`
	SpaceSlug   string `json:"space_slug"`
	DetailsLoc  string `json:"details_loc"`
	UnitDataLoc string `json:"unit_data_loc"`
	Target      string `json:"target"`
}

type TargetEntry struct {
	Slug       string `json:"slug"`
	SpaceSlug  string `json:"space_slug"`
	DetailsLoc string `json:"details_loc"`
	Worker     string `json:"worker"`
}

type WorkerEntry struct {
	Slug       string `json:"slug"`
	SpaceSlug  string `json:"space_slug"`
	DetailsLoc string `json:"details_loc"`
}

type LinkEntry struct {
	Slug       string `json:"slug"`
	SpaceSlug  string `json:"space_slug"`
	FromUnit   string `json:"from_unit"` // Format: "space-slug/unit-slug"
	ToUnit     string `json:"to_unit"`   // Format: "space-slug/unit-slug"
	DetailsLoc string `json:"details_loc"`
}

type ViewEntry struct {
	Slug       string `json:"slug"`
	SpaceSlug  string `json:"space_slug"`
	FilterSlug string `json:"filter_slug,omitempty"` // Reference to filter (optional)
	DetailsLoc string `json:"details_loc"`
}

type FilterEntry struct {
	Slug          string `json:"slug"`
	SpaceSlug     string `json:"space_slug"`
	FromSpaceSlug string `json:"from_space_slug,omitempty"` // Reference to FromSpace (optional)
	DetailsLoc    string `json:"details_loc"`
}

type TagEntry struct {
	Slug       string `json:"slug"`
	SpaceSlug  string `json:"space_slug"`
	DetailsLoc string `json:"details_loc"`
}

type InvocationEntry struct {
	Slug       string `json:"slug"`
	SpaceSlug  string `json:"space_slug"`
	WorkerSlug string `json:"worker_slug,omitempty"` // Reference to BridgeWorker (optional)
	DetailsLoc string `json:"details_loc"`
}

type TriggerEntry struct {
	Slug       string `json:"slug"`
	SpaceSlug  string `json:"space_slug"`
	DetailsLoc string `json:"details_loc"`
}

type AttributeEntry struct {
	Slug       string `json:"slug"`
	SpaceSlug  string `json:"space_slug"`
	DetailsLoc string `json:"details_loc"`
}

// RemotePackageLoader handles loading packages from remote sources
type RemotePackageLoader struct {
	baseURL    string
	httpClient *http.Client
}

// NewRemotePackageLoader creates a new remote package loader
func NewRemotePackageLoader(sourceURL string) (*RemotePackageLoader, error) {
	parsedURL, err := url.Parse(sourceURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	// Convert GitHub URLs to raw content URLs
	baseURL := sourceURL
	if strings.Contains(parsedURL.Host, "github.com") {
		baseURL = convertToGitHubRawURL(sourceURL)
	}

	// Ensure baseURL ends with /
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	return &RemotePackageLoader{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// convertToGitHubRawURL converts a GitHub repository URL to a raw content URL
func convertToGitHubRawURL(githubURL string) string {
	// Convert various GitHub URL formats to raw.githubusercontent.com format
	// Example: https://github.com/user/repo/tree/main/path -> https://raw.githubusercontent.com/user/repo/main/path
	// Example: https://github.com/user/repo/blob/main/path -> https://raw.githubusercontent.com/user/repo/main/path

	u, err := url.Parse(githubURL)
	if err != nil {
		return githubURL
	}

	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 2 {
		return githubURL
	}

	user := parts[0]
	repo := parts[1]

	// Default branch
	branch := "main"
	subPath := ""

	// Check if URL contains tree/ or blob/ indicating a specific branch/path
	if len(parts) > 3 && (parts[2] == "tree" || parts[2] == "blob") {
		branch = parts[3]
		if len(parts) > 4 {
			subPath = strings.Join(parts[4:], "/")
		}
	} else if len(parts) > 2 {
		// Direct repository URL without tree/blob
		subPath = strings.Join(parts[2:], "/")
	}

	rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", user, repo, branch)
	if subPath != "" {
		rawURL = fmt.Sprintf("%s/%s", rawURL, subPath)
	}

	return rawURL
}

// LoadManifest loads the manifest from the remote source
func (r *RemotePackageLoader) LoadManifest() (*PackageManifest, error) {
	manifestURL := r.baseURL + "manifest.json"

	resp, err := r.httpClient.Get(manifestURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch manifest: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest PackageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, nil
}

// LoadFile loads a file from the remote source given its relative path
func (r *RemotePackageLoader) LoadFile(relativePath string) ([]byte, error) {
	// Remove leading slash if present
	relativePath = strings.TrimPrefix(relativePath, "/")

	fileURL := r.baseURL + relativePath

	resp, err := r.httpClient.Get(fileURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch file %s: %w", relativePath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch file %s: HTTP %d", relativePath, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", relativePath, err)
	}

	return data, nil
}

// LoadSpaceDetails loads space details from a remote source
func (r *RemotePackageLoader) LoadSpaceDetails(space SpaceEntry) (*goclientnew.Space, error) {
	data, err := r.LoadFile(space.DetailsLoc)
	if err != nil {
		return nil, err
	}

	var spaceDetails goclientnew.Space
	if err := json.Unmarshal(data, &spaceDetails); err != nil {
		return nil, fmt.Errorf("failed to parse space details: %w", err)
	}

	spaceDetails.Slug = space.Slug
	return &spaceDetails, nil
}

// LoadWorkerDetails loads worker details from a remote source
func (r *RemotePackageLoader) LoadWorkerDetails(worker WorkerEntry) (*goclientnew.BridgeWorker, error) {
	data, err := r.LoadFile(worker.DetailsLoc)
	if err != nil {
		return nil, err
	}

	var workerDetails goclientnew.BridgeWorker
	if err := json.Unmarshal(data, &workerDetails); err != nil {
		return nil, fmt.Errorf("failed to parse worker details: %w", err)
	}

	workerDetails.Slug = worker.Slug
	return &workerDetails, nil
}

// LoadTargetDetails loads target details from a remote source
func (r *RemotePackageLoader) LoadTargetDetails(target TargetEntry) (*goclientnew.Target, error) {
	data, err := r.LoadFile(target.DetailsLoc)
	if err != nil {
		return nil, err
	}

	var targetDetails goclientnew.Target
	if err := json.Unmarshal(data, &targetDetails); err != nil {
		return nil, fmt.Errorf("failed to parse target details: %w", err)
	}

	targetDetails.Slug = target.Slug
	return &targetDetails, nil
}

// LoadUnitDetails loads unit details from a remote source
func (r *RemotePackageLoader) LoadUnitDetails(unit UnitEntry) (*goclientnew.Unit, error) {
	data, err := r.LoadFile(unit.DetailsLoc)
	if err != nil {
		return nil, err
	}

	var unitDetails goclientnew.Unit
	if err := json.Unmarshal(data, &unitDetails); err != nil {
		return nil, fmt.Errorf("failed to parse unit details: %w", err)
	}

	unitDetails.Slug = unit.Slug
	return &unitDetails, nil
}

// LoadUnitData loads unit data from a remote source
func (r *RemotePackageLoader) LoadUnitData(unit UnitEntry) ([]byte, error) {
	return r.LoadFile(unit.UnitDataLoc)
}

// LoadLinkDetails loads link details from a remote source
func (r *RemotePackageLoader) LoadLinkDetails(link LinkEntry) (*goclientnew.Link, error) {
	data, err := r.LoadFile(link.DetailsLoc)
	if err != nil {
		return nil, err
	}

	var linkDetails goclientnew.Link
	if err := json.Unmarshal(data, &linkDetails); err != nil {
		return nil, fmt.Errorf("failed to parse link details: %w", err)
	}

	linkDetails.Slug = link.Slug
	return &linkDetails, nil
}

// LoadViewDetails loads view details from a remote source
func (r *RemotePackageLoader) LoadViewDetails(view ViewEntry) (*goclientnew.View, error) {
	data, err := r.LoadFile(view.DetailsLoc)
	if err != nil {
		return nil, err
	}

	var viewDetails goclientnew.View
	if err := json.Unmarshal(data, &viewDetails); err != nil {
		return nil, fmt.Errorf("failed to parse view details: %w", err)
	}

	viewDetails.Slug = view.Slug
	return &viewDetails, nil
}

// LoadFilterDetails loads filter details from a remote source
func (r *RemotePackageLoader) LoadFilterDetails(filter FilterEntry) (*goclientnew.Filter, error) {
	data, err := r.LoadFile(filter.DetailsLoc)
	if err != nil {
		return nil, err
	}

	var filterDetails goclientnew.Filter
	if err := json.Unmarshal(data, &filterDetails); err != nil {
		return nil, fmt.Errorf("failed to parse filter details: %w", err)
	}

	filterDetails.Slug = filter.Slug
	return &filterDetails, nil
}

// LoadTagDetails loads tag details from a remote source
func (r *RemotePackageLoader) LoadTagDetails(tag TagEntry) (*goclientnew.Tag, error) {
	data, err := r.LoadFile(tag.DetailsLoc)
	if err != nil {
		return nil, err
	}

	var tagDetails goclientnew.Tag
	if err := json.Unmarshal(data, &tagDetails); err != nil {
		return nil, fmt.Errorf("failed to parse tag details: %w", err)
	}

	tagDetails.Slug = tag.Slug
	return &tagDetails, nil
}

// LoadInvocationDetails loads invocation details from a remote source
func (r *RemotePackageLoader) LoadInvocationDetails(invocation InvocationEntry) (*goclientnew.Invocation, error) {
	data, err := r.LoadFile(invocation.DetailsLoc)
	if err != nil {
		return nil, err
	}

	var invocationDetails goclientnew.Invocation
	if err := json.Unmarshal(data, &invocationDetails); err != nil {
		return nil, fmt.Errorf("failed to parse invocation details: %w", err)
	}

	invocationDetails.Slug = invocation.Slug
	return &invocationDetails, nil
}

func (r *RemotePackageLoader) LoadTriggerDetails(trigger TriggerEntry) (*goclientnew.Trigger, error) {
	data, err := r.LoadFile(trigger.DetailsLoc)
	if err != nil {
		return nil, err
	}

	var triggerDetails goclientnew.Trigger
	if err := json.Unmarshal(data, &triggerDetails); err != nil {
		return nil, fmt.Errorf("failed to parse trigger details: %w", err)
	}

	triggerDetails.Slug = trigger.Slug
	return &triggerDetails, nil
}

func (r *RemotePackageLoader) LoadAttributeDetails(attribute AttributeEntry) (*goclientnew.Attribute, error) {
	data, err := r.LoadFile(attribute.DetailsLoc)
	if err != nil {
		return nil, err
	}

	var attributeDetails goclientnew.Attribute
	if err := json.Unmarshal(data, &attributeDetails); err != nil {
		return nil, fmt.Errorf("failed to parse attribute details: %w", err)
	}

	attributeDetails.Slug = attribute.Slug
	return &attributeDetails, nil
}

// isRemoteURL determines if a path is a remote URL
func isRemoteURL(path string) bool {
	return strings.HasPrefix(path, "http://") ||
		strings.HasPrefix(path, "https://")
}
