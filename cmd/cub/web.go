// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

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

// webUIServerURL returns the base URL the web UI is served from for the active
// context.
func webUIServerURL() string {
	return contextUIURL(contextManager.ActiveContext())
}

// contextUIURL is the base URL ctx's web UI is served from: the one its server
// advertised at the last login, else the server URL, which is right wherever the
// UI is served on the server's own host.
func contextUIURL(ctx *Context) string {
	if ctx.Settings.UIURL != "" {
		return ctx.Settings.UIURL
	}
	return ctx.Coordinate.ServerURL
}

// noteWebUILocation records on the active context where its server says the web
// UI is, so that the commands that open a browser go there. When the server does
// not say and its own address serves no UI, it says so, unless quiet: those
// commands would otherwise open a dead page, and the fix is on the server
// (CONFIGHUB_UI_URL). A login never fails over any of this.
func noteWebUILocation(quiet bool) {
	ctx := contextManager.ActiveContext()
	apiInfo, err := getApiInfo(ctx.Coordinate)
	if err != nil {
		return
	}
	advertised := strings.TrimRight(apiInfo.UIURL, "/")
	if advertised != ctx.Settings.UIURL {
		ctx.Settings.UIURL = advertised
		if err := contextManager.SaveConfig(); err != nil {
			return
		}
	}
	if quiet || advertised != "" || servesWebUI(ctx.Coordinate.ServerURL) {
		return
	}
	tprint("Note: %s does not serve the web UI and does not say where it is, so the commands that open a browser have nowhere to go.", ctx.Coordinate.ServerURL)
}

// servesWebUI reports whether a web UI answers at serverURL. A UI answers
// /config.json, with its runtime configuration or, lacking one, with the app; the
// API server alone answers it 404. When that cannot be determined the answer is
// true, so that a network hiccup does not produce advice.
func servesWebUI(serverURL string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(strings.TrimRight(serverURL, "/") + "/config.json")
	if err != nil {
		return true
	}
	defer resp.Body.Close()
	return resp.StatusCode != http.StatusNotFound
}
