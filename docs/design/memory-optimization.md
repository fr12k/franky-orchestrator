# Memory Optimization

Investigation into the orchestrator tab consuming up to 5 GB of memory.
All changes in this document are applied in commit `perf(memory): reduce memory footprint in browser and server`.

## Browser Memory (Frontend — `app.js`)

### Fixed Issues

#### 1. Unbounded `timelineEvents` array
**Severity:** HIGH  
**File:** `internal/ui/assets/app.js`  
**What happened:** Every SSE event (`turn_start`, `turn_end`, `message_start`, `message_end`, `tool_execution_start`, `tool_execution_end`, `agent_error`, `agent_interrupted`, `provider_retry`) was pushed into `this.state.timelineEvents` and **never removed**. The code explicitly said *"Never discard old events — the user wants to keep all history. The array grows unboundedly (browser memory is the limit)."*

With multiple active agents firing events, the array could grow to tens of thousands of entries — each carrying the full `data` payload (tool arguments, results, messages, etc.), consuming many MB.

**Fix:** Cap at 500 entries. Older entries are trimmed from the front via `splice(0, length - 2000)`.

**Remaining concern:** 500 entries with large tool call payloads can still consume significant memory. Consider limiting the `data` payload stored per event (e.g., truncate tool args/results to 500 chars).

#### 2. Full re-render on every `message_update` delta
**Severity:** HIGH  
**File:** `internal/ui/assets/app.js`  
**What happened:** Each streaming text token from an agent (up to multiple per second) triggered `this.renderTimeline()`, which rebuilt the entire waterfall DOM from scratch — iterating all 2000+ events, creating HTML strings, and replacing `innerHTML`. This caused massive GC churn and DOM thrashing.

**Fix:** Debounced via `requestAnimationFrame` — at most one render per frame (~16ms).

**Remaining concern:** On the timeline view with 2000 events, even one render per frame is expensive. Consider virtual scrolling (only render visible entries) or incremental DOM updates.

#### 3. `message_update` events flooding the timeline
**Severity:** MEDIUM  
**File:** `internal/ui/assets/app.js`  
**What happened:** `message_update` was in the `timelineKinds` array, so every streaming delta was stored as a raw timeline event. This pushed out older `message_start` / `tool_execution_start` events needed for pairing, causing visible events to disappear despite the "never discard" policy.

**Fix:** Moved `message_update` out of `timelineKinds` and into a dedicated listener that accumulates into a `pendingMessages` buffer (per agentId). The buffer is only consumed by `buildTimelineItems()` for display — it's never stored in the raw array.

#### 4. Stale `pendingMessages` / `pendingTools` leaks
**Severity:** LOW  
**File:** `internal/ui/assets/app.js`  
**What happened:** If a `message_start` or `tool_execution_start` arrived without its matching `_end` (agent crash, disconnect, restart), the map entry stayed **forever**. Over time with repeated agent reconnects, this accumulates orphaned entries.

**Fix:** Added `cleanupStaleBuffers()` method that runs every 5 minutes and removes entries older than 5 minutes.

#### 5. Truncate large payloads in stored events
**Severity:** HIGH  
**Description:** Timeline event `data` objects carry the full tool call arguments and results. For `write`, `edit`, `subagent` tools, these can be hundreds of KB per event (file contents, prompts, etc.). With 2000 events, this adds up fast.

**Suggestion:** When storing timeline events, truncate large string fields in `data` (e.g., `content`, `resultText`, `argsJson`, `prompt`) to e.g. 500 chars with a `" … truncated"` suffix. The full data is available via the agent's API anyway.

**Location:** Around line 149 in `app.js`, in the `push()` block.

### Still To Do

#### 7. Release orphaned event data on agent disconnect
**Severity:** LOW  
**Description:** When an agent disconnects (detected via `agent_unregistered` event), its timeline events are still in the array. Over hours with many short-lived agents, this accumulates.

**Suggestion:** On `agent_unregistered`, filter out events for that agent from `timelineEvents`, or at least drop their `data` payload (set to `null`) to free the heaviest part.

## Server Memory (Backend — Go)

### Fixed Issues

#### 1. `persist.Save()` called on every `UpdateUsage()`
**Severity:** HIGH  
**File:** `internal/registry/registry.go`  
**What happened:** `UpdateUsage()` called `r.persist.Save(r.agents)` on **every** usage poll cycle (every 15 seconds per non-offline agent). `Save()` does `json.MarshalIndent` over the entire agents map and writes to disk. With many agents, this continuously allocates large JSON byte slices.

**Fix:** Removed `persist.Save()` from `UpdateUsage()`. Usage counters are ephemeral — they're re-polled every 15s anyway.

#### 2. `persist.Save()` called on every `Touch()`
**Severity:** HIGH  
**File:** `internal/registry/registry.go`  
**What happened:** `Touch()` is called on every SSE ping (every few seconds per agent), every heartbeat, and every event frame. Each call triggered a full JSON marshal of all agents to disk.

**Fix:** Removed `persist.Save()` from `Touch()`. Status transitions still persist via `SetStatus()` and `MarkOffline()`.

#### 3. `http.Client` created on every SSE reconnect
**Severity:** MEDIUM  
**File:** `internal/agents/sse_consumer.go`  
**What happened:** `connectAndRead()` created a new `&http.Client{Timeout: 0}` on every call (every reconnect). Each `http.Client` has its own transport with connection pool goroutines. With exponential backoff reconnecting every few seconds, this leaked goroutines and sockets.

**Fix:** Moved the `http.Client` to the `SSEConsumer` struct, created once in `NewSSEConsumer()`.

#### 4. Large scanner buffer allocation
**Severity:** LOW  
**File:** `internal/agents/sse_consumer.go`  
**What happened:** `scanner.Buffer(make([]byte, 0, 64KB), 1MB)` — the max line buffer was 1 MB, allocating a 1 MB backing slice on startup per SSE consumer. With 10 agents, that's 10 MB just in buffer headroom.

**Fix:** Reduced max to 256 KB (still plenty for SSE data payloads).

#### 5. Batch disk persistence
**Severity:** MEDIUM
**File:** `internal/registry/registry.go`, `internal/registry/persister.go`
**What happened:** `SetStatus()` and `MarkOffline()` called `persist.Save()` synchronously on every status change. With many agents and frequent status transitions (heartbeats, stale-detection), this caused continuous full-JSON-marshal disk writes.

**Fix:** Added a dirty-flag + background flush loop (`StartFlushLoop`/`StopFlushLoop`) that periodically (every 5s) persists the full agents map only when changes are pending. `SetStatus()` and `MarkOffline()` now set `r.dirty = true` instead of writing directly. `Register()` and `Unregister()` still persist immediately since those are user-triggered, data-critical operations.

#### 6. Reduce JSON marshal allocation
**Severity:** LOW
**File:** `internal/registry/persister.go`
**What happened:** Every persistence call used `json.MarshalIndent` (human-readable indented JSON).

**Fix:** Added `SaveCompact()` method that uses `json.Marshal` (no indent). The batched flush loop uses `SaveCompact()` since periodic writes don't need human readability. Direct user-triggered saves (`Register`/`Unregister`/`PersistAll`) still use `Save()` with indentation for debugging convenience.

#### 7. Agent `data` field in SseFrame
**Severity:** LOW
**File:** `internal/agents/sse_consumer.go`, `internal/events/broker.go`
**Description:** The `data` field of `SseFrame` is a `string`. Every SSE event payload is a JSON string that gets copied on every publish (line 110-111 of `sse_consumer.go`: `copyFrame := frame; copyFrame.Data = injectAgentID(...)`). These strings are retained in the broker channel buffer (256 entries) and in each browser subscriber's channel buffer (64 entries). With N subscribers, the same event string is held N times.

**Status:** Not changed — see "Still To Do" for discussion.

#### 8. Profile heap allocations
**Severity:** LOW
**File:** `main.go`
**Description:** Expose `pprof` debug endpoints for runtime profiling.

**Fix:** Added `net/http/pprof` import and a separate debug HTTP server on port 9001 (overridable via `ORCHESTRATOR_DEBUG_ADDR`) registered with pprof handlers (`/debug/pprof/heap`, `/debug/pprof/goroutine`, `/debug/pprof/profile`, `/debug/pprof/trace`, etc.). This keeps pprof off the main agent-facing HTTP server.

```bash
go tool pprof http://localhost:9001/debug/pprof/heap
```

### Still To Do

**Suggestion:** Add a background goroutine that collects dirty agent IDs on a channel and persists them in bulk every 5-10 seconds. `SetStatus` / `MarkOffline` / `Register` / `Unregister` push to the channel instead of writing directly.
