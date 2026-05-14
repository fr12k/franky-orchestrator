package registry

import (
	"time"
)

// Status represents the agent's current operational state.
type Status string

const (
	StatusIdle      Status = "idle"
	StatusStreaming Status = "streaming"
	StatusError     Status = "error"
	StatusOffline   Status = "offline"
)

// Agent represents a registered franky agent.
type Agent struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	APIURL       string    `json:"apiUrl"`
	Workspace    string    `json:"workspace"`
	Model        string    `json:"model"`
	Role         string    `json:"role"`
	PID          int       `json:"pid,omitempty"`
	Status       Status    `json:"status"`
	RegisteredAt time.Time `json:"registeredAt"`
	LastSeenAt   time.Time `json:"lastSeenAt"`

	// Live counters populated from agent /usage endpoint
	MessageCount int            `json:"messageCount,omitempty"`
	TurnCount    int            `json:"turnCount,omitempty"`
	TokensIn     int            `json:"tokensIn,omitempty"`
	TokensOut    int            `json:"tokensOut,omitempty"`
	ToolStats    map[string]int `json:"toolStats,omitempty"`
	ErrorMessage string         `json:"errorMessage,omitempty"`
}
