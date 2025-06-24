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
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/confighub/sdk/bridge-worker/api"
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
			DialTLS: func(network, addr string, cfg *tls.Config) (netConn net.Conn, err error) {
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
	}
}

func (c *workerClient) Start(ctx context.Context) error {
	err := c.getBridgeWorkerSlug()
	if err != nil {
		log.Printf("[ERROR] Failed to get bridge worker slug: %v", err)
		return fmt.Errorf("failed to get bridge worker slug: %v", err)
	}
	return c.startStream(ctx)
}

func (c *workerClient) startStream(ctx context.Context) error {
	eventUrl := fmt.Sprintf(eventsRoute, c.serverURL, c.workerID)
	log.Printf("[DEBUG] Opening event stream to URL: %s", eventUrl)

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
		// read body
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[ERROR] Server returned status %d: %s\n%s", resp.StatusCode, resp.Status, string(body))
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
				log.Printf("[INFO] Stream closed by server (EOF) after processing %d events", eventCount)
				break
			}
			log.Printf("[ERROR] Failed to read from stream after processing %d events: %v", eventCount, err)
			return fmt.Errorf("failed to read from event stream: %w", err)
		}

		// Check for SSE "data:" prefix
		if !strings.HasPrefix(line, "data: ") {
			log.Printf("[TRACE] Skipping non-data line: %q", strings.TrimSpace(line))
			continue
		}

		// Strip "data: " prefix and parse JSON
		data := strings.TrimPrefix(line, "data: ")
		var event api.EventMessage
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			log.Printf("[ERROR] Failed to unmarshal event: %v, raw data: %q", err, data)
			continue
		}

		eventCount++
		log.Printf("[DEBUG] Event #%d received - Type: %s, Data length: %d bytes",
			eventCount, event.Event, len(data))

		// Convert event.Data to []byte for handleEvent
		eventData, err := json.Marshal(event.Data)
		if err != nil {
			log.Printf("[ERROR] Failed to marshal event data for event #%d: %v", eventCount, err)
			continue
		}

		log.Printf("[DEBUG] Processing event #%d of type '%s'", eventCount, event.Event)
		c.handleEvent(ctx, event.Event, eventData)
		log.Printf("[DEBUG] Finished processing event #%d", eventCount)
	}

	log.Printf("[INFO] Event stream processing completed, handled %d total events", eventCount)
	return nil
}

func (c *workerClient) handleEvent(ctx context.Context, eventType string, data []byte) {
	switch eventType {
	case api.EventWorker:
		var op api.WorkerEventRequest
		if err := json.Unmarshal(data, &op); err != nil {
			log.Printf("Error unmarshaling command: %v", err)
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
		var workerContext = &defaultBridgeWorkerContext{
			// TODO with context cancel
			ctx:       ctx,
			serverURL: c.serverURL,
			workerID:  c.workerID,
		}
		var op api.BridgeWorkerEventRequest
		if err := json.Unmarshal(data, &op); err != nil {
			log.Printf("Error unmarshaling command: %v", err)
			return
		}
		err := c.processBridgeCommand(workerContext, op)
		if err != nil {
			log.Printf("Error sending result: %v", err)
			return
		}
	case api.EventFunctionWorker:
		// Handle events directed to the function worker plugin
		var workerContext = &defaultFunctionWorkerContext{
			// TODO with context cancel
			ctx:       ctx,
			serverURL: c.serverURL,
			workerID:  c.workerID,
		}
		var op api.FunctionWorkerEventRequest
		if err := json.Unmarshal(data, &op); err != nil {
			log.Printf("Error unmarshaling function worker command: %v", err)
			return
		}
		err := c.processFunctionCommand(workerContext, op)
		if err != nil {
			log.Printf("Error processing function worker command: %v", err)
			return
		}
		return // Explicit return after handling
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
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
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
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
