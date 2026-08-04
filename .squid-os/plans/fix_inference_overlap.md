<!DOCTYPE html>
<html>
<head>
<style>
body { font-family: monospace; background: #1a1a1a; color: #ccc; padding: 40px; max-width: 900px; margin: 0 auto; }
h1 { color: #5f9ea0; border-bottom: 1px solid #333; padding-bottom: 10px; }
h2 { color: #cd853f; margin-top: 30px; }
table { border-collapse: collapse; width: 100%; margin: 10px 0; }
th, td { border: 1px solid #444; padding: 8px 12px; text-align: left; }
th { background: #2a2a2a; color: #87ceeb; }
td.ok { color: #90ee90; }
td.bug { color: #ff6347; }
code { background: #333; padding: 2px 6px; border-radius: 3px; color: #daa520; }
pre { background: #111; padding: 15px; border-radius: 5px; overflow-x: auto; color: #87ceeb; }
.del { background: #400; color: #f88; }
.add { background: #040; color: #8f8; }
</style>
</head>
<body>

<h1>Fix Inference Duration Overlap — Plan</h1>

<h2>Problem</h2>
<table>
<tr><th>Scenario</th><th>Current Bug</th><th>Example</th></tr>
<tr>
<td>Text + ToolCall in same message</td>
<td><code>textDoneAt</code> set manually when first tool delta arrives. If text was still streaming, text inference duration <b>overlaps</b> with tool call inference duration.</td>
<td class="bug">msg_266: text 11tok/1446ms + tc 102tok/1404ms → 1446ms includes overlap</td>
</tr>
<tr>
<td>Thinking + Text transition</td>
<td><code>thinkingDoneAt</code> set manually when first text/tool arrives. May overlap.</td>
<td class="bug">Potential overlap if thinking still active</td>
</tr>
<tr>
<td>Consecutive tool calls</td>
<td><code>prev.doneAt</code> set when new tool starts, not on last delta of previous tool.</td>
<td class="bug">Tool N duration inflated into Tool N+1 start</td>
</tr>
</table>

<h2>Solution: Auto-Update DoneAt on Every Chunk</h2>
<p>Instead of manual <code>Mark*Done()</code> calls at transition points, set <code>*DoneAt = time.Now()</code> inside each <code>add*Chars()</code> on <b>every</b> chunk. The last chunk naturally sets the final done time.</p>

<h2>Changes</h2>

<h3>1. metrics.go — addTextChars / addThinkChars / addToolCallChars</h3>
<pre><span class="del">func (m *StreamMetrics) addTextChars(n int) {
    if m.textChars == 0 && n > 0 {
        m.firstTextTokenAt = time.Now()
    }
    m.textChars += n
}</span>
<span class="add">func (m *StreamMetrics) addTextChars(n int) {
    if m.textChars == 0 && n > 0 {
        m.firstTextTokenAt = time.Now()
    }
    m.textChars += n
    m.textDoneAt = time.Now()  // ← auto-set on every chunk
}</span></pre>

<pre><span class="del">func (m *StreamMetrics) addThinkChars(n int) {
    if m.thinkingChars == 0 && n > 0 {
        m.firstThinkingTokenAt = time.Now()
    }
    m.thinkingChars += n
}</span>
<span class="add">func (m *StreamMetrics) addThinkChars(n int) {
    if m.thinkingChars == 0 && n > 0 {
        m.firstThinkingTokenAt = time.Now()
    }
    m.thinkingChars += n
    m.thinkingDoneAt = time.Now()  // ← auto-set on every chunk
}</span></pre>

<pre><span class="del">func (m *StreamMetrics) addToolCallChars(n int) {
    if m.toolCallChars == 0 && n > 0 {
        m.firstToolCallTokenAt = time.Now()
    }
    m.toolCallChars += n
}</span>
<span class="add">func (m *StreamMetrics) addToolCallChars(n int) {
    if m.toolCallChars == 0 && n > 0 {
        m.firstToolCallTokenAt = time.Now()
    }
    m.toolCallChars += n
    m.toolCallDoneAt = time.Now()  // ← auto-set on every chunk
}</span></pre>

<h3>2. metrics.go — Remove manual Mark*Done functions</h3>
<pre><span class="del">// MarkThinkingDone records the time when thinking mode ends.
func (m *StreamMetrics) MarkThinkingDone() {
    m.thinkingDoneAt = time.Now()
}

// MarkTextDone records when the model finished streaming text tokens.
func (m *StreamMetrics) MarkTextDone() {
    m.textDoneAt = time.Now()
}

// MarkToolCallDone records when the model finished streaming tool call arguments.
func (m *StreamMetrics) MarkToolCallDone() {
    m.toolCallDoneAt = time.Now()
}</span></pre>

<h3>3. stream.go — Remove all manual Mark*Done() calls</h3>
<table>
<tr><th>Location</th><th>Remove</th></tr>
<tr>
<td>ToolCallDelta handler (line ~362)</td>
<td class="del"><code>m.stream.metrics.MarkThinkingDone()</code></td>
</tr>
<tr>
<td>ToolCallDelta handler (line ~366)</td>
<td class="del"><code>m.stream.metrics.MarkTextDone()</code></td>
</tr>
<tr>
<td>ToolCalls flush handler (line ~376)</td>
<td class="del"><code>m.stream.metrics.MarkToolCallDone()</code></td>
</tr>
<tr>
<td>Text/Thinking handler (line ~382)</td>
<td class="del"><code>m.stream.metrics.MarkThinkingDone()</code> (in thinking→non-thinking transition)</td>
</tr>
</table>

<h3>4. stream.go — partialTool: auto-update doneAt</h3>
<pre><span class="del">// In ToolCallDelta handler:
if event.ToolCallIdx != m.stream.lastToolIdx && m.stream.lastToolIdx >= 0 {
    prev := &m.stream.partialTools[m.stream.lastToolIdx]
    prev.ended = true
    prev.doneAt = time.Now()
}</span>
<span class="add">// In ToolCallDelta handler:
p := &m.stream.partialTools[event.ToolCallIdx]
// ... existing name/args logic ...
p.doneAt = time.Now()  // ← auto-set on every delta
if event.ToolCallIdx != m.stream.lastToolIdx && m.stream.lastToolIdx >= 0 {
    prev := &m.stream.partialTools[m.stream.lastToolIdx]
    prev.ended = true
    // doneAt already set by last delta — no manual set needed
}</span></pre>

<pre><span class="del">// In ToolCalls flush handler:
if !m.stream.partialTools[i].ended {
    m.stream.partialTools[i].ended = true
    if m.stream.partialTools[i].doneAt.IsZero() {
        m.stream.partialTools[i].doneAt = now
    }
}</span>
<span class="add">// In ToolCalls flush handler:
if !m.stream.partialTools[i].ended {
    m.stream.partialTools[i].ended = true
    // doneAt already set by last delta — just mark ended
}</span></pre>

<h3>5. stream.go — Add doneAt to partialTool struct</h3>
<pre>type partialTool struct {
    // ... existing fields ...
    doneAt  time.Time
}
</pre>

<h2>Result</h2>
<table>
<tr><th>Before</th><th>After</th></tr>
<tr>
<td class="bug">Text inf: 1446ms (overlapped with tool call)</td>
<td class="ok">Text inf: only actual text streaming time</td>
</tr>
<tr>
<td class="bug">Tool call inf starts before text ends</td>
<td class="ok">Tool call inf starts after text doneAt, no overlap</td>
</tr>
<tr>
<td class="bug">Manual MarkDone calls scattered in stream.go</td>
<td class="ok">Single source of truth: add*Chars() sets doneAt</td>
</tr>
</table>

<h2>Files Changed</h2>
<ul>
<li><code>internal/app/metrics.go</code> — add DoneAt in add*Chars, remove Mark*Done</li>
<li><code>internal/app/stream.go</code> — remove Mark*Done calls, add auto doneAt on partialTool</li>
</ul>

</body>
</html>