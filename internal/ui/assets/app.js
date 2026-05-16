// ── franky orchestrator — app.js ───────────────────────────
// Vanilla JS SPA: dashboard grid + waterfall timeline, live SSE updates.

const App = {

  // ── Persistence keys ─────────────────────────────────────
  STORAGE_KEY: 'franky_orchestrator_events',
  STORAGE_MAX_EVENTS: 2000,

  // ── State ────────────────────────────────────────────────
  state: {
    agents: new Map(),           // id → agent object
    view: 'dashboard',           // 'dashboard' | 'timeline'
    filter: 'all',               // 'all' | 'idle' | 'streaming' | 'error' | 'offline'
    search: '',                  // search input value
    timelineEvents: [],          // [{id, agentId, kind, time, data}]
    timelineAgentFilter: 'all',     // 'all' or a set-like string of agent IDs joined by ','
    timelineEventFilter: 'all',     // 'all' | 'user' | 'assistant' | 'reasoning' | 'tools' | 'errors'
    pendingMessages: new Map(),  // agentId -> { role, text, startTime, startEvent }
    pendingTools: new Map(),     // callId -> startEvent
    connected: false,
  },

  // ── Init ─────────────────────────────────────────────────
  init() {
    this.bindUI();
    this.connectSSE();
    this.loadInitialState();
    this.loadPersistedEvents();
    this.tickInterval = setInterval(() => this.updateRelativeTimes(), 30_000);
    this.cleanupInterval = setInterval(() => this.cleanupStaleBuffers(), 300_000);
    this._eventsVersion = 0;
    this._newItemsAboveViewport = 0;
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

    // Timeline: agent tags (delegated)
    document.getElementById('agent-tags')?.addEventListener('click', (e) => {
      const tag = e.target.closest('.agent-tag');
      if (!tag) return;
      const agentId = tag.dataset.agentId;
      this.setTimelineAgentFilter(agentId);
    });

    // Timeline: event type filters (delegated)
    document.getElementById('event-filters')?.addEventListener('click', (e) => {
      const btn = e.target.closest('.event-btn');
      if (!btn) return;
      const filter = btn.dataset.filter;
      this.setTimelineEventFilter(filter);
    });

    // Add Agent modal
    const addBtn = document.getElementById('add-agent-btn');
    const modal = document.getElementById('add-agent-modal');
    const backdrop = modal?.querySelector('.modal-backdrop');
    const cancelBtn = document.getElementById('add-agent-cancel');
    const form = document.getElementById('add-agent-form');
    const nameInput = document.getElementById('agent-name-input');
    const hostInput = document.getElementById('agent-host-input');

    if (addBtn && modal) {
      addBtn.addEventListener('click', () => {
        modal.classList.remove('hidden');
        nameInput.value = '';
        hostInput.value = '';
        nameInput.focus();
      });

      const closeModal = () => modal.classList.add('hidden');

      if (backdrop) backdrop.addEventListener('click', closeModal);
      if (cancelBtn) cancelBtn.addEventListener('click', closeModal);

      if (form) {
        form.addEventListener('submit', async (e) => {
          e.preventDefault();
          const name = nameInput.value.trim();
          const host = hostInput.value.trim();
          if (!name || !host) return;

          const submitBtn = document.getElementById('add-agent-submit');
          submitBtn.disabled = true;
          submitBtn.textContent = 'Adding…';

          try {
            const res = await fetch('/agents', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ name, host }),
            });
            if (!res.ok) {
              const err = await res.json().catch(() => ({ error: res.statusText }));
              throw new Error(err.error || 'Failed to add agent');
            }
            closeModal();
            // Re-fetch agents to get the full updated list
            await this.loadInitialState();
          } catch (err) {
            alert(`Failed to add agent: ${err.message}`);
          } finally {
            submitBtn.disabled = false;
            submitBtn.textContent = 'Add Agent';
          }
        });
      }

      // Close on Escape
      document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape' && !modal.classList.contains('hidden')) {
          closeModal();
        }
      });
    }
  },

  // ── Switch view ──────────────────────────────────────────
  switchView() {
    const grid = document.getElementById('agent-grid');
    const timelineContainer = document.getElementById('timeline-container');
    const empty = document.getElementById('empty-state');

    if (this.state.view === 'dashboard') {
      grid.classList.remove('hidden');
      timelineContainer.classList.add('hidden');
      this._itemsCache = null;
      this.renderDashboard();
    } else {
      grid.classList.add('hidden');
      timelineContainer.classList.remove('hidden');
      if (empty) empty.classList.add('hidden');
      // Reset the waterfall element
      const waterfall = document.getElementById('waterfall');
      waterfall.style.display = '';
      waterfall.style.flexDirection = '';
      waterfall.style.maxWidth = '';
      waterfall.style.margin = '';
      waterfall.style.padding = '';
      waterfall.style.height = '';
      waterfall.style.overflow = '';
      this._prevEventsVersion = -1;
      this._newItemsAboveViewport = 0;
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
      const agentId = data.agentId;

      // Release orphaned event data for this agent (Item 7: memory optimization)
      // Setting data to null frees the heaviest part (tool args, results, prompts)
      // while preserving the event structure for the remaining visible items.
      for (const ev of this.state.timelineEvents) {
        if (ev.agentId === agentId) {
          ev.data = null;
        }
      }
      // Also clean up pending buffers for this agent
      this.state.pendingMessages.delete(agentId);
      for (const [callId, startEv] of this.state.pendingTools.entries()) {
        if (startEv.agentId === agentId) {
          this.state.pendingTools.delete(callId);
        }
      }

      this.state.agents.delete(agentId);
      this.renderDashboard();
      this.updateOrchPill();
      this._eventsVersion++;
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
      'turn_start', 'turn_end', 'message_start', 'message_end',
      'tool_execution_start', 'tool_execution_end',
      'agent_error', 'agent_interrupted', 'provider_retry',
    ];
    // message_update is excluded from timelineEvents because it is
    // handled directly by the pendingMsgs buffer in buildTimelineItems.
    // Storing raw message_update events would flood the array, pushing
    // out older message_start / tool_execution_start events that are
    // needed for pairing, causing visible events to disappear.

    // ── Register handlers for tracked event types ──────────
    for (const kind of timelineKinds) {
      es.addEventListener(kind, (e) => {
        const data = JSON.parse(e.data);
        const agentId = data.agentId || data.id;
        const agent = agentId ? this.state.agents.get(agentId) : null;

        // Truncate large string payloads before storing (Item 5: memory optimization)
        this.truncatePayloads(data);

        this.state.timelineEvents.push({
          id: `ev-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
          agentId: agentId || 'unknown',
          kind,
          time: data.time || new Date().toISOString(),
          data,
        });

        // Cap at 2000 events to prevent unbounded memory growth
        if (this.state.timelineEvents.length > 2000) {
          this.state.timelineEvents.splice(0, this.state.timelineEvents.length - 2000);
        }

        // Persist to localStorage so events survive a page refresh
        this.persistEvents();

        // Bump version to invalidate cached timeline items for virtual scroll
        this._eventsVersion++;

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
          // Check if new item will be above the current viewport
          // (newest-first sort, so index 0 is the newest event)
          const entriesDiv = document.getElementById('vf-entries');
          if (entriesDiv && entriesDiv.scrollTop > 60) {
            this._newItemsAboveViewport++;
          }
          this.renderTimeline();
        }
      });
    }

    // ── message_update: streamed into a real-time buffer, never stored as raw events ──
    // This prevents flooding the timelineEvents array with thousands of delta frames
    // that would push out the start/end markers needed for pairing.
    es.addEventListener('message_update', (e) => {
      const data = JSON.parse(e.data);
      const agentId = data.agentId;
      if (!agentId) return;

      let buf = this.state.pendingMessages.get(agentId);
      if (!buf) {
        // If no buffer yet (no message_start stored), create one from context.
        // Infer role from deltaKind: thinking always means assistant.
        let role = data.role || 'unknown';
        if (data.deltaKind === 'thinking' || data.deltaKind === 'toolcall_args') {
          role = 'assistant';
        }
        buf = { role, text: '', thinking: '', startTime: data.time || new Date().toISOString(), startEvent: null, hasThinking: false };
        this.state.pendingMessages.set(agentId, buf);
      }

      // Accumulate deltas based on deltaKind
      if (data.deltaKind === 'text') {
        // Some agents send incremental text deltas
        buf.text += (data.delta || '');
      } else if (data.deltaKind === 'thinking') {
        // Thinking/reasoning deltas — accumulate separately
        buf.thinking += (data.delta || '');
        buf.hasThinking = true;
      } else if (data.deltaKind === 'toolcall_args') {
        // Tool call args — also append to text for visibility
        // (the agent may include this in message_end text too)
        buf.text += (data.delta || '');
      } else if (data.text) {
        // Other agents send the full text on each update
        buf.text = data.text;
      }

      // Also update agent last-seen
      const agent = this.state.agents.get(agentId);
      if (agent) {
        agent.lastSeenAt = data.time || new Date().toISOString();
      }

      // Throttle re-renders during streaming via requestAnimationFrame
      // Bump events version so cached display items incorporate new delta text
      this._eventsVersion++;
      if (this.state.view === 'timeline') {
        if (!this._pendingTimelineRender) {
          this._pendingTimelineRender = true;
          requestAnimationFrame(() => {
            this._pendingTimelineRender = false;
            this.renderTimeline();
          });
        }
      }
    });

    // ── agent_usage: updates live counters on the dashboard ──
    es.addEventListener('agent_usage', (e) => {
      const data = JSON.parse(e.data);
      const agent = this.state.agents.get(data.agentId);
      if (agent) {
        agent.messageCount = data.messageCount ?? agent.messageCount;
        agent.turnCount = data.turnCount ?? agent.turnCount;
        agent.tokensIn = data.tokensIn ?? agent.tokensIn;
        agent.tokensOut = data.tokensOut ?? agent.tokensOut;
        agent.toolStats = data.toolStats || agent.toolStats;
        if (this.state.view === 'dashboard') {
          this.renderDashboard();
        }
      }
    });

    // ── agent_updated: refreshes all metadata for an agent (name, model, workspace, role) ──
    es.addEventListener('agent_updated', (e) => {
      const data = JSON.parse(e.data);
      const raw = data.agent;
      if (!raw || !raw.id) return;
      const agent = this.state.agents.get(raw.id);
      if (agent) {
        agent.name = raw.name || agent.name;
        agent.workspace = raw.workspace || agent.workspace;
        agent.model = raw.model || agent.model;
        agent.role = raw.role || agent.role;
        agent.status = raw.status || agent.status;
        if (this.state.view === 'dashboard') {
          this.renderDashboard();
        }
      } else {
        // Unknown agent — add it
        const a = this.normalizeAgent(raw);
        this.state.agents.set(a.id, a);
        this.renderDashboard();
        this.updateOrchPill();
      }
    });

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

    // Last seen / active
    const lastActivity = a.lastSeenAt ? this.timeAgo(a.lastSeenAt) : '—';

    // Workspace → project (last path component only)
    const workspace = a.workspace || '';
    const project = workspace && workspace !== '—' ? workspace.split('/').pop() || workspace : '—';

    // Model
    const model = a.model || '—';

    // Profile (use a.profile, otherwise infer from model/name)
    const profile = a.profile || (model.includes('deepseek') ? 'ollama' : (model.includes('gemini') ? 'gemini' : (model.includes('claude') ? 'anthropic' : 'ollama')));

    // Compute token total
    const tokensInNum = a.tokensIn != null ? a.tokensIn : 0;
    const tokensOutNum = a.tokensOut != null ? a.tokensOut : 0;
    const totalTokens = tokensInNum + tokensOutNum;
    const tokensLabel = totalTokens >= 1000 ? (totalTokens >= 1000000 ? (totalTokens / 1000000).toFixed(1) + 'M' : (totalTokens / 1000).toFixed(0) + 'K') : String(totalTokens);

    // Cost (placeholder — no real cost data yet)
    const cost = '$0.00';

    // Tool stats
    const toolStats = a.toolStats || {};
    const toolNames = Object.keys(toolStats);
    const toolCount = toolNames.length;
    const toolCalls = toolNames.reduce((sum, k) => sum + (toolStats[k] || 0), 0);

    // Error count
    const errCount = (a.errorMessage && status === 'error') ? 1 : 0;

    // Cache hit rate (placeholder)
    const cachePct = '38%';

    // Profile accent colour — pick a hue based on profile name
    let accentColor = 'rgba(139,92,246,0.3)';
    let textColor = 'var(--accent)';
    if (profile.includes('gemini')) {
      accentColor = 'rgba(66,133,244,0.3)';
      textColor = '#4285f4';
    } else if (profile.includes('anthropic') || profile.includes('claude')) {
      accentColor = 'rgba(200,140,80,0.3)';
      textColor = '#d4a05a';
    } else if (profile.includes('openrouter')) {
      accentColor = 'rgba(255,107,53,0.3)';
      textColor = '#ff6b35';
    }

    const cardClass = isOffline ? 'agent-card offline-card' : 'agent-card';

    return `<div class="${cardClass}" data-agent-id="${this.esc(a.id)}">
      <div class="agent-card-head">
        <span class="agent-status-dot ${status}"></span>
        <span class="agent-name">${this.esc(a.name || a.id)}</span>
        <span class="agent-profile-accent" style="border-color:${accentColor};color:${textColor};background:rgba(139,92,246,0.08);">
          ${this.esc(profile)}
        </span>
      </div>
      <div class="agent-stats">
        <div class="stat"><span class="val">${this.esc(String(a.turnCount != null ? a.turnCount : (a.messageCount != null ? a.messageCount : '—'))) }</span> <span class="lbl">sessions</span></div>
        <div class="stat"><span class="val">${tokensLabel}</span> <span class="lbl">tokens</span></div>
        <div class="stat"><span class="val">${cost}</span> <span class="lbl">cost</span></div>
      </div>
      <div class="agent-tools">
        <span class="tool-badge"><span class="count">${toolCount}</span> tools</span>
        <span class="tool-badge"><span class="count">${toolCalls}</span> tool calls</span>
        <span class="tool-badge"><span class="count">${errCount}</span> errors</span>
      </div>
      <div class="agent-meta-row" style="display:flex;gap:12px;font-size:10px;color:var(--text-dim);margin-bottom:8px;">
        <span>Project: <strong style="color:var(--text);">${this.esc(project)}</strong></span>
        <span>Model: <strong style="color:var(--text);">${this.esc(model)}</strong></span>
        <span>Cache: <strong style="color:var(--success);">${cachePct}</strong></span>
      </div>
      <div class="agent-footer">
        <span class="agent-last-seen">Last active: ${lastActivity}</span>
        <div class="agent-actions">
          <button data-action="trace" data-id="${this.esc(a.id)}">Trace</button>
          <button class="primary" data-action="inspect" data-id="${this.esc(a.id)}">Inspect</button>
        </div>
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
          case 'inspect':
            if (agent.apiUrl) {
              window.open(agent.apiUrl, '_blank');
            }
            break;
          case 'remove':
            this.removeAgent(agent);
            break;
          case 'trace':
            // Switch to timeline view focused on this agent
            this.state.view = 'timeline';
            document.querySelectorAll('.view-tab').forEach(t => t.classList.remove('active'));
            document.querySelector('.view-tab[data-view="timeline"]')?.classList.add('active');
            document.getElementById('dashboard-grid')?.classList.add('hidden');
            document.getElementById('timeline-container')?.classList.remove('hidden');
            this.renderTimeline();
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

  // ── Render timeline (all items rendered naturally) ───────
  renderTimeline() {
    const waterfall = document.getElementById('waterfall');
    const empty = document.getElementById('empty-state');
    const grid = document.getElementById('agent-grid');

    if (this.state.view !== 'timeline') return;
    grid.classList.add('hidden');
    if (empty) empty.classList.add('hidden');

    // Render the timeline toolbar (agent tags + event filters)
    this.renderTimelineToolbar();

    const rawEvents = this.state.timelineEvents;

    if (rawEvents.length === 0) {
      waterfall.innerHTML = `<div class="empty-state">
        <h2>No events yet</h2>
        <p>Activity will appear here as agents work.</p>
      </div>`;
      this._itemsCache = null;
      return;
    }

    // Build display items (cached via version check for performance)
    const items = this.getOrBuildItems(rawEvents);

    if (items.length === 0) {
      waterfall.innerHTML = `<div class="empty-state">
        <h2>No events yet</h2>
        <p>Activity will appear here as agents work.</p>
      </div>`;
      this._itemsCache = null;
      return;
    }
    this._itemsCache = items;

    // ── Build waterfall layout (idempotent) ──────────────
    waterfall.innerHTML = '';
    waterfall.style.display = 'flex';
    waterfall.style.flexDirection = 'column';
    waterfall.style.height = '100%';
    waterfall.style.overflow = 'hidden';

    // ── Compute aggregate stats ──────────────────────────────
    const times = items.map(it => new Date(it.time));
    const minTime = new Date(Math.min(...times));
    const maxTime = new Date(Math.max(...times));
    if (maxTime - minTime < 60_000) maxTime.setTime(minTime.getTime() + 60_000);

    // Duration
    const durationMs = maxTime - minTime;
    const durationMin = durationMs / 60000;
    let durationStr;
    if (durationMin >= 1) {
      durationStr = durationMin < 10 ? durationMin.toFixed(1) + 'm' : Math.round(durationMin) + 'm';
    } else {
      durationStr = Math.round(durationMs / 1000) + 's';
    }

    // Tool calls
    const toolCount = items.filter(it => it.kind === 'tool_merged' || it.kind === 'tool_running').length;

    // Reasoning steps (message_merged count)
    const reasoningCount = items.filter(it => it.kind === 'message_merged').length;

    // User turns (from raw events)
    const turnCount = rawEvents.filter(it => it.kind === 'turn_start').length;

    // Retries
    const retryCount = items.filter(it => it.kind === 'provider_retry').length;

    // Tokens — aggregate from all agents
    let totalTokens = 0;
    for (const agent of this.state.agents.values()) {
      const ti = agent.tokensIn != null ? agent.tokensIn : 0;
      const to = agent.tokensOut != null ? agent.tokensOut : 0;
      totalTokens += ti + to;
    }
    let tokensStr;
    if (totalTokens >= 1000000) {
      tokensStr = (totalTokens / 1000000).toFixed(1) + 'M';
    } else if (totalTokens >= 1000) {
      tokensStr = (totalTokens / 1000).toFixed(0) + 'K';
    } else {
      tokensStr = String(totalTokens);
    }

    // Cost (rough estimate: $0.15/M input, $0.60/M output per million tokens)
    let totalCost = 0;
    for (const agent of this.state.agents.values()) {
      const ti = agent.tokensIn != null ? agent.tokensIn : 0;
      const to = agent.tokensOut != null ? agent.tokensOut : 0;
      totalCost += (ti / 1_000_000) * 0.15 + (to / 1_000_000) * 0.60;
    }
    const costStr = '$' + (totalCost < 0.01 ? '<0.01' : totalCost.toFixed(2));

    // ── Stats bar (above time axis) ─────────────────────────
    const statsDiv = document.createElement('div');
    statsDiv.className = 'wf-stats';
    statsDiv.innerHTML = `
      <span class="wf-stat-pill">⏱ Duration <span class="val accent">${this.esc(durationStr)}</span></span>
      <span class="wf-stat-pill">🔧 Tool calls <span class="val">${toolCount}</span></span>
      <span class="wf-stat-pill">🧠 Reasoning steps <span class="val">${reasoningCount}</span></span>
      <span class="wf-stat-pill">👤 User turns <span class="val">${turnCount}</span></span>
      <span class="wf-stat-pill">⚠️ Retries <span class="val error">${retryCount}</span></span>
      <span class="wf-stat-pill">📊 Tokens <span class="val">${this.esc(tokensStr)}</span></span>
      <span class="wf-stat-pill">💰 Cost <span class="val success">${this.esc(costStr)}</span></span>
    `;
    waterfall.appendChild(statsDiv);

    // Time axis (sticky top — always visible as sibling of scroll container)
    const tickCount = 5;
    const range = maxTime - minTime;
    const step = range / (tickCount - 1);
    let timeAxisHTML = '';
    for (let i = 0; i < tickCount; i++) {
      timeAxisHTML += `<span class="tick">${this.fmtTime(new Date(maxTime.getTime() - step * i))}</span>`;
    }
    const timeAxis = document.createElement('div');
    timeAxis.className = 'time-axis';
    timeAxis.id = 'vf-time-axis';
    timeAxis.innerHTML = timeAxisHTML;
    waterfall.appendChild(timeAxis);

    // New-events banner (sticky, hidden by default)
    const newEvBanner = document.createElement('div');
    newEvBanner.className = 'wf-new-events-banner hidden';
    newEvBanner.id = 'wf-new-events-banner';
    newEvBanner.addEventListener('click', () => {
      this.scrollToTop();
    });
    waterfall.appendChild(newEvBanner);

    // Scrollable entries container (flex:1 — all items rendered naturally)
    const entriesDiv = document.createElement('div');
    entriesDiv.className = 'vf-entries';
    entriesDiv.id = 'vf-entries';
    let entriesHTML = '';
    for (let i = 0; i < items.length; i++) {
      entriesHTML += this.buildTimelineEntry(items[i], i > 0 ? items[i - 1] : null);
    }
    entriesDiv.innerHTML = entriesHTML;
    waterfall.appendChild(entriesDiv);

    // Scroll handler for new-events banner auto-dismiss
    entriesDiv.addEventListener('scroll', () => {
      this.updateNewEventsBanner();
    }, { passive: true });

    // Bind click handlers
    this.bindTimelineClicks();
  },

  // ── Timeline toolbar render ──────────────────────────
  renderTimelineToolbar() {
    const container = document.getElementById('agent-tags');
    if (!container) return;

    const agents = Array.from(this.state.agents.values());

    let html = `<span class="agent-tag ${this.state.timelineAgentFilter === 'all' ? 'active' : ''}" data-agent-id="all"><span class="dot" style="background:var(--accent);"></span> All</span>`;

    for (const agent of agents) {
      const isActive = this.state.timelineAgentFilter === agent.id;
      const dotColor = this.agentColor(agent);
      html += `<span class="agent-tag ${isActive ? 'active' : ''}" data-agent-id="${this.esc(agent.id)}"><span class="dot" style="background:${dotColor};"></span> ${this.esc(agent.name || agent.id)}</span>`;
    }

    container.innerHTML = html;
  },

  // ── Set timeline agent filter ──────────────────────────
  setTimelineAgentFilter(agentId) {
    if (agentId === 'all') {
      this.state.timelineAgentFilter = 'all';
    } else {
      this.state.timelineAgentFilter = agentId;
    }
    this.renderTimelineToolbar();
    this._eventsVersion++; // invalidate cache
    this.renderTimeline();
  },

  // ── Set timeline event filter ──────────────────────────
  setTimelineEventFilter(filter) {
    this.state.timelineEventFilter = filter;
    // Update active state on buttons
    document.querySelectorAll('#event-filters .event-btn').forEach(b => {
      b.classList.toggle('active', b.dataset.filter === filter);
    });
    this._eventsVersion++; // invalidate cache
    this.renderTimeline();
  },

  // ── Apply filters to items ─────────────────────────────
  applyTimelineFilters(items) {
    // Agent filter
    if (this.state.timelineAgentFilter !== 'all') {
      items = items.filter(it => it.agentId === this.state.timelineAgentFilter);
    }

    // Event type filter
    const ef = this.state.timelineEventFilter;
    if (ef !== 'all') {
      switch (ef) {
        case 'user':
          items = items.filter(it => it.kind === 'message_merged' && it.data && it.data.role === 'user');
          break;
        case 'assistant':
          items = items.filter(it => it.kind === 'message_merged' && it.data && it.data.role === 'assistant');
          break;
        case 'reasoning':
          items = items.filter(it => it.kind === 'message_merged');
          break;
        case 'tools':
          items = items.filter(it => it.kind === 'tool_merged' || it.kind === 'tool_running');
          break;
        case 'errors':
          items = items.filter(it => it.kind === 'agent_error' || it.kind === 'agent_interrupted' || it.kind === 'provider_retry');
          break;
      }
    }

    return items;
  },

  // ── Get or build timeline items with caching ─────────────
  // Avoids re-building the display item list on every re-render
  // when the underlying raw events haven't changed.
  getOrBuildItems(rawEvents) {
    if (this._itemsCache && this._prevEventsVersion === this._eventsVersion) {
      return this._itemsCache;
    }
    this._prevEventsVersion = this._eventsVersion;
    const built = this.buildTimelineItems(rawEvents);
    this._itemsCache = this.applyTimelineFilters(built);
    return this._itemsCache;
  },

  // ── New events banner — shown when items arrive above viewport ──
  // Auto-dismiss if user scrolled to the top on their own.
  updateNewEventsBanner() {
    const banner = document.getElementById('wf-new-events-banner');
    if (!banner) return;
    const entriesDiv = document.getElementById('vf-entries');
    if (entriesDiv && entriesDiv.scrollTop < 52) {
      this._newItemsAboveViewport = 0;
    }
    if (this._newItemsAboveViewport > 0) {
      banner.textContent = `⬆ ${this._newItemsAboveViewport} new event${this._newItemsAboveViewport !== 1 ? 's' : ''} — click to view`;
      banner.classList.remove('hidden');
    } else {
      banner.classList.add('hidden');
    }
  },

  // ── Scroll timeline to top and reset new-event count ─────
  scrollToTop() {
    this._newItemsAboveViewport = 0;
    const banner = document.getElementById('wf-new-events-banner');
    if (banner) banner.classList.add('hidden');
    const entriesDiv = document.getElementById('vf-entries');
    if (entriesDiv) {
      entriesDiv.scrollTop = 0;
    }
  },

  // ── Build display items from raw SSE events ───────────────
  buildTimelineItems(events) {
    const items = [];
    let a;
    // Use state-level buffers so live message_update deltas (handled by the
    // dedicated SSE listener) are reflected even though raw message_update
    // events are never pushed into timelineEvents.
    const pendingMsgs = this.state.pendingMessages;
    const pendingTools = this.state.pendingTools;

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
              callId: start.data.callId || ev.data.callId,
              name: start.data.name,
              isError: ev.data.isError,
              resultText: ev.data.resultText,
              argsJson: start.data.argsJson,
              path: start.data.path,
            },
            agentName: (a = this.state.agents.get(ev.agentId)) ? a.name : (ev.agentId || 'unknown'),
            agentColor: (a = this.state.agents.get(ev.agentId)) ? this.agentColor(a) : 'hsl(210, 60%, 55%)',
          });
        } else {
          // orphan end — still show with whatever we have
          items.push({ ...ev, kind: 'tool_merged', time: ev.time, endTime: ev.time, durationMs: 0, durationSec: 0 });
        }
      } else if (ev.kind === 'message_start') {
        // Flush any existing pending message for this agent only if it came
        // from a DIFFERENT message_start event (orphan cleanup — the previous
        // message_end was never received).  We detect this by comparing the
        // startEvent reference: if the buffer's startEvent is the same object
        // as the current event, the buffer was already set up for this message
        // (by the live message_update handler or a prior build pass) and we
        // must NOT flush or replace it — doing so would lose accumulated text.
        const oldBuf = pendingMsgs.get(ev.agentId);
        if (oldBuf && oldBuf.startEvent !== ev && (oldBuf.text.trim() || oldBuf.thinking.trim() || (Date.now() - new Date(oldBuf.startTime).getTime()) > 1000)) {
          // Build text that includes thinking prefix if present
          let displayText = oldBuf.text;
          if (oldBuf.hasThinking && oldBuf.thinking.trim()) {
            displayText = '🧠 ' + oldBuf.thinking.trimEnd() + '\n\n' + displayText;
          }
          items.push({
            id: `msg-flushed-${ev.agentId}`,
            agentId: ev.agentId,
            kind: 'message_merged',
            time: oldBuf.startTime,
            endTime: ev.time,
            durationMs: new Date(ev.time) - new Date(oldBuf.startTime),
            durationSec: Math.round((new Date(ev.time) - new Date(oldBuf.startTime)) / 1000),
            data: { role: oldBuf.role, text: displayText },
            agentName: (a = this.state.agents.get(oldBuf.agentId || ev.agentId)) ? a.name : (oldBuf.agentId || ev.agentId || 'unknown'),
            agentColor: (a = this.state.agents.get(oldBuf.agentId || ev.agentId)) ? this.agentColor(a) : 'hsl(210, 60%, 55%)',
          });
        }
        // Only create a new buffer if one doesn't already exist.
        // The live message_update handler may have already set one up,
        // in which case we must keep its accumulated content intact
        // so message_end can later produce a valid message_merged item.
        if (!pendingMsgs.has(ev.agentId)) {
          pendingMsgs.set(ev.agentId, {
            role: ev.data.role || 'unknown',
            text: ev.data.text || '',
            thinking: '',
            hasThinking: false,
            startTime: ev.time,
            startEvent: ev,
          });
        }
      } else if (ev.kind === 'message_end') {
        const buf = pendingMsgs.get(ev.agentId);
        if (buf) {
          pendingMsgs.delete(ev.agentId);
          // If the buffer has no text (no message_update deltas received),
          // fall back to the text field from the message_end event itself.
          // This handles agents that send the full message text in the end
          // event rather than streaming it via message_update deltas.
          const text = buf.text || ev.data.text || '';
          // Build display text that includes thinking prefix if present
          let displayText = text;
          if (buf.hasThinking && buf.thinking.trim()) {
            displayText = '🧠 ' + buf.thinking.trimEnd() + '\n\n' + displayText;
          }
          // Use the role from the end event if available (it's more
          // authoritative than the role set during message_update)
          const role = ev.data.role || buf.role || 'unknown';
          const durationMs = new Date(ev.time) - new Date(buf.startTime);
          const durationSec = Math.round(durationMs / 1000);
          // Only emit if there's text or the message ran for a notable time
          if (displayText.trim() || durationSec >= 1) {
            items.push({
              id: ev.id,
              agentId: ev.agentId,
              kind: 'message_merged',
              time: buf.startTime,
              endTime: ev.time,
              durationMs,
              durationSec,
              data: { role, text: displayText },
              agentName: (a = this.state.agents.get(ev.agentId)) ? a.name : (ev.agentId || 'unknown'),
              agentColor: (a = this.state.agents.get(ev.agentId)) ? this.agentColor(a) : 'hsl(210, 60%, 55%)',
            });
          }
        } else if (ev.data && ev.data.text) {
          // Orphan message_end: no prior message_start / message_update.
          // Create a standalone message_merged entry from the end event data.
          const durationSec = ev.durationSec || 0;
          items.push({
            id: ev.id,
            agentId: ev.agentId,
            kind: 'message_merged',
            time: ev.time,
            endTime: ev.time,
            durationMs: durationSec * 1000,
            durationSec,
            data: { role: ev.data.role || 'assistant', text: ev.data.text },
            agentName: (a = this.state.agents.get(ev.agentId)) ? a.name : (ev.agentId || 'unknown'),
            agentColor: (a = this.state.agents.get(ev.agentId)) ? this.agentColor(a) : 'hsl(210, 60%, 55%)',
          });
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
      const hasContent = buf.text.trim() || buf.thinking.trim() || (Date.now() - new Date(buf.startTime).getTime()) > 1000;
      if (hasContent) {
        // Build display text that includes thinking prefix if present
        let displayText = buf.text;
        if (buf.hasThinking && buf.thinking.trim()) {
          displayText = '🧠 ' + buf.thinking.trimEnd() + '\n\n' + displayText;
        }
        items.push({
          id: `msg-running-${agentId}`,
          agentId,
          kind: 'message_merged',
          time: buf.startTime,
          endTime: new Date().toISOString(),
          durationMs: Date.now() - new Date(buf.startTime).getTime(),
          durationSec: Math.round((Date.now() - new Date(buf.startTime).getTime()) / 1000),
          data: { role: buf.role, text: displayText },
          agentName: (a = this.state.agents.get(agentId)) ? a.name : (agentId || 'unknown'),
          agentColor: (a = this.state.agents.get(agentId)) ? this.agentColor(a) : 'hsl(210, 60%, 55%)',
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
    // Look up agent metadata from central store instead of per-event fields
    const _agent = this.state.agents.get(ev.agentId);
    const agentName = _agent ? _agent.name : (ev.agentId || 'unknown');
    const agentColor = _agent ? this.agentColor(_agent) : 'hsl(210, 60%, 55%)';
    const borderStyle = `border-left-color:${agentColor};`;

    // Build event hash for deep-linking into the agent UI
    const eventHash = this.buildEventHash(ev);

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
        const rawMsg = ev.data.message || ev.data.code || 'Unknown error';
        const MAX_ERROR_PREVIEW = 200;
        if (rawMsg.length > MAX_ERROR_PREVIEW) {
          const snippet = this.esc(rawMsg.slice(0, MAX_ERROR_PREVIEW));
          const rest = this.esc(rawMsg.slice(MAX_ERROR_PREVIEW));
          contentHTML = `<span class="wf-error-preview">${snippet}</span>`;
          contentHTML += `<span class="wf-error-toggle" onclick="this.nextElementSibling.classList.toggle('wf-error-expanded');this.textContent=this.nextElementSibling.classList.contains('wf-error-expanded')?'▲ collapse':'▼ expand'">▼ expand</span>`;
          contentHTML += `<pre class="wf-error-rest">${rest}</pre>`;
        } else {
          contentHTML = this.esc(rawMsg);
        }
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

    return `<div class="waterfall-entry ${rowClass}" style="${borderStyle}" data-agent-id="${this.esc(ev.agentId)}" data-event-id="${this.esc(ev.id)}" data-kind="${this.esc(ev.kind)}" data-event-hash="${this.esc(eventHash)}">
      <span class="wf-time">${timeStr}</span>
      <div class="wf-agent"><span class="dot" style="background:${agentColor};"></span> ${this.esc(agentName)}</div>
      <div class="wf-content">
        <div class="wf-title">
          <span class="wf-kind">${kindLabel}</span>
          ${contentHTML}
        </div>
      </div>
      <span class="wf-hint">↗ open in agent</span>
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
        const eventHash = entry.dataset.eventHash;
        const kind = entry.dataset.kind;
        if (!agentId) return;

        const agent = this.state.agents.get(agentId);
        if (agent && agent.apiUrl) {
          // Open the agent's own web UI with a hash fragment pointing to the event
          let hash = '';
          if (eventHash) {
            hash = '#' + eventHash;
          }
          // If the event is a tool call, add query param to hint the view
          const isTool = (kind === 'tool_merged' || kind === 'tool_running');
          const isMsg = (kind === 'message_merged');
          let agentUrl = agent.apiUrl;
          if (isTool) {
            agentUrl += '/' + hash;
          } else if (isMsg) {
            agentUrl += '/' + hash;
          } else {
            agentUrl += hash;
          }
          window.open(agentUrl, '_blank');
        } else {
          // Fallback: if no apiUrl, just highlight in dashboard
          this.state.view = 'dashboard';
          document.querySelectorAll('#view-tabs .view-tab').forEach(t => t.classList.remove('active'));
          const dashTab = document.querySelector('#view-tabs .view-tab[data-view="dashboard"]');
          if (dashTab) dashTab.classList.add('active');
          this.switchView();
          setTimeout(() => {
            const card = document.querySelector(`.agent-card[data-agent-id="${CSS.escape(agentId)}"]`);
            if (card) {
              card.scrollIntoView({ behavior: 'smooth', block: 'center' });
              card.style.boxShadow = '0 0 0 2px var(--accent)';
              setTimeout(() => { card.style.boxShadow = ''; }, 2000);
            }
          }, 150);
        }
      });
    });
  },

  // ── Build event hash for deep-linking ───────────────────-
  // Generates a stable hash fragment that identifies the specific event
  // in the agent's own web UI.
  buildEventHash(ev) {
    const kind = ev.kind;

    if (kind === 'tool_merged' || kind === 'tool_running') {
      // Use callId if available, otherwise generate from name+time
      const callId = ev.data && ev.data.callId;
      if (callId) return `tool_${callId}`;
      const name = (ev.data && ev.data.name) || 'unknown';
      // Fallback: tool_{name}-{timestamp}
      const ts = ev.time ? new Date(ev.time).getTime() : Date.now();
      return `tool_${name}-${ts}`;
    }

    if (kind === 'message_merged') {
      // Use agent+timestamp to identify the message
      const ts = ev.time ? new Date(ev.time).getTime() : Date.now();
      return `msg-${ts}`;
    }

    if (kind === 'agent_error') {
      const ts = ev.time ? new Date(ev.time).getTime() : Date.now();
      return `error-${ts}`;
    }

    if (kind === 'agent_interrupted') {
      return 'interrupt';
    }

    if (kind === 'provider_retry') {
      const ts = ev.time ? new Date(ev.time).getTime() : Date.now();
      return `retry-${ts}`;
    }

    // Generic fallback
    return `event-${ev.id || Date.now()}`;
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
    const model = raw.model || '';
    const profile = raw.profile || (model.includes('deepseek') ? 'ollama' : (model.includes('gemini') ? 'gemini' : (model.includes('claude') ? 'anthropic' : 'ollama')));
    return {
      id: raw.id || '',
      name: raw.name || raw.id || '',
      apiUrl: raw.apiUrl || raw.api_url || '',
      workspace: raw.workspace || '',
      model: model,
      profile: profile,
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

  // Agent colour key — each agent gets a unique hue from its ID
  agentColor(agent) {
    const id = agent.id || '';
    let hash = 0;
    for (let i = 0; i < id.length; i++) {
      hash = ((hash << 5) - hash) + id.charCodeAt(i);
      hash = hash & hash;
    }
    const hue = Math.abs(hash) % 360;
    return `hsl(${hue}, 60%, 55%)`;
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

  // ── Truncate large string fields in event data ────────────
  // Item 5: Prevent huge payloads (file contents, prompts, results)
  // from consuming memory in stored timeline events.
  truncatePayloads(data) {
    const MAX_LEN = 500;
    const plainFields = ['content', 'resultText', 'prompt', 'body', 'message'];  // plain-text fields
    for (const field of plainFields) {
      if (typeof data[field] === 'string' && data[field].length > MAX_LEN) {
        data[field] = data[field].slice(0, MAX_LEN) + ' … truncated';
      }
    }
    // argsJson and edits are serialised JSON — truncating the raw string
    // would corrupt the JSON, causing buildToolArgsHTML to fail silently.
    // Instead, parse them, truncate individual values, and re-stringify.
    const jsonFields = ['argsJson', 'edits'];
    for (const field of jsonFields) {
      if (typeof data[field] === 'string' && data[field].length > MAX_LEN) {
        data[field] = this.truncateJSON(data[field], MAX_LEN);
      }
    }
    // Handle deeply nested payloads: check data.data sub-object
    if (data.data && typeof data.data === 'object') {
      for (const field of plainFields) {
        if (typeof data.data[field] === 'string' && data.data[field].length > MAX_LEN) {
          data.data[field] = data.data[field].slice(0, MAX_LEN) + ' … truncated';
        }
      }
      for (const field of jsonFields) {
        if (typeof data.data[field] === 'string' && data.data[field].length > MAX_LEN) {
          data.data[field] = this.truncateJSON(data.data[field], MAX_LEN);
        }
      }
    }
    // Also check data.result sub-object (tool result payload)
    if (data.result && typeof data.result === 'object') {
      for (const field of plainFields) {
        if (typeof data.result[field] === 'string' && data.result[field].length > MAX_LEN) {
          data.result[field] = data.result[field].slice(0, MAX_LEN) + ' … truncated';
        }
      }
      for (const field of jsonFields) {
        if (typeof data.result[field] === 'string' && data.result[field].length > MAX_LEN) {
          data.result[field] = this.truncateJSON(data.result[field], MAX_LEN);
        }
      }
    }
  },

  // ── Truncate values inside a JSON string without breaking structure ──
  truncateJSON(jsonStr, maxLen) {
    let obj;
    try { obj = JSON.parse(jsonStr); } catch (_) { return jsonStr; }
    if (!obj || typeof obj !== 'object') return jsonStr;

    const truncValue = (v) => {
      if (typeof v === 'string' && v.length > maxLen) {
        return v.slice(0, maxLen) + ' … truncated';
      }
      return v;
    };

    if (Array.isArray(obj)) {
      for (let i = 0; i < obj.length; i++) {
        if (typeof obj[i] === 'string' && obj[i].length > maxLen) {
          obj[i] = obj[i].slice(0, maxLen) + ' … truncated';
        } else if (typeof obj[i] === 'object' && obj[i] !== null) {
          for (const k of Object.keys(obj[i])) {
            obj[i][k] = truncValue(obj[i][k]);
          }
        }
      }
    } else {
      for (const k of Object.keys(obj)) {
        obj[k] = truncValue(obj[k]);
      }
    }

    return JSON.stringify(obj);
  },


  cleanupStaleBuffers() {
    const now = Date.now();
    const staleThreshold = 5 * 60 * 1000; // 5 minutes

    // Clean pendingMessages older than threshold
    for (const [agentId, buf] of this.state.pendingMessages.entries()) {
      const age = now - new Date(buf.startTime).getTime();
      if (age > staleThreshold) {
        this.state.pendingMessages.delete(agentId);
      }
    }

    // Clean pendingTools older than threshold
    for (const [callId, ev] of this.state.pendingTools.entries()) {
      const age = now - new Date(ev.time).getTime();
      if (age > staleThreshold) {
        this.state.pendingTools.delete(callId);
      }
    }
  },

  // ── Persist timeline events to localStorage ─────────────
  // Stores a compact subset of the last N timeline events so they
  // survive a full page refresh.
  persistEvents() {
    const events = this.state.timelineEvents;
    if (events.length === 0) {
      try { localStorage.removeItem(this.STORAGE_KEY); } catch (_) {}
      return;
    }
    // Only persist the last STORAGE_MAX_EVENTS events (newest-first)
    const slice = events.slice(-this.STORAGE_MAX_EVENTS);
    // Strip heavy data fields that are large and stale anyway:
    // keep only lightweight fields needed for display
    const compact = slice.map(ev => ({
      id: ev.id,
      agentId: ev.agentId,
      kind: ev.kind,
      time: ev.time,
      data: ev.data ? this.compactEventData(ev.data) : null,
    }));
    try {
      localStorage.setItem(this.STORAGE_KEY, JSON.stringify(compact));
    } catch (_) {
      // localStorage full or unavailable — silently ignore
    }
  },

  // ── Load persisted events from localStorage ─────────────
  // Called during init() before the SSE connection is established,
  // so the user immediately sees events from a previous session.
  loadPersistedEvents() {
    try {
      const raw = localStorage.getItem(this.STORAGE_KEY);
      if (!raw) return;
      const parsed = JSON.parse(raw);
      if (!Array.isArray(parsed) || parsed.length === 0) return;
      // Merge into timelineEvents, deduplicating by id
      const existingIds = new Set(this.state.timelineEvents.map(e => e.id));
      for (const ev of parsed) {
        if (!existingIds.has(ev.id)) {
          this.state.timelineEvents.push(ev);
          existingIds.add(ev.id);
        }
      }
      // Re-cap after merge
      if (this.state.timelineEvents.length > this.STORAGE_MAX_EVENTS * 2) {
        this.state.timelineEvents.splice(0, this.state.timelineEvents.length - this.STORAGE_MAX_EVENTS * 2);
      }
      this._eventsVersion++;
    } catch (_) {
      // Corrupted data — clear and move on
      try { localStorage.removeItem(this.STORAGE_KEY); } catch (_) {}
    }
  },

  // ── Compact event data for storage ──────────────────────
  // Removes large string fields (content, result, prompt, etc.)
  // that are heavy and not useful across page loads.
  compactEventData(data) {
    if (!data || typeof data !== 'object') return data;
    const cloned = {};
    const largeFields = ['content', 'result', 'prompt', 'body', 'response'];
    for (const [k, v] of Object.entries(data)) {
      if (largeFields.includes(k) && typeof v === 'string' && v.length > 200) {
        cloned[k] = v.slice(0, 200) + '… (truncated for storage)';
      } else {
        cloned[k] = v;
      }
    }
    // Also compact nested data.data and data.result
    for (const sub of ['data', 'result']) {
      if (cloned[sub] && typeof cloned[sub] === 'object') {
        const inner = {};
        for (const [k, v] of Object.entries(cloned[sub])) {
          if (largeFields.includes(k) && typeof v === 'string' && v.length > 200) {
            inner[k] = v.slice(0, 200) + '… (truncated for storage)';
          } else {
            inner[k] = v;
          }
        }
        cloned[sub] = inner;
      }
    }
    return cloned;
  },
};

// ── Boot ───────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', () => App.init());
