// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/cubbyname"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
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
	Controller  string // from clusterAnnotationController; clusterControllerArgo when absent, as on clusters made before it
	PortRange   string
	Host        string
	ArgoPort    string
	SveltosPort string // the Sveltos dashboard's NodePort, from clusterAnnotationSveltosPort
	Origin      string // clusterOriginKind or clusterOriginAttach; see clusterOriginOf
	BotSpace    string // the bot's variant Space slug, from clusterAnnotationBotSpace; "" when unrecorded
	BotChannel  string // the bot's Channel slug in the cluster Space, from clusterAnnotationBotChannel
	CreatedAt   time.Time
}

// clusterOriginOf reads the origin annotation, defaulting to kind: every
// cluster Space written before the annotation existed was created by `cub
// cluster up`.
func clusterOriginOf(annotations map[string]string) string {
	if o := annotations[clusterAnnotationOrigin]; o != "" {
		return o
	}
	return clusterOriginKind
}

// clusterServerURL returns the active context's bare server URL (no trailing
// slash, no /api), used to derive the OCI registry endpoint.
func clusterServerURL() string {
	return strings.TrimRight(contextManager.ActiveContext().Coordinate.ServerURL, "/")
}

// clusterOCIServerURL asks the server where its OCI registry is, returning ""
// when it advertises none (an older server, or one running without a registry).
// This is a live request rather than something read from the context: only the
// server knows, and the answer can change under a deployment without the local
// context having any reason to notice. See clusterOCIEndpoint for how it is used
// and what happens when the answer is empty.
func clusterOCIServerURL() (string, error) {
	coordinate := contextManager.ActiveContext().Coordinate
	apiInfo, err := getApiInfo(coordinate)
	if err != nil {
		return "", fmt.Errorf("failed to get API info: %w", err)
	}
	return strings.TrimRight(ociServerURLFromApiInfo(coordinate.ServerURL, apiInfo), "/"), nil
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

// clusterListSpacesForHost returns the cluster Spaces in the current context
// whose host annotation matches the given hostname: the kind clusters this
// machine created, in the days before the local record said where each one is
// wired. Still used for clusters that have no record.
func clusterListSpacesForHost(hostname string) ([]clusterSpace, error) {
	all, err := clusterListSpaces()
	if err != nil {
		return nil, err
	}
	var out []clusterSpace
	for _, s := range all {
		if s.Host == hostname {
			out = append(out, s)
		}
	}
	return out, nil
}

// clusterSpaceByNameInOrg returns the cluster Space named by the cluster name
// in the current context's organization, whichever machine created it, or nil.
// The cluster name is unique within an organization: it names the Space by
// default and is the annotation every cluster Space carries.
func clusterSpaceByNameInOrg(name string) (*clusterSpace, error) {
	all, err := clusterListSpaces()
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ClusterName == name {
			return &all[i], nil
		}
	}
	return nil, nil
}

// clusterRequireFreshConfigHub refuses to start when any of the Spaces an
// attachment creates already exists, so a leftover from an earlier run is
// surfaced before anything slow or irreversible happens rather than mid-run.
// argobotSlug is skipped when empty (--no-argobot): its variant Space is created
// fail-fast, without --allow-exists, during the install. The cluster's service
// account is org-scoped, so a leftover one is not caught by the Space checks
// and is checked here too.
func clusterRequireFreshConfigHub(name, spaceSlug, appsSlug, argobotSlug string) error {
	for _, slug := range []string{spaceSlug, appsSlug} {
		if exists, err := clusterSpaceExists(slug); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("ConfigHub space %q already exists", slug)
		}
	}
	if argobotSlug != "" {
		if exists, err := clusterSpaceExists(argobotSlug); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("ConfigHub space %q already exists (leftover argobot install; delete it with 'cub space delete --recursive %s' or pass --no-argobot)", argobotSlug, argobotSlug)
		}
	}
	saSlug := clusterServiceAccountSlug(name)
	if exists, err := clusterServiceAccountExists(saSlug); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("ConfigHub service account %q already exists (leftover cluster identity; delete it with 'cub serviceaccount delete %s')", saSlug, saSlug)
	}
	return nil
}

// clusterListUnitSlugs returns the slugs of every Unit in a Space. For the apps
// Space each slug is also the name of the Argo CD Application the Unit
// manifests, which is what `cub cluster detach` needs to remove them.
func clusterListUnitSlugs(spaceID uuid.UUID) ([]string, error) {
	units, err := apiListUnits(spaceID.String(), "", "UnitID,Slug")
	if err != nil {
		return nil, err
	}
	slugs := make([]string, 0, len(units))
	for _, u := range units {
		slugs = append(slugs, u.Slug)
	}
	return slugs, nil
}

// clusterListSpaces returns every cluster Space in the current context's
// organization, decoded from its label and annotations. The label filter runs
// server-side via --where; everything else is read from the annotations.
func clusterListSpaces() ([]clusterSpace, error) {
	where := fmt.Sprintf("Labels.%s = 'true'", clusterLabel)
	spaces, err := apiListSpaces(where, "*")
	if err != nil {
		return nil, fmt.Errorf("list cluster spaces: %w", err)
	}
	var out []clusterSpace
	for _, s := range spaces {
		name := s.Annotations[clusterAnnotationName]
		if name == "" {
			name = s.Slug
		}
		controller := s.Annotations[clusterAnnotationController]
		if controller == "" {
			controller = clusterControllerArgo
		}
		out = append(out, clusterSpace{
			SpaceID:     s.SpaceID,
			SpaceSlug:   s.Slug,
			ClusterName: name,
			Controller:  controller,
			PortRange:   s.Annotations[clusterAnnotationPortRange],
			Host:        s.Annotations[clusterAnnotationHost],
			ArgoPort:    s.Annotations[clusterAnnotationArgoPort],
			SveltosPort: s.Annotations[clusterAnnotationSveltosPort],
			Origin:      clusterOriginOf(s.Annotations),
			BotSpace:    s.Annotations[clusterAnnotationBotSpace],
			BotChannel:  s.Annotations[clusterAnnotationBotChannel],
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

// clusterServiceAccountSlug is the service account `cub cluster up` creates for a
// cluster: the identity Argo CD pulls as and argobot runs as. Org-scoped, so it
// is named for the cluster, beside the argobot variant Space argobot-<name>.
func clusterServiceAccountSlug(name string) string {
	return "argo-" + name
}

// clusterServiceAccountExists reports whether a service account with the slug
// exists in the current org.
func clusterServiceAccountExists(slug string) (bool, error) {
	where := fmt.Sprintf("Slug = '%s'", slug)
	res, err := cubClientNew.ListServiceAccountsWithResponse(ctx, &goclientnew.ListServiceAccountsParams{Where: &where})
	if cubapi.IsAPIError(err, res) {
		return false, cubapi.InterpretErrorGeneric(err, res)
	}
	for _, sa := range *res.JSON200 {
		if sa.ServiceAccount != nil && sa.ServiceAccount.Slug == slug {
			return true, nil
		}
	}
	return false, nil
}

// clusterCreateServiceAccount creates the cluster's service account with no
// organization role: its only access is what the cluster's Target and Channel
// grant its user, which is what the Argo side of the cluster needs and nothing
// else. Returns the service account's ID and the user it acts as. The caller is
// given Impersonate on it, which is what lets the key and token below be issued.
func clusterCreateServiceAccount(slug, displayName string, annotations map[string]string) (uuid.UUID, uuid.UUID, error) {
	body := goclientnew.ServiceAccount{
		Slug:        slug,
		DisplayName: displayName,
		OrgRole:     "none",
		Annotations: annotations,
	}
	res, err := cubClientNew.CreateServiceAccountWithResponse(ctx, &goclientnew.CreateServiceAccountParams{}, body)
	if cubapi.IsAPIError(err, res) {
		return uuid.Nil, uuid.Nil, cubapi.InterpretErrorGeneric(err, res)
	}
	sa := res.JSON200
	if uuid.UUID(sa.UserID) == uuid.Nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("service account %q: server returned no user", slug)
	}
	return uuid.UUID(sa.ServiceAccountID), uuid.UUID(sa.UserID), nil
}

// clusterCreateServiceAccountKey mints a keypair for the service account,
// registers the public half, and returns the private half as a JWK for whatever
// authenticates as the service account. The private key exists only here and
// where the caller puts it: ConfigHub holds the public half and nothing it
// could replay. The JWK records the user's external ID, so the holder can sign
// without being told separately who it is.
func clusterCreateServiceAccountKey(serviceAccountID, userID uuid.UUID, description string) (json.RawMessage, error) {
	user, err := resolveUserCore(userID.String())
	if err != nil {
		return nil, fmt.Errorf("resolve the service account's user: %w", err)
	}
	pair, err := generateEd25519Key(user.ExternalID)
	if err != nil {
		return nil, err
	}
	if _, err := apiCreateServiceAccountKey(serviceAccountID.String(), pair.PublicJWK, description); err != nil {
		return nil, err
	}
	return pair.PrivateJWK, nil
}

// clusterCreateServiceAccountToken issues a long-lived token for the service
// account, for a consumer that can hold only a static credential: Argo CD's
// repository Secret. A registry client presents it as the password with the
// username "token". It is shown once; ConfigHub keeps a record it can revoke
// (`cub serviceaccount token list|revoke`) and not the token itself.
func clusterCreateServiceAccountToken(serviceAccountID uuid.UUID, description string) (string, error) {
	body := goclientnew.CreateServiceAccountTokenJSONRequestBody{Description: description}
	res, err := cubClientNew.CreateServiceAccountTokenWithResponse(ctx, serviceAccountID.String(), &goclientnew.CreateServiceAccountTokenParams{}, body)
	if cubapi.IsAPIError(err, res) {
		return "", cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200.Token == "" {
		return "", fmt.Errorf("service account %s: server returned no token", serviceAccountID)
	}
	return res.JSON200.Token, nil
}

// clusterDeleteServiceAccount deletes the cluster's service account by slug,
// with its keys and tokens. A slug that does not resolve is already gone: a
// cluster created before service accounts were part of `cub cluster up` has
// none, and tearing it down must not fail on that.
func clusterDeleteServiceAccount(slug string) error {
	exists, err := clusterServiceAccountExists(slug)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	sa, err := resolveServiceAccount(slug, "ServiceAccountID")
	if err != nil {
		return err
	}
	res, err := cubClientNew.DeleteServiceAccountWithResponse(ctx, sa.ServiceAccount.ServiceAccountID)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return nil
}

// clusterWhereTriggerForSpace is the Target's WhereTrigger expression: every
// Trigger in the cluster Space gates every Unit bound to the cluster's Target,
// wherever that Unit lives. It is the same expression a Space defaults its own
// WhereTrigger to, which makes one rule cover both — Triggers in the cluster
// Space apply to everything released to the cluster.
//
// Triggers are resolved into the Target's read-only TriggerIDs when the Target
// is created or updated, not when they fire, so a Trigger added to the cluster
// Space later is picked up with `cub target update --refresh-triggers`.
func clusterWhereTriggerForSpace(spaceID uuid.UUID) string {
	return fmt.Sprintf("SpaceID = '%s'", spaceID)
}

// clusterCreateGateTrigger creates the default placeholder gate in the cluster
// Space: vet-placeholders on every Mutation of a Kubernetes/YAML Unit. A failure
// raises an ApplyGate, which blocks `cub release publish` — so config that still
// carries the confighubplaceholder sentinel cannot reach the cluster.
//
// Kubernetes/YAML is the only toolchain worth gating here. A Trigger matches one
// ToolchainType, AppConfig Units are forced to ProviderNone and can never carry a
// Target, and ConfigHub/YAML is the ConfigHub bridge's own format — so nothing
// else can be bound to the cluster's OCI target in the first place.
//
// The Description is what the UI shows when hovering the gate, so it says how to
// clear it rather than just what failed.
func clusterCreateGateTrigger(spaceID uuid.UUID, slug string) (uuid.UUID, error) {
	body := goclientnew.Trigger{
		SpaceID:       spaceID,
		Slug:          slug,
		DisplayName:   slug,
		Event:         "Mutation",
		ToolchainType: string(workerapi.ToolchainKubernetesYAML),
		FunctionName:  "vet-placeholders",
		Description: "This config still contains the confighubplaceholder sentinel, which means a value " +
			"is known to be missing; releasing it to the cluster would deploy config that is " +
			"definitionally incomplete. Run \"cub function do --space <space> get-placeholders\" to " +
			"see which attributes are unfilled, then fill them — via a Link from an upstream Unit, " +
			"\"cub variant create --namespace\", or a set- function. Created by \"cub cluster up\"; " +
			"skip it with --no-placeholder-gate.",
	}
	res, err := cubClientNew.CreateTriggerWithResponse(ctx, spaceID, &goclientnew.CreateTriggerParams{}, body)
	if cubapi.IsAPIError(err, res) {
		return uuid.Nil, cubapi.InterpretErrorGeneric(err, res)
	}
	return res.JSON200.TriggerID, nil
}

// clusterCreateOCITarget creates the cluster's Target and grants the cluster
// service account's user View, Edit, ViewChildren and EditChildren on it.
// ViewChildren is what authorizes Argo, pulling as that service account, to
// pull the Releases of every Space that releases to the Target; View is what
// lets argobot, running as the same service account, find it; Edit is what
// lets argobot store the facts it collects from the cluster on it; and
// EditChildren is what lets argobot record each Release's LiveStatus.
// annotations (may be nil) are attached to the Target, e.g. the URL-TargetUI
// deep link. whereTrigger selects the Triggers that gate Units bound to this
// Target; the server resolves it into TriggerIDs here, so any Trigger it
// selects must already exist.
func clusterCreateOCITarget(spaceID, botUserID uuid.UUID, slug, displayName string, annotations map[string]string, whereTrigger string) (uuid.UUID, error) {
	permissions := goclientnew.Permissions{
		"View":         {UserIDs: map[string]bool{botUserID.String(): true}},
		"Edit":         {UserIDs: map[string]bool{botUserID.String(): true}},
		"ViewChildren": {UserIDs: map[string]bool{botUserID.String(): true}},
		"EditChildren": {UserIDs: map[string]bool{botUserID.String(): true}},
	}
	body := goclientnew.Target{
		SpaceID:      spaceID,
		Slug:         slug,
		DisplayName:  displayName,
		Permissions:  &permissions,
		Annotations:  annotations,
		WhereTrigger: whereTrigger,
	}
	res, err := cubClientNew.CreateTargetWithResponse(ctx, spaceID, &goclientnew.CreateTargetParams{}, body)
	if cubapi.IsAPIError(err, res) {
		return uuid.Nil, cubapi.InterpretErrorGeneric(err, res)
	}
	return res.JSON200.TargetID, nil
}

// clusterUnitTargetUIAnnotations returns the annotations for a controller-app
// Unit whose Application or ClusterProfile is named name: the target's
// URL-TargetUI with "{slug}" replaced by that name, so the Unit links to its own
// page in the controller's UI. It returns nil when the target has no UI link (a
// Flux cluster, or one not created by `cub cluster up`).
func clusterUnitTargetUIAnnotations(targetAnnotations map[string]string, name string) map[string]string {
	template := targetAnnotations[clusterAnnotationTargetUI]
	if template == "" {
		return nil
	}
	return map[string]string{clusterAnnotationTargetUI: strings.ReplaceAll(template, "{slug}", name)}
}

// clusterCreateK8sYAMLUnit creates a Kubernetes/YAML Unit with the given
// manifest bytes, bound to the OCI target. Used for the root app-of-apps Unit
// and for each child Argo Application Unit in the apps Space; the target it
// binds to lives in the cluster (target-prefix) Space, so the binding is
// cross-Space. annotations (may be nil) are attached to the Unit.
func clusterCreateK8sYAMLUnit(spaceID, targetID uuid.UUID, slug, displayName string, annotations map[string]string, manifest []byte) (uuid.UUID, error) {
	body := goclientnew.Unit{
		SpaceID:       spaceID,
		Slug:          slug,
		DisplayName:   displayName,
		ToolchainType: "Kubernetes/YAML",
		TargetID:      &targetID,
		Annotations:   annotations,
	}
	res, err := cubClientNew.CreateUnitWithResponse(ctx, spaceID, &goclientnew.CreateUnitParams{}, body)
	if cubapi.IsAPIError(err, res) {
		return uuid.Nil, cubapi.InterpretErrorGeneric(err, res)
	}
	created, err := unitFromWrite(res.JSON200)
	if err != nil {
		return uuid.Nil, err
	}
	unitID := created.UnitID
	if _, err := putUnitData(spaceID, unitID, string(manifest), nil); err != nil {
		return unitID, fmt.Errorf("unit %s was created, but its config data could not be written: %w", slug, err)
	}
	return unitID, nil
}

// clusterSetReleaseTarget sets the Space's ReleaseTargetID to the OCI target.
// Releases are published per Space ("cub release publish <space>") and consume
// the Space's release Target; the OCI registry serves the Space's Releases at
// /space/<slug> only when it is set. The server validates that the Target's
// provider is OCI.
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

// clusterSetClusterUnitID sets ClusterUnitID on the Space spaceID to the Unit
// whose Application, ClusterProfile or Kustomization deploys it. The merge patch
// leaves the Space's other annotations alone.
func clusterSetClusterUnitID(spaceID, unitID uuid.UUID) error {
	patchData, err := json.Marshal(map[string]interface{}{
		"Annotations": map[string]string{clusterAnnotationClusterUnitID: unitID.String()},
	})
	if err != nil {
		return err
	}
	if _, err := patchSpace(spaceID, patchData); err != nil {
		return fmt.Errorf("set %s on space: %w", clusterAnnotationClusterUnitID, err)
	}
	return nil
}

// clusterClearReleaseTarget clears the Space's ReleaseTargetID. Used by the
// `cub cluster up` rollback before recursively deleting a Space it just
// created: the Space's release-target reference blocks deleting the OCI target
// it points at (spaces_release_target_id_fkey is ON DELETE RESTRICT). Both the
// apps Space and the argobot variant point their release target cross-Space at
// the OCI target in the cluster Space, so clearing that reference lets their
// deletion proceed before the cluster Space (holding the target) is deleted.
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
// the unit carries an `awaiting/triggers` ApplyGate. Publishing a Release does
// check gates and refuses to bundle a Unit that has any, so without this wait a
// publish issued right after a mutation fails on the transient gate rather than
// on anything real. Wait for it to clear (up to 30s); a gate that is still there
// afterwards is a real verdict and is left to the server to enforce.
func clusterWaitUnitTriggers(spaceID, unitID uuid.UUID) error {
	deadline := time.Now().Add(30 * time.Second)
	backoff := 1 * time.Second
	for {
		unit, err := resolveUnit(unitID.String(), spaceID.String(), "ValidationErrors")
		if err != nil {
			return err
		}
		if _, pending := unit.Unit.ValidationErrors["awaiting/triggers"]; !pending {
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

// clusterDependentSpaceSlugs returns the slugs of the Spaces whose release
// target is the OCI target in the given cluster Space — the apps Space, the
// argobot variant Space, and any deployment variant Spaces. They reference the
// target cross-Space (spaces_release_target_id_fkey is ON DELETE RESTRICT), so
// they must be deleted before the cluster Space that holds the target. The
// cluster Space itself is excluded (its own release target is nil). A cluster
// Space whose target is already gone returns an empty list: with ON DELETE
// RESTRICT the target could not have been deleted while anything referenced it.
func clusterDependentSpaceSlugs(clusterSpaceID uuid.UUID) ([]string, error) {
	target, err := resolveTargetCore(clusterTargetSlug, clusterSpaceID.String(), "TargetID")
	if err != nil || target == nil {
		return nil, nil
	}
	spaces, err := apiListSpaces("Space.ReleaseTargetID = '"+target.TargetID.String()+"'", "SpaceID,Slug")
	if err != nil {
		return nil, fmt.Errorf("list spaces referencing the cluster target: %w", err)
	}
	var slugs []string
	for _, s := range spaces {
		if s.SpaceID != clusterSpaceID {
			slugs = append(slugs, s.Slug)
		}
	}
	return slugs, nil
}

// clusterDeleteSpace deletes a space by slug. If recursive is true, the server
// cascades deletion to units, targets, and workers within the space (subject
// to delete gates).
func clusterDeleteSpace(slug string, recursive bool) error {
	space, err := resolveSpace(slug, "SpaceID")
	if err != nil {
		return err
	}
	params := &goclientnew.DeleteSpaceParams{}
	if recursive {
		t := "true"
		params.Recursive = &t
	}
	res, err := cubClientNew.DeleteSpaceWithResponse(ctx, space.Space.SpaceID, params)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return nil
}
