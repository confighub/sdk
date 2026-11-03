// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package profiles

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/confighub/sdk/core/cubapi"
)

// fleet-edit refuses to run without a selector, before it reaches the server.
func TestFleetEditRequiresSelector(t *testing.T) {
	errSession := errors.New("session checked")
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "no selector", args: []string{"--profile", "p"}, want: "needs a selector"},
		{name: "--where", args: []string{"--profile", "p", "--where", "Slug = 'web'"}, want: errSession.Error()},
		{name: "label shorthand", args: []string{"--profile", "p", "--environment", "prod"}, want: errSession.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := lib
			l.FleetEdit = FleetEdit{WhereData: "kind = 'HorizontalPodAutoscaler'"}
			l.Preflight = func(context.Context) (*cubapi.Client, error) { return nil, errSession }
			cmd := l.FleetEditCommand()
			cmd.SetArgs(tc.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			err := cmd.ExecuteContext(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
