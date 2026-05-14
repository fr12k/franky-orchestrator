package events

import (
	"context"
	"encoding/json"
	"sync"
)

// Broker is a fan-out broker for SSE frames.
// It accepts frames from agent SSE consumers and forwards them
// to all subscribed browser clients.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[chan *SseFrame]struct{}
	publishCh   chan *SseFrame
}

// NewBroker creates a new Broker with a buffered publish channel (256).
func NewBroker() *Broker {
	return &Broker{
		subscribers: make(map[chan *SseFrame]struct{}),
		publishCh:   make(chan *SseFrame, 256),
	}
}

// Subscribe adds a subscriber channel.
func (b *Broker) Subscribe(ch chan *SseFrame) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[ch] = struct{}{}
}

// Unsubscribe removes a subscriber channel and closes it.
func (b *Broker) Unsubscribe(ch chan *SseFrame) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.subscribers[ch]; ok {
		delete(b.subscribers, ch)
		close(ch)
	}
}

// Publish sends a frame to all subscribers via the buffered channel.
// Non-blocking — if the publish channel is full, the frame is dropped.
func (b *Broker) Publish(frame *SseFrame) {
	select {
	case b.publishCh <- frame:
		// published
	default:
		// channel full — drop frame (protects against slow consumers)
	}
}

// PublishOrchestratorEvent creates and publishes an orchestrator-originated event.
func (b *Broker) PublishOrchestratorEvent(kind string, data map[string]any) {
	payload := map[string]any{
		"kind": kind,
	}
	for k, v := range data {
		payload[k] = v
	}

	bytes, err := json.Marshal(payload)
	if err != nil {
		return
	}

	frame := &SseFrame{
		Event: kind,
		Data:  string(bytes),
	}

	b.Publish(frame)
}

// Run processes subscriptions and publishes. Blocks until ctx is cancelled.
func (b *Broker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-b.publishCh:
			b.mu.RLock()
			for ch := range b.subscribers {
				select {
				case ch <- frame:
					// delivered
				default:
					// slow consumer — skip (non-blocking)
				}
			}
			b.mu.RUnlock()
		}
	}
}
