// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/cubutil/blame"
	"github.com/spf13/cobra"
)

var variantBlameArgs struct {
	path         string
	resource     string
	noUpstream   bool
	upstreamMax  int
	showInternal bool
}

var variantBlameCmd = &cobra.Command{
	Use:         "blame <variant>",
	Short:       "Show what set each field of every unit in a variant",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Show, for every field of every unit in a variant, what set the value that is there:
the operation, the revision, who ran it, and when. It is 'cub unit blame' for a whole variant
space, under a header naming each unit, and the columns and flags are the ones described in
'cub unit blame --help'.

A value that arrived from upstream is followed to where it was set. The units of a variant
usually merge from the units of one base, and each upstream unit is read once however many
of the variant's units lead to it.

--where narrows the units, taking the same expressions 'cub unit list' does. --path and
--resource narrow the fields, as on 'cub unit blame'. -o json emits one object per field,
each naming its unit, and -o wide adds the value, the function a link or trigger ran, the
mutation number, and the ChangeOrder.

Copy a REF and its unit into 'cub unit path' for everything recorded about a field.

Examples:
`+"```"+`
  # Every field of every unit in the variant
  cub variant blame apptique/prod

  # Every container image in the variant, and what set it
  cub variant blame apptique/prod --path image

  # Only the deployments, with values
  cub variant blame apptique-prod --where "Slug LIKE 'deployment-%'" -o wide

  # Every field a link set, as JSON
  cub variant blame apptique-prod -o jq='[.[] | select(.Flags | contains("L"))]'
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: variantBlameCmdRun,
}

func init() {
	addStandardDisplayFlags(variantBlameCmd)
	variantBlameCmd.Flags().StringVar(&variantBlameArgs.path, "path", "",
		"only fields whose path contains this substring")
	variantBlameCmd.Flags().StringVar(&variantBlameArgs.resource, "resource", "",
		"only fields of this resource, as <type>/<name> or <name>")
	variantBlameCmd.Flags().BoolVar(&variantBlameArgs.noUpstream, "no-upstream", false,
		"report each unit's own record without following a merge to where the value was set")
	variantBlameCmd.Flags().IntVar(&variantBlameArgs.upstreamMax, "upstream-depth", blame.MaxUpstreamDepth,
		"how many upstream units to follow before giving up")
	variantBlameCmd.Flags().BoolVar(&variantBlameArgs.showInternal, "show-comments", false,
		"include the comment-carrying pseudo-fields ($comment$...) that hold YAML comments")
	enableWhereFlag(variantBlameCmd)
	variantCmd.AddCommand(variantBlameCmd)
}

func variantBlameCmdRun(cmd *cobra.Command, args []string) error {
	space, err := resolveVariantOrSpace(args[0], "SpaceID,Slug")
	if err != nil {
		return err
	}
	resolver := blame.NewResolver(cubClient, blame.Options{
		MaxUpstreamDepth: variantBlameArgs.upstreamMax,
		NoUpstream:       variantBlameArgs.noUpstream,
	})
	fields, err := resolver.Variant(ctx, space.Space, where)
	if err != nil {
		return err
	}
	fields = blame.Filter(fields, variantBlameArgs.path, variantBlameArgs.resource, variantBlameArgs.showInternal)

	if renderPayload(fields) {
		return nil
	}
	displayBlame(fields, true)
	return nil
}
