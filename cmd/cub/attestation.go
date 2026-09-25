// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var attestationCmd = &cobra.Command{
	Use:   "attestation",
	Short: "Attestation commands",
	Long: getCommandHelp(`The attestation subcommands record, list, get, and revoke attestations.

An attestation records that someone made a claim about specific revisions: that they
approve them, that a review or check passed or failed, or anything else its type names.
Approval is the most common type, and `+"`cub variant approve`"+` records one for the
units of one or more spaces. These commands record any type.

An attestation is never changed after it is recorded. Withdrawing one records another
that names it: `+"`cub attestation revoke`"+`.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(attestationCmd)
	rootCmd.AddCommand(attestationCmd)
	addExplainCmd(attestationCmd, "Attestation")
}

// attestationStatementArgs are the flags that say what an attestation claims, shared by the
// commands that record one.
type attestationStatementArgs struct {
	attestationType string
	reject          bool
	note            string
	claims          []string
	evidence        []string
	expiresIn       time.Duration
}

func addAttestationStatementFlags(cmd *cobra.Command, args *attestationStatementArgs, withType bool) {
	if withType {
		cmd.Flags().StringVar(&args.attestationType, "type", "Approval", "what is being claimed, such as Approval or SecurityReview")
	}
	cmd.Flags().BoolVar(&args.reject, "reject", false, "record a Fail result: a rejection, for an approval")
	cmd.Flags().StringVar(&args.note, "note", "", "the reason for the claim, in your own words")
	cmd.Flags().StringArrayVar(&args.claims, "claim", nil, "a key=value pair to record with the attestation, such as servicenow.com/change=CHG0012345; repeatable")
	cmd.Flags().StringArrayVar(&args.evidence, "evidence", nil, "the ID of another attestation this one relied on; repeatable")
	cmd.Flags().DurationVar(&args.expiresIn, "expires-in", 0, "how long the attestation satisfies requirements for, such as 72h; by default it does not expire")
}

// attestationStatement is what the flags claim, in the fields both requests that record an
// attestation carry.
type attestationStatement struct {
	Type                   string
	Result                 string
	Note                   string
	Claims                 map[string]string
	EvidenceAttestationIDs []goclientnew.UUID
	ExpiresAt              time.Time
	ChangeOrderID          *uuid.UUID
}

// statement builds what the flags claim. The ChangeOrder is resolved by the caller, since the
// commands scope it differently.
func (args *attestationStatementArgs) statement(changeOrderID *uuid.UUID) (*attestationStatement, error) {
	statement := &attestationStatement{
		Type:          args.attestationType,
		Note:          args.note,
		ChangeOrderID: changeOrderID,
	}
	if args.reject {
		statement.Result = "Fail"
	}
	if len(args.claims) > 0 {
		statement.Claims = map[string]string{}
		for _, claim := range args.claims {
			key, value, ok := strings.Cut(claim, "=")
			if !ok || key == "" {
				return nil, errors.Newf("--claim %q must be key=value", claim)
			}
			statement.Claims[key] = value
		}
	}
	for _, evidence := range args.evidence {
		id, err := uuid.Parse(evidence)
		if err != nil {
			return nil, errors.Newf("--evidence %q must be an attestation ID", evidence)
		}
		statement.EvidenceAttestationIDs = append(statement.EvidenceAttestationIDs, id)
	}
	if args.expiresIn > 0 {
		statement.ExpiresAt = time.Now().Add(args.expiresIn).UTC()
	}
	return statement, nil
}

func (s *attestationStatement) createRequest() goclientnew.AttestationCreateRequest {
	return goclientnew.AttestationCreateRequest{
		Type: s.Type, Result: s.Result, Note: s.Note, Claims: s.Claims,
		EvidenceAttestationIDs: s.EvidenceAttestationIDs, ExpiresAt: s.ExpiresAt, ChangeOrderID: s.ChangeOrderID,
	}
}

func (s *attestationStatement) attestRequest() goclientnew.AttestRequest {
	return goclientnew.AttestRequest{
		Type: s.Type, Result: s.Result, Note: s.Note, Claims: s.Claims,
		EvidenceAttestationIDs: s.EvidenceAttestationIDs, ExpiresAt: s.ExpiresAt, ChangeOrderID: s.ChangeOrderID,
	}
}

// attestationRevisionParameter turns --revision into the revision specification the API takes,
// resolving slugs. A delta from the head is refused: each Unit has its own head, and the server
// resolves the specification Unit by Unit.
func attestationRevisionParameter(revision string) (string, error) {
	if revision == "" {
		return "", nil
	}
	if strings.HasPrefix(revision, "-") {
		return "", errors.Newf("--revision %q: a delta from the head names a different revision on each unit, so name the revision instead", revision)
	}
	formatted, isRevisionUUID, err := parseSelectedRevisionParameter(revision, serverResolvedRevision, 0)
	if err != nil {
		return "", err
	}
	if isRevisionUUID {
		formatted = "Revision:" + formatted
	}
	return formatted, nil
}

// displayAttestationCreateResult prints what recording an attestation in one space did.
func displayAttestationCreateResult(spaceSlug string, result *goclientnew.AttestationCreateResponse, dryRun bool) {
	if quiet || isAlternativeOutput() {
		return
	}
	verb := "Recorded"
	if dryRun {
		verb = "Would record"
	}
	switch {
	case result.Attestation != nil && result.Attestation.RevokedAttestationID != nil:
		tprint("%s a revocation of attestation %s in %s", verb, result.Attestation.RevokedAttestationID.String(), spaceSlug)
	case result.Attestation != nil:
		id := ""
		if !dryRun {
			id = " " + result.Attestation.AttestationID.String()
		}
		tprint("%s %s %s attestation%s in %s, covering %d revision(s)",
			verb, strings.ToLower(string(result.Attestation.Result)), result.Attestation.Type, id, spaceSlug, len(result.Subjects))
	default:
		tprint("Nothing to record in %s: no unit has the selected revision", spaceSlug)
	}
	if verbose {
		for _, subject := range result.Subjects {
			tprint("  %s revision %d", subject.UnitSlug, subject.RevisionNum)
		}
	}
	for _, skipped := range result.SkippedUnits {
		tprint("  skipped %s: %s", skipped.UnitSlug, skipped.Reason)
	}
}

// apiFindAttestation reads one attestation by ID, from whichever space holds it.
func apiFindAttestation(id uuid.UUID) (*goclientnew.ExtendedAttestation, error) {
	found, err := cubapi.ListAttestations(ctx, cubClient, cubapi.NewWhere("").Eq("AttestationID", id.String()),
		cubapi.ListOpts{Include: "SpaceID"})
	if err != nil {
		return nil, err
	}
	if len(found) == 0 || found[0].Attestation == nil {
		return nil, fmt.Errorf("attestation %s not found", id)
	}
	return found[0], nil
}

// describeRevisionAttestations says what the Attestations covering a Revision claim, one per
// line: type, result, who recorded it and when, and whether it has been revoked since. A
// Revision carries only the IDs, so the Attestations and any revocations naming them are listed
// from its Space. Covering is not satisfying a requirement: expiry, eligibility and the author
// exclusion are decided by the ChangeWorkflow that reads them.
func describeRevisionAttestations(spaceID goclientnew.UUID, attestationIDs map[string]string) (string, error) {
	if len(attestationIDs) == 0 {
		return "", nil
	}
	ids := make([]goclientnew.UUID, 0, len(attestationIDs))
	for id := range attestationIDs {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return "", errors.Wrapf(err, "invalid attestation id %q", id)
		}
		ids = append(ids, parsed)
	}
	attestations, err := cubapi.ListAttestations(ctx, cubClient, cubapi.NewWhere("").SpaceID(spaceID).In("AttestationID", ids), cubapi.ListOpts{})
	if err != nil {
		return "", err
	}
	revocations, err := cubapi.ListAttestations(ctx, cubClient, cubapi.NewWhere("").SpaceID(spaceID).In("RevokedAttestationID", ids), cubapi.ListOpts{})
	if err != nil {
		return "", err
	}
	revoked := map[goclientnew.UUID]bool{}
	for _, revocation := range revocations {
		if revocation.Attestation != nil && revocation.Attestation.RevokedAttestationID != nil {
			revoked[*revocation.Attestation.RevokedAttestationID] = true
		}
	}
	covering := make([]*goclientnew.Attestation, 0, len(attestations))
	for _, ea := range attestations {
		if ea.Attestation != nil {
			covering = append(covering, ea.Attestation)
		}
	}
	sort.Slice(covering, func(i, j int) bool { return covering[i].CreatedAt.Before(covering[j].CreatedAt) })
	usernames := map[goclientnew.UUID]string{}
	lines := make([]string, 0, len(covering))
	for _, a := range covering {
		username, ok := usernames[a.UserID]
		if !ok {
			username = a.UserID.String()
			if user, err := apiGetUser(username); err == nil && user != nil && user.Username != "" {
				username = user.Username
			}
			usernames[a.UserID] = username
		}
		line := fmt.Sprintf("%s %s by %s at %s (%s)", a.Type, a.Result, username,
			a.CreatedAt.UTC().Format(time.RFC3339), a.AttestationID)
		if revoked[a.AttestationID] {
			line += ", revoked"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}
