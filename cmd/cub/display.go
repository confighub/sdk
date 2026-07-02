// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/confighub/sdk/configkit/yqkit"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/itchyny/gojq"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/olekukonko/tablewriter"
	"gopkg.in/yaml.v3"
)

// Do not call this directly from a command for error responses from API requests.
// Call cubapi.InterpretErrorGeneric and return the result instead.
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

func permissionsToString(permissions *goclientnew.Permissions) string {
	if permissions == nil || len(*permissions) == 0 {
		return ""
	}
	var parts []string
	for actionCategory, subjects := range *permissions {
		if len(subjects.UserIDs) > 0 {
			userIDs := make([]string, 0, len(subjects.UserIDs))
			for userID := range subjects.UserIDs {
				userIDs = append(userIDs, userID)
			}
			parts = append(parts, fmt.Sprintf("%s: [%s]", actionCategory, strings.Join(userIDs, ", ")))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

func uuidSliceToString(uuids []openapi_types.UUID) string {
	if len(uuids) == 0 {
		return ""
	}
	strs := make([]string, len(uuids))
	for i, u := range uuids {
		strs[i] = u.String()
	}
	return strings.Join(strs, ", ")
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

// renderPayload applies the requested alternative output format to payload.
// Returns true if the format was handled (caller should skip default output).
// The default case (OutputDefault, OutputWide, OutputName, OutputCustomColumns,
// OutputMutations) are not rendered here; the caller decides how to surface them.
func renderPayload(payload any) bool {
	spec := effectiveOutput()
	switch spec.Kind {
	case OutputJSON:
		displayJSON(payload)
		return true
	case OutputYAML:
		displayYAML(payload)
		return true
	case OutputJQ:
		outBytes, err := json.Marshal(payload)
		failOnError(err)
		displayJQForBytes(outBytes, spec.Arg)
		return true
	case OutputYQ:
		displayYQWith(payload, spec.Arg)
		return true
	}
	return false
}

func displayYQWith(entity any, expr string) {
	outBytes, err := yaml.Marshal(entity)
	failOnError(err)
	yqBytes, err := yqkit.EvalYQExpression(expr, string(outBytes))
	failOnError(err)
	tprintRaw(string(yqBytes))
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
	displayYQWith(entity, yq)
}

// displayResponseErrorDetails displays detailed information for a single ResponseError
func displayResponseErrorDetails(respError *goclientnew.ResponseError) {
	table := detailView()

	table.Append([]string{"Message:", respError.Message})
	table.Append([]string{"Status:", fmt.Sprintf("%d", respError.Status)})

	if respError.Type != "" {
		table.Append([]string{"Type:", respError.Type})
	}

	if respError.ErrorCategory != "" {
		table.Append([]string{"Category:", respError.ErrorCategory})
	}

	if respError.ErrorMetadata != nil {
		if respError.ErrorMetadata.EntityType != "" {
			table.Append([]string{"Entity Type:", respError.ErrorMetadata.EntityType})
		}
		if respError.ErrorMetadata.EntityID != "" {
			table.Append([]string{"Entity ID:", respError.ErrorMetadata.EntityID})
		}
	}

	if len(respError.Details) > 0 {
		table.Append([]string{"Details:", strings.Join(respError.Details, "; ")})
	}

	table.Render()
}

var verboseErrors = os.Getenv("CONFIGHUB_DEBUG") == "errors"

// displayResponseErrorTable displays a table of multiple ResponseErrors
func displayResponseErrorTable(errors []*goclientnew.ResponseError) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Entity Type", "Entity ID", "Status", "Message"})
	}

	for _, respError := range errors {
		entityType := ""
		entityID := ""

		if respError.ErrorMetadata != nil {
			entityType = respError.ErrorMetadata.EntityType
			entityID = respError.ErrorMetadata.EntityID
		}

		// If no entity metadata, show "unknown"
		if entityType == "" {
			entityType = "unknown"
		}
		if entityID == "" {
			entityID = "unknown"
		}

		table.Append([]string{
			entityType,
			entityID,
			fmt.Sprintf("%d", respError.Status),
			respError.Message,
		})
	}

	table.Render()

	if verboseErrors {
		for _, respError := range errors {
			displayResponseErrorDetails(respError)
		}
	}
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
		goclientnew.UnitAction |
		goclientnew.ExtendedUnit |
		goclientnew.Attribute |
		goclientnew.ExtendedAttribute |
		goclientnew.Release |
		goclientnew.ExtendedRelease |
		goclientnew.OAuthClient |
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
		goclientnew.DeleteSpaceResponse |
		goclientnew.DeleteTagResponse |
		goclientnew.DeleteTargetResponse |
		goclientnew.DeleteTriggerResponse |
		goclientnew.DeleteUnitResponse |
		goclientnew.DeleteViewResponse |
		goclientnew.DeleteAttributeResponse
}

func displayCreateResults[Entity ModelConstraint](entity *Entity, entityName, slug, id string, display func(entity *Entity)) {
	alt := isAlternativeOutput()
	if !quiet && !alt {
		tprint("Successfully created %s %s (%s)", entityName, slug, id)
	}
	if verbose {
		display(entity)
	}
	renderPayload(entity)
}

func displayUpdateResults[Entity ModelConstraint](entity *Entity, entityName, slug, id string, display func(entity *Entity)) {
	alt := isAlternativeOutput()
	if !quiet && !alt {
		tprint("Successfully updated %s %s (%s)", entityName, slug, id)
	}
	if verbose {
		display(entity)
	}
	renderPayload(entity)
}

func displayListResults[Entity ModelConstraint](entities []*Entity, getSlug func(entity *Entity) string, display func(entities []*Entity)) {
	spec := effectiveOutput()
	alt := isAlternativeOutput()

	if (!quiet || verbose) && !alt {
		display(entities)
	}
	if spec.Kind == OutputName && getSlug != nil {
		// Don't print as table. Extra whitespace makes the output harder to use in scripts.
		for _, entity := range entities {
			tprintRaw(getSlug(entity))
		}
	}
	renderPayload(entities)
}

func displayGetResults[Entity ModelConstraint](entity *Entity, display func(entity *Entity)) {
	alt := isAlternativeOutput()
	if !quiet && !alt {
		display(entity)
	}
	renderPayload(entity)
}

func displayDeleteResults(entityName, slug, id string, response any) {
	alt := isAlternativeOutput()
	if !quiet && !alt {
		tprint("Successfully deleted %s %s (%s)", entityName, slug, id)
	}
	renderPayload(response)
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

	alt := isAlternativeOutput()

	renderPayload(responses)

	// Display regular output unless quiet or alternative output is specified
	if !quiet && !alt {
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

	alt := isAlternativeOutput()

	renderPayload(responses)

	// Display regular output unless quiet or alternative output is specified
	if !quiet && !alt {
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

// handleBulkUnitActionResponse handles responses from bulk unit action operations (apply, destroy, refresh)
func handleBulkUnitActionResponse(results *[]goclientnew.UnitActionResponse, action string, isDryRun bool) error {
	if results == nil || len(*results) == 0 {
		if !quiet && !isAlternativeOutput() {
			tprint("No units found matching the filter")
		}
		renderPayload(results)
		return nil
	}

	// Count successes and failures
	var successCount, failureCount int
	var queuedOps []*goclientnew.QueuedOperation
	var hasErrors bool

	for _, result := range *results {
		if result.Error != nil {
			failureCount++
			hasErrors = true
			if !quiet {
				// Display error for this unit
				if result.Error.ErrorMetadata != nil && result.Error.ErrorMetadata.EntityID != "" {
					if result.Error.ErrorMetadata.EntitySlug != "" {
						tprint(
							"Failed to %s unit %s (%s): %s",
							action,
							result.Error.ErrorMetadata.EntitySlug,
							result.Error.ErrorMetadata.EntityID,
							result.Error.Message,
						)
					} else {
						tprint("Failed to %s unit %s: %s", action, result.Error.ErrorMetadata.EntityID, result.Error.Message)
					}
				} else {
					tprint("Failed: %s", result.Error.Message)
				}
			}
		} else if result.Action != nil {
			successCount++
			queuedOps = append(queuedOps, result.Action)
			if !quiet && !actionWait {
				// Fetch unit details to get the slug
				unitDetails, err := apiGetUnitInSpace(result.Action.UnitID.String(), result.Action.SpaceID.String(), "Slug")
				unitSlug := result.Action.UnitID.String()
				if err == nil && unitDetails != nil && unitDetails.Slug != "" {
					unitSlug = unitDetails.Slug
				}
				tprint("%s queued for %s (operation: %s)",
					strings.Title(action), unitSlug, result.Action.QueuedOperationID)
			}
		}
	}

	// Handle wait flag by awaiting operations (skip if dry run)
	if actionWait && len(queuedOps) > 0 && !isDryRun {
		if !quiet {
			tprint("Awaiting %d %s operations...", len(queuedOps), action)
		}
		// Poll all operations in a single loop instead of sequentially
		// This prevents false failures when later operations complete before earlier ones
		results := awaitBulkCompletion(action, queuedOps)
		for _, err := range results {
			if err != nil {
				hasErrors = true
			}
		}
	}

	// Display results in alternative formats
	renderPayload(results)

	// Display summary
	if !quiet && !isAlternativeOutput() {
		if isDryRun {
			tprint("") // blank line before summary
			tprint("Dry run completed (no changes made)")
			if successCount > 0 {
				tprint("Units that would be %sed: %d", action, successCount)
			}
			if failureCount > 0 {
				tprint("Units that would fail: %d", failureCount)
			}
			tprint("Total units processed: %d", successCount+failureCount)
		} else {
			if actionWait {
				// Operations were waited for, report completion status
				if successCount > 0 && failureCount > 0 {
					tprint("Bulk %s completed with partial success: %d succeeded, %d failed", action, successCount, failureCount)
				} else if successCount > 0 {
					tprint("Bulk %s completed successfully: %d units", action, successCount)
				} else if failureCount > 0 {
					tprint("Bulk %s failed: %d units", action, failureCount)
				}
			} else {
				// Operations were queued but not waited for
				if successCount > 0 && failureCount > 0 {
					tprint("Bulk %s queued with partial success: %d queued, %d failed to queue", action, successCount, failureCount)
				} else if successCount > 0 {
					tprint("Bulk %s queued successfully: %d units", action, successCount)
				} else if failureCount > 0 {
					tprint("Bulk %s failed to queue: %d units", action, failureCount)
				}
			}
		}
	}

	if hasErrors {
		return fmt.Errorf("bulk %s had failures", action)
	}

	return nil
}
