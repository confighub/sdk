// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var reviewCommentCmd = &cobra.Command{
	Use:   "review-comment",
	Short: "Review comment commands",
	Long: getCommandHelp(`The review-comment subcommands are used to manage review comments.

A review comment is a remark made in review of a revision of a unit, optionally about one resource
or one path in it, and optionally in reply to another review comment on the same unit. It is review
discussion, not an explanation of the configuration, which belongs in a configuration comment.

A review comment has no slug. It is addressed by its unit and its ID.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(reviewCommentCmd)
	rootCmd.AddCommand(reviewCommentCmd)
	addExplainCmd(reviewCommentCmd, "ReviewComment")
}
