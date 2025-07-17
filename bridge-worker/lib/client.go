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
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alitto/pond"
	"github.com/confighub/sdk/bridge-worker/api"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/shirou/gopsutil/v3/mem"
	"golang.org/x/net/http2"
)

const (
	eventsRoute        = "%s/api/bridge_worker/%s/stream"
	resultRoute        = "%s/api/bridge_worker/%s/action_result"
	workerSelfGetRoute = "%s/api/bridge_worker/%s/me"
)

type workerClient struct {
	serverURL      string
	workerID       string
	workerSecret   string
	workerSlug     string
	client         *http.Client
	done           chan struct{}
	bridgeWorker   api.BridgeWorker
	functionWorker api.FunctionWorker
	watcherPool    *pond.WorkerPool
	unitQueues     *UnitQueueManager
}

func newClient(serverURL, workerID, workerSecret string, bridgeWorker api.BridgeWorker, functionWorker api.FunctionWorker) *workerClient {
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

	return &workerClient{
		serverURL:      serverURL,
		workerID:       workerID,
		workerSecret:   workerSecret,
		client:         &http.Client{Transport: transport},
		done:           make(chan struct{}),
		bridgeWorker:   bridgeWorker,
		functionWorker: functionWorker,
		watcherPool:    pond.New(10, 50),
		unitQueues:     NewUnitQueueManager(),
	}
}

func (c *workerClient) Start(ctx context.Context) error {
	err := c.getBridgeWorkerSlug()
	if err != nil {
		log.Printf("[ERROR] Failed to get bridge worker slug: %v", err)
		return fmt.Errorf("failed to get bridge worker slug: %v", err)
	}

	// Start the unit queue manager
	c.unitQueues.Start(ctx)

	// Ensure cleanup on exit
	defer c.unitQueues.Stop()

	return c.startStream(ctx)
}

func (c *workerClient) startStream(ctx context.Context) error {
	eventUrl := fmt.Sprintf(eventsRoute, c.serverURL, c.workerID)
	log.Printf("[DEBUG] Opening event stream to URL: %s", eventUrl)

	// TODO accumulate from all supported workers
	bridgeWorkerInfo := c.bridgeWorker.Info(api.InfoOptions{Slug: c.workerSlug})
	functionWorkerInfo := c.functionWorker.Info()

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
		_, _ = io.ReadAll(resp.Body)
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
			log.Printf("[ERROR] Network/connection error while reading stream after %d events: %v", eventCount, err)
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
		c.handleEvent(ctx, eventType, data)
		return nil
	default:
		return fmt.Errorf("unknown event type: %s", eventType)
	}
}

func (c *workerClient) handleEvent(ctx context.Context, eventType string, data []byte) {
	switch eventType {
	case api.EventWorker:
		var op api.WorkerEventRequest
		if err := json.Unmarshal(data, &op); err != nil {
			log.Printf("[ERROR] Failed to unmarshal worker event: %v", err)
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
				log.Printf("Error getting memory stats: %v", err)
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
				log.Printf("Failed to respond heartbeat: %v", err)
			}
			return
		}
	case api.EventBridgeWorker:
		var op api.BridgeWorkerEventRequest
		if err := json.Unmarshal(data, &op); err != nil {
			log.Printf("[ERROR] Failed to unmarshal bridge worker event: %v", err)
			return
		}

		log.Printf("[INFO] Queueing bridge worker event: Unit=%s, Action=%s", op.Payload.UnitID.String(), op.Action)
		// Queue the bridge worker event for async processing
		c.unitQueues.QueueBridgeEvent(ctx, op, func(event api.BridgeWorkerEventRequest) {
			var workerContext = &defaultBridgeWorkerContext{
				ctx:       ctx,
				serverURL: c.serverURL,
				workerID:  c.workerID,
			}
			err := c.processBridgeCommand(workerContext, event)
			if err != nil {
				log.Printf("[ERROR] Bridge command processing failed for Unit=%s, Action=%s: %v", event.Payload.UnitID.String(), event.Action, err)
			} else {
				log.Printf("[INFO] Bridge command completed successfully for Unit=%s, Action=%s", event.Payload.UnitID.String(), event.Action)
			}
		})
		return
	case api.EventFunctionWorker:
		// Handle events directed to the function worker plugin
		var op api.FunctionWorkerEventRequest
		if err := json.Unmarshal(data, &op); err != nil {
			log.Printf("[ERROR] Failed to unmarshal function worker event: %v", err)
			return
		}

		log.Printf("[INFO] Queueing function worker event: Unit=%s, Action=%s", op.Payload.InvocationRequest.UnitID.String(), op.Action)
		// Queue the function worker event for async processing
		c.unitQueues.QueueFunctionEvent(ctx, op, func(event api.FunctionWorkerEventRequest) {
			var workerContext = &defaultFunctionWorkerContext{
				ctx:       ctx,
				serverURL: c.serverURL,
				workerID:  c.workerID,
			}
			err := c.processFunctionCommand(workerContext, event)
			if err != nil {
				log.Printf("[ERROR] Function command processing failed for Unit=%s, Action=%s: %v", event.Payload.InvocationRequest.UnitID.String(), event.Action, err)
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
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, resp.Status)
	}

	var info goclientnew.BridgeWorker
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}

	c.workerSlug = info.Slug
	return nil
}

func (c *workerClient) sendResult(result *api.ActionResult) error {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %v", err)
	}
	// Do not log ActionResult of Hearbeat.
	// It's too noisy.
	if result.Action != api.ActionHeartbeat {
		log.Printf("Result body: %s", string(resultJSON))
	} else {
		log.Printf("Sending Heartbeat acknowledged")
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
		return fmt.Errorf("failed to send result: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.ReadAll(resp.Body)
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, resp.Status)
	}

	return nil
}
