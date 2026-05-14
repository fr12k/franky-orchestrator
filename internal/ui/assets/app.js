// ── franky orchestrator — app.js ───────────────────────────
// Vanilla JS SPA: dashboard grid + waterfall timeline, live SSE updates.

const App = {

  // ── State ────────────────────────────────────────────────
  state: {
    agents: new Map(),           // id → agent object
    view: 'dashboard',           // 'dashboard' | 'timeline'
    filter: 'all',               // 'all' | 'idle' | 'streaming' | 'error' | 'offline'
    search: '',                  // search input value
    timelineEvents: [],          // [{id, agentId, kind, time, data, agentName, agentColor}]
    connected: false,
  },

  // ── Init ─────────────────────────────────────────────────
  init() {
    this.bindUI();
    this.connectSSE();
    this.loadInitialState();
    this.tickInterval = setInterval(() => this.updateRelativeTimes(), 30_000);
  },

  // ── Bind UI ──────────────────────────────────────────────
  bindUI() {
    // Search
    const searchInput = document.getElementById('search-input');
    searchInput.addEventListener('input', (e) => {
      this.state.search = e.target.value.toLowerCase();
      this.renderDashboard();
    });

    // Filter buttons
    document.querySelectorAll('#filter-group .filter-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        document.querySelectorAll('#filter-group .filter-btn').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        this.state.filter = btn.dataset.filter;
        this.renderDashboard();
      });
    });

    // View tabs
    document.querySelectorAll('#view-tabs .view-tab').forEach(tab => {
      tab.addEventListener('click', () => {
        document.querySelectorAll('#view-tabs .view-tab').forEach(t => t.classList.remove('active'));
        tab.classList.add('active');
        this.state.view = tab.dataset.view;
        this.switchView();
      });
    });
  },

  // ── Switch view ──────────────────────────────────────────
  switchView() {
    const grid = document.getElementById('agent-grid');
    const timelineContainer = document.getElementById('timeline-container');
    const empty = document.getElementById('empty-state');

    if (this.state.view === 'dashboard') {
      grid.classList.remove('hidden');
      timelineContainer.classList.add('hidden');
      this.renderDashboard();
    } else {
      grid.classList.add('hidden');
      timelineContainer.classList.remove('hidden');
      if (empty) empty.classList.add('hidden');
      this.renderTimeline();
    }
  },

  // ── Load initial state ──────────────────────────────────
  async loadInitialState() {
    try {
      const res = await fetch('/agents');
      if (!res.ok) throw new Error('Failed to fetch agents');
      const data = await res.json();
      const agentList = data.agents || [];
      for (const a of agentList) {
        this.state.agents.set(a.id, this.normalizeAgent(a));
      }
      this.renderDashboard();
      this.updateOrchPill();
    } catch (err) {
      console.warn('loadInitialState:', err.message);
      // still render — will show empty state
      this.renderDashboard();
    }
  },

  // ── SSE ──────────────────────────────────────────────────
  connectSSE() {
    const es = new EventSource('/events');
    this._es = es;

    es.addEventListener('open', () => {
      this.state.connected = true;
      this.updateOrchPill();
    });

    es.addEventListener('agent_registered', (e) => {
      const data = JSON.parse(e.data);
      const a = this.normalizeAgent(data.agent);
      this.state.agents.set(a.id, a);
      this.renderDashboard();
      this.updateOrchPill();
    });

    es.addEventListener('agent_unregistered', (e) => {
      const data = JSON.parse(e.data);
      this.state.agents.delete(data.agentId);
      this.renderDashboard();
      this.updateOrchPill();
    });

    es.addEventListener('agent_status', (e) => {
      const data = JSON.parse(e.data);
      const agent = this.state.agents.get(data.agentId);
      if (agent) {
        agent.status = data.status;
        agent.lastSeenAt = data.lastSeenAt || new Date().toISOString();
        if (data.errorMessage !== undefined) agent.errorMessage = data.errorMessage;
        this.renderDashboard();
      }
    });

    // ── Timeline events ────────────────────────────────────
    const timelineKinds = [
      'turn_start', 'turn_end', 'message_start', 'message_update', 'message_end',
      'tool_execution_start', 'tool_execution_end',
      'agent_error', 'agent_interrupted', 'provider_retry',
    ];

    for (const kind of timelineKinds) {
      es.addEventListener(kind, (e) => {
        const data = JSON.parse(e.data);
        const agentId = data.agentId || data.id;
        const agent = agentId ? this.state.agents.get(agentId) : null;

        this.state.timelineEvents.push({
          id: `ev-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
          agentId: agentId || 'unknown',
          kind,
          time: data.time || new Date().toISOString(),
          data,
          agentName: agent ? agent.name : (agentId || 'unknown'),
          agentColor: agent ? this.agentColor(agent) : 'api',
        });

        // Keep max 500 events
        if (this.state.timelineEvents.length > 500) {
          this.state.timelineEvents = this.state.timelineEvents.slice(-500);
        }

        // Update agent's last-seen and status for tool/turn activity
        if (agent) {
          agent.lastSeenAt = data.time || new Date().toISOString();
          if (kind === 'turn_start' || kind === 'message_start' || kind === 'tool_execution_start') {
            if (agent.status === 'idle') agent.status = 'streaming';
          }
          if (kind === 'turn_end' || kind === 'agent_interrupted') {
            agent.status = 'idle';
          }
          if (kind === 'agent_error' && data.isFatal) {
            agent.status = 'error';
            agent.errorMessage = data.message;
          }
        }

        if (this.state.view === 'dashboard') {
          this.renderDashboard();
        } else {
          this.renderTimeline();
        }
      });
    }

    es.addEventListener('error', (e) => {
      // SSE errors are expected on reconnect — just wait
      this.state.connected = false;
      this.updateOrchPill();
    });
  },

  // ── Render dashboard ─────────────────────────────────────
  renderDashboard() {
    const grid = document.getElementById('agent-grid');
    const empty = document.getElementById('empty-state');
    const timelineContainer = document.getElementById('timeline-container');

    if (this.state.view !== 'dashboard') return;
    if (timelineContainer) timelineContainer.classList.add('hidden');

    // Filter
    let agents = Array.from(this.state.agents.values());
    const f = this.state.filter;
    if (f !== 'all') {
      agents = agents.filter(a => a.status === f);
    }
    const q = this.state.search;
    if (q) {
      agents = agents.filter(a =>
        (a.name || '').toLowerCase().includes(q) ||
        (a.workspace || '').toLowerCase().includes(q) ||
        (a.model || '').toLowerCase().includes(q)
      );
    }

    // Empty state
    if (this.state.agents.size === 0) {
      grid.innerHTML = '';
      grid.classList.remove('hidden');
      if (empty) empty.classList.remove('hidden');
      return;
    }

    if (empty) empty.classList.add('hidden');

    // Build grid
    let html = '';
    for (const a of agents) {
      html += this.buildAgentCardHTML(a);
    }

    // Add-hint card (always last when no search/filter active)
    if (f === 'all' && !q) {
      html += `<div class="agent-card add-hint">
        <span class="plus">+</span>
        <span>Start a franky agent with</span>
        <code>--register http://localhost:9000</code>
      </div>`;
    }

    grid.innerHTML = html;

    // Bind card buttons
    this.bindCardButtons();
  },

  // ── Build agent card HTML ────────────────────────────────
  buildAgentCardHTML(a) {
    const status = a.status || 'offline';
    const isOffline = status === 'offline';
    const uptime = a.registeredAt ? this.uptime(a.registeredAt) : '—';
    const msgs = a.messageCount != null ? a.messageCount : (a.stats?.messages ?? '—');
    const turns = a.turnCount != null ? a.turnCount : (a.stats?.turns ?? '—');
    const tokensIn = a.tokensIn != null ? this.fmtNum(a.tokensIn) : '—';
    const tokensOut = a.tokensOut != null ? this.fmtNum(a.tokensOut) : '—';
    const lastActivity = a.lastSeenAt ? this.timeAgo(a.lastSeenAt) : '—';
    const workspace = a.workspace || '—';
    const model = a.model || '—';
    const role = a.role || '';
    const errorMsg = a.errorMessage || '';

    // Tool stats
    let toolsHTML = '';
    if (a.toolStats && Object.keys(a.toolStats).length > 0) {
      for (const [name, count] of Object.entries(a.toolStats)) {
        toolsHTML += `<span class="tool-stat"><span class="tcount">${this.esc(String(count))}</span> <span class="tname">${this.esc(name)}</span></span>`;
      }
    }

    // Error message row
    let errorHTML = '';
    if (errorMsg && status === 'error') {
      errorHTML = `<div class="agent-meta-row">
        <span class="label">Error</span>
        <span class="error-msg">${this.esc(errorMsg)}</span>
      </div>`;
    }

    const cardClass = isOffline ? 'agent-card offline-card' : 'agent-card';

    return `<div class="${cardClass}" data-agent-id="${this.esc(a.id)}">
      <div class="agent-card-head">
        <span class="agent-status-dot ${status}"></span>
        <span class="agent-name">${this.esc(a.name || a.id)}</span>
        ${role ? `<span class="agent-role-tag">${this.esc(role)}</span>` : ''}
        <span class="agent-model">${this.esc(model)}</span>
      </div>
      <div class="agent-stats">
        <div class="stat"><span class="val">${uptime}</span><span class="lbl">uptime</span></div>
        <div class="stat"><span class="val">${msgs}</span><span class="lbl">msgs</span></div>
        <div class="stat"><span class="val">${turns}</span><span class="lbl">turns</span></div>
        <div class="stat"><span class="val">${tokensIn}</span><span class="lbl">in</span></div>
        <div class="stat"><span class="val">${tokensOut}</span><span class="lbl">out</span></div>
      </div>
      ${toolsHTML ? `<div class="agent-tools">${toolsHTML}</div>` : ''}
      <div class="agent-meta">
        <div class="agent-meta-row">
          <span class="label">Workspace</span>
          <span class="value workspace">${this.esc(workspace)}</span>
        </div>
        ${errorHTML}
      </div>
      <div class="agent-footer">
        <div class="agent-actions">
          <button class="primary" data-action="open" data-id="${this.esc(a.id)}">Open</button>
          <button class="danger" data-action="remove" data-id="${this.esc(a.id)}">Remove</button>
        </div>
        <span class="last-activity">${lastActivity}</span>
      </div>
    </div>`;
  },

  // ── Bind card buttons ────────────────────────────────────
  bindCardButtons() {
    document.querySelectorAll('.agent-card button[data-action]').forEach(btn => {
      // Avoid double-binding
      if (btn._bound) return;
      btn._bound = true;

      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        const action = btn.dataset.action;
        const id = btn.dataset.id;
        const agent = this.state.agents.get(id);
        if (!agent) return;

        switch (action) {
          case 'open':
            if (agent.apiUrl) {
              window.open(agent.apiUrl, '_blank');
            }
            break;
          case 'remove':
            this.removeAgent(agent);
            break;
        }
      });
    });
  },

  // ── Remove agent ─────────────────────────────────────────
  async removeAgent(agent) {
    if (!confirm(`Remove "${agent.name}" from the orchestrator?`)) return;
    try {
      const res = await fetch(`/agents/${encodeURIComponent(agent.id)}`, { method: 'DELETE' });
      if (!res.ok) throw new Error(await res.text());
      this.state.agents.delete(agent.id);
      this.renderDashboard();
      this.updateOrchPill();
    } catch (err) {
      alert(`Failed to remove: ${err.message}`);
    }
  },

  // ── Render timeline ──────────────────────────────────────
  renderTimeline() {
    const waterfall = document.getElementById('waterfall');
    const empty = document.getElementById('empty-state');
    const grid = document.getElementById('agent-grid');

    if (this.state.view !== 'timeline') return;
    grid.classList.add('hidden');
    if (empty) empty.classList.add('hidden');

    const rawEvents = this.state.timelineEvents;

    if (rawEvents.length === 0) {
      waterfall.innerHTML = `<div class="empty-state">
        <h2>No events yet</h2>
        <p>Activity will appear here as agents work.</p>
      </div>`;
      return;
    }

    // Merge tool start+end pairs, filter noise, produce display items
    const items = this.buildTimelineItems(rawEvents);

    if (items.length === 0) {
      waterfall.innerHTML = `<div class="empty-state">
        <h2>No events yet</h2>
        <p>Activity will appear here as agents work.</p>
      </div>`;
      return;
    }

    // Build time-axis ticks from item times (last 5 rounded minutes), descending
    const times = items.map(it => new Date(it.time));
    const minTime = new Date(Math.min(...times));
    const maxTime = new Date(Math.max(...times));
    if (maxTime - minTime < 60_000) maxTime.setTime(minTime.getTime() + 60_000);

    const tickCount = 5;
    const range = maxTime - minTime;
    const step = range / (tickCount - 1);
    const ticks = [];
    for (let i = 0; i < tickCount; i++) {
      ticks.push(new Date(maxTime.getTime() - step * i));  // descending ticks
    }

    let timeAxisHTML = '<div class="time-axis">';
    for (const t of ticks) {
      timeAxisHTML += `<span class="tick">${this.fmtTime(t)}</span>`;
    }
    timeAxisHTML += '</div>';

    // Build entries (items already sorted descending)
    let entriesHTML = '';
    for (let i = 0; i < items.length; i++) {
      entriesHTML += this.buildTimelineEntry(items[i], i > 0 ? items[i - 1] : null);
    }

    // Summary
    const tools = items.filter(it => it.kind === 'tool_merged').length;
    const errors = items.filter(it => it.kind === 'agent_error').length;
    const retries = items.filter(it => it.kind === 'provider_retry').length;
    const interrupts = items.filter(it => it.kind === 'agent_interrupted').length;
    const running = items.filter(it => it.kind === 'tool_running').length;
    const msgs = items.filter(it => it.kind === 'message_merged').length;

    const summaryHTML = `<div class="wf-summary">
      <span><strong>${msgs}</strong> messages</span>
      <span><strong>${tools + running}</strong> tool calls</span>
      <span style="color:var(--error);"><strong>${errors}</strong> errors</span>
      <span style="color:var(--warn);"><strong>${retries}</strong> retries</span>
      ${interrupts > 0 ? `<span><strong>${interrupts}</strong> interrupts</span>` : ''}
      <span style="margin-left:auto;color:var(--accent);">${items.length} items total</span>
    </div>`;

    waterfall.innerHTML = timeAxisHTML + entriesHTML + summaryHTML;
    
    // Bind click handlers on timeline entries
    this.bindTimelineClicks();
  },

  // ── Build display items from raw SSE events ───────────────
  buildTimelineItems(events) {
    const items = [];
    const pendingTools = new Map(); // callId -> { startEvent }
    const pendingMsgs = new Map();  // agentId -> { role, text, startTime, startEvent }

    for (const ev of events) {
      if (ev.kind === 'tool_execution_start') {
        const cid = ev.data.callId;
        if (cid) pendingTools.set(cid, ev);
      } else if (ev.kind === 'tool_execution_end') {
        const cid = ev.data.callId;
        const start = cid ? pendingTools.get(cid) : null;
        if (start) {
          pendingTools.delete(cid);
          const durationMs = new Date(ev.time) - new Date(start.time);
          const durationSec = Math.round(durationMs / 1000);
          items.push({
            id: ev.id,
            agentId: ev.agentId,
            kind: 'tool_merged',
            time: start.time,
            endTime: ev.time,
            durationMs,
            durationSec,
            data: {
              name: start.data.name,   // name always comes from start
              isError: ev.data.isError,
              resultText: ev.data.resultText,
              argsJson: start.data.argsJson,
              path: start.data.path,
            },
            agentName: ev.agentName,
            agentColor: ev.agentColor,
          });
        } else {
          // orphan end — still show with whatever we have
          items.push({ ...ev, kind: 'tool_merged', time: ev.time, endTime: ev.time, durationMs: 0, durationSec: 0 });
        }
      } else if (ev.kind === 'message_start') {
        // Flush any existing pending message for this agent before starting new one
        const oldBuf = pendingMsgs.get(ev.agentId);
        if (oldBuf && (oldBuf.text.trim() || (Date.now() - new Date(oldBuf.startTime).getTime()) > 1000)) {
          items.push({
            id: `msg-flushed-${ev.agentId}`,
            agentId: ev.agentId,
            kind: 'message_merged',
            time: oldBuf.startTime,
            endTime: ev.time,
            durationMs: new Date(ev.time) - new Date(oldBuf.startTime),
            durationSec: Math.round((new Date(ev.time) - new Date(oldBuf.startTime)) / 1000),
            data: { role: oldBuf.role, text: oldBuf.text },
            agentName: oldBuf.startEvent ? oldBuf.startEvent.agentName : ev.agentName,
            agentColor: oldBuf.startEvent ? oldBuf.startEvent.agentColor : ev.agentColor,
          });
        }
        // Start accumulating text for this agent's new message
        pendingMsgs.set(ev.agentId, {
          role: ev.data.role || 'unknown',
          text: '',
          startTime: ev.time,
          startEvent: ev,
        });
      } else if (ev.kind === 'message_update') {
        const buf = pendingMsgs.get(ev.agentId);
        if (buf && ev.data.deltaKind === 'text') {
          buf.text += (ev.data.delta || '');
        }
      } else if (ev.kind === 'message_end') {
        const buf = pendingMsgs.get(ev.agentId);
        if (buf) {
          pendingMsgs.delete(ev.agentId);
          const durationMs = new Date(ev.time) - new Date(buf.startTime);
          const durationSec = Math.round(durationMs / 1000);
          // Only emit if there's text or the message ran for a notable time
          if (buf.text.trim() || durationSec >= 1) {
            items.push({
              id: ev.id,
              agentId: ev.agentId,
              kind: 'message_merged',
              time: buf.startTime,
              endTime: ev.time,
              durationMs,
              durationSec,
              data: { role: buf.role, text: buf.text },
              agentName: ev.agentName,
              agentColor: ev.agentColor,
            });
          }
        }
      } else if (ev.kind === 'agent_error' || ev.kind === 'provider_retry' || ev.kind === 'agent_interrupted') {
        items.push(ev);
      }
      // skip turn_start, turn_end
    }

    // Leftover pending tool starts → show as running
    for (const start of pendingTools.values()) {
      items.push({
        ...start,
        kind: 'tool_running',
        data: {
          name: start.data.name,
          argsJson: start.data.argsJson,
          path: start.data.path,
        },
      });
    }

    // Leftover pending messages → show as in-progress
    for (const [agentId, buf] of pendingMsgs) {
      if (buf.text.trim() || (Date.now() - new Date(buf.startTime).getTime()) > 1000) {
        items.push({
          id: `msg-running-${agentId}`,
          agentId,
          kind: 'message_merged',
          time: buf.startTime,
          endTime: new Date().toISOString(),
          durationMs: Date.now() - new Date(buf.startTime).getTime(),
          durationSec: Math.round((Date.now() - new Date(buf.startTime).getTime()) / 1000),
          data: { role: buf.role, text: buf.text },
          agentName: buf.startEvent ? buf.startEvent.agentName : 'unknown',
          agentColor: buf.startEvent ? buf.startEvent.agentColor : 'api',
        });
      }
    }

    // Sort descending by time (newest first)
    items.sort((a, b) => new Date(b.time) - new Date(a.time));

    return items;
  },

  // ── Build timeline entry ─────────────────────────────────
  buildTimelineEntry(ev, prev) {
    const d = new Date(ev.time);
    const timeStr = this.fmtTimeFull(d);
    const borderClass = `border-${ev.agentColor}`;

    let rowClass = '';
    let badge = '●';
    let kindLabel = '';
    let contentHTML = '';

    switch (ev.kind) {
      case 'tool_merged':
      case 'tool_running': {
        const toolName = ev.data.name;
        const isRunning = ev.kind === 'tool_running';
        if (ev.data.isError && !isRunning) {
          rowClass = ' wf-tool';
          badge = '❌';
          kindLabel = 'tool';
          contentHTML = `<span class="wf-highlight">${this.esc(toolName)}</span>`;
          contentHTML += ` <span style="color:var(--error);margin-left:6px;">✗ error</span>`;
        } else if (isRunning) {
          rowClass = ' wf-tool';
          badge = '🔧';
          kindLabel = 'tool';
          contentHTML = `<span class="wf-highlight">${this.esc(toolName)}</span>`;
          contentHTML += ` <span class="wf-running">● running</span>`;
        } else {
          rowClass = ' wf-tool';
          badge = '🔧';
          kindLabel = 'tool';
          contentHTML = `<span class="wf-highlight">${this.esc(toolName)}</span>`;
          const len = ev.data.resultText ? ev.data.resultText.length : 0;
          contentHTML += ` <span class="wf-ok">✓ ${this.fmtNum(len)}B</span>`;
        }

        // Show key tool args with truncation for large values
        const argsHTML = this.buildToolArgsHTML(ev.data);
        if (argsHTML) {
          contentHTML += ` <span class="wf-tool-args">${argsHTML}</span>`;
        }

        // Show duration if >= 1 second
        if (ev.durationSec >= 1) {
          contentHTML += ` <span class="wf-duration">· ${ev.durationSec}s</span>`;
        }
        break;
      }

      case 'message_merged': {
        rowClass = ' wf-msg';
        badge = '💬';
        kindLabel = 'msg';
        const text = ev.data.text || '';
        const display = this.snippet(text, 125);
        contentHTML = `<span class="wf-msg-text">${this.esc(display)}</span>`;
        if (ev.durationSec >= 1) {
          contentHTML += ` <span class="wf-duration">· ${ev.durationSec}s</span>`;
        }
        break;
      }

      case 'agent_error':
        rowClass = ' wf-error';
        badge = '⚠';
        kindLabel = 'error';
        contentHTML = this.esc(ev.data.message || ev.data.code || 'Unknown error');
        if (ev.data.isFatal) {
          contentHTML += ' <span style="color:var(--error);">(fatal)</span>';
        }
        break;

      case 'agent_interrupted':
        rowClass = '';
        badge = '⏸';
        kindLabel = 'interrupt';
        contentHTML = 'Agent interrupted by user';
        break;

      case 'provider_retry':
        rowClass = ' wf-retry';
        badge = '↻';
        kindLabel = 'retry';
        contentHTML = `${this.esc(ev.data.reason || 'rate_limited')} — attempt ${ev.data.attempt} of ${ev.data.maxAttempts}`;
        if (ev.data.delayMs) {
          contentHTML += `, retrying in ${ev.data.delayMs}ms`;
        }
        break;

      default:
        badge = '●';
        kindLabel = ev.kind;
        contentHTML = this.esc(JSON.stringify(ev.data).slice(0, 120));
    }

    return `<div class="waterfall-entry ${rowClass} ${borderClass}" data-agent-id="${this.esc(ev.agentId)}" data-event-id="${this.esc(ev.id)}">
      <span class="wf-time">${timeStr}</span>
      <div class="wf-agent"><span class="dot ${ev.agentColor}"></span> ${this.esc(ev.agentName)}</div>
      <span class="wf-badge">${badge}</span>
      <div class="wf-content">
        <div class="wf-title">
          <span class="wf-kind">${kindLabel}</span>
          ${contentHTML}
        </div>
      </div>
    </div>`;
  },

  // ── Build tool args display with truncation ───────────────
  buildToolArgsHTML(data) {
    if (!data.argsJson) return '';
    let args;
    try { args = JSON.parse(data.argsJson); } catch (_) { return ''; }
    if (!args || typeof args !== 'object') return '';

    const toolName = (data.name || '').toLowerCase();
    const parts = [];

    // Common tool: write / edit / subagent
    if (toolName === 'write') {
      if (args.path) parts.push(`📄 ${this.esc(this.truncate(args.path, 50))}`);
      if (args.overwrite !== undefined) parts.push(`overwrite=${args.overwrite}`);
      let contentLen = '';
      if (args.content) contentLen = `${args.content.length}B`;
      else if (args.body) contentLen = `${args.body.length}B`;
      if (contentLen) parts.push(`content=${contentLen}`);
    } else if (toolName === 'edit') {
      if (args.path) parts.push(`📄 ${this.esc(this.truncate(args.path, 50))}`);
      if (args.edits && Array.isArray(args.edits)) {
        parts.push(`${args.edits.length} edit(s)`);
      }
    } else if (toolName === 'subagent') {
      if (args.preset) parts.push(`preset=${this.esc(this.truncate(String(args.preset), 24))}`);
      let promptLen = '';
      if (args.prompt) promptLen = `${args.prompt.length}B`;
      if (promptLen) parts.push(`prompt=${promptLen}`);
      if (args.role) parts.push(`role=${this.esc(String(args.role))}`);
    } else {
      // Generic: show path and key scalar args
      if (args.path) parts.push(`→ ${this.esc(this.truncate(args.path, 60))}`);
      // Show up to 2 additional small scalar args
      const smallKeys = Object.keys(args).filter(k => k !== 'path' && k !== 'content' && k !== 'prompt' && k !== 'body' && k !== 'edits' && k !== 'argsJson');
      let shown = 0;
      for (const k of smallKeys) {
        if (shown >= 2) break;
        const v = args[k];
        if (typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean') {
          parts.push(`${k}=${this.esc(this.truncate(String(v), 30))}`);
          shown++;
        }
      }
    }

    if (parts.length === 0) return '';
    return parts.join(' · ');
  },

  // ── Bind timeline click handlers ─────────────────────────
  bindTimelineClicks() {
    document.querySelectorAll('.waterfall-entry[data-agent-id]').forEach(entry => {
      if (entry._bound) return;
      entry._bound = true;
      entry.style.cursor = 'pointer';
      entry.addEventListener('click', () => {
        const agentId = entry.dataset.agentId;
        if (!agentId) return;
        // Switch to dashboard view
        this.state.view = 'dashboard';
        document.querySelectorAll('#view-tabs .view-tab').forEach(t => t.classList.remove('active'));
        const dashTab = document.querySelector('#view-tabs .view-tab[data-view="dashboard"]');
        if (dashTab) dashTab.classList.add('active');
        this.switchView();
        // Highlight the agent card briefly
        setTimeout(() => {
          const card = document.querySelector(`.agent-card[data-agent-id="${CSS.escape(agentId)}"]`);
          if (card) {
            card.scrollIntoView({ behavior: 'smooth', block: 'center' });
            card.style.boxShadow = '0 0 0 2px var(--accent)';
            setTimeout(() => { card.style.boxShadow = ''; }, 2000);
          }
        }, 150);
      });
    });
  },

  // ── Update orchestrator pill ─────────────────────────────
  updateOrchPill() {
    const pill = document.getElementById('orch-pill');
    if (!pill) return;
    const count = this.state.agents.size;
    if (this.state.connected) {
      pill.textContent = `● ${count} agent${count !== 1 ? 's' : ''}`;
      pill.className = 'orch-pill live';
    } else {
      pill.textContent = count > 0 ? `○ ${count} agent${count !== 1 ? 's' : ''}` : '○ waiting';
      pill.className = 'orch-pill';
    }
  },

  // ── Update relative times ────────────────────────────────
  updateRelativeTimes() {
    if (this.state.view !== 'dashboard') return;
    document.querySelectorAll('.last-activity').forEach(el => {
      const card = el.closest('.agent-card');
      if (!card) return;
      const agentId = card.dataset.agentId;
      if (!agentId) return;
      const agent = this.state.agents.get(agentId);
      if (agent && agent.lastSeenAt) {
        el.textContent = this.timeAgo(agent.lastSeenAt);
      }
    });
  },

  // ── Helpers ──────────────────────────────────────────────

  // Normalize an agent from server JSON
  normalizeAgent(raw) {
    return {
      id: raw.id || '',
      name: raw.name || raw.id || '',
      apiUrl: raw.apiUrl || raw.api_url || '',
      workspace: raw.workspace || '',
      model: raw.model || '',
      role: raw.role || '',
      pid: raw.pid,
      status: raw.status || 'idle',
      registeredAt: raw.registeredAt || raw.registered_at || null,
      lastSeenAt: raw.lastSeenAt || raw.last_seen_at || new Date().toISOString(),
      messageCount: raw.messageCount ?? raw.message_count,
      turnCount: raw.turnCount ?? raw.turn_count,
      tokensIn: raw.tokensIn ?? raw.tokens_in,
      tokensOut: raw.tokensOut ?? raw.tokens_out,
      toolStats: raw.toolStats || raw.tool_stats || {},
      errorMessage: raw.errorMessage || raw.error_message || '',
      stats: raw.stats || {},
    };
  },

  // Agent colour key
  agentColor(agent) {
    const name = (agent.name || '').toLowerCase();
    if (name.includes('api') || name.includes('backend')) return 'api';
    if (name.includes('web') || name.includes('frontend') || name.includes('portal')) return 'web';
    if (name.includes('infra') || name.includes('deploy') || name.includes('ops')) return 'ops';
    if (name.includes('doc')) return 'docs';
    // Deterministic from name hash
    const colors = ['api', 'web', 'ops', 'docs'];
    let hash = 0;
    for (let i = 0; i < name.length; i++) hash = ((hash << 5) - hash) + name.charCodeAt(i);
    return colors[Math.abs(hash) % colors.length];
  },

  esc(str) {
    if (str == null) return '';
    const s = String(str);
    let out = '';
    for (let i = 0; i < s.length; i++) {
      const c = s[i];
      if (c === '&') out += '&amp;';
      else if (c === '<') out += '&lt;';
      else if (c === '>') out += '&gt;';
      else if (c === '"') out += '&quot;';
      else if (c === "'") out += '&#39;';
      else out += c;
    }
    return out;
  },

  timeAgo(iso) {
    if (!iso) return '—';
    const diff = Date.now() - new Date(iso).getTime();
    if (diff < 0) return 'now';
    const sec = Math.floor(diff / 1000);
    if (sec < 10) return 'now';
    if (sec < 60) return `${sec}s ago`;
    const min = Math.floor(sec / 60);
    if (min < 60) return `${min} min ago`;
    const hrs = Math.floor(min / 60);
    if (hrs < 24) return `${hrs}h ${min % 60}m ago`;
    const days = Math.floor(hrs / 24);
    return `${days}d ago`;
  },

  uptime(iso) {
    if (!iso) return '—';
    const diff = Date.now() - new Date(iso).getTime();
    if (diff < 0) return '0m';
    const min = Math.floor(diff / 60_000);
    if (min < 60) return `${min}m`;
    const hrs = Math.floor(min / 60);
    if (hrs < 24) return `${hrs}h ${min % 60}m`;
    const days = Math.floor(hrs / 24);
    return `${days}d ${hrs % 24}h`;
  },

  fmtNum(n) {
    if (n == null) return '—';
    const num = Number(n);
    if (num >= 1_000_000) return (num / 1_000_000).toFixed(num % 1_000_000 === 0 ? 0 : 1) + 'M';
    if (num >= 1000) return (num / 1000).toFixed(num % 1000 === 0 ? 0 : 1) + 'k';
    return String(num);
  },

  fmtTime(d) {
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  },

  fmtTimeFull(d) {
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  },

  truncate(s, max) {
    if (!s) return '';
    if (s.length <= max) return s;
    return s.slice(0, max - 1) + '…';
  },

  // snippet returns first N + last N chars joined by "…"
  snippet(s, n) {
    if (!s) return '';
    if (s.length <= n * 2 + 2) return s;
    return s.slice(0, n) + ' … ' + s.slice(-n);
  },
};

// ── Boot ───────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', () => App.init());
