# v0 Implementation Plan — franky-orchestrator

> Based on: `docs/design/v3.1-franky-orchestrator.md` + wireframes in `docs/wireframes/`
> Language: Go 1.26.3  |  Frontend: Vanilla JS (embedded)

---

## 0. Project Bootstrapping

### 0.1 Module & directory layout

```
franky-orchestrator/
  go.mod                    # module github.com/franky/orchestrator
  go.sum
  main.go                   # entrypoint: wires everything, starts server
  internal/
    server/
      server.go             # HTTP server setup, graceful shutdown
      routes.go             # all route registration (Go 1.22+ method patterns)
      middleware.go         # logging, CORS, recovery
    registry/
      registry.go           # AgentRegistry struct + in-memory operations
      persistence.go        # JSON file load/save with atomic write
      stale.go              # SSE-ping watchdog goroutine
    agents/
      client.go             # HTTP read-only client for agent APIs (transcript, role, usage)
      sse_consumer.go       # per-agent SSE connection + event buffer
    events/
      broker.go             # fan-out broker: ingests agent events → forwards to browser subscribers
      types.go              # SSE frame types, event structs
    ui/
      ui.go                 # embed.FS for static assets + handler
      assets/
        index.html
        app.js
        style.css
  skills/
    go-best-practices.md    # Go skill file (already exists)
```

### 0.2 Go toolchain

```bash
go mod init github.com/franky/orchestrator
# Go 1.26.3; no external dependencies for v0 — stdlib only
```

---

## 1. Milestones (build order)

| # | Milestone | What it delivers |
|---|-----------|-----------------|
| M0 | **Scaffold** | `main.go`, server start/stop, `/health`, embedded UI serving |
| M1 | **Registry core** | In-memory registry, JSON persistence, CRUD via API |
| M2 | **Agent liveness + stale detection** | `/register`, `/unregister`, SSE-ping-based watchdog goroutine |
| M3 | **Agent read client** | HTTP client for read-only agent endpoints (`/transcript`, `/role`, `/usage`) |
| M4 | **SSE multiplexing** | Per-agent SSE consumer → broker → browser `/events` stream |
| M5 | **Dashboard UI** | Grid of agent cards (status, stats, actions), live updates via SSE |
| M6 | **Timeline UI** | Waterfall view of cross-agent events, filter by agent/event-type |

---

## 2. M0 — Scaffold

### 2.1 `main.go`

```go
package main

import (
    "context"
    "log/slog"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/franky/orchestrator/internal/server"
)

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    srv := server.New(":9000")

    // Start in background
    go func() {
        slog.Info("orchestrator starting", "addr", ":9000")
        if err := srv.ListenAndServe(); err != nil {
            slog.Error("listen error", "err", err)
        }
    }()

    <-ctx.Done()
    slog.Info("shutting down…")

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := srv.Shutdown(shutdownCtx); err != nil {
        slog.Error("shutdown error", "err", err)
    }
}
```

Key patterns:
- `signal.NotifyContext` — idiomatic Go 1.16+ signal handling
- 10-second drain budget for in-flight requests
- Structured logging via `log/slog` (stdlib since Go 1.21)

### 2.2 `internal/server/server.go`

```go
func New(addr string) *http.Server {
    mux := http.NewServeMux()
    registerRoutes(mux)
    return &http.Server{
        Addr:    addr,
        Handler: withMiddleware(mux),
    }
}
```

Middleware stack (in order): request logging → CORS → recovery (no auth in v0).

### 2.3 `internal/server/routes.go`

Using Go 1.22+ method patterns:

```go
func registerRoutes(mux *http.ServeMux) {
    // Orchestrator self
    mux.HandleFunc("GET /health", handleHealth)

    // Agent registry (agents talk to these)
    mux.HandleFunc("POST /register",     handleRegister)
    mux.HandleFunc("POST /unregister",   handleUnregister)

    // Browser API (read-only)
    mux.HandleFunc("GET /agents",                 handleListAgents)
    mux.HandleFunc("GET /agents/{id}",            handleGetAgent)
    mux.HandleFunc("GET /agents/{id}/transcript",  handleProxyTranscript)
    mux.HandleFunc("GET /agents/{id}/role",        handleProxyRole)
    mux.HandleFunc("GET /agents/{id}/usage",       handleProxyUsage)

    // SSE (browser)
    mux.HandleFunc("GET /events", handleBrowserSSE)

    // Static UI (catch-all — registered last)
    mux.HandleFunc("/", ui.Serve)
}
```

### 2.4 Embedded UI

`internal/ui/ui.go`:

```go
package ui

import "embed"

//go:embed assets/*
var assets embed.FS

func Serve(w http.ResponseWriter, r *http.Request) {
    // Strip prefix, serve from embed.FS
    // / → index.html
    // /app.js → assets/app.js
}
```

The `embed.FS` is compiled into the binary. No external files needed at runtime.

---

## 3. M1 — Registry Core

### 3.1 Data model

```go
// internal/registry/registry.go
type Status string
const (
    StatusIdle      Status = "idle"
    StatusStreaming Status = "streaming"
    StatusError     Status = "error"
    StatusOffline   Status = "offline"
)

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
    LastSeenAt   time.Time `json:"lastSeenAt"` // updated on SSE ping or any event frame
}

type AgentRegistry struct {
    mu      sync.RWMutex
    agents  map[string]*Agent  // id → agent
    persist *Persister
}
```

Thread-safety: `sync.RWMutex` guards all reads/writes. Every public method acquires the lock.

### 3.2 Public API

```go
func (r *AgentRegistry) Register(a *Agent) error   // disallow duplicate apiUrl
func (r *AgentRegistry) Unregister(id string) error
func (r *AgentRegistry) Get(id string) (*Agent, bool)
func (r *AgentRegistry) List() []*Agent
func (r *AgentRegistry) SetStatus(id string, s Status)
func (r *AgentRegistry) Touch(id string)           // update LastSeenAt to now
```

### 3.3 Persistence (`internal/registry/persistence.go`)

- File: `~/.franky-orchestrator/agents.json` (create dir if missing)
- **Atomic write**: marshal to temp file (`agents.json.tmp`), `os.Rename` to final path
- **Load on startup**: read + unmarshal; all agents start as `offline` until their SSE stream delivers a ping/event
- **Save**: called after every register/unregister/status change (debounce optional for v0)

```go
func (p *Persister) Save(agents map[string]*Agent) error {
    tmpPath := p.path + ".tmp"
    data, err := json.MarshalIndent(agents, "", "  ")
    if err != nil { return err }
    if err := os.WriteFile(tmpPath, data, 0644); err != nil { return err }
    return os.Rename(tmpPath, p.path)
}
```

No external locking library — single orchestrator process, single goroutine writes (registry serialized via mutex). The `os.Rename` is atomic on POSIX.

---

## 4. M2 — Agent Liveness & Stale Detection

There is no explicit `/heartbeat` endpoint. Instead, the franky agent sends periodic pings over its SSE stream (as SSE comments: `: ping\n\n`). The orchestrator's SSE consumer detects these and updates `LastSeenAt` on the agent. A stale-watcher goroutine periodically scans the registry and marks agents `offline` if they haven't been seen recently.

### 4.1 Registration flow

```
Agent                    Orchestrator
  │                          │
  ├─ POST /register ────────►│  validate, add to map, persist, set status=idle
  │◄─ 200 {"ok":true} ───────┤
  │                          │
  ├─ GET /events (SSE) ─────►│  SSE consumer starts, receives ping frames
  │   : ping                 │  → Touch(id) updates LastSeenAt
  │   event: …               │  → Touch(id) + forward to broker
  │   data: …                │
```

### 4.2 Uniqueness constraint

`apiUrl` must be unique. If `POST /register` arrives with an `apiUrl` already in the registry, check status:
- If existing agent is `offline`: replace (old agent died, new one on same port)
- If existing agent is `idle`/`streaming`/`error`: reject with `409 Conflict`

### 4.3 SSE Ping handling

The SSE consumer parses each line from the agent's event stream. When it encounters a comment line starting with `: ping`, it calls `registry.Touch(agentID)` to update `LastSeenAt`. Regular event frames also count as liveness — every parsed frame triggers `Touch`.

### 4.4 Stale detection goroutine

```go
// internal/registry/stale.go
func (r *AgentRegistry) StartStaleWatcher(ctx context.Context) {
    ticker := time.NewTicker(15 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            r.mu.Lock()
            for _, a := range r.agents {
                if a.Status != StatusOffline && time.Since(a.LastSeenAt) > 90*time.Second {
                    a.Status = StatusOffline
                    // emit agent_status event to broker
                }
            }
            r.mu.Unlock()
        }
    }
}
```

- 90-second silence threshold (configurable). An agent that sends no SSE frames and no pings for 90s is marked `offline`.
- Agents are NOT auto-removed — only marked `offline`
- User clicks "Remove" to delete from registry

### 4.5 Agent unregistration

`POST /unregister` called on graceful agent exit. Removes agent from registry immediately (unlike stale detection).

---

## 5. M3 — Agent Read Client

The orchestrator is **read-only** — it never sends prompts, aborts, or interrupts to agents. It only reads data that the agent already exposes.

### 5.1 HTTP client (`internal/agents/client.go`)

```go
type Client struct {
    httpClient *http.Client  // with 10s timeout
}

func (c *Client) GetTranscript(ctx context.Context, apiURL string) ([]Message, error)
func (c *Client) GetRole(ctx context.Context, apiURL string) (*RoleInfo, error)
func (c *Client) GetUsage(ctx context.Context, apiURL string) (*UsageInfo, error)
```

Each method:
1. Builds HTTP request to `apiURL + endpoint`
2. Uses `NewRequestWithContext` so the caller controls cancellation/timeout
3. Returns typed response or error

### 5.2 Error mapping

| Agent error | Orchestrator response |
|-------------|----------------------|
| Dial timeout / connection refused | `502 Bad Gateway` + `{"errorCode":"agent_unreachable"}` |
| Agent returns 4xx/5xx | `502 Bad Gateway` + proxied error body |
| HTTP client internal error | `500 Internal Server Error` |

---

## 6. M4 — SSE Multiplexing

This is the heart of the orchestrator.

### 6.1 Architecture

```
Agent-1 /events ──► SSE Consumer-1 ──┐
Agent-2 /events ──► SSE Consumer-2 ──┤
Agent-3 /events ──► SSE Consumer-3 ──┘
                                       │
                                       ▼
                                  Event Broker
                                  (fan-out)
                                       │
                          ┌────────────┼────────────┐
                          ▼            ▼            ▼
                      Browser-1   Browser-2   Browser-N
                      /events     /events     /events
```

```go
type SSEConsumer struct {
    agentID     string
    apiURL      string
    registry    *registry.AgentRegistry  // to call Touch() on ping/events
    broker      *events.Broker
    eventBuffer *RingBuffer[SseFrame]     // last 25 events for replay
    cancel      context.CancelFunc
}
```

Lifecycle:
1. **Start** when agent registers (or comes back from offline → idle)
2. **Stop** when agent unregisters or goes offline
3. On disconnect: auto-reconnect with exponential backoff (1s, 2s, 4s, …, max 30s)
4. Each received frame → `registry.Touch(agentID)` + `broker.Publish(agentEvent)` + push to ring buffer
5. SSE comments (`: ping`) → `registry.Touch(agentID)` only (no broker publish)

```go
func (c *SSEConsumer) Run(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        default:
            c.connectAndRead(ctx)
            // wait with backoff before reconnect
            time.Sleep(backoff)
        }
    }
}

func (c *SSEConsumer) connectAndRead(ctx context.Context) {
    req, _ := http.NewRequestWithContext(ctx, "GET", c.apiURL+"/events", nil)
    req.Header.Set("Accept", "text/event-stream")
    resp, _ := http.DefaultClient.Do(req)
    defer resp.Body.Close()

    scanner := bufio.NewScanner(resp.Body)
    for scanner.Scan() {
        line := scanner.Text()

        // Detect SSE ping (comment starting with ": ping")
        if strings.HasPrefix(line, ": ping") {
            c.registry.Touch(c.agentID)
            continue
        }

        // Parse SSE frame fields: id:, event:, data:
        // On complete frame (blank line), publish to broker + touch
    }
}
```

### 6.3 Ring buffer

```go
type RingBuffer[T any] struct {
    buf    []T
    head   int
    size   int
    cap    int
}
```

Capacity: 50 events per agent (from open question #1 in design doc). Used for:
- Late-joining browser clients requesting `Last-Event-ID` replay
- Not persisted — ephemeral, in-memory only

### 6.4 Event Broker (`internal/events/broker.go`)

Channels-based fan-out pattern:

```go
type Broker struct {
    subscribers   map[chan *SseFrame]bool
    registerCh    chan chan *SseFrame
    unregisterCh  chan chan *SseFrame
    publishCh     chan *SseFrame
}

func (b *Broker) Run(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case ch := <-b.registerCh:
            b.subscribers[ch] = true
        case ch := <-b.unregisterCh:
            delete(b.subscribers, ch)
            close(ch)
        case frame := <-b.publishCh:
            for ch := range b.subscribers {
                select {
                case ch <- frame:
                default:
                    // subscriber too slow — skip
                }
            }
        }
    }
}
```

Non-blocking send to each subscriber (fast path via `default` skip).

### 6.5 Browser SSE endpoint

`GET /events` handler:

```go
func handleBrowserSSE(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    ch := make(chan *SseFrame, 64)
    broker.Subscribe(ch)
    defer broker.Unsubscribe(ch)

    flusher := w.(http.Flusher)

    for {
        select {
        case <-r.Context().Done():
            return
        case frame := <-ch:
            writeSSEFrame(w, frame)
            flusher.Flush()
        }
    }
}
```

### 6.6 Orchestrator-only events

Not from agents, emitted by orchestrator itself:

```json
{"kind":"agent_registered","agent":{…}}
{"kind":"agent_unregistered","agentId":"…"}
{"kind":"agent_status","agentId":"…","status":"offline"}
```

These are published to the broker after registry mutations.

---

## 7. M5 — Dashboard UI

### 7.1 Tech constraints
- **Zero build pipeline** — no npm, no bundler, no TypeScript
- **Vanilla JS** — `fetch()`, `EventSource`, DOM manipulation
- **Single HTML file** `index.html` + inline CSS + `app.js`
- **Dark theme** matching franky proxy UI (same CSS variables as wireframes)

### 7.2 State management

```javascript
// app.js — simple state
const state = {
    agents: new Map(),       // id → agent object
    filter: 'all',          // 'all' | 'idle' | 'streaming' | 'offline'
    search: '',             // free-text filter
};
```

### 7.3 Data flow

```
SSE /events ──► EventSource listener
                  ├─ agent_registered → state.agents.set(id, agent) → renderGrid()
                  ├─ agent_unregistered → state.agents.delete(id) → renderGrid()
                  ├─ agent_status → state.agents.get(id).status = newStatus → renderGrid()
                  └─ all agent events → update stats counters → renderGrid()
```

### 7.4 Agent card template

Each card renders from `state.agents` map:
- Status dot (color-coded CSS class)
- Agent name + model badge
- Stats row: uptime, message count, turns, token in/out (from SSE events)
- Tool usage counters (top 3 tools)
- Workspace path (truncated)
- "Open" button → `window.open(agent.apiUrl, '_blank')`
- "Remove" button (only for offline agents) → `POST /agents/{id}/remove` (or `DELETE /agents/{id}`)

### 7.5 Re-render strategy

Simple: re-render entire grid on any state change. With < 20 agents, this is fast enough.

Alternative: use `data-agent-id` attributes and patch individual cards. v0 can start with full re-render.

### 7.6 Search/filter toolbar

- Text search: filter `state.agents` by name, workspace, model (case-insensitive)
- Status filter buttons: All / Idle / Streaming / Offline
- Combined: agents must match BOTH search AND status filter

### 7.7 Periodic stats refresh

Fetch `/agents` every 15 seconds as fallback (SSE is primary). This handles:
- Page load initial state
- Missed SSE events
- Token counters from agent `/usage` endpoint

---

## 8. M6 — Timeline UI

### 8.1 View

A single chronological feed of events from all agents. Waterfall layout (as in wireframe).

### 8.2 Event buffer

Browser-side ring buffer of last 200 events (across all agents). Older events drop off.

```javascript
const timelineBuffer = []; // max 200 entries
```

### 8.3 Filtering

- Agent filter: toggle which agents' events are shown (agent-tag pills)
- Event type filter: All / Turns / Tools / Errors / Permissions

Filtering is client-side — iterate `timelineBuffer`, show/hide DOM rows.

### 8.4 Row template

Each event row:
- Timestamp (HH:MM:SS)
- Agent name + color dot
- Event type icon (▶ turn, 🔧 tool, 💬 message, ⚠ error, ↻ retry, 🔒 permission)
- One-line summary (tool name + target file, message preview, error message)

### 8.5 Navigation

- "← Dashboard" link in topbar returns to dashboard view
- Clicking an agent name opens agent's proxy UI in new tab
- View tabs: Grid / Gantt / Waterfall (only Waterfall implemented in v0; others are visual stubs)

### 8.6 Implementation approach

Both Dashboard and Timeline live in the same `index.html`. A simple view-switcher:

```javascript
function showView(view) {
    document.getElementById('dashboard-view').hidden = (view !== 'dashboard');
    document.getElementById('timeline-view').hidden = (view !== 'timeline');
}
```

---

## 9. API Contract Summary

### Orchestrator → Browser (all read-only)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check |
| `GET` | `/agents` | List all agents |
| `GET` | `/agents/{id}` | Get single agent |
| `GET` | `/agents/{id}/transcript` | Get agent transcript |
| `GET` | `/agents/{id}/role` | Get agent role info |
| `GET` | `/agents/{id}/usage` | Get agent usage stats |
| `GET` | `/events` | Multiplexed SSE stream |
| `GET` | `/` | Dashboard UI |

### Agent → Orchestrator

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/register` | Self-registration |
| `POST` | `/unregister` | Graceful exit |

---

## 10. Configuration

### 10.1 Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ORCHESTRATOR_PORT` | `9000` | Listen port |
| `ORCHESTRATOR_DATA_DIR` | `~/.franky-orchestrator` | Registry persistence dir |
| `ORCHESTRATOR_STALE_TIMEOUT` | `90` | Seconds of SSE silence until agent marked offline |
| `ORCHESTRATOR_EVENT_BUFFER_SIZE` | `25` | Events buffered per agent |

### 10.2 CLI (future)

```bash
franky-orchestrator --port 9000 --data-dir ~/.franky-orchestrator
```

v0 can just read env vars.

---

## 11. Error Handling Conventions

### Go handler pattern

```go
func handleX(w http.ResponseWriter, r *http.Request) {
    // 1. Parse/validate
    // 2. Call registry/client
    // 3. Write JSON response
    // Every error path writes a structured error:
    writeError(w, http.StatusNotFound, "agent_not_found", "agent not found")
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(map[string]any{
        "ok":        false,
        "error":     msg,
        "errorCode": code,
    })
}
```

### Error codes

| Code | HTTP status | Meaning |
|------|-------------|---------|
| `agent_not_found` | 404 | Unknown agent ID |
| `agent_offline` | 502 | Agent registered but unreachable |
| `agent_unreachable` | 502 | Cannot connect to agent API |
| `duplicate_api_url` | 409 | Another agent already at this apiUrl |
| `capacity_exceeded` | 503 | Too many agents (reserved, not enforced in v0) |

---

## 12. Testing Strategy

### 12.1 Unit tests

- Registry CRUD + uniqueness + stale detection: table-driven tests
- Ring buffer: push/pop/overflow
- Persistence: write→read round-trip, atomic write behavior
- SSE frame parsing (including ping comment detection)

### 12.2 Integration tests

- Start orchestrator, register agent stub, verify `/agents` returns it
- SSE stream with pings → verify agent stays `idle`
- SSE silence (no pings) → verify agent becomes `offline` after timeout
- SSE multiplexing: publish agent events → verify browser endpoint receives them

### 12.3 Mock agent

For integration tests: a small test helper that speaks the agent API (health, register, SSE with synthetic events and pings).

---

## 13. Open Decisions (from design doc §10)

| # | Question | v0 Decision |
|---|----------|-------------|
| 1 | Event buffer depth | **25 events per agent** (configurable via env) |
| 2 | Auto-discovery (mDNS/UDP) | **Deferred to v1**. Only explicit `--register` in v0 |
| 3 | Naming conflict (same workspace) | **Allow duplicate workspace, unique apiUrl required**. Only one agent per `apiUrl`; slot freed on unregister or after user removes offline agent |
| 4 | Token-based auth | **Deferred to v1**. No auth in v0 |

---

## 14. File Checklist (by milestone)

### M0 — Scaffold
- [ ] `go.mod`
- [ ] `main.go`
- [ ] `internal/server/server.go`
- [ ] `internal/server/routes.go`
- [ ] `internal/server/middleware.go`
- [ ] `internal/ui/ui.go`
- [ ] `internal/ui/assets/index.html` (skeleton)
- [ ] `internal/ui/assets/app.js` (skeleton)
- [ ] `internal/ui/assets/style.css` (skeleton)

### M1 — Registry core
- [ ] `internal/registry/registry.go`
- [ ] `internal/registry/persistence.go`

### M2 — SSE-ping liveness + stale detection
- [ ] `internal/registry/stale.go`
- [ ] Register/unregister handlers (in routes or separate file)
- [ ] SSE consumer ping detection + `registry.Touch` integration

### M3 — Agent read client
- [ ] `internal/agents/client.go` (read-only: transcript, role, usage)

### M4 — SSE multiplexing
- [ ] `internal/agents/sse_consumer.go`
- [ ] `internal/events/broker.go`
- [ ] `internal/events/types.go`
- [ ] Browser SSE handler

### M5 — Dashboard UI
- [ ] `internal/ui/assets/index.html` (full dashboard)
- [ ] `internal/ui/assets/app.js` (dashboard logic)
- [ ] `internal/ui/assets/style.css` (full theme)

### M6 — Timeline UI
- [ ] Extend `app.js` with timeline view
- [ ] Extend `index.html` with timeline markup
