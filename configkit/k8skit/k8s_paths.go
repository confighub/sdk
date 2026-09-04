// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

// ContainersPaths lists the relative paths under a PodSpec where containers
// can be found.
var ContainersPaths = []string{"containers", "initContainers", "ephemeralContainers"}
