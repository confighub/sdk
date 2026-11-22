// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var auditGetCmd = &cobra.Command{
	Use:   "get <audit-event-id>",
	Short: "Get details about an audit event",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about an audit event, by ID. An audit event has no slug.

Examples:
`+"```"+`
  cub audit get 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c
  cub audit get 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c -o json
`+"```"+`
`, ""),
	RunE: auditGetCmdRun,
}

func init() {
	addStandardGetFlags(auditGetCmd)
	auditCmd.AddCommand(auditGetCmd)
}

func auditGetCmdRun(cmd *cobra.Command, args []string) error {
	id, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid audit event id %q: %w", args[0], err)
	}
	found, err := cubapi.ListAuditEvents(ctx, cubClient, cubapi.NewWhere("").Eq("AuditEventID", id.String()),
		cubapi.ListOpts{Select: cubapi.SelectFields(selectFields)})
	if err != nil {
		return err
	}
	if len(found) == 0 || found[0].AuditEvent == nil {
		return fmt.Errorf("audit event %s not found", id)
	}
	displayGetResults(found[0], displayExtendedAuditEventDetails)
	return nil
}

// displayExtendedAuditEventDetails renders what get returns: the AuditEvent wrapped the way every
// other get's entity is, so -o json and -o jq read it as .AuditEvent, as list does.
func displayExtendedAuditEventDetails(extendedAuditEvent *goclientnew.ExtendedAuditEvent) {
	displayAuditEventDetails(extendedAuditEvent.AuditEvent)
}

func displayAuditEventDetails(auditEvent *goclientnew.AuditEvent) {
	view := tableView()
	view.Append([]string{"ID", auditEvent.AuditEventID.String()})
	view.Append([]string{"Occurred At", auditEvent.OccurredAt.Format(time.RFC3339)})
	view.Append([]string{"Request ID", auditEvent.RequestID})
	view.Append([]string{"Origin", string(auditEvent.Origin)})
	view.Append([]string{"User ID", uuidPtrString(auditEvent.UserID)})
	view.Append([]string{"On Behalf Of User ID", uuidPtrString(auditEvent.OnBehalfOfUserID)})
	view.Append([]string{"Service Account ID", uuidPtrString(auditEvent.ServiceAccountID)})
	view.Append([]string{"Source IP", auditEvent.SourceIP})
	view.Append([]string{"User Agent", auditEvent.UserAgent})
	view.Append([]string{"Role", auditEvent.Role})
	for _, grant := range auditEvent.Grants {
		view.Append([]string{"Grant", strings.Join([]string{string(grant.Source), grant.EntityType, grant.EntityID.String(), uuidPtrString(grant.GroupID)}, " ")})
	}
	view.Append([]string{"Action", auditEvent.Action})
	view.Append([]string{"Entity Type", auditEvent.SubjectEntityType})
	view.Append([]string{"Entity ID", auditEvent.SubjectEntityID.String()})
	view.Append([]string{"Entity Slug", auditEvent.SubjectSlug})
	view.Append([]string{"Space ID", uuidPtrString(auditEvent.SpaceID)})
	view.Append([]string{"Outcome", auditEvent.Outcome})
	if auditEvent.Error != "" {
		view.Append([]string{"Error", auditEvent.Error})
	}
	view.Append([]string{"Revision ID", uuidPtrString(auditEvent.RevisionID)})
	if len(auditEvent.ChangedFields) > 0 {
		view.Append([]string{"Changed Fields", strings.Join(auditEvent.ChangedFields, ",")})
	}
	if auditEvent.HeadRevisionHash != "" {
		view.Append([]string{"Head Revision", fmt.Sprintf("%d %s", auditEvent.HeadRevisionNum, auditEvent.HeadRevisionHash)})
	}
	view.Render()
}
