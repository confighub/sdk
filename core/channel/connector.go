// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/signal"
	"strings"
	"syscall"

	"github.com/confighub/sdk/core/cubapi"
)

// Connector serves a function executor on a Channel. It is the channel counterpart of the
// worker package's Connector, and takes the same executor: build the executor, register the
// functions, and Start. The functions are declared on the Channel when it starts, so the bot
// has nothing to declare out of band.
type Connector struct {
	opts ConnectorOptions
}

// ConnectorOptions configure a Connector. FunctionExecutor is required, and Channel is required
// to serve.
type ConnectorOptions struct {
	// Channel names the Channel to serve, as space/slug or its ID.
	Channel string
	// ConfigHubURL and PrivateKey are the server and the service account's private key (its
	// JWK), as a bot deployment carries them in CONFIGHUB_URL and CONFIGHUB_AUTH_PRIVATE_KEY.
	// The key signs a short-lived assertion for each session, renewed for as long as the
	// process runs. Without a key, the client is the one cubapi.ResolveClient builds: CUB_SERVER
	// and CUB_TOKEN, or the active cub context, which is enough for development.
	ConfigHubURL string
	PrivateKey   string
	// FunctionExecutor runs the functions; the SDK's executors satisfy it.
	FunctionExecutor FunctionExecutor
	// Concurrency is how many runs execute at once. Zero means one, which keeps a Unit's
	// functions in order, as a function worker ran them.
	Concurrency int
	// UserAgent names the bot in the server's logs. Empty means "function-bot".
	UserAgent string
}

// NewConnector checks the options and returns a Connector; Start runs it.
func NewConnector(opts ConnectorOptions) (*Connector, error) {
	if opts.FunctionExecutor == nil {
		return nil, errors.New("channel connector: FunctionExecutor is required")
	}
	if opts.PrivateKey != "" && opts.ConfigHubURL == "" {
		return nil, errors.New("channel connector: ConfigHubURL is required with PrivateKey")
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "function-bot"
	}
	return &Connector{opts: opts}, nil
}

// Start serves the Channel until SIGINT or SIGTERM, which stop it cleanly: in-flight runs
// finish and the lease is released. Any other end is an error.
func (c *Connector) Start() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return c.Run(ctx)
}

// PrintCommands writes the commands the Connector declares when it starts, as the JSON object
// cub channel create and cub channel update take with --commands. It is for declaring them out
// of band, so the bot's identity needs only Claim: a bot that finds its declaration current
// writes nothing to the Channel.
func (c *Connector) PrintCommands(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(FunctionCommands(c.opts.FunctionExecutor.RegisteredFunctions()))
}

// Run serves the Channel until ctx is done, which is a clean stop.
func (c *Connector) Run(ctx context.Context) error {
	if c.opts.Channel == "" {
		return errors.New("channel connector: Channel is required (space/slug or ID)")
	}
	client, err := c.client(ctx)
	if err != nil {
		return err
	}
	listener := NewListener(client, cubapi.ParseRef(c.opts.Channel), Options{Concurrency: c.opts.Concurrency}).
		HandleFunctions(c.opts.FunctionExecutor)
	if err := listener.Run(ctx); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

func (c *Connector) client(ctx context.Context) (*cubapi.Client, error) {
	if c.opts.PrivateKey == "" {
		return cubapi.ResolveClient(ctx, cubapi.ClientOptions{UserAgent: c.opts.UserAgent})
	}
	server := strings.TrimRight(c.opts.ConfigHubURL, "/")
	signer, err := cubapi.NewAssertionSigner([]byte(c.opts.PrivateKey), "", "")
	if err != nil {
		return nil, fmt.Errorf("channel connector: private key: %w", err)
	}
	authenticate := cubapi.AssertionAuthenticator(server, signer)
	// Authenticate once here, so a bad key fails the start rather than the first poll.
	session, err := authenticate(ctx)
	if err != nil {
		return nil, fmt.Errorf("channel connector: authenticating to %s with key %s: %w", server, signer.Kid(), err)
	}
	return cubapi.NewClient(cubapi.ClientOptions{
		ServerURL:     server,
		Session:       session,
		Authenticator: authenticate,
		UserAgent:     c.opts.UserAgent,
	})
}
