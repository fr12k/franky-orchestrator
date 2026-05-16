# Timeline View

# Dashbaord View

# Timeline summary is incorrect (Done)

```
<div class="wf-summary">
    <span><strong>0</strong> messages</span>
    <span><strong>4</strong> tool calls</span>
    <span style="color:var(--error);"><strong>0</strong> errors</span>
    <span style="color:var(--warn);"><strong>0</strong> retries</span>
    
    <span style="margin-left:auto;color:var(--accent);">4 items total</span>
</div>
```

# Display message summary (Done)

The message could be long so lets show the first x characters and the last x characters in a style like:
The update was successful .... Continue with the work now.

```
<div class="waterfall-entry  wf-msg border-web" data-agent-id="01KRKB2KM4TK4RR97KTQE5VYV1" data-event-id="ev-1778766429990-8j3f" data-kind="message_merged" data-event-hash="msg-1778766429031" style="cursor: pointer;">
    <span class="wf-time">03:47:09 PM</span>
    <div class="wf-agent"><span class="dot web"></span> franky</div>
    <span class="wf-badge">💬</span>
    <div class="wf-content">
    <div class="wf-title">
        <span class="wf-kind">msg</span>
        <span class="wf-msg-text"></span> <span class="wf-duration">· 1s</span>
    </div>
    </div>
    <span class="wf-hint">↗ open in agent</span>
</div>
```

# High Memory Browser usage (Done)

The orchectrator tab has high memory consumptions like 5 GB for to connect agents.
Lets investigate how to reduce the memory footprint.

See [memory-optimization.md](design/memory-optimization.md) for full findings, applied fixes,
and remaining work items.

# Timeline stop working after some time

The following events nebver were rendered on the Timeline a page refresh fix it for a short amount of time.
```
id: 18038
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":"Now I"}

id: 18039
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":" have a"}

id: 18040
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":" clear plan"}

id: 18041
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":". Let"}

id: 18042
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":" me create"}

id: 18043
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":" the shared"}

id: 18044
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":" SSE module"}

id: 18045
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":","}

id: 18046
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":" refactor proxy"}

id: 18047
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":"."}

id: 18048
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":"zig to use it"}

id: 18049
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":", then"}

id: 18050
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":" wire print"}

id: 18051
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":"."}

id: 18052
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":"zig for"}

id: 18053
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":" orchestrator registration"}

id: 18054
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":" + SSE when"}

id: 18055
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":" `--"}

id: 18056
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":"register` is"}

id: 18057
event: message_update
data: {"kind":"message_update","deltaKind":"text","blockIndex":0,"delta":" set."}

id: 18058
event: message_update
data: {"kind":"message_update","deltaKind":"toolcall_args","blockIndex":0,"delta":"{\"limit\":60,\"offset\":516,\"path\":\"/Users/frankittermann/github/franky/src/coding/modes/proxy.zig\"}"}

id: 18059
event: message_update
data: {"kind":"message_update","deltaKind":"toolcall_args","blockIndex":1,"delta":"{\"limit\":60,\"offset\":395,\"path\":\"/Users/frankittermann/github/franky/src/coding/modes/proxy.zig\"}"}

id: 18060
event: message_end
data: {"kind":"message_end","role":"assistant","contentBlocks":4}

id: 18061
event: tool_execution_start
data: {"kind":"tool_execution_start","callId":"call_lw50kshe","name":"read","argsJson":"{\"limit\":60,\"offset\":516,\"path\":\"/Users/frankittermann/github/franky/src/coding/modes/proxy.zig\"}"}

id: 18062
event: tool_execution_start
data: {"kind":"tool_execution_start","callId":"call_5aazo84x","name":"read","argsJson":"{\"limit\":60,\"offset\":395,\"path\":\"/Users/frankittermann/github/franky/src/coding/modes/proxy.zig\"}"}

id: 18063
event: tool_execution_end
data: {"kind":"tool_execution_end","callId":"call_5aazo84x","isError":false,"resultText":"   395\t    fn deinit(self: *Session) void {\n   396\t        // Join any in-flight /retry worker before freeing the\n   397\t        // session: the worker holds `*Session` and dereferences it\n   398\t        // (run_mutex, allocator, io) right up until its function\n   399\t        // returns. `run_mutex.unlock` releases ownership of the\n   400\t        // critical section but the OS still has to unwind the\n   401\t        // thread; joining here closes that window.\n   402\t        if (self.pending_retry_worker) |t| {\n   403\t            t.join();\n   404\t            self.pending_retry_worker = null;\n   405\t        }\n   406\t        self.transcript.deinit();\n   407\t        self.allocator.free(self.system_prompt);\n   408\t        self.allocator.free(self.session_id);\n   409\t        if (self.parent_dir) |p| self.allocator.free(p);\n   410\t        self.bash_state.deinit();\n   411\t        {\n   412\t            var it = self.tool_usage.iterator();\n   413\t            while (it.next()) |entry| self.allocator.free(entry.key_ptr.*);\n   414\t            self.tool_usage.deinit();\n   415\t        }\n   416\t        self.guardrail_state.deinit();\n   417\t        self.registry.deinit();\n   418\t        self.faux.deinit();\n   419\t        self.permission_store.deinit();\n   420\t        self.role_arena.deinit();\n   421\t        // v2.17 — release restart module globals (owned dupes).\n   422\t        restart_mod.deinit(self.allocator);\n   423\t        // v1.16.0 — release any retained replay frames.\n   424\t        for (self.replay_ring[0..]) |maybe| {\n   425\t            if (maybe) |entry| self.allocator.free(entry.frame);\n   426\t        }\n   427\t    }\n   428\t\n   429\t    fn addSub(self: *Session, sub: *SseSubscriber) bool {\n   430\t        self.events_mutex.lockUncancelable(self.io);\n   431\t        defer self.events_mutex.unlock(self.io);\n   432\t        return self.addSubLocked(sub);\n   433\t    }\n   434\t\n   435\t    /// Caller must hold `events_mutex`. Returns false if the\n   436\t    /// subscriber pool is full.\n   437\t    fn addSubLocked(self: *Session, sub: *SseSubscriber) bool {\n   438\t        for (self.subs[0..], 0..) |s, i| {\n   439\t            if (s == null) {\n   440\t                self.subs[i] = sub;\n   441\t                // v1.28.1 — log lifecycle so a future pool-fill is\n   442\t                // visible without curl-probing. `live` is the count\n   443\t                // AFTER this addition.\n   444\t                ai.log.log(.info, \"proxy\", \"subscriber.added\", \"live={d}/{d}\", .{ self.liveSubsLocked(), max_subs });\n   445\t                return true;\n   446\t            }\n   447\t        }\n   448\t        ai.log.log(.warn, \"proxy\", \"subscriber.refused\", \"pool_full={d}/{d}\", .{ max_subs, max_subs });\n   449\t        return false;\n   450\t    }\n   451\t\n   452\t    /// v1.28.1 — count occupied slots. Caller must hold `events_mutex`.\n   453\t    fn liveSubsLocked(self: *const Session) usize {\n   454\t        var n: usize = 0;\n\n[Showing lines 395-454 of 5499. Use offset=455 to continue.]\n"}

id: 18064
event: tool_execution_end
data: {"kind":"tool_execution_end","callId":"call_lw50kshe","isError":false,"resultText":"   516\t    fn broadcastEvent(self: *Session, allocator: std.mem.Allocator, frame_body: []const u8) void {\n   517\t        self.events_mutex.lockUncancelable(self.io);\n   518\t        defer self.events_mutex.unlock(self.io);\n   519\t\n   520\t        const id = self.next_event_id;\n   521\t        self.next_event_id += 1;\n   522\t\n   523\t        // Pre-size the stamped buffer in one allocation. Going through\n   524\t        // `fmt.allocPrint` here used to cost an alloc + a remap per\n   525\t        // call (initCapacity is small, the format result overflows it),\n   526\t        // which dominated allocator traffic on tests that broadcast\n   527\t        // through the ring. `bufPrint` for the id header is bounded\n   528\t        // (u64 → at most 20 digits + \"id: \\n\").\n   529\t        var id_buf: [32]u8 = undefined;\n   530\t        const id_str = std.fmt.bufPrint(&id_buf, \"id: {d}\\n\", .{id}) catch unreachable;\n   531\t        const stamped = allocator.alloc(u8, id_str.len + frame_body.len) catch {\n   532\t            // Allocation failed — give up on storing this event,\n   533\t            // but still try to fan out the unstamped frame so live\n   534\t            // subscribers don't miss it. Future reconnects after\n   535\t            // this point will see a `replay_gap` if they last\n   536\t            // received an id ≥ this one's predecessor.\n   537\t            self.fanOutLocked(frame_body);\n   538\t            return;\n   539\t        };\n   540\t        @memcpy(stamped[0..id_str.len], id_str);\n   541\t        @memcpy(stamped[id_str.len..], frame_body);\n   542\t\n   543\t        // `id` is u64 but replay_ring is indexed by usize. The\n   544\t        // modulus is bounded by replay_ring_capacity, so the\n   545\t        // narrow cast is always safe.\n   546\t        const slot: usize = @intCast(id % replay_ring_capacity);\n   547\t        if (self.replay_ring[slot]) |old| {\n   548\t            self.allocator.free(old.frame);\n   549\t        }\n   550\t        self.replay_ring[slot] = .{ .id = id, .frame = stamped };\n   551\t\n   552\t        self.fanOutLocked(stamped);\n   553\t    }\n   554\t};\n   555\t\n   556\tfn initSession(\n   557\t    session: *Session,\n   558\t    allocator: std.mem.Allocator,\n   559\t    io: std.Io,\n   560\t    environ: std.process.Environ,\n   561\t    environ_map: *std.process.Environ.Map,\n   562\t    cfg: *cli_mod.Config,\n   563\t    original_argv: []const []const u8,\n   564\t) !void {\n   565\t    // v2.17 — cache argv + exe path so a later `/restart` can\n   566\t    // spawn a fresh binary. Best-effort; restart simply won't work\n   567\t    // if this fails.\n   568\t    restart_mod.init(original_argv, io, allocator) catch {\n   569\t        ai.log.log(.warn, \"proxy\", \"restart.init\", \"failed to cache argv — restart will not be available\", .{});\n   570\t    };\n   571\t    // Resolve where (or whether) sessions live on disk. Mirrors\n   572\t    // print mode's SessionState.init policy: explicit `--session-dir`\n   573\t    // > `$FRANKY_HOME/sessions` > `~/.franky/sessions` > `./.franky-sessions`.\n   574\t    // `--no-session` disables persistence (parent_dir = null).\n   575\t    const parent_dir: ?[]u8 = if (cfg.no_session) null else blk: {\n\n[Showing lines 516-575 of 5499. Use offset=576 to continue.]\n"}

id: 18065
event: message_start
data: {"kind":"message_start","role":"tool_result"}

id: 18066
event: message_end
data: {"kind":"message_end","role":"tool_result","contentBlocks":1}

id: 18067
event: message_start
data: {"kind":"message_start","role":"tool_result"}

id: 18068
event: message_end
data: {"kind":"message_end","role":"tool_result","contentBlocks":1}

id: 18069
event: turn_end
data: {"kind":"turn_end"}

id: 18070
event: turn_start
data: {"kind":"turn_start"}

event: ping
data: {}

event: ping
data: {}


event: ping
data: {}

event: ping
data: {}

event: ping
data: {}

event: ping
data: {}
```

# Add Agent manually (Done)

Lets make it possible to add agents in the dashboard view by give it a new and the url like
```
name: frankie
host: localhost:8789
```
That's it. 

# Messages are nbot shown

The message appear shortly real shortly and then get deleted from the timeline