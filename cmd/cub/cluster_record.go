// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The local record of a cub-managed cluster: <config-dir>/clusters/<name>.json,
// written beside the kubeconfig by `cub cluster up` and `cub cluster attach`.
//
// A cluster has three facets. The cluster itself, which this machine reaches
// through the kubeconfig. Its wiring in ConfigHub, which lives in exactly one
// organization on one server (a coordinate). And the link between the two,
// which this record is. The Space stays the source of truth for everything
// about the wiring; the record holds only what is needed to find that Space
// from here, and nothing that can go stale: the coordinate, the names, and
// how the cluster came to be managed. A context is a local, renamable name for
// a coordinate, and several contexts can name the same one, so the record
// stores the coordinate and the context is looked up when needed.
//
// Clusters from before the record existed have a kubeconfig and no record.
// `cub cluster list` writes one for them the first time it matches the Space
// in the current context, so the migration needs nothing run by hand.
type clusterRecord struct {
	Name           string    `json:"name"`
	SpaceSlug      string    `json:"spaceSlug"`
	ServerURL      string    `json:"serverURL"`
	OrganizationID string    `json:"organizationID"`
	Origin         string    `json:"origin"`     // clusterOriginKind or clusterOriginAttach
	Controller     string    `json:"controller"` // clusterControllerArgo or clusterControllerSveltos
	CreatedAt      time.Time `json:"createdAt"`
}

// clusterRecordPath returns the record path for a cluster name.
func clusterRecordPath(name string) string {
	return filepath.Join(clusterKubeconfigDir(), name+".json")
}

// clusterWriteRecord writes the record, creating the clusters directory.
func clusterWriteRecord(rec clusterRecord) error {
	if err := clusterEnsureKubeconfigDir(); err != nil {
		return err
	}
	rec.ServerURL = strings.TrimRight(rec.ServerURL, "/")
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(clusterRecordPath(rec.Name), append(data, '\n'), 0o600)
}

// clusterReadRecord returns the cluster's record, or nil when there is none: a
// cluster from before records existed, or one this machine never managed.
func clusterReadRecord(name string) (*clusterRecord, error) {
	data, err := os.ReadFile(clusterRecordPath(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read cluster record: %w", err)
	}
	var rec clusterRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parse %s: %w", clusterRecordPath(name), err)
	}
	if rec.Name == "" {
		rec.Name = name
	}
	return &rec, nil
}

// clusterRemoveRecord removes the record; a missing one is not an error.
func clusterRemoveRecord(name string) error {
	if err := os.Remove(clusterRecordPath(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// clusterNewRecord fills a record for a cluster being wired to the active
// context now.
func clusterNewRecord(name, spaceSlug, origin, controller string) clusterRecord {
	coordinate := contextManager.ActiveContext().Coordinate
	return clusterRecord{
		Name:           name,
		SpaceSlug:      spaceSlug,
		ServerURL:      strings.TrimRight(coordinate.ServerURL, "/"),
		OrganizationID: coordinate.OrganizationID,
		Origin:         origin,
		Controller:     controller,
		CreatedAt:      time.Now().UTC(),
	}
}

// clusterRecordMatchesCoordinate reports whether the record is wired to the
// coordinate's server and organization. The user is not part of it: the wiring
// belongs to the organization, whoever logged in.
func clusterRecordMatchesCoordinate(rec *clusterRecord, c Coordinate) bool {
	return rec != nil &&
		strings.TrimRight(rec.ServerURL, "/") == strings.TrimRight(c.ServerURL, "/") &&
		rec.OrganizationID == c.OrganizationID
}

// clusterRecordIsActive reports whether the record is wired to the active
// context's organization.
func clusterRecordIsActive(rec *clusterRecord) bool {
	return clusterRecordMatchesCoordinate(rec, contextManager.ActiveContext().Coordinate)
}

// clusterContextForRecord finds a context for the record's coordinate: the
// active one if it matches, else the first that does, else nil. Several
// contexts can name one coordinate, and any of them will do.
func clusterContextForRecord(rec *clusterRecord) *Context {
	if rec == nil {
		return nil
	}
	if active := contextManager.ActiveContext(); clusterRecordMatchesCoordinate(rec, active.Coordinate) {
		return active
	}
	for _, c := range contextManager.ListContexts() {
		if clusterRecordMatchesCoordinate(rec, c.Coordinate) {
			return c
		}
	}
	return nil
}

// clusterRecordWhere names where the record is wired to, for display: the
// context name when one exists, else the server and organization.
func clusterRecordWhere(rec *clusterRecord) string {
	if c := clusterContextForRecord(rec); c != nil {
		return c.Name
	}
	return fmt.Sprintf("%s (org %s, no context)", rec.ServerURL, rec.OrganizationID)
}

// clusterUseRecordedContext switches this command to the context the cluster
// is wired to, when that is not the active one, and re-initializes the API
// client for it. The switch is for this process only; the persisted current
// context is untouched. A record without any matching context is an error that
// says what to create.
func clusterUseRecordedContext(out io.Writer, rec *clusterRecord) error {
	if rec == nil || clusterRecordIsActive(rec) {
		return nil
	}
	c := clusterContextForRecord(rec)
	if c == nil {
		return fmt.Errorf("cluster %q is wired to %s in organization %s, and no cub context names that server and organization; create one with 'cub context create' and log in, or pass --force to act on the local cluster only",
			rec.Name, rec.ServerURL, rec.OrganizationID)
	}
	if err := contextManager.OverrideCurrentContext(c.Name); err != nil {
		return err
	}
	var err error
	cubClientNew, err = InitializeClient(contextManager.ActiveContext())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Using context %q, which cluster %q is wired to.\n", c.Name, rec.Name)
	return nil
}

// clusterLocalRecords returns the records of every cluster on this machine,
// plus a record-less entry (nil record) for each kubeconfig without one.
func clusterLocalRecords() (map[string]*clusterRecord, error) {
	names, err := clusterLocalNames()
	if err != nil {
		return nil, err
	}
	out := map[string]*clusterRecord{}
	for _, n := range names {
		rec, err := clusterReadRecord(n)
		if err != nil {
			return nil, err
		}
		out[n] = rec
	}
	return out, nil
}
