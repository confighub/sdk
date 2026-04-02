// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Unmarshal well known output types.
func UnmarshalOutput(outputBytes []byte, outputType OutputType) (any, error) {
	switch outputType {
	case OutputTypeValidationResult, OutputTypeValidationResultList:
		var output ValidationResultList
		err := json.Unmarshal(outputBytes, &output)
		if err != nil {
			// Fallback to ValidationResult
			var output ValidationResult
			err := json.Unmarshal(outputBytes, &output)
			return output, err
		}
		return output, err
	case OutputTypeAttributeValueList:
		var output AttributeValueList
		err := json.Unmarshal(outputBytes, &output)
		return output, err
	case OutputTypeResourceInfoList:
		var output ResourceInfoList
		err := json.Unmarshal(outputBytes, &output)
		return output, err
	case OutputTypeResourceList:
		var output ResourceList
		err := json.Unmarshal(outputBytes, &output)
		return output, err
	case OutputTypeYAML:
		var output YAMLPayload
		err := json.Unmarshal(outputBytes, &output)
		return output, err
	default:
		var output any
		err := json.Unmarshal(outputBytes, &output)
		return output, err
	}
}

// Merge outputs from multiple function calls as a map by output type, combining lists of the same well known type.
func CombineOutputs(
	functionName string,
	instance string,
	newOutputType OutputType,
	outputs map[OutputType]any,
	newOutput any,
	functionInvocationIndex int,
	messages []string,
) (map[OutputType]any, []string) {
	if outputs == nil {
		outputs = make(map[OutputType]any)
	}

	// Get or initialize the output for this type
	output, exists := outputs[newOutputType]
	if !exists {
		// Initialize the output based on type
		switch newOutputType {
		case OutputTypeValidationResult, OutputTypeValidationResultList:
			output = ValidationResultList{}
		case OutputTypeAttributeValueList:
			output = AttributeValueList{}
		case OutputTypeResourceInfoList:
			output = ResourceInfoList{}
		case OutputTypeResourceList:
			output = ResourceList{}
		case OutputTypeYAML:
			output = YAMLPayload{}
		default:
			// includes OutputTypeJSON, etc.
			outputs[newOutputType] = newOutput
			return outputs, messages
		}
	}

	// Combine outputs of the same type
	switch newOutputType {
	case OutputTypeValidationResult:
		// Should be a ValidationResultList in actuality
		newResult, newExpectedType := newOutput.(ValidationResultList)
		if !newExpectedType {
			// Fall back to ValidationResult
			newResult, newExpectedType := newOutput.(ValidationResult)
			if !newExpectedType {
				messages = append(messages, "couldn't convert new result to ValidationResultList or ValidationResult")
				return outputs, messages
			}
			previousResults, previousExpectedType := output.(ValidationResultList)
			if !previousExpectedType {
				messages = append(messages, "couldn't convert previous result to ValidationResultList")
				return outputs, messages
			}
			newResult.Index = functionInvocationIndex
			newResult.FunctionName = functionName
			previousResults = append(previousResults, newResult)
			output = previousResults
		} else {
			previousResults, previousExpectedType := output.(ValidationResultList)
			if !previousExpectedType {
				messages = append(messages, "couldn't convert previous result to ValidationResultList")
				return outputs, messages
			}
			for i := range newResult {
				newResult[i].Index = functionInvocationIndex
				newResult[i].FunctionName = functionName
			}
			previousResults = append(previousResults, newResult...)
			output = previousResults
		}

	case OutputTypeValidationResultList:
		newResult, newExpectedType := newOutput.(ValidationResultList)
		if !newExpectedType {
			messages = append(messages, "couldn't convert new result to ValidationResult")
			return outputs, messages
		}
		for i := range newResult {
			newResult[i].Index = functionInvocationIndex
			newResult[i].FunctionName = functionName
		}
		previousResults, previousExpectedType := output.(ValidationResultList)
		if !previousExpectedType {
			messages = append(messages, "couldn't convert previous result to ValidationResultList")
			return outputs, messages
		}
		previousResults = append(previousResults, newResult...)
		output = previousResults

	case OutputTypeAttributeValueList:
		previousOutput, previousExpectedType := output.(AttributeValueList)
		if !previousExpectedType {
			messages = append(messages, "couldn't convert previous result to AttributeValueList")
			return outputs, messages
		}
		newOutput, newExpectedType := newOutput.(AttributeValueList)
		if !newExpectedType {
			messages = append(messages, "couldn't convert new result to AttributeValueList")
			return outputs, messages
		}
		for i := range newOutput {
			newOutput[i].Index = functionInvocationIndex
			newOutput[i].FunctionName = functionName
		}
		previousOutput = append(previousOutput, newOutput...)
		output = previousOutput

	case OutputTypeResourceInfoList:
		previousOutput, previousExpectedType := output.(ResourceInfoList)
		if !previousExpectedType {
			messages = append(messages, "couldn't convert previous result to ResourceInfoList")
			return outputs, messages
		}
		newOutput, newExpectedType := newOutput.(ResourceInfoList)
		if !newExpectedType {
			messages = append(messages, "couldn't convert new result to ResourceInfoList")
			return outputs, messages
		}
		previousOutput = append(previousOutput, newOutput...)
		output = previousOutput

	case OutputTypeResourceList:
		previousOutput, previousExpectedType := output.(ResourceList)
		if !previousExpectedType {
			messages = append(messages, "couldn't convert previous result to ResourceList")
			return outputs, messages
		}
		newOutput, newExpectedType := newOutput.(ResourceList)
		if !newExpectedType {
			messages = append(messages, "couldn't convert new result to ResourceList")
			return outputs, messages
		}
		previousOutput = append(previousOutput, newOutput...)
		output = previousOutput

	case OutputTypeYAML:
		previousOutput, previousExpectedType := output.(YAMLPayload)
		if !previousExpectedType {
			messages = append(messages, "couldn't convert previous result to YAMLPayload")
			return outputs, messages
		}
		newOutput, newExpectedType := newOutput.(YAMLPayload)
		if !newExpectedType {
			messages = append(messages, "couldn't convert new result to YAMLPayload")
			return outputs, messages
		}
		// Concatenate YAML documents
		if len(newOutput.Payload) > 0 {
			if len(previousOutput.Payload) == 0 {
				previousOutput.Payload = newOutput.Payload
			} else {
				payloads := []string{previousOutput.Payload, newOutput.Payload}
				previousOutput.Payload = strings.Join(payloads, "\n---\n")
			}
		}
		output = previousOutput

	default:
		messages = append(messages, fmt.Sprintf("functions with unmergeable output types; output of %s discarded", instance))
	}

	// Store the combined output back in the map
	outputs[newOutputType] = output
	return outputs, messages
}
