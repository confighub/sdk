// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// This enables use of the ConfigHub frontdoor (main) API by the worker.

type WorkerFrontdoorClient struct {
	serverURL    string
	serverPort   string
	workerID     string
	workerSecret string
	client       *goclientnew.ClientWithResponses
	authSession  *cubapi.AuthSession
	mu           sync.RWMutex
	stopRefresh  chan struct{}
}

// NewWorkerFrontdoorClient creates a new WorkerFrontdoorClient
func NewWorkerFrontdoorClient(serverURL, serverPort, workerID, workerSecret string) *WorkerFrontdoorClient {
	worker := &WorkerFrontdoorClient{
		serverURL:    serverURL,
		serverPort:   serverPort,
		workerID:     workerID,
		workerSecret: workerSecret,
		stopRefresh:  make(chan struct{}),
	}

	// Initialize the client with authentication
	if err := worker.refreshAuth(); err != nil {
		log.Log.Error(err, "Failed to initialize authentication for ConfigHub bridge worker")
	}

	// Start token refresh goroutine
	go worker.tokenRefreshLoop()

	return worker
}

// refreshAuth exchanges worker credentials for a JWT token and updates the client
func (w *WorkerFrontdoorClient) refreshAuth() error {
	// Get JWT token using worker credentials
	url := w.serverURL
	if w.serverPort != "" {
		url += ":" + w.serverPort
	}

	log.Log.Info("Attempting ConfigHub bridge worker authentication",
		"url", url,
		"workerID", w.workerID,
		"hasSecret", w.workerSecret != "")

	authSession, err := cubapi.PerformWorkerAuth(url, w.workerID, w.workerSecret)
	if err != nil {
		wrappedErr := errors.WithStack(err)
		log.Log.Error(wrappedErr, "Worker authentication failed",
			"url", url,
			"workerID", w.workerID)
		return errors.Wrap(err, "failed to authenticate worker")
	}

	// Create a new client with the JWT token
	client, err := goclientnew.NewClientWithResponses(url+"/api", func(c *goclientnew.Client) error {
		c.RequestEditors = append(c.RequestEditors, func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", authSession.AccessToken))
			return nil
		})
		return nil
	})
	if err != nil {
		return errors.Wrap(err, "failed to create authenticated client")
	}

	// Update the client and auth session under lock
	w.mu.Lock()
	w.client = client
	w.authSession = authSession
	w.mu.Unlock()

	log.Log.Info("Successfully refreshed ConfigHub bridge worker authentication")
	return nil
}

// tokenRefreshLoop refreshes the token every 23 hours
func (w *WorkerFrontdoorClient) tokenRefreshLoop() {
	ticker := time.NewTicker(23 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := w.refreshAuth(); err != nil {
				log.Log.Error(err, "Failed to refresh authentication token")
			}
		case <-w.stopRefresh:
			return
		}
	}
}

// getClient returns the authenticated client (thread-safe)
func (w *WorkerFrontdoorClient) GetClient() *goclientnew.ClientWithResponses {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.client
}

func (w *WorkerFrontdoorClient) Close() error {
	// Stop the token refresh goroutine
	close(w.stopRefresh)
	return nil
}
