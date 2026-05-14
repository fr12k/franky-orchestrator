package events

import (
	"encoding/json"
	"testing"
)

func TestSseFrameMarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		frame SseFrame
		want  string
	}{
		{
			name:  "all fields",
			frame: SseFrame{ID: "1", Event: "message", Data: `{"text":"hello"}`},
			want:  `{"id":"1","event":"message","data":"{\"text\":\"hello\"}"}`,
		},
		{
			name:  "only data",
			frame: SseFrame{Data: "payload"},
			want:  `{"data":"payload"}`,
		},
		{
			name:  "empty frame",
			frame: SseFrame{},
			want:  `{}`,
		},
		{
			name:  "id only",
			frame: SseFrame{ID: "42"},
			want:  `{"id":"42"}`,
		},
		{
			name:  "event only",
			frame: SseFrame{Event: "ping"},
			want:  `{"event":"ping"}`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(tt.frame)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}
			if string(data) != tt.want {
				t.Errorf("marshal mismatch:\n  got:  %s\n  want: %s", string(data), tt.want)
			}
		})
	}
}

func TestSseFrameUnmarshal(t *testing.T) {
	t.Parallel()

	input := `{"id":"5","event":"update","data":"{\"x\":1}"}`
	var frame SseFrame
	if err := json.Unmarshal([]byte(input), &frame); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if frame.ID != "5" {
		t.Errorf("expected ID '5', got '%s'", frame.ID)
	}
	if frame.Event != "update" {
		t.Errorf("expected Event 'update', got '%s'", frame.Event)
	}
	if frame.Data != `{"x":1}` {
		t.Errorf("expected Data '{\"x\":1}', got '%s'", frame.Data)
	}
}

func TestSseFrameRoundTrip(t *testing.T) {
	t.Parallel()

	original := SseFrame{
		ID:    "99",
		Event: "agent_event",
		Data:  `{"kind":"task_complete","agentId":"agent-1"}`,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SseFrame
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID: got '%s', want '%s'", decoded.ID, original.ID)
	}
	if decoded.Event != original.Event {
		t.Errorf("Event: got '%s', want '%s'", decoded.Event, original.Event)
	}
	if decoded.Data != original.Data {
		t.Errorf("Data: got '%s', want '%s'", decoded.Data, original.Data)
	}
}

func TestSseFrameOmitEmpty(t *testing.T) {
	t.Parallel()

	// Verify omitempty behavior: empty ID and Event should be omitted
	frame := SseFrame{Data: "hello"}
	data, _ := json.Marshal(frame)
	if string(data) != `{"data":"hello"}` {
		t.Errorf("expected '{\"data\":\"hello\"}', got '%s'", string(data))
	}
}
