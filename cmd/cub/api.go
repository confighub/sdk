// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var apiArgs struct {
	query       []string
	data        string
	contentType string
}

var apiCmd = &cobra.Command{
	Use:   "api <method> <path>",
	Short: "Send a request to the ConfigHub API",
	Long: getCommandHelp(`Send a request to any ConfigHub API endpoint, with the active context's server and
credentials, and print the response body.

The path is relative to the API root, with or without a leading /api, and may carry
its own query string. Use it for an endpoint or a parameter no other command exposes,
or to see exactly what the server returns.

The command exits non-zero when the response status is 400 or above, after printing
the body, which carries the server's error.`,
		"Authentication required. The path addresses Spaces and other entities by UUID, as the API does."),
	Example: `  # What an upload would change, as the server reports it
  cub api POST upload --query dry_run=true --query include=Mutations --data request.json

  # A Space's Units
  cub api GET "space/$SPACE_ID/unit?select=Slug,UnitID"

  # A request body from stdin
  jq -n '{Slug: "demo"}' | cub api POST space --data -`,
	Args: cobra.ExactArgs(2),
	RunE: apiCmdRun,
}

func init() {
	apiCmd.Flags().StringArrayVarP(&apiArgs.query, "query", "q", nil, "query parameter key=value (repeatable)")
	apiCmd.Flags().StringVarP(&apiArgs.data, "data", "d", "", "request body: a file, or - for stdin")
	apiCmd.Flags().StringVar(&apiArgs.contentType, "content-type", "application/json", "Content-Type of the request body, sent when --data is given")
	rootCmd.AddCommand(apiCmd)
}

func apiCmdRun(cmd *cobra.Command, args []string) error {
	method, path := args[0], args[1]

	query := url.Values{}
	for _, kv := range apiArgs.query {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			return fmt.Errorf("--query must be key=value: %s", kv)
		}
		query.Add(key, value)
	}

	var body io.Reader
	contentType := ""
	switch apiArgs.data {
	case "":
	case "-":
		body = os.Stdin
		contentType = apiArgs.contentType
	default:
		f, err := os.Open(apiArgs.data)
		if err != nil {
			return err
		}
		defer f.Close()
		body = f
		contentType = apiArgs.contentType
	}

	res, err := cubClient.Do(ctx, method, path, query, body, contentType)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	os.Stdout.Write(out)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		fmt.Println()
	}
	if res.StatusCode >= 400 {
		return fmt.Errorf("%s %s: %s", strings.ToUpper(method), path, res.Status)
	}
	return nil
}
