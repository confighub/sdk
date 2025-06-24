// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

type APIResponse interface {
	StatusCode() int
}

func IsAPIError(err error, resp APIResponse) bool {
	// TODO: We should also check for nil JSON200 here
	if err != nil || resp.StatusCode() != http.StatusOK {
		return true
	}

	// Check for no ok response
	v := reflect.ValueOf(resp)
	// If it's a pointer, dereference it.
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	json200Field := v.FieldByName("JSON200")
	// This will cause commands to bail and call InterpretErrorGeneric
	if json200Field.IsValid() && json200Field.IsNil() {
		return true
	}
	return false
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
		if strings.HasPrefix(name, "JSON") && !strings.HasSuffix(name, "200") && !field.IsNil() {
			// Should always be a http.Response Code integer
			res := v.MethodByName("StatusCode").Call(nil)
			code := res[0].Int()
			stdErrRes := field.Interface().(*goclientnew.StandardErrorResponse)
			return fmt.Errorf("HTTP %d for req %s: %s", code, requestID, stdErrRes.Message)
		}
	}
	// This should be a nil JSON200 response
	return errors.New("no response body from server for req " + requestID)
}
