// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigMapHashAnnotation(t *testing.T) {
	assert.Equal(t, "hash.confighub.com/app-config", ConfigMapHashAnnotation("app-config"))

	fits := strings.Repeat("a", 63)
	assert.Equal(t, ConfigMapHashAnnotationPrefix+fits, ConfigMapHashAnnotation(fits))

	long := strings.Repeat("a", 60) + "-" + strings.Repeat("b", 40)
	other := strings.Repeat("a", 60) + "-" + strings.Repeat("c", 40)
	key := ConfigMapHashAnnotation(long)
	name := strings.TrimPrefix(key, ConfigMapHashAnnotationPrefix)
	assert.LessOrEqual(t, len(name), 63, "the name part of an annotation key is at most 63 characters")
	assert.Regexp(t, `^a+-[0-9a-f]{10}$`, name, "cut short, ending in an alphanumeric, and followed by a hash")
	assert.NotEqual(t, key, ConfigMapHashAnnotation(other), "two long names sharing a beginning get distinct keys")
}
