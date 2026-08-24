// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// invokeIncludeConfigData asks an invocation to return the configuration it produced, for the
// output modes that are going to display it. The response carries the configuration only when
// the invocation changed it -- right for the worker boundary, where re-sending what was just
// sent is waste -- so displaying an unchanged one would otherwise cost a second request.
// Requested only when needed, since most invocations do not want the document back.
func invokeIncludeConfigData() *string {
	if !dataOnly && !verbose && effectiveShow() != ShowData {
		return nil
	}
	s := "ConfigData"
	return &s
}

// responseConfigData returns the configuration a function response describes. It is on the
// response when the invocation changed it, or when the caller asked for it with
// invokeIncludeConfigData. The fetch is the fallback for neither: a caller that did not ask
// and an invocation that changed nothing. Returns "" if there is no Unit to fetch from, which
// is the Revision-invocation case.
func responseConfigData(respMsg *goclientnew.FunctionInvocationsResponse) (string, error) {
	if respMsg.ConfigData != "" {
		return respMsg.ConfigData, nil
	}
	if respMsg.UnitID == uuid.Nil || respMsg.SpaceID == uuid.Nil {
		return "", nil
	}
	return fetchUnitData(respMsg.SpaceID, respMsg.UnitID)
}

// unitDisplayName returns the unit slug with ID in parentheses if available, otherwise just the UUID.
func unitDisplayName(respMsg *goclientnew.FunctionInvocationsResponse) string {
	if respMsg.UnitSlug != "" {
		slug := respMsg.UnitSlug
		if respMsg.SpaceSlug != "" {
			slug = respMsg.SpaceSlug + "/" + slug
		}
		return fmt.Sprintf("%s (%s)", slug, respMsg.UnitID.String())
	}
	return respMsg.UnitID.String()
}

// functionDisplayName returns the function name if available, otherwise the index.
func functionDisplayName(functionName string, index int) string {
	if functionName != "" {
		return functionName
	}
	return fmt.Sprintf("#%d", index)
}

// displayAttributeValue prints a single attribute value with labeled fields.
func displayAttributeValue(av *api.AttributeValue, indent string) {
	description := ""
	if av.Details != nil && av.Details.Description != "" {
		description = av.Details.Description
	}
	// DataType not shown because it often can be inferred from the value.
	// If needed, it's in the JSON.
	tprint("%s  Value: %v  Path: %s  Resource: %s  Type: %s",
		indent, av.Value, av.Path, av.ResourceName, av.ResourceType)
	if av.Score != "" {
		tprint("%s    Score: %s", indent, av.Score)
	}
	if description != "" {
		tprint("%s    Description: %s", indent, description)
	}
	displayIssues(av.Issues, indent)
}

// displayResourceInfo prints a single resource info entry.
func displayResourceInfo(ri *api.ResourceInfo) {
	tprint("  Resource: %s  Type: %s", ri.ResourceName, ri.ResourceType)
}

// displayResource prints a single resource entry with its body.
func displayResource(r *api.Resource) {
	tprint("  Resource: %s  Type: %s:", r.ResourceName, r.ResourceType)
	tprintRaw(r.ResourceBody)
}

// displayResponseError prints a response error with details on separate indented lines.
func displayResponseError(respErr *goclientnew.ResponseError) {
	detail := detailView()
	detail.Append([]string{strings.ToUpper("Error"), respErr.Message})
	detail.Render()
	for _, d := range respErr.Details {
		tprint("  %s", d)
	}
}

// displayIssues prints issues if present.
func displayIssues(issues []api.Issue, indent string) {
	for _, issue := range issues {
		if issue.Identifier != "" {
			tprint("%s  Issue [%s]: %s", indent, issue.Identifier, issue.Message)
		} else {
			tprint("%s  Issue: %s", indent, issue.Message)
		}
	}
}

// displayValidationResult prints a single validation result with its details.
func displayValidationResult(vr *api.ValidationResult) {
	funcDisplay := functionDisplayName(vr.FunctionName, vr.Index)
	tprint("  Passed: %v  Function: %s", vr.Passed, funcDisplay)
	if vr.MaxScore != "" {
		tprint("    MaxScore: %s", vr.MaxScore)
	}
	for _, detail := range vr.Details {
		tprint("  %s", detail)
	}
	displayIssues(vr.Issues, "  ")
	if len(vr.FailedAttributes) > 0 {
		tprintRaw("    Attributes:")
		for j := range vr.FailedAttributes {
			displayAttributeValue(&vr.FailedAttributes[j], "  ")
		}
	}
}

func outputFunctionInvocationResponse(respMsgs *[]goclientnew.FunctionInvocationsResponse) {
	for i := range *respMsgs {
		respMsg := &(*respMsgs)[i]
		if !quiet && !outputOnly && !dataOnly && !outputValuesOnly && !outputRaw {
			statusVerb := "failed"
			if respMsg.Success {
				statusVerb = "succeeded"
			}
			unitDisplay := unitDisplayName(respMsg)
			if respMsg.RevisionID != uuid.Nil {
				tprint("Function(s) %s on revision %s of unit %s", statusVerb, respMsg.RevisionID.String(), unitDisplay)
			} else {
				tprint("Function(s) %s on unit %s", statusVerb, unitDisplay)
			}
			if !respMsg.Success && respMsg.Error != nil {
				displayResponseError(respMsg.Error)
			}
		}
		if dataOnly || (!quiet && !outputOnly && !outputValuesOnly && !outputRaw) {
			if dataOnly || len(respMsg.Mutators) > 0 {
				// Don't use detailView to print the data because it pads the entire width with spaces.
				if !dataOnly {
					if verbose {
						tprintRaw("CONFIGDATA\n---------\n")
					} else {
						tprintRaw("Config data changed")
					}
				}
				if dataOnly || verbose {
					data, err := responseConfigData(respMsg)
					failOnError(err)
					if len(data) != 0 {
						tprintRaw(data)
					}
				}
			} else if !dataOnly && len(respMsg.Outputs) == 0 {
				tprintRaw("Config data not changed")
			}
		}
		if (outputOnly || (!quiet && !dataOnly && !outputValuesOnly)) && len(respMsg.Outputs) != 0 {
			// Don't use detailView to print the output because it pads the entire width with spaces.
			if !outputOnly && !outputValuesOnly && !outputRaw {
				tprintRaw("OUTPUT\n------\n")
			}

			// Handle all output types
			for outputType, outputData := range respMsg.Outputs {
				if len(outputData) == 0 {
					continue
				}

				outputBytes, err := base64.StdEncoding.DecodeString(outputData)
				if err != nil {
					tprintRaw(outputData)
					failOnError(fmt.Errorf("%s: Failed to decode output", err.Error()))
				}
				if strings.TrimSpace(string(outputBytes)) == "null" {
					continue
				}

				if len(respMsg.Outputs) > 1 {
					tprintRaw(fmt.Sprintf("%s:\n", outputType))
				}

				switch outputType {
				case string(api.OutputTypeYAML):
					var payload api.YAMLPayload
					err := json.Unmarshal(outputBytes, &payload)
					// If there's an error print the raw output
					if err != nil || outputRaw {
						tprintRaw(string(outputBytes))
					} else {
						tprintRaw(payload.Payload)
					}
				case string(api.OutputTypeAttributeValueList):
					var payload api.AttributeValueList
					err := json.Unmarshal(outputBytes, &payload)
					// If there's an error print the raw output
					if err != nil {
						tprintRaw(string(outputBytes))
					} else if outputRaw {
						displayJSON(payload)
					} else {
						for i := range payload {
							funcDisplay := functionDisplayName(payload[i].FunctionName, payload[i].Index)
							tprint("Function: %s", funcDisplay)
							displayAttributeValue(&payload[i], "")
						}
					}
				case string(api.OutputTypeValidationResultList), string(api.OutputTypeValidationResult):
					var payload api.ValidationResultList
					err := json.Unmarshal(outputBytes, &payload)
					if err != nil {
						// Try parsing as a single result. Shouldn't happen now.
						var single api.ValidationResult
						err := json.Unmarshal(outputBytes, &single)
						// If there's an error print the raw output
						if err != nil {
							tprintRaw(string(outputBytes))
						} else if outputRaw {
							displayJSON(single)
						} else {
							displayValidationResult(&single)
						}
					} else if outputRaw {
						displayJSON(payload)
					} else {
						for i := range payload {
							displayValidationResult(&payload[i])
						}
					}
				case string(api.OutputTypeResourceInfoList):
					var payload api.ResourceInfoList
					err := json.Unmarshal(outputBytes, &payload)
					// If there's an error print the raw output
					if err != nil {
						tprintRaw(string(outputBytes))
					} else if outputRaw {
						displayJSON(payload)
					} else {
						for i := range payload {
							displayResourceInfo(&payload[i])
						}
					}
				case string(api.OutputTypeResourceList):
					var payload api.ResourceList
					err := json.Unmarshal(outputBytes, &payload)
					// If there's an error print the raw output
					if err != nil {
						tprintRaw(string(outputBytes))
					} else if outputRaw {
						displayJSON(payload)
					} else {
						for i := range payload {
							displayResource(&payload[i])
						}
					}
				default:
					// Output should be JSON, but if there's an error print the raw output
					var out bytes.Buffer
					err := json.Indent(&out, outputBytes, "", "  ")
					if err != nil || outputRaw {
						tprintRaw(string(outputBytes))
					} else {
						tprintRaw(out.String())
					}
				}

				if len(respMsg.Outputs) > 1 {
					tprintRaw("\n")
				}
			}
		}
	}
}
