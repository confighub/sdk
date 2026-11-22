// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"time"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var auditListCmd = &cobra.Command{
	Use:   "list",
	Short: "List audit events",
	Long: getCommandHelp(`List the audit events of the organization.

--since and --until take an RFC 3339 time, and select the events that occurred at or after
--since and before --until.

Examples:
`+"```"+`
  # List audit events with headers
  cub audit list

  # List the audit events of the last day
  cub audit list --since 2026-11-18T00:00:00Z

  # List the deletes in a time range
  cub audit list --where "Action = 'Delete'" --since 2026-11-01T00:00:00Z --until 2026-12-01T00:00:00Z

  # List audit events in JSON format
  cub audit list -o json

  # List audit events with custom JQ filter
  cub audit list -o jq='.[].AuditEvent.AuditEventID'
`+"```"+`
`, ""),
	RunE: auditListCmdRun,
}

var (
	auditSince string
	auditUntil string
)

// Default columns to display when no custom columns are specified
var defaultAuditEventColumns = []string{"AuditEvent.AuditEventID", "AuditEvent.OccurredAt", "AuditEvent.Action",
	"AuditEvent.SubjectEntityType", "AuditEvent.SubjectSlug", "AuditEvent.Outcome", "AuditEvent.UserID"}

// auditEventBaseSelectFields are the fields always returned by audit event list queries. An
// AuditEvent has no Slug, so it is named by its ID, in -o name and to audit get.
var auditEventBaseSelectFields = []string{"AuditEventID"}

// AuditEvent-specific aliases
var auditEventAliases = map[string]string{
	"ID": "AuditEvent.AuditEventID",
}

// AuditEvent custom column dependencies
var auditEventCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(auditListCmd)
	enableWherePermissionFlag(auditListCmd)
	enableListPagingFlags(auditListCmd)
	enableViewFlag(auditListCmd)
	auditListCmd.Flags().StringVar(&auditSince, "since", "", "list the events that occurred at or after this RFC 3339 time")
	auditListCmd.Flags().StringVar(&auditUntil, "until", "", "list the events that occurred before this RFC 3339 time")
	auditCmd.AddCommand(auditListCmd)
}

// auditTimeRange adds the OccurredAt terms of --since and --until to where. The times are
// parsed, and written back in RFC 3339, so that nothing but a time reaches the expression.
func auditTimeRange(where cubapi.Where, since, until string) (cubapi.Where, error) {
	for _, bound := range []struct {
		flag, value, operator string
	}{
		{"--since", since, ">="},
		{"--until", until, "<"},
	} {
		if bound.value == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, bound.value)
		if err != nil {
			return where, fmt.Errorf("%s must be an RFC 3339 time, such as 2026-11-01T00:00:00Z: %w", bound.flag, err)
		}
		where = where.And(fmt.Sprintf("OccurredAt %s '%s'", bound.operator, t.UTC().Format(time.RFC3339Nano)))
	}
	return where, nil
}

func auditListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}
	auditWhere, err := auditTimeRange(cubapi.NewWhere(where), auditSince, auditUntil)
	if err != nil {
		return err
	}

	selectValue := handleSelectParameter(selectFields, selectFields, func() string {
		return buildSelectList("AuditEvent", listColumnsFor("cub audit list"), "", defaultAuditEventColumns, auditEventAliases, auditEventCustomColumnDependencies, auditEventBaseSelectFields)
	})
	auditEvents, err := cubapi.ListAuditEvents(ctx, cubClient, auditWhere, cubapi.ListOpts{
		Limit:           listLimit,
		OrderBy:         listOrderBy,
		Select:          cubapi.SelectFields(selectValue),
		Filter:          filterID,
		Contains:        contains,
		WherePermission: wherePermission,
		View:            listViewFor("cub audit list"),
	})
	if err != nil {
		return err
	}
	displayListResults(auditEvents, getNameForAuditEvent, displayAuditEventList)
	return nil
}

func getNameForAuditEvent(extendedAuditEvent *goclientnew.ExtendedAuditEvent) string {
	return extendedAuditEvent.AuditEvent.AuditEventID.String()
}

func displayAuditEventList(auditEvents []*goclientnew.ExtendedAuditEvent) {
	if displayRequestedColumns(auditEvents, auditEventAliases, nil) {
		return
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"ID", "Occurred-At", "Action", "Entity-Type", "Entity", "Outcome", "User-ID"})
	}
	for _, extendedAuditEvent := range auditEvents {
		auditEvent := extendedAuditEvent.AuditEvent
		table.Append([]string{
			auditEvent.AuditEventID.String(),
			auditEvent.OccurredAt.Format(time.RFC3339),
			auditEvent.Action,
			auditEvent.SubjectEntityType,
			auditEvent.SubjectSlug,
			auditEvent.Outcome,
			uuidPtrString(auditEvent.UserID),
		})
	}
	table.Render()
}

// uuidPtrString renders an optional ID, empty when it is absent.
func uuidPtrString(id *goclientnew.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
