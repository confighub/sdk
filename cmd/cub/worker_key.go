// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

// `cub worker key` is `cub user key --worker` said the way an operator thinks.
//
// Keys belong to identities, not to workers, and the API and the storage are
// identity-shaped for that reason: a credential proves who you are, and a
// worker's bot user outlives the worker's own model. But nobody sets out to
// manage an identity -- they set out to give a worker a credential, and making
// them find the bot user first is asking them to learn an indirection that
// exists for ConfigHub's benefit rather than theirs.
//
// So this is a naming shell over the same commands, not a second
// implementation: each subcommand fills in the worker and delegates. There is
// one code path, one set of authorization rules, and one place a bug can live.
var workerKeyCmd = &cobra.Command{
	Use:   "key",
	Short: "Manage the keys a worker authenticates with",
	Long: getCommandHelp(`Manage the public keys a worker authenticates with.

The holder of the matching private key authenticates by signing a short-lived
assertion, so ConfigHub never receives or stores a secret. This replaces the
worker secret, which ConfigHub generates, stores, returns through the API, and
receives again on every token refresh.

A key is registered against the worker's bot user, which is the identity the
worker runs as. These commands resolve that for you; "cub user key" is the same
thing addressed by identity instead.

Registering a key hands out an identity: whoever holds the private key
authenticates as that worker from then on. It requires the admin or manager
role.`, ""),
}

var workerKeyAddCmd = &cobra.Command{
	Use:   "add <worker>",
	Short: "Register a public key for a worker",
	Long: getCommandHelp(`Register a public key against a worker's identity.

Either generate a keypair here with --generate, which stores the private key
locally and registers the public half, or register a public key you already have
with --public-key.

--generate takes an alias or a path. A bare name is an alias stored under
~/.confighub/keys, which is what "cub auth login --private-key=<alias>" looks
up; anything containing a path separator is written there instead.

The private key never reaches ConfigHub in either case.

Examples:
`+"```"+`
  # Generate a keypair for a worker and register the public half
  cub worker key add my-worker --generate my-worker

  # Register a public key you already have
  cub worker key add my-worker --public-key ./worker.pub.jwk

  # Record which host holds the private key
  cub worker key add my-worker --generate ci --description "CI runner, us-east"
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: workerKeyAddCmdRun,
}

var workerKeyListCmd = &cobra.Command{
	Use:   "list <worker>",
	Short: "List the keys a worker can authenticate with",
	Long: getCommandHelp(`List the public keys registered against a worker's identity.

Last-Used is what makes a key safe to retire: a key nothing has used is a key
nothing depends on. It is also how an unexpected key gives itself away, by
starting to be used.

Examples:
`+"```"+`
  cub worker key list my-worker
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: workerKeyListCmdRun,
}

var workerKeyDeleteCmd = &cobra.Command{
	Use:   "delete <worker> <kid>",
	Short: "Remove a public key from a worker",
	Long: getCommandHelp(`Remove one registered public key, by its thumbprint.

This is the second half of rotation and the response to a private key you no
longer trust. Whatever holds that private key stops being able to authenticate;
anything else the worker has registered keeps working.

To rotate without an outage, add the new key first, restart whatever uses it,
confirm the new key's Last-Used is moving, and only then delete the old one.

Examples:
`+"```"+`
  cub worker key delete my-worker NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(2),
	RunE: workerKeyDeleteCmdRun,
}

func init() {
	workerKeyAddCmd.Flags().StringVar(&userKeyPublicKey, "public-key", "",
		"file holding the public key as a JWK, or - for stdin")
	workerKeyAddCmd.Flags().StringVar(&userKeyGenerate, "generate", "",
		"generate an Ed25519 keypair, store the private key under this alias (or path), and register the public half")
	workerKeyAddCmd.Flags().StringVar(&userKeyDescription, "description", "",
		"note recording which host or pipeline holds the private key")

	workerKeyCmd.AddCommand(workerKeyAddCmd)
	workerKeyCmd.AddCommand(workerKeyListCmd)
	workerKeyCmd.AddCommand(workerKeyDeleteCmd)
	workerCmd.AddCommand(workerKeyCmd)
}

// selectWorkerKeyTarget points the shared resolver at a worker named
// positionally, so that these commands and "cub user key --worker" arrive at
// the same identity by the same code.
//
// Clearing the user is not defensive: both are package-level flag variables, so
// a --user left set by anything else in the process would make the resolver
// reject the pair as naming one thing two ways.
func selectWorkerKeyTarget(worker string) {
	userKeyWorker = worker
	userKeyUser = ""
}

func workerKeyAddCmdRun(cmd *cobra.Command, args []string) error {
	selectWorkerKeyTarget(args[0])
	return userKeyAddCmdRun(cmd, nil)
}

func workerKeyListCmdRun(cmd *cobra.Command, args []string) error {
	selectWorkerKeyTarget(args[0])
	return userKeyListCmdRun(cmd, nil)
}

func workerKeyDeleteCmdRun(cmd *cobra.Command, args []string) error {
	selectWorkerKeyTarget(args[0])
	return userKeyDeleteCmdRun(cmd, args[1:])
}
