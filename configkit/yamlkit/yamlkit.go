// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// yamlkit is a package for parsing, traversing, and updating lists of configuration elements,
// called resources, represented as yaml doc lists.
package yamlkit

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.
