// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/confighub/sdk/core/function/executor"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/shirou/gopsutil/v3/mem"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/net/http2"
)

const (
	eventsRoute        = "%s/api/bridge_worker/%s/stream"
	resultRoute        = "%s/api/bridge_worker/%s/action_result"
	workerSelfGetRoute = "%s/api/bridge_worker/%s/me"
)

const (
	ErrorTypeUnmarshalWorkerEvent   = "failed_to_unmarshal_worker_event"
	ErrorTypeUnmarshalBridgeEvent   = "failed_to_unmarshal_bridge_event"
	ErrorTypeUnmarshalFunctionEvent = "failed_to_unmarshal_function_event"
	ErrorTypeMemoryStatsFailure     = "failed_to_fetch_memory_stats"
	ErrorTypeHeartbeatResponse      = "failed_to_response_to_heartbeat"
	ErrorTypeBridgeCommand          = "failed_to_execute_bridge_command"
	ErrorTypeFunctionCommand        = "failed_to_execute_function_command"
)

type workerClient struct {
	serverURL      string
	workerID       string
	workerSecret   string
	workerSlug     string
	client         *http.Client
	done           chan struct{}
	bridgeWorker     api.BridgeWorker
	functionExecutor executor.FunctionExecutor
	watcherManager *WatcherManager // Manages watcher lifecycle and execution
	unitQueues     *UnitQueueManager
	operationsWg   sync.WaitGroup // Track in-flight operations (Apply/Destroy/etc)
	metricsMeter   metric.Meter
	eventCounter   metric.Int64Counter
	actionCounter  metric.Int64Counter
	errorCounter   metric.Int64Counter
}

func newClient(
	serverURL, workerID, workerSecret string,
	bridgeWorker api.BridgeWorker, functionExecutor executor.FunctionExecutor,
	metricsMeter metric.Meter,
) *workerClient {
	// Improved: Parse URL and select transport based on scheme
	parsedURL, err := url.Parse(serverURL)
	if err != nil {
		panic("invalid serverURL: " + err.Error())
	}

	var transport *http2.Transport
	// Shared performance options
	perfOpts := func(t *http2.Transport) {
		t.StrictMaxConcurrentStreams = true
		t.MaxReadFrameSize = 1 << 20 // 1MB
		t.ReadIdleTimeout = 2 * time.Minute
		t.PingTimeout = 20 * time.Second
		t.IdleConnTimeout = 90 * time.Second
		t.DisableCompression = true
	}

	switch parsedURL.Scheme {
	case "https":
		transport = &http2.Transport{
			TLSClientConfig: &tls.Config{
				NextProtos: []string{"h2"},
			},
		}
		perfOpts(transport)
	case "http":
		transport = &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (netConn net.Conn, err error) {
				return net.Dial(network, addr)
			},
		}
		perfOpts(transport)
	default:
		panic("unsupported URL scheme: " + parsedURL.Scheme)
	}

	client := &workerClient{
		serverURL:      serverURL,
		workerID:       workerID,
		workerSecret:   workerSecret,
		client:         &http.Client{Transport: transport},
		done:           make(chan struct{}),
		bridgeWorker:   bridgeWorker,
		functionExecutor: functionExecutor,
		watcherManager: NewWatcherManager(10, 50), // 10 workers, queue size 50
		unitQueues:     NewUnitQueueManager(),
		metricsMeter:   metricsMeter,
	}

	// Fetch /api/info to get the WorkerPort and update serverURL
	if err := client.fetchAndApplyWorkerPort(); err != nil {
		log.Printf("[WARN] Failed to fetch worker port from /api/info: %v. Using provided URL as-is: %s", err, serverURL)
	}

	if err := client.initializeEventCounters(); err != nil {
		log.Printf("[WARN] Failed to initialize metric counters: %v", err)
	}

	return client
}

func (c *workerClient) initializeEventCounters() (err error) {
	c.eventCounter, err = c.metricsMeter.Int64Counter("worker.events")
	if err != nil {
		return err
	}

	c.actionCounter, err = c.metricsMeter.Int64Counter("worker.actions")
	if err != nil {
		return err
	}

	c.errorCounter, err = c.metricsMeter.Int64Counter("worker.errors")
	return err
}

// fetchAndApplyWorkerPort fetches /api/info from the server and updates serverURL with the WorkerPort
func (c *workerClient) fetchAndApplyWorkerPort() error {
	parsedURL, err := url.Parse(c.serverURL)
	if err != nil {
		return fmt.Errorf("invalid server URL: %w", err)
	}

	// Fetch /api/info from the CONFIGHUB_URL to get the correct WorkerPort
	// CONFIGHUB_URL should be the main server URL (e.g., http://localhost:9090)
	confighubURL := os.Getenv("CONFIGHUB_URL")
	if confighubURL == "" {
		// Fallback to using serverURL if CONFIGHUB_URL not set
		confighubURL = c.serverURL
	}

	log.Printf("[DEBUG] Fetching /api/info from: %s/api/info", confighubURL)

	// Create an unauthenticated API client to fetch /api/info (public endpoint)
	// Use the generated goclient-new instead of manual HTTP requests
	apiClient, err := goclientnew.NewClientWithResponses(confighubURL + "/api")
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use the generated client to fetch /api/info
	resp, err := apiClient.ApiInfoWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch /api/info: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("server returned status %d", resp.StatusCode())
	}

	if resp.JSON200 == nil {
		return fmt.Errorf("unexpected response format from /api/info")
	}

	if resp.JSON200.WorkerPort == "" {
		return fmt.Errorf("WorkerPort not specified in /api/info response")
	}

	// Update serverURL to use the WorkerPort
	parsedURL.Host = parsedURL.Hostname() + ":" + resp.JSON200.WorkerPort
	newServerURL := parsedURL.String()

	log.Printf("[INFO] Fetched worker port from /api/info: %s. Updated connection URL from %s to %s", resp.JSON200.WorkerPort, c.serverURL, newServerURL)
	c.serverURL = newServerURL

	return nil
}

// Start initializes the worker client and establishes a persistent connection to the ConfigHub server.
// This method implements automatic reconnection with exponential backoff and graceful shutdown.
//
// The function performs the following operations:
// 1. Retrieves the bridge worker's unique slug identifier from the server
// 2. Starts the unit queue manager for handling work units
// 3. Enters an infinite reconnection loop that:
//   - Attempts to establish and maintain a streaming connection
//   - Implements exponential backoff (1s -> 60s max) with randomization to prevent thundering herd
//   - Adds up to 10% jitter to backoff delays
//   - Resets backoff timer after successful long-lived connections
//   - Respects context cancellation for graceful shutdown
//
// Backend crash/failure handling:
// When the ConfigHub backend crashes or becomes unavailable:
//   - The streaming connection (startStream) will fail with a network/connection error
//   - The error is logged and the reconnection loop immediately triggers
//   - Exponential backoff delays reconnection attempts: 1s -> 2s -> 4s -> 8s -> ... -> 60s (max)
//   - Each reconnection attempt includes ±50% randomization plus 10% jitter to prevent all workers
//     from hammering the recovering server simultaneously (thundering herd prevention)
//   - The worker will continue retrying indefinitely until either:
//     a) The backend recovers and accepts the connection, OR
//     b) The worker receives a shutdown signal (context cancellation)
//   - In-flight operations (Apply/Destroy/etc) use context.WithoutCancel to complete gracefully
//     even during reconnection
//   - Operation results are sent via sendResult() with its own retry mechanism (up to 5 minutes)
//     to ensure status updates reach the server even during backend restarts, maintaining data consistency
//
// The reconnection loop will continue indefinitely until the context is cancelled.
// On context cancellation, it ensures the unit queue manager is properly stopped.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//
// Returns:
//   - error: Returns ctx.Err() when context is cancelled, or initial setup errors
func (c *workerClient) Start(ctx context.Context) error {
	// Retrieve the unique slug identifier for this bridge worker from the server.
	// This slug is used to identify this worker instance in subsequent API calls.
	err := c.getBridgeWorkerSlug()
	if err != nil {
		log.Printf("[ERROR] Failed to get bridge worker slug: %v", err)
		return fmt.Errorf("failed to get bridge worker slug: %v", err)
	}

	// Start the unit queue manager which handles concurrent processing of work units.
	// This manager maintains per-unit queues to ensure sequential processing of units.
	c.unitQueues.Start(ctx)

	// Ensure the unit queue manager is properly stopped when this function exits.
	// This guarantees cleanup even if the context is cancelled or an error occurs.
	defer c.unitQueues.Stop()

	// Stop all watchers and wait for them to finish on shutdown
	defer func() {
		log.Printf("🛑 Shutting down: stopping all active watchers")
		c.watcherManager.StopAndWait()
	}()

	// Configure exponential backoff for connection retry logic.
	// The backoff parameters control how aggressively we retry failed connections:
	eb := backoff.NewExponentialBackOff()
	eb.InitialInterval = 1 * time.Second // Start with 1 second delay
	eb.RandomizationFactor = 0.5         // Add ±50% randomization to prevent thundering herd
	eb.Multiplier = 2.0                  // Double the delay on each retry
	eb.MaxInterval = 60 * time.Second    // Cap maximum delay at 60 seconds
	// Note: In backoff/v5, ExponentialBackOff.NextBackOff() never returns backoff.Stop,
	// so retries continue forever, capped at MaxInterval.

	attempt := 0
	for {
		attempt++
		log.Printf("[INFO] Connection attempt #%d to ConfigHub server", attempt)

		// Attempt to establish and maintain a streaming connection to the server.
		// This call blocks until the stream ends (normally or due to error).
		err := c.startStream(ctx)

		if err == nil {
			log.Printf("[INFO] Stream ended normally, will reconnect")
		} else if errors.Is(err, context.Canceled) {
			log.Printf("[INFO] Stream stopped: %v", err)
		} else {
			log.Printf("[ERROR] Stream error: %v", err)
		}

		// Check if the context has been cancelled (e.g., shutdown signal).
		// If so, exit the reconnection loop immediately.
		select {
		case <-ctx.Done():
			log.Printf("[INFO] Context cancelled, stopping reconnection attempts")
			return ctx.Err()
		default:
		}

		// Calculate the next backoff delay using exponential backoff.
		delay := eb.NextBackOff()

		// Add random jitter (up to 10% of delay) to prevent multiple workers
		// from reconnecting simultaneously (thundering herd problem).
		jitter := time.Duration(rand.Float64() * float64(delay) * 0.1)
		totalDelay := delay + jitter

		log.Printf("[INFO] Waiting %.1f seconds before reconnection attempt #%d", totalDelay.Seconds(), attempt+1)

		// Wait for the backoff delay, but allow interruption if context is cancelled.
		// This ensures we can shut down gracefully even during the wait period.
		select {
		case <-time.After(totalDelay):
			// Delay completed, continue to next iteration
		case <-ctx.Done():
			log.Printf("[INFO] Context cancelled during backoff, stopping reconnection attempts")
			return ctx.Err()
		}

		// If the stream ended normally (no error) and we've had previous attempts,
		// it indicates a successful long-lived connection. Reset the backoff strategy
		// so the next reconnection (if needed) starts with minimal delay.
		if err == nil && attempt > 1 {
			log.Printf("[INFO] Resetting backoff after successful connection")
			eb.Reset()
			attempt = 0
		}
	}
}

func (c *workerClient) startStream(ctx context.Context) error {
	eventUrl := fmt.Sprintf(eventsRoute, c.serverURL, c.workerID)
	log.Printf("[DEBUG] Opening event stream to URL: %s", eventUrl)

	bridgeWorkerInfo := c.bridgeWorker.Info(api.InfoOptions{WorkerSlug: c.workerSlug})

	var functionWorkerInfo api.FunctionWorkerInfo
	if c.functionExecutor != nil {
		functionWorkerInfo = c.functionExecutor.Info()
	}

	workerInfo := api.WorkerInfo{
		BridgeWorkerInfo:   bridgeWorkerInfo,
		FunctionWorkerInfo: functionWorkerInfo,
	}
	infoJson, err := json.Marshal(workerInfo)
	if err != nil {
		log.Printf("[ERROR] Failed to marshal worker info: %v", err)
		return fmt.Errorf("error marshaling info: %v", err)
	}
	log.Printf("[DEBUG] BridgeWorker info payload: %s ...", string(infoJson)[0:40])

	reader := strings.NewReader(string(infoJson))
	req, err := http.NewRequest("POST", eventUrl, reader)
	log.Printf("[DEBUG] Request created for URL: %v", eventUrl)
	if err != nil {
		log.Printf("[ERROR] Failed to create request: %v", err)
		return fmt.Errorf("error creating request: %v", err)
	}

	// Match server protocol.
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.workerSecret))

	log.Printf("[DEBUG] Request headers configured: Accept=%s, Cache-Control=%s, Connection=%s",
		req.Header.Get("Accept"),
		req.Header.Get("Cache-Control"),
		req.Header.Get("Connection"))

	req = req.WithContext(ctx)
	log.Printf("[DEBUG] Initiating connection to event stream...")
	startTime := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to stream after %v: %v", time.Since(startTime), err)
		return fmt.Errorf("error connecting to stream: %v", err)
	}

	log.Printf("[INFO] Successfully connected to event stream in %v, status: %d %s",
		time.Since(startTime), resp.StatusCode, resp.Status)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[ERROR]: Server returned status %d: %s", resp.StatusCode, resp.Status)
		log.Printf("[ERROR]: Response body:\n%s\n", string(body))

		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, resp.Status)
	}

	// Log response headers for debugging
	log.Printf("[DEBUG] Response headers:")
	for k, v := range resp.Header {
		log.Printf("[DEBUG]   %s: %v", k, v)
	}

	// We connect via HTTP2 stream and use the SSE format to make it easy to scan payloads.
	scanner := bufio.NewReader(resp.Body)
	log.Printf("[INFO] Starting to read events from stream")
	eventCount := 0

	for {
		line, err := scanner.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				if eventCount > 0 {
					log.Printf("[INFO] Stream connection closed gracefully by server after processing %d events", eventCount)
				} else {
					log.Printf("[WARNING] Stream connection closed immediately - no events processed (possible server issue)")
				}
				break
			}
			// Context cancellation during shutdown is expected, not an error
			if errors.Is(err, context.Canceled) {
				log.Printf("[INFO] Stream read stopped after %d events: %v", eventCount, err)
			} else {
				log.Printf("[ERROR] Network/connection error while reading stream after %d events: %v", eventCount, err)
			}
			return fmt.Errorf("failed to read from event stream: %w", err)
		}

		// Check for SSE "data:" prefix
		if !strings.HasPrefix(line, "data: ") {
			// Skip heartbeat lines, comments, etc. - this is normal SSE behavior
			continue
		}

		// Strip "data: " prefix and parse JSON
		data := strings.TrimPrefix(line, "data: ")
		var event api.EventMessage
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			log.Printf("[ERROR] Malformed event data received (invalid JSON): %v, raw data: %q", err, strings.TrimSpace(data))
			continue
		}

		eventCount++
		log.Printf("[INFO] Event #%d received: Type=%s", eventCount, event.Event)

		// Convert event.Data to []byte for handleEvent
		eventData, err := json.Marshal(event.Data)
		if err != nil {
			log.Printf("[ERROR] Failed to serialize event data for processing (event #%d): %v", eventCount, err)
			continue
		}

		// Process the event and track success/failure
		if err := c.handleEventWithLogging(ctx, event.Event, eventData, eventCount); err != nil {
			log.Printf("[ERROR] Failed to process event #%d: %v", eventCount, err)
		} else {
			log.Printf("[INFO] Successfully processed event #%d", eventCount)
		}
	}

	log.Printf("[INFO] Event stream processing completed, handled %d total events", eventCount)
	return nil
}

// handleEventWithLogging wraps handleEvent with proper error handling and logging
func (c *workerClient) handleEventWithLogging(ctx context.Context, eventType string, data []byte, eventNumber int) error {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ERROR] Panic while processing event #%d (type: %s): %v", eventNumber, eventType, r)
		}
	}()

	switch eventType {
	case api.EventWorker, api.EventBridgeWorker, api.EventFunctionWorker:
		if c.eventCounter != nil {
			c.eventCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("type", eventType),
			))
		}

		c.handleEvent(ctx, eventType, data)
		return nil
	default:
		if c.eventCounter != nil {
			c.eventCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("type", "unknown"),
			))
		}

		return fmt.Errorf("unknown event type: %s", eventType)
	}
}

func (c *workerClient) handleEvent(ctx context.Context, eventType string, data []byte) {
	switch eventType {
	case api.EventWorker:
		var op api.WorkerEventRequest
		if err := json.Unmarshal(data, &op); err != nil {
			if c.errorCounter != nil {
				c.errorCounter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error", ErrorTypeUnmarshalWorkerEvent),
				))
			}

			log.Printf("[ERROR] Failed to unmarshal worker event: %v", err)
			return
		}

		if c.actionCounter != nil {
			c.actionCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("action", string(op.Action)),
			))
		}

		// Handle keepalive events from standby connections
		if op.Action == "keepalive" || op.Action == "standby" {
			// Log and discard keepalive/standby events - they're just to keep the connection alive
			log.Printf("[DEBUG] Received %s event (connection keepalive)", op.Action)
			return
		}

		if op.Action == api.ActionHeartbeat {
			timestamp := time.UnixMicro(op.Payload.Timestamp)
			latency := time.Since(timestamp)
			log.Printf("Received heartbeat: %s (latency: %.3fms)", timestamp.Format(time.RFC3339), float64(latency.Microseconds())/1000.0)

			// --- Check Resource Pressure --- START ---
			pressureMessages := []string{}

			// Memory Pressure (less than 200MB available)
			vmStat, err := mem.VirtualMemory()
			if err != nil {
				if c.errorCounter != nil {
					c.errorCounter.Add(ctx, 1, metric.WithAttributes(
						attribute.String("action", string(op.Action)),
						attribute.String("error", ErrorTypeMemoryStatsFailure),
					))
				}

				log.Printf("[ERROR] failed to get memory stats: %v", err)
			} else if vmStat.Available < 200*1024*1024 { // 200MB in bytes
				pressureMessages = append(pressureMessages, "MemoryPressure")
			}

			/* TODO enable other checks
			// CPU Pressure (>80%)
			// Get total CPU usage over 1 second. Interval 0 would use differential from last call.
			cpuPercent, err := cpu.Percent(time.Second, false)
			if err != nil {
				log.Printf("Error getting CPU stats: %v", err)
			} else if len(cpuPercent) > 0 && cpuPercent[0] > 80.0 {
				pressureMessages = append(pressureMessages, "CPUPressure")
			}

			// Disk Pressure (>90% on root '/'. Adjust path if needed.)
			diskStat, err := disk.Usage("/")
			if err != nil {
				log.Printf("Error getting disk stats for '/': %v", err)
			} else if diskStat.UsedPercent > 90.0 {
				pressureMessages = append(pressureMessages, "DiskPressure")
			}
			*/

			heartbeatMessage := ""
			if len(pressureMessages) > 0 {
				// e.g., "MemoryPressure+DiskPressure"
				heartbeatMessage = strings.Join(pressureMessages, "+")
			}
			// --- Check Resource Pressure --- END ---

			// Respond back to heartbeat probe
			startedAt := time.Now()
			terminatedAt := startedAt
			result := &api.ActionResult{
				ActionResultBaseMeta: api.ActionResultBaseMeta{
					Action:       api.ActionHeartbeat,
					Result:       api.ActionResultNone, // Result type is less important here
					Status:       api.ActionStatusCompleted,
					Message:      heartbeatMessage, // Send pressure status or empty string
					StartedAt:    startedAt,
					TerminatedAt: &terminatedAt,
				},
			}

			if err := c.sendResult(result); err != nil {
				if c.errorCounter != nil {
					c.errorCounter.Add(ctx, 1, metric.WithAttributes(
						attribute.String("action", string(op.Action)),
						attribute.String("error", ErrorTypeHeartbeatResponse),
					))
				}

				log.Printf("[ERROR] Failed to respond heartbeat: %v", err)
			}
			return
		}
	case api.EventBridgeWorker:
		var op api.BridgeWorkerEventRequest
		if err := json.Unmarshal(data, &op); err != nil {
			if c.errorCounter != nil {
				c.errorCounter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error", ErrorTypeUnmarshalBridgeEvent),
				))
			}

			log.Printf("[ERROR] Failed to unmarshal bridge worker event: %v", err)
			return
		}

		if c.actionCounter != nil {
			c.actionCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("action", string(op.Action)),
			))
		}

		// Intercept Cancel action to process immediately, bypassing the queue
		// Cancel is special - we don't send a result for the Cancel action itself,
		// only update the status of the cancelled operation
		if op.Action == api.ActionCancel {
			log.Printf("📥 Received CANCEL command for operation %s (Bypassing queue)", op.Payload.QueuedOperationID)

			// Look up the running operation BEFORE cancelling to get its info (action type, startedAt, etc.)
			runningOp := c.unitQueues.GetRunningOperationByID(op.Payload.QueuedOperationID)

			success := c.unitQueues.CancelOperation(op.Payload.QueuedOperationID)

			if !success {
				log.Printf("[WARN] Cancel failed: operation %s not found or already completed", op.Payload.QueuedOperationID)
			}

			// Send cancelled status for the ORIGINAL operation only
			// This marks the Apply/Destroy operation as Canceled so the CLI knows it's done
			if runningOp != nil && success {
				// Try atomic transition to Canceled status
				if runningOp.TryTransitionToTerminal(api.ActionStatusCanceled) {
					c.sendCancelledStatus(runningOp)
				} else {
					log.Printf("⚠️ Operation %s already terminal, skipping cancel status",
						runningOp.operationID)
				}
			}
			return
		}

		log.Printf("[INFO] Queueing bridge worker event: Unit=%s, Action=%s", op.Payload.UnitID.String(), op.Action)

		// Override check BEFORE queueing (for Apply/Destroy only)
		// This cancels any running same-type operation immediately, before the new operation enters the queue.
		// Critical: This must happen before QueueBridgeEvent because the queue is serial per unit,
		// so the new operation would be blocked waiting for the old one to complete otherwise.
		if op.Action == api.ActionApply || op.Action == api.ActionDestroy {
			if overridden, ok := c.unitQueues.GetAndCancelRunningOperation(op.Payload.UnitID, op.Action); ok {
				// Try atomic transition to Aborted status
				if overridden.TryTransitionToTerminal(api.ActionStatusAborted) {
					log.Printf("🔄 OVERRIDE: Successfully marked operation %s as Aborted",
						overridden.operationID)
					c.sendOverriddenStatus(overridden, op.Payload.QueuedOperationID)
				} else {
					log.Printf("⚠️ OVERRIDE: Operation %s already terminal, skipping status",
						overridden.operationID)
				}
			}
		}

		// Queue the bridge worker event for async processing
		c.unitQueues.QueueBridgeEvent(ctx, op, func(event api.BridgeWorkerEventRequest) {
			// Track this operation
			c.operationsWg.Add(1)
			defer c.operationsWg.Done()

			// Create cancellable context for this operation
			// WithoutCancel first to survive connection drops, then WithCancel for explicit cancellation
			operationCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

			// Register the cancel function so operation can be canceled later
			operationID := event.Payload.QueuedOperationID
			c.unitQueues.RegisterCancelFunc(operationID, cancel)
			defer c.unitQueues.UnregisterCancelFunc(operationID)

			// Register this operation as running (for Apply/Destroy only)
			// Note: The override check is done BEFORE queueing to ensure immediate cancellation
			if event.Action == api.ActionApply || event.Action == api.ActionDestroy {
				c.unitQueues.SetRunningOperation(event.Payload.UnitID, event.Payload.SpaceID,
					operationID, event.Action, event.Payload.RevisionNum)
				defer c.unitQueues.ClearRunningOperation(event.Payload.UnitID, event.Action, operationID)
			}

			var workerContext = &defaultBridgeWorkerContext{
				ctx:       operationCtx,
				serverURL: c.serverURL,
				workerID:  c.workerID,
			}
			err := c.processBridgeCommand(workerContext, event)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					log.Printf("[INFO] Operation %s was canceled for Unit=%s, Action=%s", operationID, event.Payload.UnitID.String(), event.Action)
				} else {
					log.Printf("[ERROR] Bridge command processing failed for Unit=%s, Action=%s: %v", event.Payload.UnitID.String(), event.Action, err)
					if c.errorCounter != nil {
						c.errorCounter.Add(ctx, 1, metric.WithAttributes(
							attribute.String("action", string(event.Action)),
							attribute.String("unit", event.Payload.UnitID.String()),
							attribute.String("error", ErrorTypeBridgeCommand),
						))
					}
				}
			} else {
				log.Printf("[INFO] Bridge command completed successfully for Unit=%s, Action=%s", event.Payload.UnitID.String(), event.Action)
			}
		})
		return
	case api.EventFunctionWorker:
		// Handle events directed to the function worker plugin
		var op api.FunctionWorkerEventRequest
		if err := json.Unmarshal(data, &op); err != nil {
			if c.errorCounter != nil {
				c.errorCounter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error", ErrorTypeUnmarshalFunctionEvent),
				))
			}

			log.Printf("[ERROR] Failed to unmarshal function worker event: %v", err)
			return
		}

		if c.actionCounter != nil {
			c.actionCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("action", string(op.Action)),
			))
		}

		// Intercept Cancel action to process immediately, bypassing the queue
		if op.Action == api.ActionCancel {
			log.Printf("📥 Received CANCEL command for function operation %s (Bypassing queue)", op.Payload.QueuedOperationID)

			success := c.unitQueues.CancelOperation(op.Payload.QueuedOperationID)

			// Send acknowledgment
			startedAt := time.Now()
			terminatedAt := startedAt
			status := api.ActionStatusCompleted
			message := "Operation cancelled"
			if !success {
				status = api.ActionStatusFailed
				message = "Operation not found or already completed"
			}

			result := &api.ActionResult{
				UnitID:            op.Payload.InvocationRequest.UnitID,
				SpaceID:           op.Payload.InvocationRequest.SpaceID,
				QueuedOperationID: op.Payload.QueuedOperationID,
				ActionResultBaseMeta: api.ActionResultBaseMeta{
					Action:       api.ActionCancel,
					Result:       api.ActionResultNone,
					Status:       status,
					Message:      message,
					StartedAt:    startedAt,
					TerminatedAt: &terminatedAt,
				},
			}
			c.sendResult(result)
			return
		}

		log.Printf("[INFO] Queueing function worker event: Unit=%s, Action=%s", op.Payload.InvocationRequest.UnitID.String(), op.Action)
		// Queue the function worker event for async processing
		c.unitQueues.QueueFunctionEvent(ctx, op, func(event api.FunctionWorkerEventRequest) {
			// Track this operation
			c.operationsWg.Add(1)
			defer c.operationsWg.Done()

			// Create cancellable context for this operation
			// WithoutCancel first to survive connection drops, then WithCancel for explicit cancellation
			operationCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

			// Register the cancel function so operation can be canceled later
			operationID := event.Payload.QueuedOperationID
			c.unitQueues.RegisterCancelFunc(operationID, cancel)
			defer c.unitQueues.UnregisterCancelFunc(operationID)

			var workerContext = &defaultFunctionWorkerContext{
				ctx:       operationCtx,
				serverURL: c.serverURL,
				workerID:  c.workerID,
			}
			err := c.processFunctionCommand(workerContext, event)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					log.Printf("[INFO] Operation %s was canceled for Unit=%s, Action=%s", operationID, event.Payload.InvocationRequest.UnitID.String(), event.Action)
				} else {
					log.Printf("[ERROR] Function command processing failed for Unit=%s, Action=%s: %v", event.Payload.InvocationRequest.UnitID.String(), event.Action, err)

					if c.errorCounter != nil {
						c.errorCounter.Add(ctx, 1, metric.WithAttributes(
							attribute.String("action", string(event.Action)),
							attribute.String("unit", event.Payload.InvocationRequest.UnitID.String()),
							attribute.String("error", ErrorTypeFunctionCommand),
						))
					}
				}
			} else {
				log.Printf("[INFO] Function command completed successfully for Unit=%s, Action=%s", event.Payload.InvocationRequest.UnitID.String(), event.Action)
			}
		})
		return
	}
}

func (c *workerClient) getBridgeWorkerSlug() error {
	getUrl := fmt.Sprintf(workerSelfGetRoute, c.serverURL, c.workerID)
	req, err := http.NewRequest(http.MethodGet, getUrl, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.workerSecret))

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to get worker info: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.ReadAll(resp.Body)
		return fmt.Errorf("server returned status %d for request to %s: %s", resp.StatusCode, getUrl, resp.Status)
	}

	var info goclientnew.BridgeWorker
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}

	c.workerSlug = info.Slug
	return nil
}

// sendOverriddenStatus sends an aborted status for an operation that was overridden by a newer one.
// This is called when a new Apply/Destroy operation arrives and supersedes an existing same-type operation.
// Status is queued for async delivery with infinite retry.
func (c *workerClient) sendOverriddenStatus(overridden *runningOperation, newOperationID uuid.UUID) {
	terminatedAt := time.Now()
	result := &api.ActionResult{
		UnitID:            overridden.unitID,
		SpaceID:           overridden.spaceID,
		QueuedOperationID: overridden.operationID,
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			RevisionNum:  overridden.revisionNum,
			Action:       overridden.action,
			Result:       api.ActionResultNone,
			Status:       api.ActionStatusAborted,
			Message:      fmt.Sprintf("Operation overridden by newer operation %s", newOperationID),
			StartedAt:    overridden.startedAt,
			TerminatedAt: &terminatedAt,
		},
	}
	// Queue for async delivery with infinite retry
	c.unitQueues.QueueStatusEvent(
		overridden.unitID.String(),
		result,
		c.sendResultOnce,
	)
}

// sendCancelledStatus sends a cancelled status for an operation that was explicitly cancelled.
// This marks the original Apply/Destroy operation as Canceled so the CLI knows it's done.
// Status is queued for async delivery with infinite retry.
func (c *workerClient) sendCancelledStatus(cancelled *runningOperation) {
	terminatedAt := time.Now()
	result := &api.ActionResult{
		UnitID:            cancelled.unitID,
		SpaceID:           cancelled.spaceID,
		QueuedOperationID: cancelled.operationID,
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			RevisionNum:  cancelled.revisionNum,
			Action:       cancelled.action,
			Result:       api.ActionResultNone,
			Status:       api.ActionStatusCanceled,
			Message:      "Operation cancelled by user request",
			StartedAt:    cancelled.startedAt,
			TerminatedAt: &terminatedAt,
		},
	}
	// Queue for async delivery with infinite retry
	c.unitQueues.QueueStatusEvent(
		cancelled.unitID.String(),
		result,
		c.sendResultOnce,
	)
}

// sendResult sends an action result to the ConfigHub server with automatic retry on failure.
// This method implements exponential backoff retry logic to handle backend restarts or temporary
// network issues, ensuring that operation results are not lost even if the server is temporarily unavailable.
//
// Retry behavior:
//   - Initial retry: 1 second delay
//   - Subsequent retries: exponential backoff with 2x multiplier (2s, 4s, 8s, ...) up to 30 seconds max
//   - Maximum retries: 15 attempts (approximately 5 minutes total)
//   - For heartbeat actions: no retry logging (to avoid noise)
//   - For critical operations: detailed retry logging
//
// This ensures data consistency by persisting operation results even during backend crashes.
func (c *workerClient) sendResult(result *api.ActionResult) error {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %v", err)
	}
	// Do not log ActionResult of Hearbeat.
	// It's too noisy.
	if result.Action != api.ActionHeartbeat {
		// Create a copy for logging with sensitive data redacted
		logResult := *result
		// Clear the large/sensitive fields and replace with size info
		logResult.Data = []byte(fmt.Sprintf("redacted: %d bytes", len(result.Data)))
		logResult.LiveData = []byte(fmt.Sprintf("redacted: %d bytes", len(result.LiveData)))
		logResult.LiveState = []byte(fmt.Sprintf("redacted: %d bytes", len(result.LiveState)))

		filteredJSON, _ := json.Marshal(logResult)
		log.Printf("Result body: %s", string(filteredJSON))
	} else {
		log.Printf("Sending Heartbeat acknowledged")
	}

	// Configure exponential backoff for result sending
	eb := &backoff.ExponentialBackOff{
		InitialInterval:     1 * time.Second,
		RandomizationFactor: backoff.DefaultRandomizationFactor, /* 0.5 */
		Multiplier:          2.0,                                /* double the delay each retry */
		MaxInterval:         30 * time.Second,
	}

	// Create a context with 5-minute timeout for the entire retry operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	attempt := 0
	operation := func() (any, error) {
		attempt++

		req, err := http.NewRequest(http.MethodPost,
			fmt.Sprintf(resultRoute, c.serverURL, c.workerID),
			bytes.NewBuffer(resultJSON))
		if err != nil {
			return nil, backoff.Permanent(fmt.Errorf("failed to create request: %v", err))
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.workerSecret))
		req = req.WithContext(ctx)
		resp, err := c.client.Do(req)
		if err != nil {
			// Log retry attempts for non-heartbeat actions
			if result.Action != api.ActionHeartbeat {
				log.Printf("[WARN] Failed to send result (attempt #%d): %v - will retry", attempt, err)
			}
			return nil, err // Retryable error
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			// 4xx errors are permanent (client errors), 5xx are retryable (server errors)
			// EXCEPTION: 409 Conflict is retryable for action_result endpoint only.
			// This handles optimistic locking conflicts during operation status updates
			// (e.g., when worker retries sending a result and the operation's version changed).
			// The storage layer uses optimistic locking (version field) which can cause
			// transient 409 conflicts that resolve on retry.
			if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusConflict {
				return nil, backoff.Permanent(fmt.Errorf("server returned status %d: %s, body: %s", resp.StatusCode, resp.Status, string(body)))
			}
			// Log retry for retryable errors (5xx server errors and 409 Conflict) on non-heartbeat actions
			if result.Action != api.ActionHeartbeat {
				if resp.StatusCode == http.StatusConflict {
					log.Printf("[WARN] Version conflict %d (attempt #%d): %s - will retry", resp.StatusCode, attempt, resp.Status)
				} else {
					log.Printf("[WARN] Server error %d (attempt #%d): %s - will retry", resp.StatusCode, attempt, resp.Status)
				}
			}
			return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, resp.Status)
		}

		// Success
		if attempt > 1 && result.Action != api.ActionHeartbeat {
			log.Printf("[INFO] Successfully sent result after %d attempts", attempt)
		}
		return nil, nil
	}

	// Use backoff.Retry with proper v5 API signature
	// Maximum 15 retries with context timeout of 5 minutes
	_, err = backoff.Retry(
		ctx,
		operation,
		backoff.WithBackOff(eb),
		backoff.WithMaxTries(15),
	)
	if err != nil {
		log.Printf("[ERROR] Failed to send result after retries: %v", err)
		return err
	}

	return nil
}

// sendResultOnce sends an action result without retry logic.
// Used by status queue which handles its own infinite retry.
// Returns error immediately for caller to handle retry.
func (c *workerClient) sendResultOnce(result *api.ActionResult) error {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %v", err)
	}

	// Log non-heartbeat results with sensitive data redacted
	if result.Action != api.ActionHeartbeat {
		logResult := *result
		logResult.Data = []byte(fmt.Sprintf("redacted: %d bytes", len(result.Data)))
		logResult.LiveData = []byte(fmt.Sprintf("redacted: %d bytes", len(result.LiveData)))
		logResult.LiveState = []byte(fmt.Sprintf("redacted: %d bytes", len(result.LiveState)))
		filteredJSON, _ := json.Marshal(logResult)
		log.Printf("Sending result (once): %s", string(filteredJSON))
	}

	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf(resultRoute, c.serverURL, c.workerID),
		bytes.NewBuffer(resultJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.workerSecret))

	resp, err := c.client.Do(req)
	if err != nil {
		return err // Retryable network error
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned status %d: %s, body: %s",
			resp.StatusCode, resp.Status, string(body))
	}

	return nil
}

// WaitForPendingOperations blocks until all in-flight operations are complete
func (c *workerClient) WaitForPendingOperations() {
	log.Printf("[INFO] Waiting for pending operations to complete...")
	c.operationsWg.Wait()
	log.Printf("[INFO] All operations completed")
}
