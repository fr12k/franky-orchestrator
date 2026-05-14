# franky orchestrator

A standalone web application that discovers, monitors, and controls multiple [franky](https://github.com/fr12k/franky) agents through their REST API. Think of it as mission control for your fleet of AI coding agents.

<p align="center">
  <em>Dashboard</em><br>
  <img src="docs/screenshots/dashboard.png" alt="Dashboard view showing agent cards with live status, stats, and tool usage" width="800"><br><br>
  <em>Timeline</em><br>
  <img src="docs/screenshots/timeline.png" alt="Waterfall timeline showing cross-agent event streams in real time" width="800">
</p>

## What it does

The orchestrator gives you a single pane of glass for every franky agent you're running:

- **Dashboard** — a grid of agent cards with live status (idle / streaming / error / offline), uptime, message & turn counters, token throughput, and per-tool usage stats.
- **Timeline** — a waterfall view of every event flowing through all agents in real time: turns, messages, tool calls, errors, retries. See exactly what every agent is doing right now.
- **Agent control** — interrupt, restart, or remove agents from the dashboard. Proxy through to each agent's own web UI, transcript, session list, and design docs.

All communication with agents happens exclusively over HTTP — no shared code, no import coupling.

## Architecture

```
┌─────────────────────────────────┐
│  Browser                        │
│  ── orchestrator dashboard      │
│     ┌── Dashboard (card grid)    │
│     ├── Agent detail (proxy)     │
│     └── Timeline (waterfall)     │
└──────────┬──────────────────────┘
           │  HTTP (orchestrator API + SSE)
┌──────────▼──────────────────────┐
│  franky-orchestrator            │
│  ── HTTP server (port 9000)     │
│  ── Agent registry (JSON file)  │
│  ── SSE multiplexer             │
└──────┬──────────┬─────────────────┘
       │          │  HTTP (agent API)
┌──────▼──┐  ┌────▼──┐  ┌────▼──┐
│ proxy-1 │  │proxy-2│  │proxy-3│  … franky --mode proxy
│ :8787   │  │:8788  │  │:8789  │
└─────────┘  └───────┘  └───────┘
```

### Agent lifecycle

1. Start a franky agent: `franky --mode proxy --port 8787`
2. Agent self-registers with the orchestrator via `POST /register`
3. Orchestrator starts consuming the agent's SSE event stream (`GET /events`)
4. Agent sends periodic `POST /heartbeat` to stay alive
5. Agent sends `POST /unregister` on graceful exit — or the orchestrator marks it stale after missed heartbeats

## Quick start

### Prerequisites

- Go 1.25+
- One or more [franky](https://github.com/fr12k/franky) agents running in proxy mode

### Install

```bash
git clone https://github.com/fr12k/franky-orchestrator.git
cd franky-orchestrator
go build ./...
```

### Run

```bash
# Start the orchestrator (defaults to port 9000)
go run .

# Or with custom port and data directory
ORCHESTRATOR_PORT=8080 ORCHESTRATOR_DATA_DIR=/tmp/orch go run .
```

Open http://localhost:9000 in your browser.

### Connect agents

Start your franky agents with the orchestrator's address so they self-register:

```bash
franky --mode proxy --port 8787 --register http://localhost:9000
franky --mode proxy --port 8788 --register http://localhost:9000
```

Agents appear instantly on the dashboard. Send them prompts, watch their activity on the timeline, and control them — all from one place.

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ORCHESTRATOR_PORT` | `9000` | Port the orchestrator HTTP server listens on |
| `ORCHESTRATOR_DATA_DIR` | `~/.franky-orchestrator` | Directory for persisted agent state (`agents.json`) |

## API reference

### Agent-facing endpoints (used by franky agents)

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/register` | Agent self-registration |
| `POST` | `/heartbeat` | Periodic liveness ping |
| `POST` | `/unregister` | Graceful agent removal |

### Browser-facing endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/agents` | List all registered agents |
| `GET` | `/agents/{id}` | Get a single agent's details |
| `DELETE` | `/agents/{id}` | Remove an agent from the registry |
| `GET` | `/agents/{id}/transcript` | Proxy to agent transcript |
| `GET` | `/agents/{id}/role` | Proxy to agent role config |
| `GET` | `/agents/{id}/usage` | Proxy to agent usage stats |
| `GET` | `/agents/{id}/session` | Proxy to agent current session |
| `GET` | `/agents/{id}/sessions` | Proxy to agent session list |
| `GET` | `/agents/{id}/design-docs` | Proxy to agent design docs |
| `GET` | `/agents/{id}/health` | Proxy to agent health check |
| `POST` | `/agents/{id}/interrupt` | Graceful interrupt |
| `POST` | `/agents/{id}/restart` | Restart agent |
| `POST` | `/agents/{id}/command` | Send a slash-command to an agent |
| `GET` | `/events` | SSE stream — all agent events multiplexed |

### Orchestrator self

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/health` | Orchestrator health check |

## Project structure

```
franky-orchestrator/
├── main.go                  # Entry point, wiring, graceful shutdown
├── internal/
│   ├── agents/              # HTTP client, SSE consumer for agent streams
│   ├── events/              # Event broker + frame model for SSE multiplexing
│   ├── registry/            # Agent registry, JSON persister, stale watcher
│   ├── server/              # HTTP server, routes, handlers, middleware
│   └── ui/                  # Embedded SPA (HTML/CSS/JS dashboard + timeline)
└── docs/
    ├── design/              # Architecture and design documents
    ├── wireframes/          # Early wireframe mockups
    └── screenshots/         # Screenshots for README
```

## Tech stack

- **Backend**: Go 1.25, standard library HTTP server with Go 1.22+ method-pattern routing
- **Frontend**: Vanilla JavaScript SPA, no framework — dark theme, CSS custom properties, SSE for live updates
- **Persistence**: JSON file on disk (`~/.franky-orchestrator/agents.json`)

## License

MIT

---

Built to orchestrate [franky](https://github.com/fr12k/franky) agents.
