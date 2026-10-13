// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"regexp"
	"strings"

	"github.com/confighub/sdk/core/function/api"
)

// schemaVersionSegment matches a version in an application configuration schema: v and a number,
// optionally followed by a lowercase stage and a number -- v1, v2alpha3, v1beta2, v1a1, v2b3,
// v1rc2. A segment that only begins like one, such as v2app, names something else.
var schemaVersionSegment = regexp.MustCompile(`^v[0-9]+([a-z]+[0-9]+)?$`)

// RemoveSchemaVersionSegments removes the /-separated segments of an application configuration
// schema that are versions, wherever they are: registrybot.confighub.com/v1alpha3 becomes
// registrybot.confighub.com, and example.com/v1/Settings becomes example.com/Settings. The
// application configuration providers implement RemoveAPIVersionFromResourceType with it.
//
// A schema has no structure that says which segment is the version, as a Kubernetes apiVersion
// does, so a version is recognized by its form. Schemas follow the Kubernetes convention of
// putting the version after what is versioned.
//
// A schema that is nothing but versions is returned as it is, so that a type never reduces to
// the empty string.
func RemoveSchemaVersionSegments(resourceType api.ResourceType) api.ResourceType {
	if !strings.Contains(string(resourceType), "/") {
		return resourceType
	}
	segments := strings.Split(string(resourceType), "/")
	kept := segments[:0:0]
	for _, segment := range segments {
		if !schemaVersionSegment.MatchString(segment) {
			kept = append(kept, segment)
		}
	}
	if len(kept) == 0 {
		return resourceType
	}
	return api.ResourceType(strings.Join(kept, "/"))
}
