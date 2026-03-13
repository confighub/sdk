// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"log/slog"
	"sync"

	"github.com/swaggest/jsonschema-go"
)

// Pre-generated JSON schemas for common function output types.
// There is just one set of pre-generated schemas for all executors and all handlers.
// Call InitTypeSchemas() to ensure they are initialized before use.
var (
	schemasOnce                sync.Once
	ResourceListSchema         jsonschema.Schema
	ResourceInfoListSchema     jsonschema.Schema
	AttributeValueListSchema   jsonschema.Schema
	ValidationResultListSchema jsonschema.Schema
	YAMLPayloadSchema          jsonschema.Schema
	ResourceMutationListSchema jsonschema.Schema
)

// InitTypeSchemas initializes the shared JSON schemas for common function types.
// It is safe to call multiple times; initialization only happens once.
func InitTypeSchemas() {
	schemasOnce.Do(func() {
		var err error
		reflector := jsonschema.Reflector{}
		ResourceListSchema, err = reflector.Reflect(ResourceList{})
		if err != nil {
			slog.Error("couldn't get schema for api.ResourceList")
		}
		ResourceInfoListSchema, err = reflector.Reflect(ResourceInfoList{})
		if err != nil {
			slog.Error("couldn't get schema for api.ResourceInfoList")
		}
		AttributeValueListSchema, err = reflector.Reflect(AttributeValueList{})
		if err != nil {
			slog.Error("couldn't get schema for api.AttributeValueList")
		}
		ValidationResultListSchema, err = reflector.Reflect(ValidationResultList{})
		if err != nil {
			slog.Error("couldn't get schema for api.ValidationResultList")
		}
		YAMLPayloadSchema, err = reflector.Reflect(YAMLPayload{})
		if err != nil {
			slog.Error("couldn't get schema for api.YAMLPayload")
		}
		ResourceMutationListSchema, err = reflector.Reflect(ResourceMutationList{})
		if err != nil {
			slog.Error("couldn't get schema for api.ResourceMutationList")
		}
	})
}
