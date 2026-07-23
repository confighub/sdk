// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/cubbyname"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/google/uuid"
)

// ConfigHub-side operations for `cub cluster`. These use the global cubClientNew
// client and ctx, and reuse the shared api* helpers where they exist, matching
// the rest of the cub command package.

// clusterSpace is the decoded view of one cub-cluster-managed Space, from its
// cluster label and confighub.com/cluster-* annotations.
type clusterSpace struct {
	SpaceID     uuid.UUID
	SpaceSlug   string
	ClusterName string // from clusterAnnotationName; falls back to SpaceSlug
	PortRange   string
	Host        string
	ArgoPort    string
	CreatedAt   time.Time
}

// clusterServerURL returns the active context's bare server URL (no trailing
// slash, no /api), used to derive the OCI registry endpoint.
func clusterServerURL() string {
	return strings.TrimRight(contextManager.ActiveContext().Coordinate.ServerURL, "/")
}

// clusterNewPrefix mirrors `cub space new-prefix`: generate a random name that
// is not a prefix of any existing space slug. Returns after up to 1000 tries.
func clusterNewPrefix() (string, error) {
	spaces, err := apiListSpaces("", "*")
	if err != nil {
		return "", fmt.Errorf("list spaces: %w", err)
	}
	for range 1000 {
		candidate := cubbyname.Random()
		collide := false
		for _, s := range spaces {
			if strings.HasPrefix(s.Slug, candidate) {
				collide = true
				break
			}
		}
		if !collide {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not generate a unique prefix after 1000 attempts")
}

// clusterSpaceExists reports whether a space with the given slug exists.
func clusterSpaceExists(slug string) (bool, error) {
	spaces, err := apiListSpaces("", "*")
	if err != nil {
		return false, err
	}
	for _, s := range spaces {
		if s.Slug == slug {
			return true, nil
		}
	}
	return false, nil
}

// clusterListSpacesForHost returns all Spaces in the current context that carry
// the cluster label and whose host annotation matches the given hostname. The
// label filter runs server-side via --where; the host filter is applied
// client-side because Annotations.X is not currently queryable in --where.
func clusterListSpacesForHost(hostname string) ([]clusterSpace, error) {
	where := fmt.Sprintf("Labels.%s = 'true'", clusterLabel)
	spaces, err := apiListSpaces(where, "*")
	if err != nil {
		return nil, fmt.Errorf("list cluster spaces: %w", err)
	}
	var out []clusterSpace
	for _, s := range spaces {
		if s.Annotations[clusterAnnotationHost] != hostname {
			continue
		}
		name := s.Annotations[clusterAnnotationName]
		if name == "" {
			name = s.Slug
		}
		out = append(out, clusterSpace{
			SpaceID:     s.SpaceID,
			SpaceSlug:   s.Slug,
			ClusterName: name,
			PortRange:   s.Annotations[clusterAnnotationPortRange],
			Host:        s.Annotations[clusterAnnotationHost],
			ArgoPort:    s.Annotations[clusterAnnotationArgoPort],
			CreatedAt:   s.CreatedAt,
		})
	}
	return out, nil
}

// clusterCreateSpace creates a new space and returns its UUID.
func clusterCreateSpace(slug, displayName string, labels, annotations map[string]string) (uuid.UUID, error) {
	body := goclientnew.Space{
		Slug:        slug,
		DisplayName: displayName,
		Labels:      labels,
		Annotations: annotations,
	}
	res, err := cubClientNew.CreateSpaceWithResponse(ctx, &goclientnew.CreateSpaceParams{}, body)
	if cubapi.IsAPIError(err, res) {
		return uuid.Nil, cubapi.InterpretErrorGeneric(err, res)
	}
	return res.JSON200.SpaceID, nil
}

// clusterCreateOCIWorker creates a server-hosted OCI worker — no pod runs in
// the user's cluster; ConfigHub hosts it. OrgRole is forced to "none" because
// this worker's secret is handed to Argo as OCI repo-creds and must carry no
// org-level REST privileges. The (OCI, Any) ConfigType must match the server
// bridge registry's OCIBridge entry exactly — including the empty
// LiveStateType — or the server rejects the server-worker create. Returns the
// worker's UUID and its Secret (needed for the Argo repo-creds Secret).
func clusterCreateOCIWorker(spaceID uuid.UUID, slug, displayName string) (uuid.UUID, string, error) {
	body := &goclientnew.BridgeWorker{
		SpaceID:     spaceID,
		Slug:        slug,
		DisplayName: displayName,
		OrgRole:     "none",
		ProvidedInfo: &goclientnew.WorkerInfo{
			IsServerWorker: true,
			BridgeWorkerInfo: &goclientnew.BridgeWorkerInfo{
				SupportedConfigTypes: []goclientnew.SupportedConfigType{
					{ProviderType: string(api.ProviderOCI), ToolchainType: string(workerapi.ToolchainAny)},
				},
			},
		},
	}
	worker, err := apiCreateWorker(body, spaceID)
	if err != nil {
		return uuid.Nil, "", err
	}
	secret := worker.Secret
	if secret == "" {
		// Some server responses blank Secret on create; fetch via Get as
		// `cub worker get-secret` does.
		got, gerr := apiGetBridgeWorker(spaceID, worker.BridgeWorkerID, "*")
		if gerr != nil {
			return uuid.Nil, "", fmt.Errorf("get worker secret after create: %w", gerr)
		}
		secret = got.Secret
	}
	if secret == "" {
		return uuid.Nil, "", fmt.Errorf("worker %q: server returned no Secret", slug)
	}
	return worker.BridgeWorkerID, secret, nil
}

// clusterCreateOCITarget creates an OCI target bound to the given worker. The
// worker OWNS the target — that ownership is what authorizes OCI bundle pull,
// no separate permission grant needed. ToolchainType is the ToolchainAny
// wildcard so Units of any toolchain can bind; the OCI provider takes no
// parameters. annotations (may be nil) are attached to the Target, e.g. the
// URL-TargetUI deep link.
func clusterCreateOCITarget(spaceID, workerID uuid.UUID, slug, displayName string, annotations map[string]string) (uuid.UUID, error) {
	body := goclientnew.Target{
		SpaceID:        spaceID,
		Slug:           slug,
		DisplayName:    displayName,
		BridgeWorkerID: workerID,
		ToolchainType:  string(workerapi.ToolchainAny),
		ProviderType:   string(api.ProviderOCI),
		Parameters:     "{}",
		Annotations:    annotations,
	}
	res, err := cubClientNew.CreateTargetWithResponse(ctx, spaceID, &goclientnew.CreateTargetParams{}, body)
	if cubapi.IsAPIError(err, res) {
		return uuid.Nil, cubapi.InterpretErrorGeneric(err, res)
	}
	return res.JSON200.TargetID, nil
}

// clusterCreateK8sYAMLUnit creates a Kubernetes/YAML Unit with the given
// manifest bytes, bound to the OCI target. Used for the root app-of-apps Unit
// and for each child Argo Application Unit in the cluster Space.
func clusterCreateK8sYAMLUnit(spaceID, targetID uuid.UUID, slug, displayName string, manifest []byte) (uuid.UUID, error) {
	body := goclientnew.Unit{
		SpaceID:       spaceID,
		Slug:          slug,
		DisplayName:   displayName,
		ToolchainType: "Kubernetes/YAML",
		Data:          base64.StdEncoding.EncodeToString(manifest),
		TargetID:      &targetID,
	}
	res, err := cubClientNew.CreateUnitWithResponse(ctx, spaceID, &goclientnew.CreateUnitParams{}, body)
	if cubapi.IsAPIError(err, res) {
		return uuid.Nil, cubapi.InterpretErrorGeneric(err, res)
	}
	return res.JSON200.UnitID, nil
}

// clusterSetReleaseTarget sets the Space's ReleaseTargetID to the OCI target.
// Releases are published per Space ("cub release publish <space>") and consume
// the Space's release Target; the OCI registry serves the Space's Releases at
// /space/<slug> only when it is set. The server validates that the Target's
// provider is OCI and computes the Space's ReleaseURL.
func clusterSetReleaseTarget(spaceID, targetID uuid.UUID) error {
	patchData, err := json.Marshal(map[string]interface{}{
		"ReleaseTargetID": targetID.String(),
	})
	if err != nil {
		return err
	}
	if _, err := patchSpace(spaceID, patchData); err != nil {
		return fmt.Errorf("set release target on space: %w", err)
	}
	return nil
}

// clusterClearReleaseTarget clears the Space's ReleaseTargetID. Used by the
// `cub cluster up` rollback before recursively deleting a Space it just
// created: the Space's own release-target reference blocks deleting the OCI
// target it points at (spaces_release_target_id_fkey is ON DELETE RESTRICT) —
// for the cluster Space that target lives in the Space being deleted, and for
// the argobot variant it lives cross-space in the cluster Space.
//
// TODO: remove once the server's recursive space delete clears the
// reference itself (#4782).
func clusterClearReleaseTarget(spaceID uuid.UUID) error {
	patchData, err := json.Marshal(map[string]interface{}{
		"ReleaseTargetID": nil,
	})
	if err != nil {
		return err
	}
	if _, err := patchSpace(spaceID, patchData); err != nil {
		return fmt.Errorf("clear release target on space: %w", err)
	}
	return nil
}

// clusterWaitUnitTriggers waits for org-wide Triggers, which run asynchronously
// on every Mutation, to finish evaluating the unit: while evaluation is pending
// the unit carries an `awaiting/triggers` ApplyGate. Publishing a Release
// bundles the unit's head Revision without consulting gates, so wait for the
// gate to clear (up to 30s) to avoid bundling a pre-trigger revision. Any other
// gate is left to the server to enforce.
func clusterWaitUnitTriggers(spaceID, unitID uuid.UUID) error {
	deadline := time.Now().Add(30 * time.Second)
	backoff := 1 * time.Second
	for {
		unit, err := apiGetUnitInSpace(unitID.String(), spaceID.String(), "ApplyGates")
		if err != nil {
			return err
		}
		if _, pending := unit.ApplyGates["awaiting/triggers"]; !pending {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("unit %s still awaiting trigger evaluation after 30s", unitID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 5*time.Second {
			backoff *= 2
		}
	}
}

// clusterPublishRelease publishes a Release for the Space: an immutable bundle
// of the Space's Units that are assigned to its release Target, served to Argo
// at /space/<slug> with tag "latest".
func clusterPublishRelease(spaceID uuid.UUID) error {
	res, err := cubClientNew.PublishReleaseWithResponse(ctx, spaceID, goclientnew.PublishReleaseJSONRequestBody{})
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return nil
}

// clusterDeleteSpace deletes a space by slug. If recursive is true, the server
// cascades deletion to units, targets, and workers within the space (subject
// to delete gates).
func clusterDeleteSpace(slug string, recursive bool) error {
	space, err := apiGetSpaceFromSlug(slug, "SpaceID")
	if err != nil {
		return err
	}
	params := &goclientnew.DeleteSpaceParams{}
	if recursive {
		t := "true"
		params.Recursive = &t
	}
	res, err := cubClientNew.DeleteSpaceWithResponse(ctx, space.SpaceID, params)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return nil
}
