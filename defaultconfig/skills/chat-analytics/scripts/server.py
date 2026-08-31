#!/usr/bin/env python3
"""
Chat Analytics Server.
Serves a static frontend and provides precomputed analytics endpoints
for Squid-OS chat session JSON files.
"""
import json
import os
import sys
import glob
import time
from http.server import HTTPServer, SimpleHTTPRequestHandler
from datetime import datetime, timezone

SKILL_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SESSIONS_DIR = os.environ.get('SQUID_OS_SESSIONS_DIR', os.path.join(SKILL_DIR, "sessions"))
ASSETS_DIR = os.path.join(SKILL_DIR, "assets")

CACHE = {}
CACHE_TTL = 5  # seconds

def now_iso():
    return datetime.now(timezone.utc).isoformat()

def time_ago(dt_str):
    """Return human-readable time ago string."""
    try:
        # Handle various ISO formats
        dt_str = dt_str.replace('Z', '+00:00')
        if '+' not in dt_str and '-' not in dt_str[10:]:
            dt_str = dt_str + '+00:00'
        dt = datetime.fromisoformat(dt_str)
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        diff = datetime.now(timezone.utc) - dt
        total_seconds = int(diff.total_seconds())
        if total_seconds < 60:
            return f"{total_seconds}s ago"
        elif total_seconds < 3600:
            return f"{total_seconds // 60}m ago"
        elif total_seconds < 86400:
            return f"{total_seconds // 3600}h ago"
        elif total_seconds < 2592000:  # 30 days
            return f"{total_seconds // 86400}d ago"
        else:
            return f"{total_seconds // 2592000}y ago"
    except:
        return "unknown"

def find_session_file(ref):
    """Resolve a session reference to an actual file path."""
    candidates = [
        os.path.join(SESSIONS_DIR, ref),
        os.path.join(SESSIONS_DIR, ref, 'chat.json'),
        os.path.join(SESSIONS_DIR, ref + '.json'),
        os.path.join(SESSIONS_DIR, ref + '.chat.json'),
    ]
    for c in candidates:
        if os.path.isfile(c):
            return c
    import glob as _glob
    matches = _glob.glob(os.path.join(SESSIONS_DIR, ref + '*', 'chat.json'))
    if matches:
        return matches[0]
    return None


def load_session(filename):
    path = find_session_file(filename)
    if not path:
        raise FileNotFoundError(f"Session not found: {filename}")
    with open(path, 'r') as f:
        return json.load(f)

def _think_to_bool(v):
    """Convert thinking value from session JSON to bool."""
    if isinstance(v, bool):
        return v
    if isinstance(v, dict):
        return v.get('enabled', False)
    return False


def session_view(data):
    """Return meta, initial config, current config, and pending state."""
    meta = data.get('meta') or {}
    initial = data.get('initial') or {}
    current = data.get('config') or {}
    pending = data.get('pending') or {}
    return meta, initial, current, pending


def analyze_file_compaction(messages):
    """Forecast file-result compaction using the latest full-state checkpoint."""
    events_by_path = {}
    sequence = 0
    for message in messages:
        for tool_call in message.get('tool_calls', []) or []:
            instruction = tool_call.get('instruction') or {}
            execution = tool_call.get('execution') or {}
            tool_name = tool_call.get('name') or instruction.get('name') or 'unknown'
            status = execution.get('status') or ''
            instruction_tokens = instruction.get('tokens') or 0
            execution_tokens = execution.get('tokens') or 0
            for file_entry in execution.get('files', []) or []:
                path = file_entry.get('path') or ''
                if not path:
                    continue
                trace = file_entry.get('trace') or 'unknown'
                is_checkpoint = (
                    (tool_name == 'read_file' and trace == 'read')
                    or (tool_name == 'write_file' and trace in ('write', 'create'))
                )
                events_by_path.setdefault(path, []).append({
                    'sequence': sequence,
                    'trace': trace,
                    'tool': tool_name,
                    'status': status,
                    'instruction_tokens': instruction_tokens,
                    'execution_tokens': execution_tokens,
                    'tokens': instruction_tokens + execution_tokens,
                    'is_checkpoint': is_checkpoint,
                })
            sequence += 1

    trace_names = ('read', 'edit', 'write', 'create')
    totals = {
        'current_tokens': 0,
        'current_instruction_tokens': 0,
        'current_execution_tokens': 0,
        'retained_tokens': 0,
        'retained_instruction_tokens': 0,
        'retained_execution_tokens': 0,
        'saved_tokens': 0,
        'saved_instruction_tokens': 0,
        'saved_execution_tokens': 0,
        'saved_percent': 0,
        'events': 0,
        'kept_events': 0,
        'compacted_events': 0,
        'compacted_by_trace': {trace: {'events': 0, 'tokens': 0, 'instruction_tokens': 0, 'execution_tokens': 0} for trace in trace_names},
    }
    files = {}

    for path, events in events_by_path.items():
        latest_checkpoint = None
        for index, event in enumerate(events):
            if event['status'] == 'success' and event['is_checkpoint']:
                latest_checkpoint = index

        row = {
            'current_tokens': 0,
            'current_instruction_tokens': 0,
            'current_execution_tokens': 0,
            'retained_tokens': 0,
            'retained_instruction_tokens': 0,
            'retained_execution_tokens': 0,
            'saved_tokens': 0,
            'saved_instruction_tokens': 0,
            'saved_execution_tokens': 0,
            'saved_percent': 0,
            'kept_events': 0,
            'compacted_events': 0,
            'compacted_by_trace': {trace: {'events': 0, 'tokens': 0, 'instruction_tokens': 0, 'execution_tokens': 0} for trace in trace_names},
        }
        for index, event in enumerate(events):
            tokens = event['tokens']
            instruction_tokens = event['instruction_tokens']
            execution_tokens = event['execution_tokens']
            compact = (
                latest_checkpoint is not None
                and index < latest_checkpoint
                and event['status'] == 'success'
            )
            row['current_tokens'] += tokens
            row['current_instruction_tokens'] += instruction_tokens
            row['current_execution_tokens'] += execution_tokens
            totals['current_tokens'] += tokens
            totals['current_instruction_tokens'] += instruction_tokens
            totals['current_execution_tokens'] += execution_tokens
            totals['events'] += 1
            if compact:
                row['saved_tokens'] += tokens
                row['saved_instruction_tokens'] += instruction_tokens
                row['saved_execution_tokens'] += execution_tokens
                row['compacted_events'] += 1
                totals['saved_tokens'] += tokens
                totals['saved_instruction_tokens'] += instruction_tokens
                totals['saved_execution_tokens'] += execution_tokens
                totals['compacted_events'] += 1
                trace = event['trace']
                if trace not in row['compacted_by_trace']:
                    row['compacted_by_trace'][trace] = {'events': 0, 'tokens': 0, 'instruction_tokens': 0, 'execution_tokens': 0}
                if trace not in totals['compacted_by_trace']:
                    totals['compacted_by_trace'][trace] = {'events': 0, 'tokens': 0, 'instruction_tokens': 0, 'execution_tokens': 0}
                row['compacted_by_trace'][trace]['events'] += 1
                row['compacted_by_trace'][trace]['tokens'] += tokens
                row['compacted_by_trace'][trace]['instruction_tokens'] += instruction_tokens
                row['compacted_by_trace'][trace]['execution_tokens'] += execution_tokens
                totals['compacted_by_trace'][trace]['events'] += 1
                totals['compacted_by_trace'][trace]['tokens'] += tokens
                totals['compacted_by_trace'][trace]['instruction_tokens'] += instruction_tokens
                totals['compacted_by_trace'][trace]['execution_tokens'] += execution_tokens
            else:
                row['retained_tokens'] += tokens
                row['retained_instruction_tokens'] += instruction_tokens
                row['retained_execution_tokens'] += execution_tokens
                row['kept_events'] += 1
                totals['retained_tokens'] += tokens
                totals['retained_instruction_tokens'] += instruction_tokens
                totals['retained_execution_tokens'] += execution_tokens
                totals['kept_events'] += 1

        current = row['current_tokens']
        row['saved_percent'] = round(row['saved_tokens'] * 100 / current, 1) if current else 0
        files[path] = row

    current = totals['current_tokens']
    totals['saved_percent'] = round(totals['saved_tokens'] * 100 / current, 1) if current else 0
    return {'totals': totals, 'files': files}


def parse_session_data(data):
    """Parse a session JSON and extract analytics."""
    meta, initial_config, current_config, pending_config = session_view(data)
    messages = data.get('messages', [])
    # Session total: computed from messages (current schema has no doc-level
    # total_tokens scalar; token_tally.lifetime.total is the stored equivalent).
    session_total_tokens = ((data.get('token_tally') or {}).get('lifetime') or {}).get('total')

    # Counts
    total_messages = len(messages)
    user_messages = [m for m in messages if m['role'] == 'user']
    assistant_messages = [m for m in messages if m['role'] == 'assistant']
    synthetic_messages = [m for m in messages if m['role'] == 'synthetic']
    system_messages = [m for m in messages if m['role'] == 'system']
    tool_messages = [m for m in messages if m['role'] == 'tool']
    internal_messages = [m for m in messages if m['role'] == 'internal']

    # Token tallies
    total_input_tokens = sum(m.get('input_tokens', 0) for m in messages)
    total_output_tokens = sum(m.get('output_tokens', 0) for m in messages)

    # Breakdown by role
    user_input_tokens = sum(m.get('input_tokens', 0) for m in user_messages)
    assistant_output_tokens = sum(m.get('output_tokens', 0) for m in assistant_messages)

    # Thinking tokens (from nested metrics, top-level may be None)
    total_thinking_tokens = 0
    total_text_tokens = 0
    total_tool_call_tokens = 0
    for m in messages:
        total_thinking_tokens += (m.get('thinking_metrics') or {}).get('tokens', 0)
        total_text_tokens += (m.get('text_metrics') or {}).get('tokens', 0)
        total_tool_call_tokens += (m.get('tool_call_metrics') or {}).get('tokens', 0)

    # Aborted messages (synthetic with label='aborted')
    aborted_messages = len([m for m in messages if m.get('label') == 'aborted'])

    # Tool-enabled messages
    tool_enabled_count = len([m for m in messages if m.get('label') == 'tool_enabled'])

    # Tool call execution tokens (from tool_calls[].execution.tokens — these are instruction tokens)
    total_tool_instruction_tokens = 0
    total_tool_execution_tokens = 0
    for m in messages:
        for tc in m.get('tool_calls', []):
            inst = tc.get('instruction', {})
            if inst.get('tokens'):
                total_tool_instruction_tokens += inst['tokens']
            exe = tc.get('execution', {})
            if exe.get('tokens'):
                total_tool_execution_tokens += exe['tokens']

    # System prompt tokens (from system messages only: sys0, env0)
    system_prompt_tokens = sum(m.get('input_tokens', 0) + m.get('output_tokens', 0) for m in system_messages)

    # Tool definition tokens (from tools0)
    tool_definition_tokens = 0
    tools0 = next((m for m in messages if m.get('id') == 'tools0'), None)
    if tools0:
        tool_definition_tokens = (tools0.get('input_tokens', 0) or 0) + (tools0.get('output_tokens', 0) or 0)

    # Synthetic tokens
    synthetic_tokens = sum(m.get('input_tokens', 0) + m.get('output_tokens', 0) for m in synthetic_messages)

    # Tool calls
    all_tool_calls = []
    for m in messages:
        if m.get('tool_calls'):
            all_tool_calls.extend(m['tool_calls'])

    tool_call_count = len(all_tool_calls)

    # Tool usage breakdown
    tool_usage = {}
    for tc in all_tool_calls:
        name = tc.get('name') or (tc.get('instruction', {}) or {}).get('name') or 'unknown'
        if name not in tool_usage:
            tool_usage[name] = {'calls': 0, 'tokens_in': 0, 'tokens_out': 0, 'inference_ms': 0, 'execution_ms': 0}
        tool_usage[name]['calls'] += 1
        # Get tokens from instruction/execution
        inst = tc.get('instruction', {})
        exec_ = tc.get('execution', {})
        tool_usage[name]['tokens_in'] += inst.get('tokens', 0) + tc.get('call_tokens', 0)
        tool_usage[name]['tokens_out'] += exec_.get('tokens', 0) + tc.get('result_tokens', 0)
        tool_usage[name]['inference_ms'] += inst.get('duration_ms', 0) or 0
        tool_usage[name]['execution_ms'] += exec_.get('duration_ms', 0) or 0

    # Skills loaded — only from actual skill_load events (synthetic messages or tool calls)
    skills_data = {}

    # Check for skill_load synthetic messages and tool calls
    for m in messages:
        if m.get('label') == 'skill_load':
            params = m.get('params', {})
            skill_name = params.get('name', '')
            if skill_name:
                if skill_name not in skills_data:
                    skills_data[skill_name] = {'loads': 0, 'tokens': 0}
                skills_data[skill_name]['loads'] += 1
                skills_data[skill_name]['tokens'] += (m.get('input_tokens') or 0)
        for tc in m.get('tool_calls', []):
            tc_name = tc.get('name', '') or (tc.get('instruction', {}) or {}).get('name', '')
            if tc_name == 'skill_load':
                args_raw = tc.get('arguments', '') or (tc.get('instruction', {}) or {}).get('arguments', '')
                if args_raw:
                    try:
                        args = json.loads(args_raw)
                        skill_name = args.get('name', args.get('skill', ''))
                        if skill_name:
                            if skill_name not in skills_data:
                                skills_data[skill_name] = {'loads': 0, 'tokens': 0}
                            skills_data[skill_name]['loads'] += 1
                            skills_data[skill_name]['tokens'] += ((tc.get('execution') or {}).get('tokens') or 0)
                    except:
                        pass


    # Build skill timeline
    skill_timeline = []
    not_skill_tokens = 0
    for m in messages:
        role = m.get('role', '')

        # Synthetic skill_load message
        if m.get('label') == 'skill_load':
            params = m.get('params', {}) or {}
            skill_name = params.get('name', '')
            skill_tok = (m.get('input_tokens') or 0)
            if skill_name and skill_tok > 0:
                if not_skill_tokens > 0:
                    skill_timeline.append({'type': 'not_skill', 'tokens': not_skill_tokens})
                    not_skill_tokens = 0
                skill_timeline.append({'type': 'skill', 'name': skill_name, 'tokens': skill_tok, 'id': m.get('id', '')})
            continue

        if role == 'assistant':
            has_skill_tc = False
            skill_segments = []
            non_skill_tool_instr = 0
            non_skill_tool_exec = 0

            for tc in m.get('tool_calls', []):
                tc_name = tc.get('name', '') or (tc.get('instruction', {}) or {}).get('name', '')
                inst = tc.get('instruction', {}) or {}
                exe = tc.get('execution', {}) or {}
                if tc_name == 'skill_load':
                    has_skill_tc = True
                    skill_tok = (exe.get('tokens') or 0)
                    args_raw = tc.get('arguments', '') or inst.get('arguments', '')
                    skill_name = ''
                    if args_raw:
                        try:
                            args = json.loads(args_raw)
                            skill_name = args.get('name', args.get('skill', ''))
                        except:
                            pass
                    if skill_name and skill_tok > 0:
                        skill_segments.append({'type': 'skill', 'name': skill_name, 'tokens': skill_tok, 'id': m.get('id', '')})
                else:
                    non_skill_tool_instr += (inst.get('tokens') or 0)
                    non_skill_tool_exec += (exe.get('tokens') or 0)

            tm = m.get('text_metrics') or {}
            thm = m.get('thinking_metrics') or {}
            text_tok = tm.get('tokens', 0)
            think_tok = thm.get('tokens', 0)

            if skill_segments:
                not_skill_tokens += text_tok + think_tok + non_skill_tool_instr + non_skill_tool_exec
                if not_skill_tokens > 0:
                    skill_timeline.append({'type': 'not_skill', 'tokens': not_skill_tokens})
                    not_skill_tokens = 0
                skill_timeline.extend(skill_segments)
            else:
                tcm = m.get('tool_call_metrics') or {}
                instr_tok = tcm.get('tokens', 0)
                exec_tok = sum((tc.get('execution') or {}).get('tokens') or 0 for tc in (m.get('tool_calls') or []))
                not_skill_tokens += text_tok + think_tok + instr_tok + exec_tok
        elif role in ('user', 'system', 'internal'):
            not_skill_tokens += (m.get('input_tokens') or 0)

    if not_skill_tokens > 0:
        skill_timeline.append({'type': 'not_skill', 'tokens': not_skill_tokens})

    if skill_timeline:
        skills_data['timeline'] = skill_timeline

    # TTFT (time to first token)
    ttft_values = []
    for m in assistant_messages:
        ttft = m.get('time_to_first_token_ms')
        if ttft and ttft > 0:
            ttft_values.append(ttft)
    avg_ttft = sum(ttft_values) / len(ttft_values) if ttft_values else 0

    # Speed (tokens per second)
    speeds = []
    for m in assistant_messages:
        tps = m.get('tok_per_sec')
        if tps and tps > 0:
            speeds.append(tps)
        # Also check sequence_stat
        ss = m.get('sequence_stat', {})
        if ss.get('avg_tok_per_sec', 0) > 0:
            speeds.append(ss['avg_tok_per_sec'])
    avg_speed = sum(speeds) / len(speeds) if speeds else 0

    # Duration — sum only actual processing time (not wall-clock idle)
    def msg_processing_ms(m):
        ttft = (m.get('time_to_first_token_ms') or 0)
        tm = m.get('text_metrics') or {}
        text = tm.get('inference_duration_ms') or 0
        thm = m.get('thinking_metrics') or {}
        thinking = thm.get('inference_duration_ms') or 0
        tcm = m.get('tool_call_metrics') or {}
        tool_call = tcm.get('inference_duration_ms') or 0
        tool_exec = sum((tc.get('execution') or {}).get('duration_ms') or 0 for tc in (m.get('tool_calls') or []))
        return ttft + text + thinking + tool_call + tool_exec
    total_duration_ms = sum(msg_processing_ms(m) for m in messages)

    # Turns (user + assistant = 1 turn)
    num_turns = len(user_messages)  # Each user message starts a turn

    # Files touched — aggregate from tool_calls[].execution[].files[]
    file_compaction = analyze_file_compaction(messages)
    files_touched = {}
    for m in messages:
        for tc in m.get('tool_calls', []):
            exe = tc.get('execution', {}) or {}
            inst = tc.get('instruction', {}) or {}
            exec_files = exe.get('files', []) or []
            for fi in exec_files:
                filepath = fi.get('path', '')
                if not filepath:
                    continue
                if filepath not in files_touched:
                    files_touched[filepath] = {
                        'path': filepath,
                        'checksum': '',
                        'traces': [],
                        'reads': 0,
                        'edits': 0,
                        'creates': 0,
                        'writes': 0,
                        'reads_tokens': 0,
                        'edits_tokens': 0,
                        'creates_tokens': 0,
                        'writes_tokens': 0,
                        'total_tokens': 0,
                        'last_updated': ''
                    }
                trace = fi.get('trace', 'unknown')
                files_touched[filepath]['traces'].append(trace)
                cs = fi.get('checksum', '')
                if cs:
                    files_touched[filepath]['checksum'] = cs
                ts = fi.get('time', '')
                if ts and ts > files_touched[filepath]['last_updated']:
                    files_touched[filepath]['last_updated'] = ts
                call_tokens = inst.get('tokens', 0) + exe.get('tokens', 0)
                if trace == 'read':
                    files_touched[filepath]['reads'] += 1
                    files_touched[filepath]['reads_tokens'] += call_tokens
                elif trace == 'edit':
                    files_touched[filepath]['edits'] += 1
                    files_touched[filepath]['edits_tokens'] += call_tokens
                elif trace == 'create':
                    files_touched[filepath]['creates'] += 1
                    files_touched[filepath]['creates_tokens'] += call_tokens
                elif trace == 'write':
                    files_touched[filepath]['writes'] += 1
                    files_touched[filepath]['writes_tokens'] += call_tokens
                files_touched[filepath]['total_tokens'] += call_tokens

    for filepath, file_data in files_touched.items():
        file_data['compaction'] = file_compaction['files'].get(filepath, {
            'current_tokens': 0,
            'retained_tokens': 0,
            'saved_tokens': 0,
            'saved_percent': 0,
            'kept_events': 0,
            'compacted_events': 0,
            'compacted_by_trace': {},
        })

    # General info
    system_prompt_file = current_config.get('system_prompt_file', '')
    inference_initial = initial_config.get('inference') or {}
    inference_current = current_config.get('inference') or {}
    thinking_val = inference_current.get('thinking')
    thinking_enabled = _think_to_bool(thinking_val)

    # Timeline data
    timeline = []
    perf_timeline = []
    initial_model = inference_initial.get('model', '')
    initial_provider = inference_initial.get('provider', '')
    current_model_label = initial_model
    current_provider_label = initial_provider
    ctx_in = 0
    ctx_out = 0
    last_user_tokens = 0
    for i, m in enumerate(messages):
        role = m.get('role', 'unknown')
        created = m.get('created_at', '')
        input_tok = m.get('input_tokens', 0)
        output_tok = m.get('output_tokens', 0)
        thinking_tok = (m.get('thinking_metrics') or {}).get('tokens', 0)
        text_tok = (m.get('text_metrics') or {}).get('tokens', 0)
        duration = m.get('duration_ms', 0) or 0

        # Accumulate cumulative tokens
        ctx_in += input_tok
        ctx_out += output_tok
        ctx_total = ctx_in + ctx_out

        # Tool count for this message
        msg_tools = len(m.get('tool_calls', []) or [])

        # Files in this message (from tool calls)
        msg_files = []
        for tc in m.get('tool_calls') or []:
            exe = tc.get('execution') or {}
            for fe in exe.get('files') or []:
                fp = fe.get('path', '')
                if fp and fp not in msg_files:
                    msg_files.append(fp)

        if role == 'user':
            last_user_tokens = input_tok or 0

        if m.get('label') == 'Model Switched':
            params = m.get('params') or {}
            to_model = params.get('to', '')
            if to_model:
                if '/' in to_model:
                    current_provider_label, current_model_label = to_model.split('/', 1)
                else:
                    current_model_label = to_model

        # Performance data
        tps = m.get('tok_per_sec')
        ss_speed = ss.get('avg_tok_per_sec', 0)
        speed = tps or ss_speed

        timeline.append({
            'index': i,
            'id': m.get('id', ''),
            'role': role,
            'timestamp': created,
            'input_tokens': input_tok,
            'output_tokens': output_tok,
            'thinking_tokens': thinking_tok,
            'text_tokens': text_tok,
            'duration_ms': duration,
            'tool_count': msg_tools,
            'files': msg_files,
            'current_model_label': current_model_label,
            'current_provider_label': current_provider_label,
            'tokens_per_second': speed if speed and speed > 0 else 0,
            'ctx_in': ctx_in,
            'ctx_out': ctx_out,
            'ctx_total': ctx_total,
        })

        # Duration breakdown for assistant messages
        ttft_dur = 0
        text_dur = 0
        thinking_dur = 0
        tool_call_dur = 0
        tool_exec_dur = 0
        # Token breakdown for assistant messages
        text_tok_b = 0
        think_tok_b = 0
        tool_instr_tok = 0
        tool_exec_tok = 0
        if role == 'assistant':
            user_tok_for_bar = last_user_tokens
            last_user_tokens = 0
            ttft_dur = m.get('time_to_first_token_ms') or 0
            tm = m.get('text_metrics') or {}
            text_dur = tm.get('inference_duration_ms') or 0
            text_tok_b = tm.get('tokens', 0)
            thm = m.get('thinking_metrics') or {}
            thinking_dur = thm.get('inference_duration_ms') or 0
            think_tok_b = thm.get('tokens', 0)
            tcm = m.get('tool_call_metrics') or {}
            tool_call_dur = tcm.get('inference_duration_ms') or 0
            tool_instr_tok = tcm.get('tokens', 0)
            # Sum execution duration across all tool calls
            for tc in m.get('tool_calls') or []:
                exe = tc.get('execution') or {}
                tool_exec_dur += (exe.get('duration_ms') or 0)
                tool_exec_tok += (exe.get('tokens') or 0)

        # Performance data — combined speed + TTFT + duration breakdown
        if speed and speed > 0:
            entry = {
                'index': i,
                'id': m.get('id', ''),
                'tokens_per_second': speed,
                'ttft_ms': ttft_dur if ttft_dur > 0 else None,
                'role': role,
                'current_model_label': current_model_label,
                'current_provider_label': current_provider_label,
                'ctx_in': ctx_in,
                'ctx_out': ctx_out,
                'ctx_total': ctx_total,
            }
            # Always include duration breakdown on timeline
            timeline[-1]['ttft_dur_ms'] = ttft_dur
            timeline[-1]['text_dur_ms'] = text_dur
            timeline[-1]['thinking_dur_ms'] = thinking_dur
            timeline[-1]['tool_call_dur_ms'] = tool_call_dur
            timeline[-1]['tool_exec_dur_ms'] = tool_exec_dur
            # Token breakdown
            timeline[-1]['text_tok_b'] = text_tok_b
            timeline[-1]['think_tok_b'] = think_tok_b
            timeline[-1]['tool_instr_tok'] = tool_instr_tok
            timeline[-1]['tool_exec_tok'] = tool_exec_tok
            timeline[-1]['user_input_tok'] = user_tok_for_bar if role == 'assistant' else 0
            perf_timeline.append(entry)

    # Extract system prompt content
    system_prompt_text = ''
    for m in system_messages:
        system_prompt_text = m.get('text', '')
        break

    # Collect init message params (sys0, env0, tools0, config0)
    init_params = {}
    for m in messages:
        mid = m.get('id', '')
        params = m.get('params') or {}
        label = m.get('label', '')
        if mid == 'sys0' and params:
            init_params['system_prompt'] = params.get('file', label)
        elif mid == 'env0' and params:
            init_params['environment'] = params.get('sections', '')
        elif mid == 'tools0' and params:
            init_params['tools'] = params.get('tools', '')
        elif mid == 'config0' and params:
            init_params['config'] = params

    return {
        'session_info': {
            'id': meta.get('id', ''),
            'title': meta.get('title', ''),
            'model': inference_current.get('model', ''),
            'provider': inference_current.get('provider', ''),
            'created_at': meta.get('created_at', ''),
            'updated_at': meta.get('updated_at', ''),
            'system_prompt_file': system_prompt_file,
            'thinking': thinking_enabled,
            'working_dir': current_config.get('working_dir', ''),
            'target': current_config.get('target', ''),
            'agent_name': current_config.get('agent_name', ''),
            'active_skill': current_config.get('active_skill', ''),
            'auth_mode': current_config.get('auth_mode', ''),
        },
        'counts': {
            'total_messages': total_messages,
            'user_messages': len(user_messages),
            'assistant_messages': len(assistant_messages),
            'synthetic_messages': len(synthetic_messages),
            'system_messages': len(system_messages),
            'tool_messages': len(tool_messages),
            'internal_messages': len(internal_messages),
            'turns': num_turns,
            'tool_calls': tool_call_count,
            'aborted': aborted_messages,
            'tool_enabled': tool_enabled_count,
        },
        'tokens': {
            'total_input': total_input_tokens,
            'total_output': total_output_tokens,
            'session_total': session_total_tokens,
            'user_input': user_input_tokens,
            'assistant_output': assistant_output_tokens,
            'thinking': total_thinking_tokens,
            'text': total_text_tokens,
            'tool_call': total_tool_call_tokens,
            'system_prompt': system_prompt_tokens,
            'synthetic': synthetic_tokens,
            'tool_instruction': total_tool_instruction_tokens,
            'tool_execution': total_tool_execution_tokens,
            'tool_definition': tool_definition_tokens,
        },
        'performance': {
            'avg_ttft_ms': round(avg_ttft, 1),
            'avg_speed_tok_per_sec': round(avg_speed, 1),
            'total_duration_ms': total_duration_ms,
        },
        'tools': tool_usage,
        'skills': skills_data,
        'files': list(files_touched.values()),
        'file_compaction': file_compaction['totals'],
        'timeline': timeline,
        'performance_timeline': perf_timeline,
        'general': {
            'version': data.get('version', 1),
            'system_prompt_file': system_prompt_file,
            'system_prompt_preview': system_prompt_text[:200] if system_prompt_text else '',
            'thinking_enabled': thinking_enabled,
            'init_params': init_params,
            'initial': initial_config,
            'config': current_config,
            'pending': pending_config or None,
            'inference': {
                'initial': {
                    'provider': inference_initial.get('provider', ''),
                    'model': inference_initial.get('model', ''),
                    'thinking': _think_to_bool(inference_initial.get('thinking')),
                },
                'current': {
                    'provider': inference_current.get('provider', ''),
                    'model': inference_current.get('model', ''),
                    'thinking': _think_to_bool(inference_current.get('thinking')),
                },
            },
        }
    }

def get_session_list():
    """Get list of all sessions with metadata."""
    sessions = []
    for entry in os.listdir(SESSIONS_DIR):
        session_dir = os.path.join(SESSIONS_DIR, entry)
        chat_path = os.path.join(session_dir, 'chat.json')
        if not os.path.isfile(chat_path):
            continue
        try:
            stat = os.stat(chat_path)
            data = load_session(entry)
            parsed = parse_session_data(data)
            sessions.append({
                'filename': entry,
                'created_at': parsed['session_info']['created_at'],
                'updated_at': parsed['session_info']['updated_at'],
                'time_ago': time_ago(parsed['session_info']['updated_at']),
                'model': parsed['session_info']['model'],
                'provider': parsed['session_info']['provider'],
                'messages': parsed['counts']['total_messages'],
                'turns': parsed['counts']['turns'],
                'input_tokens': parsed['tokens']['total_input'],
                'output_tokens': parsed['tokens']['total_output'],
                'total_tokens': parsed['tokens'].get('session_total') or 0,
                'tool_calls': parsed['counts']['tool_calls'],
                'file_size': stat.st_size,
                'mtime': stat.st_mtime,
            })
        except Exception as e:
            print(f"Error parsing {entry}: {e}", file=sys.stderr)
            continue
    # Sort by updated_at descending
    sessions.sort(key=lambda s: s['updated_at'], reverse=True)
    return sessions

def get_dashboard_summary():
    """Compute global dashboard statistics."""
    sessions = get_session_list()
    if not sessions:
        return {'error': 'No sessions found'}

    total_conversations = len(sessions)
    total_input = sum(s['input_tokens'] for s in sessions)
    total_output = sum(s['output_tokens'] for s in sessions)
    avg_input = total_input // total_conversations if total_conversations else 0
    avg_output = total_output // total_conversations if total_conversations else 0

    # Average file touch (based on mtime spread)
    mtimes = [s['mtime'] for s in sessions]
    if mtimes:
        avg_mtime = sum(mtimes) / len(mtimes)
        now = time.time()
        avg_age_seconds = now - avg_mtime
        if avg_age_seconds < 86400:
            avg_age_str = f"{int(avg_age_seconds // 3600)}h ago"
        elif avg_age_seconds < 2592000:
            avg_age_str = f"{int(avg_age_seconds // 86400)}d ago"
        else:
            avg_age_str = f"{int(avg_age_seconds // 2592000)}y ago"
    else:
        avg_age_str = "N/A"

    # Average turns
    total_turns = sum(s['turns'] for s in sessions)
    avg_turns = total_turns // total_conversations if total_conversations else 0

    # Average tool calls
    total_tools = sum(s['tool_calls'] for s in sessions)
    avg_tools = total_tools // total_conversations if total_conversations else 0

    # Average TTFT and speed (need to load each session)
    all_ttfts = []
    all_speeds = []
    for s in sessions[:50]:  # Limit to avoid slowdown
        try:
            data = load_session(s['filename'])
            parsed = parse_session_data(data)
            if parsed['performance']['avg_ttft_ms'] > 0:
                all_ttfts.append(parsed['performance']['avg_ttft_ms'])
            if parsed['performance']['avg_speed_tok_per_sec'] > 0:
                all_speeds.append(parsed['performance']['avg_speed_tok_per_sec'])
        except:
            pass

    avg_ttft = sum(all_ttfts) / len(all_ttfts) if all_ttfts else 0
    avg_speed = sum(all_speeds) / len(all_speeds) if all_speeds else 0

    # Top 10 biggest by total tokens
    top_10 = sorted(sessions, key=lambda s: s['total_tokens'], reverse=True)[:10]

    return {
        'total_conversations': total_conversations,
        'total_input_tokens': total_input,
        'total_output_tokens': total_output,
        'avg_input_tokens': avg_input,
        'avg_output_tokens': avg_output,
        'avg_ttft_ms': round(avg_ttft, 1),
        'avg_speed_tok_per_sec': round(avg_speed, 1),
        'avg_turns': avg_turns,
        'avg_tool_calls': avg_tools,
        'avg_file_age': avg_age_str,
        'top_10': [{
            'filename': s['filename'],
            'total_tokens': s['total_tokens'],
            'messages': s['messages'],
            'turns': s['turns'],
            'model': s['model'],
            'created_at': s['created_at'],
        } for s in top_10],
    }

def get_models_table():
    """Get per-model statistics."""
    sessions = get_session_list()
    models = {}
    for s in sessions:
        model_key = f"{s['provider']}/{s['model']}"
        if model_key not in models:
            models[model_key] = {
                'model': s['model'],
                'provider': s['provider'],
                'sessions': 0,
                'total_input': 0,
                'total_output': 0,
                'total_turns': 0,
                'total_tools': 0,
                'ttfts': [],
                'speeds': [],
            }
        m = models[model_key]
        m['sessions'] += 1
        m['total_input'] += s['input_tokens']
        m['total_output'] += s['output_tokens']
        m['total_turns'] += s['turns']
        m['total_tools'] += s['tool_calls']

    # Compute averages and TTFT/speed from actual data
    for key, m in models.items():
        # Need to sample sessions for TTFT/speed
        # For efficiency, use the session list data
        pass

    result = []
    for key, m in models.items():
        result.append({
            'model': m['model'],
            'provider': m['provider'],
            'sessions': m['sessions'],
            'avg_input_tokens': m['total_input'] // m['sessions'] if m['sessions'] else 0,
            'avg_output_tokens': m['total_output'] // m['sessions'] if m['sessions'] else 0,
            'avg_turns': m['total_turns'] // m['sessions'] if m['sessions'] else 0,
            'avg_tool_calls': m['total_tools'] // m['sessions'] if m['sessions'] else 0,
        })
    result.sort(key=lambda x: x['sessions'], reverse=True)
    return result

def get_activity_chart():
    """Get sessions grouped by week for activity chart."""
    sessions = get_session_list()
    weekly = {}
    for s in sessions:
        dt_str = s['created_at'][:10]  # YYYY-MM-DD
        try:
            dt = datetime.strptime(dt_str, '%Y-%m-%d')
            iso = dt.isocalendar()
            week_key = f"{iso[0]}-W{iso[1]:02d}"
        except:
            week_key = dt_str
        if week_key not in weekly:
            weekly[week_key] = {
                'week': week_key,
                'sessions': 0,
                'input_tokens': 0,
                'output_tokens': 0,
                'providers': {},
            }
        w = weekly[week_key]
        w['sessions'] += 1
        w['input_tokens'] += s['input_tokens']
        w['output_tokens'] += s['output_tokens']
        p = s['provider']
        if p not in w['providers']:
            w['providers'][p] = 0
        w['providers'][p] += 1

    all_providers = set()
    for w in weekly.values():
        all_providers.update(w['providers'].keys())

    result = sorted(weekly.values(), key=lambda x: x['week'])
    return {'weeks': result, 'providers': sorted(list(all_providers))}


def get_skills_weekly():
    """Get skill loads grouped by week. Skills appear via synthetic messages
    with label='skill-load' and params.name, or via tool_calls named 'skill-load'.
    Week assignment is based on the message timestamp, not the session updated_at."""
    sessions = []
    for entry in os.listdir(SESSIONS_DIR):
        chat_path = os.path.join(SESSIONS_DIR, entry, 'chat.json')
        if not os.path.isfile(chat_path):
            continue
        filepath = chat_path
        filename = entry
        try:
            data = load_session(filename)
        except:
            continue

        messages = data.get('messages', [])

        for m in messages:
            # Use the message's own timestamp for week assignment
            msg_time = m.get('created_at', '')[:10]
            try:
                dt = datetime.strptime(msg_time, '%Y-%m-%d')
                iso = dt.isocalendar()
                week_key = f"{iso[0]}-W{iso[1]:02d}"
            except:
                continue

            if week_key not in weekly:
                weekly[week_key] = {}
            wk = weekly[week_key]

            skill_name = None

            # Check synthetic/system message with label='skill_load'
            if m.get('label') == 'skill_load':
                params = m.get('params', {})
                skill_name = params.get('name', '')

            # Check tool_calls for skill_load
            if not skill_name:
                for tc in m.get('tool_calls', []):
                    tc_name = tc.get('name', '') or (tc.get('instruction', {}) or {}).get('name', '')
                    if tc_name == 'skill_load':
                        args_raw = tc.get('arguments', '') or (tc.get('instruction', {}) or {}).get('arguments', '')
                        if args_raw:
                            try:
                                args = json.loads(args_raw)
                                skill_name = args.get('name', args.get('skill', ''))
                            except:
                                pass

            if skill_name:
                all_skills.add(skill_name)
                if skill_name not in wk:
                    wk[skill_name] = 0
                wk[skill_name] += 1

    weekly_ordered = sorted(weekly.items(), key=lambda x: x[0])

    return {
        'weeks': [w[1] for w in weekly_ordered],
        'week_keys': [w[0] for w in weekly_ordered],
        'skills': sorted(list(all_skills)),
    }


def build_turn(messages, msg_id):
    """Find the turn containing msg_id and return its messages in phone-conversation format.
    
    A turn is defined as: starting from the user message (or the target msg if it IS user),
    then all subsequent assistant/tool/synthetic/internal messages until the next user message.
    We find the enclosing turn by locating the nearest preceding user message.
    """
    if not messages:
        return None
    
    # Find index of target message
    target_idx = None
    for i, m in enumerate(messages):
        if m.get('id') == msg_id:
            target_idx = i
            break
    if target_idx is None:
        return None
    
    target = messages[target_idx]
    
    # Find the turn start: the nearest user message at or before target_idx
    turn_start = None
    for i in range(target_idx, -1, -1):
        if messages[i]['role'] == 'user':
            turn_start = i
            break
    
    # If no user message found (shouldn't happen normally), use the target itself
    if turn_start is None:
        turn_start = target_idx
    
    # Find the turn end: the next user message after turn_start, or end of list
    turn_end = len(messages)
    for i in range(turn_start + 1, len(messages)):
        if messages[i]['role'] == 'user':
            turn_end = i
            break
    
    # Extract the turn messages
    turn_msgs = messages[turn_start:turn_end]
    
    # Build rich formatted output
    rich_msgs = []
    for m in turn_msgs:
        rich = {
            'id': m.get('id', ''),
            'role': m.get('role', ''),
            'label': m.get('label', ''),
            'text': m.get('text', ''),
            'thinking_text': m.get('thinking_text', ''),
            'created_at': m.get('created_at', ''),
            'input_tokens': m.get('input_tokens', 0) or 0,
            'output_tokens': m.get('output_tokens', 0) or 0,
            'time_to_first_token_ms': m.get('time_to_first_token_ms'),
            'duration_ms': m.get('duration_ms'),
            'tok_per_sec': m.get('tok_per_sec'),
            'stop_reason': m.get('stop_reason', ''),
            'text_metrics': m.get('text_metrics') or {},
            'thinking_metrics': m.get('thinking_metrics') or {},
            'tool_call_metrics': m.get('tool_call_metrics') or {},
            'params': m.get('params') or {},
            'sequence_stat': m.get('sequence_stat') or None,
            'tool_calls': [],
        }
        
        # Process tool calls — handle both flat and nested formats
        for tc in m.get('tool_calls', []):
            inst = tc.get('instruction') or {}
            exe = tc.get('execution') or {}
            tc_name = tc.get('name') or inst.get('name') or 'unknown'
            tc_args = tc.get('arguments', '') or inst.get('arguments', '') or ''
            tc_result = tc.get('result', '') or exe.get('result', '') or ''
            tc_error = exe.get('error', '') or tc.get('error', '') or ''
            tc_inst_dur = inst.get('duration_ms', 0) or tc.get('duration_ms', 0)
            tc_exec_dur = exe.get('duration_ms', 0) or 0
            tc_inst_tokens = inst.get('tokens', 0) or 0
            tc_exec_tokens = exe.get('tokens', 0) or 0
            tc_files = exe.get('files', []) or []
            
            rich['tool_calls'].append({
                'id': tc.get('id', ''),
                'name': tc_name,
                'arguments': tc_args,
                'result': tc_result,
                'error': tc_error,
                'instruction_duration_ms': tc_inst_dur or 0,
                'execution_duration_ms': tc_exec_dur or 0,
                'instruction_tokens': tc_inst_tokens,
                'execution_tokens': tc_exec_tokens,
                'files': tc_files,
            })
        
        rich_msgs.append(rich)
    
    # Find prev/next turn user messages for navigation
    # Prev turn: user message before turn_start
    prev_msg_id = None
    for i in range(turn_start - 1, -1, -1):
        if messages[i]['role'] == 'user':
            prev_msg_id = messages[i]['id']
            break
    
    # Next turn: user message at turn_end (or after turn_end if turn_end points to the next user)
    next_msg_id = None
    if turn_end < len(messages):
        # turn_end is the index of the next user message
        if messages[turn_end]['role'] == 'user':
            next_msg_id = messages[turn_end]['id']
        else:
            # Look ahead for the next user message
            for i in range(turn_end, len(messages)):
                if messages[i]['role'] == 'user':
                    next_msg_id = messages[i]['id']
                    break
    
    return {
        'turn_start_msg_id': messages[turn_start]['id'],
        'turn_end_idx': turn_end,
        'prev_msg_id': prev_msg_id,
        'next_msg_id': next_msg_id,
        'messages': rich_msgs,
    }


class AnalyticsHandler(SimpleHTTPRequestHandler):
    def do_GET(self):
        path = self.path

        # API endpoints
        if path == '/api/health':
            return self.json_response({'status': 'ok', 'time': now_iso()})

        if path == '/api/sessions':
            return self.json_response(get_session_list())

        if path == '/api/dashboard/summary':
            return self.json_response(get_dashboard_summary())

        if path == '/api/dashboard/models':
            return self.json_response(get_models_table())

        if path == '/api/dashboard/activity':
            return self.json_response(get_activity_chart())

        if path == '/api/dashboard/skills':
            return self.json_response(get_skills_weekly())

        # Turn detail: /api/sessions/<filename>/turn?msg_id=msg_X
        import re
        import urllib.parse
        m_turn = re.match(r'^/api/sessions/([^/]+?)(?:\.chat\.json)?/turn', path)
        if m_turn:
            filename = m_turn.group(1)
            # Extract msg_id from query string
            parts = urllib.parse.urlparse(path)
            qs = urllib.parse.parse_qs(parts.query)
            msg_id = qs.get('msg_id', [''])[0]
            if not msg_id:
                return self.json_response({'error': 'msg_id parameter required'}, status=400)
            try:
                data = load_session(filename)
                messages = data.get('messages', [])
                turn = build_turn(messages, msg_id)
                return self.json_response(turn if turn else {'error': f'No message found with id {msg_id}'}, status=200 if turn else 404)
            except Exception as e:
                return self.json_response({'error': str(e)}, status=500)

        # Per-session endpoints: /api/sessions/<filename>/<endpoint>
        import re
        m = re.match(r'^/api/sessions/([^/]+?)(?:\.chat\.json)?/(tally|timeline|performance|tools|skills|files|general)$', path)
        if m:
            filename = m.group(1)
            endpoint = m.group(2)

            try:
                data = load_session(filename)
                parsed = parse_session_data(data)

                if endpoint == 'tally':
                    return self.json_response({
                        'filename': filename,
                        **parsed['tokens'],
                        **parsed['counts'],
                        'total_tokens': parsed['tokens'].get('session_total') or 0,
                    })
                elif endpoint == 'timeline':
                    return self.json_response({
                        'filename': filename,
                        'timeline': parsed['timeline'],
                    })
                elif endpoint == 'performance':
                    return self.json_response({
                        'filename': filename,
                        'timeline': parsed['performance_timeline'],
                        **parsed['performance'],
                    })
                elif endpoint == 'tools':
                    return self.json_response({
                        'filename': filename,
                        'tools': parsed['tools'],
                    })
                elif endpoint == 'skills':
                    stl = parsed['skills'].pop('timeline', [])
                    return self.json_response({
                        'filename': filename,
                        'skills': parsed['skills'],
                        'timeline': stl,
                    })
                elif endpoint == 'files':
                    return self.json_response({
                        'filename': filename,
                        'files': parsed['files'],
                        'compaction': parsed['file_compaction'],
                    })
                elif endpoint == 'general':
                    return self.json_response({
                        'filename': filename,
                        **parsed['session_info'],
                        **parsed['general'],
                        **parsed['counts'],
                        **parsed['performance'],
                    })
            except Exception as e:
                return self.json_response({'error': str(e)}, status=500)

        # Full session JSON: /api/sessions/<filename>
        m = re.match(r'^/api/sessions/([^/]+?)(?:\.chat\.json)?$', path)
        if m:
            filename = m.group(1)
            try:
                data = load_session(filename)
                return self.json_response(data)
            except Exception as e:
                return self.json_response({'error': str(e)}, status=500)

        # Serve index.html for root path
        if path == '/' or path == '/index.html':
            index_path = os.path.join(ASSETS_DIR, 'index.html')
            if os.path.exists(index_path):
                with open(index_path, 'rb') as f:
                    content = f.read()
                self.send_response(200)
                self.send_header('Content-Type', 'text/html')
                self.send_header('Content-Length', str(len(content)))
                self.end_headers()
                self.wfile.write(content)
                return

        # Serve assets under /assets/...
        if path.startswith('/assets/'):
            rel_path = path[len('/assets/'):]
            asset_path = os.path.join(ASSETS_DIR, rel_path)
            if os.path.isfile(asset_path):
                self.directory = ASSETS_DIR
                self.path = '/' + rel_path
                super().do_GET()
                return
        # Fallback to skill directory
        skill_path = os.path.join(SKILL_DIR, path.lstrip('/'))
        if os.path.isfile(skill_path):
            self.directory = SKILL_DIR
            super().do_GET()
            return

        self.send_response(404)
        self.send_header('Content-Type', 'text/plain')
        self.end_headers()
        self.wfile.write(b'Not found')

    def json_response(self, data, status=200):
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Access-Control-Allow-Origin', '*')
        self.end_headers()
        self.wfile.write(json.dumps(data).encode('utf-8'))

    def log_message(self, format, *args):
        # Quiet logging
        pass


def main():
    port = 17771

    # Verify sessions directory exists
    if not os.path.isdir(SESSIONS_DIR):
        print(f"Error: Sessions directory not found: {SESSIONS_DIR}", file=sys.stderr)
        print("Create a symlink: ln -s ~/.config/squid-os/sessions ./sessions", file=sys.stderr)
        sys.exit(1)

    # Count available sessions
    count = len([d for d in os.listdir(SESSIONS_DIR) if os.path.isfile(os.path.join(SESSIONS_DIR, d, 'chat.json'))])
    print(f"Chat Analytics Server")
    print(f"Sessions directory: {SESSIONS_DIR}")
    print(f"Sessions found: {count}")
    print(f"Serving on http://localhost:{port}")
    print(f"Press Ctrl+C to stop\n")

    server = HTTPServer(('0.0.0.0', port), AnalyticsHandler)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nShutting down.")
        server.server_close()


if __name__ == '__main__':
    main()
