package events

// SseFrame represents a parsed Server-Sent Events frame.
type SseFrame struct {
	ID    string `json:"id,omitempty"`
	Event string `json:"event,omitempty"`
	Data  string `json:"data,omitempty"`
}
