package events

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestSubscribeAndPublish(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go broker.Run(ctx)

	ch := make(chan *SseFrame, 16)
	broker.Subscribe(ch)

	frame := &SseFrame{Event: "test", Data: `{"msg":"hello"}`}
	broker.Publish(frame)

	select {
	case got := <-ch:
		if got.Event != "test" {
			t.Errorf("expected event 'test', got '%s'", got.Event)
		}
		if got.Data != `{"msg":"hello"}` {
			t.Errorf("expected data '{\"msg\":\"hello\"}', got '%s'", got.Data)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for frame")
	}
}

func TestUnsubscribe(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go broker.Run(ctx)

	ch := make(chan *SseFrame, 16)
	broker.Subscribe(ch)
	broker.Unsubscribe(ch)

	// Channel should be closed after unsubscribe
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after unsubscribe")
	}

	// Publishing should not send to unsubscribed channel (no panic)
	frame := &SseFrame{Event: "test", Data: "data"}
	broker.Publish(frame)
}

func TestSlowConsumer(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go broker.Run(ctx)

	// Small buffer that fills quickly
	ch := make(chan *SseFrame, 1)
	broker.Subscribe(ch)

	// First frame fills the buffer
	broker.Publish(&SseFrame{Event: "e1", Data: "d1"})
	// Second frame fills broker's publishCh but subscriber's buffer is full
	// Publish should not block
	broker.Publish(&SseFrame{Event: "e2", Data: "d2"})

	// Wait for broker to process
	time.Sleep(100 * time.Millisecond)

	// Drain to avoid deadlock
	select {
	case <-ch:
	default:
	}
}

func TestPublishOrchestratorEvent(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go broker.Run(ctx)

	ch := make(chan *SseFrame, 16)
	broker.Subscribe(ch)

	broker.PublishOrchestratorEvent("agent_status", map[string]any{
		"agentId": "agent-1",
		"status":  "offline",
	})

	select {
	case frame := <-ch:
		if frame.Event != "agent_status" {
			t.Errorf("expected event 'agent_status', got '%s'", frame.Event)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(frame.Data), &payload); err != nil {
			t.Fatalf("failed to unmarshal data: %v", err)
		}
		if payload["kind"] != "agent_status" {
			t.Errorf("expected kind 'agent_status', got '%v'", payload["kind"])
		}
		if payload["agentId"] != "agent-1" {
			t.Errorf("expected agentId 'agent-1', got '%v'", payload["agentId"])
		}
		if payload["status"] != "offline" {
			t.Errorf("expected status 'offline', got '%v'", payload["status"])
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for orchestrator event")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go broker.Run(ctx)

	n := 3
	chs := make([]chan *SseFrame, n)
	for i := 0; i < n; i++ {
		chs[i] = make(chan *SseFrame, 16)
		broker.Subscribe(chs[i])
	}

	frame := &SseFrame{Event: "broadcast", Data: "to all"}
	broker.Publish(frame)

	for i, ch := range chs {
		select {
		case got := <-ch:
			if got.Event != "broadcast" {
				t.Errorf("subscriber %d: expected event 'broadcast', got '%s'", i, got.Event)
			}
		case <-time.After(1 * time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}
}

func TestContextCancellation(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		broker.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// Run returned, success
	case <-time.After(1 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestPublishDropOnFullChannel(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	// Don't start Run goroutine — fill the publish channel to force drops
	// publishCh has capacity 256
	for i := 0; i < 300; i++ {
		broker.Publish(&SseFrame{Event: "fill", Data: "data"})
	}

	// After 256, frames should be dropped without blocking
	// If this completes without deadlock, the test passes
}

func TestConcurrentPublishAndSubscribe(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go broker.Run(ctx)

	var wg sync.WaitGroup
	n := 10

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ch := make(chan *SseFrame, 16)
			broker.Subscribe(ch)
			broker.Publish(&SseFrame{Event: "concurrent", Data: "test"})
			// Receive one frame
			select {
			case <-ch:
			case <-time.After(500 * time.Millisecond):
			}
			broker.Unsubscribe(ch)
		}(i)
	}

	wg.Wait()
}

func TestPublishOrchestratorEventNilMap(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go broker.Run(ctx)

	ch := make(chan *SseFrame, 16)
	broker.Subscribe(ch)

	// nil data map
	broker.PublishOrchestratorEvent("test_kind", nil)

	select {
	case frame := <-ch:
		var payload map[string]any
		if err := json.Unmarshal([]byte(frame.Data), &payload); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if payload["kind"] != "test_kind" {
			t.Errorf("expected kind 'test_kind', got '%v'", payload["kind"])
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out")
	}
}
