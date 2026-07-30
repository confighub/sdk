// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	"github.com/skratchdot/open-golang/open"
	"github.com/spf13/cobra"
)

// printURL suppresses the browser launch and writes the destination URL to
// stdout instead. Needed wherever there is no browser to launch — SSH sessions,
// containers, CI — and for piping a link somewhere else.
var printURL = false

// enableOpenFlags registers the flags common to every "cub <noun> open" command.
func enableOpenFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&printURL, "print-url", false, "Print the URL instead of opening a browser")
}

// openWebUI launches the user's browser on url, or prints it when --print-url is
// set. The URL is printed on stdout so it can be piped; the confirmation for the
// browser case goes through tprint like other status output.
//
// Every URL is stamped with the active context's organization here rather than in
// the individual cubapi.Get*URL builders: this is the one path all of them reach
// the browser through, so a new open command cannot forget it.
func openWebUI(url string) error {
	url = cubapi.WithOrganization(url, contextManager.ActiveContext().Coordinate.OrganizationID)
	if printURL {
		tprintRaw(url)
		return nil
	}
	if err := open.Start(url); err != nil {
		return fmt.Errorf("failed to open browser: %w", err)
	}
	tprint("Opened in web UI: %s", url)
	return nil
}

// webUIServerURL returns the base URL the web UI is served from, which is the
// same origin as the API server in the active context.
func webUIServerURL() string {
	return contextManager.ActiveContext().Coordinate.ServerURL
}
