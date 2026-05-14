package agents

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/franky/orchestrator/internal/events"
)

// SSEConsumer reads an agent's SSE stream and forwards events to the broker.
type SSEConsumer struct {
	AgentID string
	APIURL  string
	TouchFn func(string)         // called on ping/events to update LastSeenAt
	Broker  *events.Broker
	CheckFn func(string) bool    // returns false if agent no longer exists

	log *slog.Logger
}

// NewSSEConsumer creates a new SSEConsumer.
func NewSSEConsumer(agentID, apiURL string, broker *events.Broker, touchFn func(string), checkFn func(string) bool) *SSEConsumer {
	return &SSEConsumer{
		AgentID:  agentID,
		APIURL:   apiURL,
		Broker:   broker,
		TouchFn:  touchFn,
		CheckFn:  checkFn,
		log:      slog.With("component", "sse_consumer", "agentId", agentID, "apiUrl", apiURL),
	}
}

// Run connects to the agent SSE stream with exponential backoff reconnect.
func (c *SSEConsumer) Run() {
	bk := newBackoff(1*time.Second, 30*time.Second)

	for {
		// Check if agent still exists
		if c.CheckFn != nil && !c.CheckFn(c.AgentID) {
			c.log.Info("agent removed from registry, stopping SSE consumer")
			return
		}

		c.log.Info("connecting to agent SSE stream")
		err := c.connectAndRead()
		if err != nil {
			c.log.Warn("SSE connection lost", "err", err)
		}

		// Check again before sleeping
		if c.CheckFn != nil && !c.CheckFn(c.AgentID) {
			return
		}

		delay := bk.next()
		c.log.Info("reconnecting", "delay", delay)
		time.Sleep(delay)
	}
}

// connectAndRead establishes SSE connection and reads frames.
func (c *SSEConsumer) connectAndRead() error {
	req, err := http.NewRequest("GET", c.APIURL+"/events", nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 0} // no timeout for streaming
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("agent returned %d: %s", resp.StatusCode, string(body))
	}

	c.log.Info("SSE stream connected")

	scanner := bufio.NewScanner(resp.Body)
	// Increase buffer for potentially large SSE data lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var frame events.SseFrame

	for scanner.Scan() {
		line := scanner.Text()

		// SSE ping comment
		if strings.HasPrefix(line, ": ping") {
			if c.TouchFn != nil {
				c.TouchFn(c.AgentID)
			}
			continue
		}

		// Empty line marks frame boundary
		if line == "" {
			if frame.Data != "" {
				// Copy frame before publishing — the pointer is enqueued
				// asynchronously and frame is reset below.
				copyFrame := frame
				copyFrame.Data = injectAgentID(copyFrame.Data, c.AgentID)
				c.Broker.Publish(&copyFrame)
				if c.TouchFn != nil {
					c.TouchFn(c.AgentID)
				}
			}
			frame = events.SseFrame{}
			continue
		}

		// Parse SSE fields
		if strings.HasPrefix(line, "id: ") {
			frame.ID = line[4:]
		} else if strings.HasPrefix(line, "event: ") {
			frame.Event = line[7:]
		} else if strings.HasPrefix(line, "data: ") {
			frame.Data = line[6:]
		}
		// Comments (lines starting with :) are ignored unless ": ping"
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	return nil
}

// injectAgentID adds the agentId field to a JSON object string.
func injectAgentID(data, agentID string) string {
	if len(data) > 0 && data[0] == '{' {
		return fmt.Sprintf(`{"agentId":%q,%s`, agentID, data[1:])
	}
	return data
}

// backoff implements exponential backoff with a cap.
type backoff struct {
	current time.Duration
	max     time.Duration
}

func newBackoff(initial, max time.Duration) *backoff {
	return &backoff{current: initial, max: max}
}

func (b *backoff) next() time.Duration {
	delay := b.current
	b.current *= 2
	if b.current > b.max {
		b.current = b.max
	}
	return delay
}

func (b *backoff) reset() {
	b.current = 1 * time.Second
}
