// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package managerkit

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/cliutil"
	"github.com/confighub/sdk/core/cubapi"
)

// BindComponentResolver lets --component on q name a Component by slug as well as
// by ID. QueryFlags cannot look a slug up on its own, having no client, so this
// resolves it with the client preflight returns when the command runs.
func BindComponentResolver(cmd *cobra.Command, q *cliutil.QueryFlags, preflight func(context.Context) (*cubapi.Client, error)) {
	q.ResolveComponent = func(slug string) (string, error) {
		ctx := cmd.Context()
		client, err := preflight(ctx)
		if err != nil {
			return "", err
		}
		comp, err := cubapi.ResolveComponent(ctx, client, cubapi.ParseRef(slug), cubapi.ResolveOpts{})
		if err != nil {
			return "", fmt.Errorf("--component %q: %w", slug, err)
		}
		if comp.Component == nil {
			return "", fmt.Errorf("--component %q: no such Component", slug)
		}
		return comp.Component.ComponentID.String(), nil
	}
}
