// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

type APIResponse interface {
	StatusCode() int
}

func IsAPIError(err error, resp APIResponse) bool {
	if err != nil || resp == nil {
		return true
	}

	// TODO: We should also check for nil JSON200 here
	statusCode := resp.StatusCode()
	if statusCode != http.StatusOK && statusCode != http.StatusMultiStatus {
		return true
	}

	// Check for no ok response
	v := reflect.ValueOf(resp)
	// If it's a pointer, dereference it.
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	json200Field := v.FieldByName("JSON200")
	json207Field := v.FieldByName("JSON207")
	// This will cause commands to bail and call InterpretErrorGeneric
	// We're successful if we have either a 200 or 207 response
	hasValidResponse := false
	if json200Field.IsValid() && !json200Field.IsNil() {
		hasValidResponse = true
	}
	if json207Field.IsValid() && !json207Field.IsNil() {
		hasValidResponse = true
	}
	if !hasValidResponse {
		return true
	}
	return false
}

// IsNotFoundError returns true if the error represents a 404 Not Found response
// or a "not found" error from a slug lookup.
func IsNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "HTTP 404") || strings.Contains(msg, "not found")
}

// ErrorType is the Type of the error an API response holds: the error's category as a code, such
// as Conflict or AlreadyExists, which tells apart errors that share an HTTP status. It is empty
// when the response holds no error, or the server did not say.
func ErrorType(resp APIResponse) string {
	v := reflect.ValueOf(resp)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	// The body is in the field for the status, or in JSONDefault for a status the operation
	// does not declare.
	for _, name := range []string{fmt.Sprintf("JSON%d", resp.StatusCode()), "JSONDefault"} {
		field := v.FieldByName(name)
		if !field.IsValid() || field.Kind() != reflect.Ptr || field.IsNil() {
			continue
		}
		if responseError := errorResponseError(field.Interface()); responseError != nil {
			return responseError.Type
		}
	}
	return ""
}

// errorResponseError is the error an error response body carries as data: the standard body's
// Error, or that of an operation's own response type. Nil if it has none.
func errorResponseError(body any) *goclientnew.ResponseError {
	if standard, ok := body.(*goclientnew.StandardErrorResponse); ok {
		return standard.Error
	}
	v := reflect.ValueOf(body)
	if v.Kind() != reflect.Ptr || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return nil
	}
	if field := v.Elem().FieldByName("Error"); field.IsValid() {
		if responseError, ok := field.Interface().(*goclientnew.ResponseError); ok {
			return responseError
		}
	}
	return nil
}

// errorResponseMessage reads the message of an error response: a StandardErrorResponse, or an
// operation's own response type carrying an Error, as a write response does to report a failure in
// the shape of its success, or a Message.
func errorResponseMessage(body any) (string, bool) {
	if standard, ok := body.(*goclientnew.StandardErrorResponse); ok {
		return standard.Message, true
	}
	if responseError := errorResponseError(body); responseError != nil {
		return responseError.Message, true
	}
	v := reflect.ValueOf(body)
	if v.Kind() != reflect.Ptr || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return "", false
	}
	if message := v.Elem().FieldByName("Message"); message.IsValid() && message.Kind() == reflect.String {
		return message.String(), true
	}
	return "", false
}

// InterpretErrorGeneric checks the response for any errors and returns an error if found.
// If we found no non-nil JSON4xx or JSON5xx, presumably it is a 2xx success or client initiated termination.
func InterpretErrorGeneric(err error, resp interface{}) error {
	if err != nil {
		return err
	}
	if resp == nil {
		return errors.New("no response data")
	}

	v := reflect.ValueOf(resp)
	// If it's a pointer, dereference it.
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	// Look for the HTTPResponse to get the request ID
	requestID := ""
	httpResponseField := v.FieldByName("HTTPResponse")
	if httpResponseField.IsValid() && !httpResponseField.IsNil() {
		httpResponseValue := httpResponseField.Interface()
		httpResponse, ok := httpResponseValue.(*http.Response)
		if ok {
			requestID = httpResponse.Header.Get("X-Request-Id")
		}
	}

	// Iterate over fields, looking for JSON<StatusCode> that isn't 200.
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := v.Type().Field(i)
		name := fieldType.Name

		// For a JSONxxx field, check if it's non-nil
		// Skip JSON200 and JSON207 as they are success responses
		if strings.HasPrefix(name, "JSON") && !strings.HasSuffix(name, "200") && !strings.HasSuffix(name, "207") && !strings.HasSuffix(name, "Default") && !field.IsNil() {
			// Should always be a http.Response Code integer
			res := v.MethodByName("StatusCode").Call(nil)
			code := res[0].Int()
			message, ok := errorResponseMessage(field.Interface())
			if !ok {
				return fmt.Errorf("unexpected response type for %s status %d req %s: %v", name, code, requestID, field.Type())
			}
			if code == 401 {
				return fmt.Errorf("authentication problem. Try logging in (again).\nDetailed message: %s. ", message)
			}
			return fmt.Errorf("HTTP %d for req %s: %s", code, requestID, message)
		}
	}
	// This should be a nil JSON200 response
	return errors.New("no response body from server for req " + requestID)
}
