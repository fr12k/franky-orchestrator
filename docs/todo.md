# Timeline View

## Add extra tool args to the event message

Just be carefull for write, edit and subagent they can have huge parameter value like content, prompt thos should be not rendered or really hard truncated.


## Mmake events in timeline clickable

THe user should be able to click on the event and jump to the corresponding agent view and the corresponding event. Maybe we need to also adjust the franky web-ui to support this.

## The event content is disappearing

AFter a short time the event content is disappearing there were no new events.

# Dashbaord View

## the agent status is empty

```
<div class="agent-stats">
<div class="stat"><span class="val">1m</span><span class="lbl">uptime</span></div>
<div class="stat"><span class="val">undefined</span><span class="lbl">msgs</span></div>
<div class="stat"><span class="val">undefined</span><span class="lbl">turns</span></div>
<div class="stat"><span class="val">—</span><span class="lbl">in</span></div>
<div class="stat"><span class="val">—</span><span class="lbl">out</span></div>
</div>
```

# Finished Agent appear as offline

They should appear as idle