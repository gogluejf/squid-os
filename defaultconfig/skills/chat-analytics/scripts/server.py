#!/usr/bin/env python3
"""
Chat Analytics Server.
Serves a static frontend and provides precomputed analytics endpoints
for Squid-OS session documents (sessionDir/chat.json with meta/initial/token_tally).
"""
import json
import os
import re
import sys
import glob
import time
from datetime import datetime, timezone, timedelta
from http.server import HTTPServer, SimpleHTTPRequestHandler
from urllib.parse import parse_qs

SKILL_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SESSIONS_DIR = os.path.join(SKILL_DIR, "sessions")
ASSETS_DIR = os.path.join(SKILL_DIR, "assets")


def now_iso():
    return datetime.now(timezone.utc).isoformat()


def parse_dt(dt_str):
    if not dt_str or str(dt_str).startswith('0001-'):
        return None
    try:
        s = str(dt_str).replace('Z', '+00:00')
        if '+' not in s and '-' not in s[10:]:
            s += '+00:00'
        dt = datetime.fromisoformat(s)
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        return dt
    except Exception:
        return None


def time_ago(dt_str):
    dt = parse_dt(dt_str)
    if not dt:
        return "unknown"
    total_seconds = int((datetime.now(timezone.utc) - dt).total_seconds())
    if total_seconds < 60:
        return f"{total_seconds}s ago"
    elif total_seconds < 3600:
        return f"{total_seconds // 60}m ago"
    elif total_seconds < 86400:
        return f"{total_seconds // 3600}h ago"
    elif total_seconds < 2592000:
        return f"{total_seconds // 86400}d ago"
    else:
        return f"{total_seconds // 2592000}y ago"


def _week_key(dt_str):
    dt = parse_dt(dt_str)
    if not dt:
        return None
    monday = dt - timedelta(days=dt.weekday())
    return monday.strftime('%Y-%m-%d')


def norm_tool_call(tc):
    out = {
        'id': tc.get('id', ''),
        'name': tc.get('name'),
        'arguments': tc.get('arguments'),
        'result': tc.get('result'),
        'error': tc.get('error'),
        'call_tokens': tc.get('call_tokens') or 0,
        'result_tokens': tc.get('result_tokens') or 0,
        'inference_ms': tc.get('duration_ms', 0) or 0,
        'execution_ms': tc.get('result_duration_ms', 0) or tc.get('call_duration_ms', 0) or 0,
        'tokens_per_sec': tc.get('tokens_per_sec'),
        'time_to_first_ms': tc.get('time_to_first_ms'),
    }
    inst = tc.get('instruction')
    exe = tc.get('execution')
    if isinstance(inst, dict):
        out['name'] = out['name'] or inst.get('name')
        if out['arguments'] is None:
            out['arguments'] = inst.get('arguments')
        out['call_tokens'] = out['call_tokens'] or inst.get('tokens', 0) or 0
        out['inference_ms'] = out['inference_ms'] or inst.get('duration_ms', 0) or 0
    if isinstance(exe, dict):
        if out['result'] is None:
            out['result'] = exe.get('result')
        out['error'] = out['error'] or exe.get('error')
        out['result_tokens'] = out['result_tokens'] or exe.get('tokens', 0) or 0
        out['execution_ms'] = out['execution_ms'] or exe.get('duration_ms', 0) or 0
    return out


def msg_metrics(m, key):
    v = m.get(key)
    return v if isinstance(v, dict) else {}


def skill_name_from_args(args):
    if isinstance(args, str):
        try:
            return json.loads(args).get('name')
        except Exception:
            return None
    if isinstance(args, dict):
        return args.get('name')
    return None


SKILL_HEADER_RE = re.compile('\u2550\u2550\u2550 SKILL: ([A-Za-z0-9._-]+)')


def extract_skills_from_text(text):
    return [m.group(1).strip() for m in SKILL_HEADER_RE.finditer(text)]


def parse_session_data(data):
    messages = data.get('messages', [])
    meta = data.get('meta', {}) or {}
    initial = data.get('initial', {}) or {}
    inference = initial.get('inference', {}) or {}
    tally_root = data.get('token_tally', {}) or {}
    lifetime = tally_root.get('lifetime', {}) or {}
    lt_in = lifetime.get('input', {}) or {}
    lt_out = lifetime.get('output', {}) or {}
    attachments = data.get('attachments', []) or []
    top_level_files = data.get('file_state', {}) or {}

    model = inference.get('model', '')
    provider = inference.get('provider', '')
    thinking_enabled = bool((inference.get('thinking', {}) or {}).get('enabled'))
    working_dir = initial.get('working_dir', '') or ''
    system_prompt_file = initial.get('system_prompt_file', '') or ''

    user_messages = [m for m in messages if m.get('role') == 'user']
    assistant_messages = [m for m in messages if m.get('role') == 'assistant']
    synthetic_messages = [m for m in messages if m.get('role') == 'synthetic']
    internal_messages = [m for m in messages if m.get('role') == 'internal']
    system_messages = [m for m in messages if m.get('role') == 'system']
    tool_messages = [m for m in messages if m.get('role') == 'tool']
    aborted_count = sum(1 for m in (synthetic_messages + internal_messages)
                        if (m.get('label') or '') == 'aborted')

    total_input = lt_in.get('total') or sum(m.get('input_tokens', 0) or 0 for m in messages)
    total_output = lt_out.get('total') or sum(m.get('output_tokens', 0) or 0 for m in messages)
    user_input = lt_in.get('user', 0) or sum(m.get('input_tokens', 0) or 0 for m in user_messages)
    attachment_tok = lt_in.get('attachment', 0)
    tool_execution_tok = lt_in.get('tool_execution', 0)
    tool_attachment_tok = lt_in.get('tool_attachment', 0)
    system_prompt_tok = lt_in.get('system_prompt', 0) or sum(
        (m.get('input_tokens', 0) or 0) + (msg_metrics(m, 'text_metrics').get('tokens', 0) or 0)
        for m in system_messages)
    tool_definition_tok = lt_in.get('tool_definitions', 0)
    synthetic_tok = lt_in.get('synthetic', 0) or sum(
        (m.get('input_tokens', 0) or 0) + (m.get('output_tokens', 0) or 0)
        for m in synthetic_messages + internal_messages)
    assistant_output = lt_out.get('assistant', 0) or sum(
        max((m.get('output_tokens') or 0)
            - (msg_metrics(m, 'thinking_metrics').get('tokens') or 0)
            - (msg_metrics(m, 'tool_call_metrics').get('tokens') or 0), 0)
        for m in assistant_messages if 'output_tokens' in m)
    thinking_tok = lt_out.get('thinking', 0) or sum(
        msg_metrics(m, 'thinking_metrics').get('tokens', 0) or 0 for m in assistant_messages)
    tool_call_tok = lt_out.get('tool_calls', 0) or sum(
        msg_metrics(m, 'tool_call_metrics').get('tokens', 0) or 0 for m in assistant_messages)

    all_tool_calls = []
    for m in messages:
        for tc in (m.get('tool_calls') or []):
            if isinstance(tc, dict):
                all_tool_calls.append(norm_tool_call(tc))

    tool_usage = {}
    for tc in all_tool_calls:
        name = tc['name'] or 'unknown'
        if name not in tool_usage:
            tool_usage[name] = {'calls': 0, 'tokens_in': 0, 'tokens_out': 0,
                                'inference_ms': 0, 'execution_ms': 0}
        t = tool_usage[name]
        t['calls'] += 1
        t['tokens_in'] += tc['call_tokens']
        t['tokens_out'] += tc['result_tokens']
        t['inference_ms'] += tc['inference_ms']
        t['execution_ms'] += tc['execution_ms']

    skills_data = {}

    def add_skill(name, tokens=0):
        if not name:
            return
        if name not in skills_data:
            skills_data[name] = {'loads': 0, 'tokens': 0}
        skills_data[name]['loads'] += 1
        skills_data[name]['tokens'] += tokens

    for tc in all_tool_calls:
        if tc['name'] == 'skill_load':
            add_skill(skill_name_from_args(tc['arguments']), tc['call_tokens'])
    for m in user_messages + assistant_messages:
        t = m.get('text') or ''
        if 'def ' in t or 'function ' in t or 'import ' in t:
            continue  # code dump, not a real skill banner
        for sk in extract_skills_from_text(t):
            add_skill(sk)

    ttft_values = [m['time_to_first_token_ms'] for m in assistant_messages
                   if 0 < (m.get('time_to_first_token_ms') or 0) <= 600000]
    avg_ttft = sum(ttft_values) / len(ttft_values) if ttft_values else 0

    speeds = []
    for m in assistant_messages:
        ss = m.get('sequence_stat', {}) or {}
        if (ss.get('avg_tok_per_sec') or 0) > 0:
            speeds.append(ss['avg_tok_per_sec'])
        elif (m.get('tok_per_sec') or 0) > 0:
            speeds.append(m['tok_per_sec'])
    avg_speed = sum(speeds) / len(speeds) if speeds else 0

    total_duration_ms = sum(m.get('duration_ms', 0) or 0 for m in assistant_messages)
    num_turns = len(user_messages)

    files_touched = {}

    def touch_file(filepath, trace, tokens=0, updated_at='', checksum=''):
        if filepath not in files_touched:
            files_touched[filepath] = {
                'path': filepath, 'checksum': '', 'reads': 0, 'edits': 0,
                'creates': 0, 'writes': 0, 'traces': [], 'total_tokens': 0,
                'reads_tokens': 0, 'edits_tokens': 0, 'creates_tokens': 0,
                'writes_tokens': 0, 'last_updated': '',
            }
        f = files_touched[filepath]
        f['traces'].append(trace)
        if trace == 'read':
            f['reads'] += 1
        elif trace == 'edit':
            f['edits'] += 1
        elif trace == 'create':
            f['creates'] += 1
        elif trace == 'write':
            f['writes'] += 1
        f['total_tokens'] += tokens
        key = {'read': 'reads_tokens', 'edit': 'edits_tokens',
               'create': 'creates_tokens', 'write': 'writes_tokens'}.get(trace)
        if key:
            f[key] = f.get(key, 0) + tokens
        if updated_at > f['last_updated']:
            f['last_updated'] = updated_at
        if checksum and not f['checksum']:
            f['checksum'] = checksum

    for fp, info in top_level_files.items():
        if isinstance(info, dict):
            touch_file(fp, info.get('trace', 'unknown'), 0,
                       info.get('updated_at', ''), info.get('checksum', ''))
    for m in messages:
        ss = m.get('sequence_stat', {}) or {}
        for fp, info in (ss.get('file_state') or {}).items():
            if isinstance(info, dict):
                touch_file(fp, info.get('trace', 'unknown'), ss.get('output_tokens', 0) or 0,
                           info.get('updated_at', ''), info.get('checksum', ''))

    timeline = []
    perf_timeline = []
    skill_timeline = []
    ctx_total = 0
    for i, m in enumerate(messages):
        role = m.get('role', 'unknown')
        input_tok = m.get('input_tokens', 0) or 0
        output_tok = m.get('output_tokens', 0) or 0
        duration = m.get('duration_ms', 0) or 0
        ss = m.get('sequence_stat', {}) or {}
        msg_files = list((ss.get('file_state') or {}).keys())

        norm_tcs = [norm_tool_call(tc) for tc in (m.get('tool_calls') or [])
                    if isinstance(tc, dict)]
        think_tok_b = msg_metrics(m, 'thinking_metrics').get('tokens', 0) or 0
        text_tok_b = msg_metrics(m, 'text_metrics').get('tokens', 0) or 0
        tool_instr_tok = msg_metrics(m, 'tool_call_metrics').get('tokens', 0) or 0
        tool_exec_tok = sum(tc['result_tokens'] for tc in norm_tcs)

        entry = {
            'index': i,
            'id': m.get('id', ''),
            'role': role,
            'timestamp': m.get('created_at', ''),
            'input_tokens': input_tok,
            'output_tokens': output_tok,
            'thinking_tokens': think_tok_b,
            'text_tokens': text_tok_b,
            'duration_ms': duration,
            'tool_count': len(norm_tcs),
            'files': msg_files,
            'user_input_tok': input_tok if role == 'user' else 0,
            'think_tok_b': think_tok_b,
            'text_tok_b': text_tok_b,
            'tool_instr_tok': tool_instr_tok,
            'tool_exec_tok': tool_exec_tok,
            'ttft_dur_ms': m.get('time_to_first_token_ms', 0) or 0,
            'thinking_dur_ms': msg_metrics(m, 'thinking_metrics').get('inference_duration_ms', 0) or 0,
            'text_dur_ms': msg_metrics(m, 'text_metrics').get('inference_duration_ms', 0) or 0,
            'tool_call_dur_ms': msg_metrics(m, 'tool_call_metrics').get('inference_duration_ms', 0) or 0,
            'tool_exec_dur_ms': sum(tc['execution_ms'] for tc in norm_tcs),
        }
        timeline.append(entry)

        speed = ss.get('avg_tok_per_sec') or m.get('tok_per_sec') or 0
        if role == 'assistant' and speed and speed > 0:
            ctx_total += input_tok + output_tok
            perf_timeline.append({
                'index': i,
                'id': m.get('id', ''),
                'tokens_per_second': round(speed, 1),
                'ttft_ms': m.get('time_to_first_token_ms', 0) or 0,
                'ctx_total': ctx_total,
                'current_model_label': f"{provider}/{model}" if provider else model,
                'role': role,
            })

        skill_names = [skill_name_from_args(tc.get('arguments'))
                       for tc in (m.get('tool_calls') or [])
                       if isinstance(tc, dict) and tc.get('name') == 'skill_load']
        skill_names = [n for n in skill_names if n]
        tok = input_tok + output_tok
        if skill_names:
            for sn in skill_names:
                skill_timeline.append({'type': 'skill', 'name': sn,
                                       'id': m.get('id', ''), 'tokens': max(tok, 1)})
        elif tok > 0:
            if skill_timeline and skill_timeline[-1]['type'] == 'not_skill':
                skill_timeline[-1]['tokens'] += tok
            else:
                skill_timeline.append({'type': 'not_skill', 'id': m.get('id', ''), 'tokens': tok})

    system_prompt_text = ''
    for m in system_messages:
        if m.get('text'):
            system_prompt_text = m['text']
            break

    init_params = {}
    for m in system_messages:
        if m.get('params'):
            init_params = m['params']
            break

    return {
        'session_info': {
            'id': (data.get('identity', {}) or {}).get('id', ''),
            'title': working_dir.split('/')[-1] if working_dir else '',
            'model': model,
            'provider': provider,
            'created_at': meta.get('created_at', ''),
            'updated_at': meta.get('updated_at', ''),
            'system_prompt_file': system_prompt_file,
            'thinking': thinking_enabled,
            'working_dir': working_dir,
        },
        'counts': {
            'total_messages': len(messages),
            'user_messages': len(user_messages),
            'assistant_messages': len(assistant_messages),
            'synthetic_messages': len(synthetic_messages),
            'system_messages': len(system_messages),
            'tool_messages': len(tool_messages),
            'internal_messages': len(internal_messages),
            'turns': num_turns,
            'tool_calls': len(all_tool_calls),
            'aborted': aborted_count,
            'attachments': len(attachments),
        },
        'tokens': {
            'total_input': total_input,
            'total_output': total_output,
            'total_tokens': total_input + total_output,
            'user_input': user_input,
            'attachment': attachment_tok,
            'tool_attachment': tool_attachment_tok,
            'assistant_output': assistant_output,
            'thinking': thinking_tok,
            'text': assistant_output,
            'tool_call': tool_call_tok,
            'tool_instruction': tool_call_tok,
            'tool_execution': tool_execution_tok,
            'system_prompt': system_prompt_tok,
            'tool_definition': tool_definition_tok,
            'synthetic': synthetic_tok,
        },
        'performance': {
            'avg_ttft_ms': round(avg_ttft, 1),
            'avg_speed_tok_per_sec': round(avg_speed, 1),
            'total_duration_ms': total_duration_ms,
        },
        'tools': tool_usage,
        'skills': skills_data,
        'files': list(files_touched.values()),
        'timeline': timeline,
        'performance_timeline': perf_timeline,
        'skill_timeline': skill_timeline,
        'general': {
            'system_prompt_file': system_prompt_file,
            'system_prompt_preview': system_prompt_text[:200] if system_prompt_text else '',
            'init_params': init_params,
            'thinking_enabled': thinking_enabled,
            'inference': inference,
            'environment': working_dir,
        },
    }


def find_session_file(ref):
    candidates = [
        os.path.join(SESSIONS_DIR, ref),
        os.path.join(SESSIONS_DIR, ref, 'chat.json'),
        os.path.join(SESSIONS_DIR, ref + '.json'),
        os.path.join(SESSIONS_DIR, ref + '.chat.json'),
    ]
    for c in candidates:
        if os.path.isfile(c):
            return c
    matches = glob.glob(os.path.join(SESSIONS_DIR, ref + '*', 'chat.json'))
    if matches:
        return matches[0]
    return None


def get_session_list():
    pattern = os.path.join(SESSIONS_DIR, "*", "chat.json")
    files = glob.glob(pattern)
    sessions = []
    for filepath in files:
        filename = os.path.basename(os.path.dirname(filepath))
        try:
            stat = os.stat(filepath)
            with open(filepath, 'r') as f:
                data = json.load(f)
            parsed = parse_session_data(data)
            si = parsed['session_info']
            sessions.append({
                'filename': filename,
                'created_at': si['created_at'],
                'updated_at': si['updated_at'],
                'time_ago': time_ago(si['created_at']),
                'model': si['model'],
                'provider': si['provider'],
                'messages': parsed['counts']['total_messages'],
                'turns': parsed['counts']['turns'],
                'input_tokens': parsed['tokens']['total_input'],
                'output_tokens': parsed['tokens']['total_output'],
                'total_tokens': parsed['tokens']['total_input'] + parsed['tokens']['total_output'],
                'tool_calls': parsed['counts']['tool_calls'],
                'file_size': stat.st_size,
                'mtime': stat.st_mtime,
            })
        except Exception as e:
            print(f"Error parsing {filepath}: {e}", file=sys.stderr)
            continue
    sessions.sort(key=lambda s: s['created_at'] or '', reverse=True)
    return sessions


def get_dashboard_summary():
    sessions = get_session_list()
    if not sessions:
        return {'error': 'No sessions found'}

    total_conversations = len(sessions)
    total_input = sum(s['input_tokens'] for s in sessions)
    total_output = sum(s['output_tokens'] for s in sessions)
    avg_input = total_input // total_conversations if total_conversations else 0
    avg_output = total_output // total_conversations if total_conversations else 0

    mtimes = [s['mtime'] for s in sessions]
    if mtimes:
        avg_age_seconds = time.time() - (sum(mtimes) / len(mtimes))
        if avg_age_seconds < 86400:
            avg_age_str = f"{int(avg_age_seconds // 3600)}h ago"
        elif avg_age_seconds < 2592000:
            avg_age_str = f"{int(avg_age_seconds // 86400)}d ago"
        else:
            avg_age_str = f"{int(avg_age_seconds // 2592000)}y ago"
    else:
        avg_age_str = "N/A"

    total_turns = sum(s['turns'] for s in sessions)
    avg_turns = total_turns // total_conversations if total_conversations else 0
    total_tools = sum(s['tool_calls'] for s in sessions)
    avg_tools = total_tools // total_conversations if total_conversations else 0

    all_ttfts = []
    all_speeds = []
    for s in sessions[:50]:
        path = find_session_file(s['filename'])
        if not path:
            continue
        try:
            with open(path, 'r') as f:
                parsed = parse_session_data(json.load(f))
            if parsed['performance']['avg_ttft_ms'] > 0:
                all_ttfts.append(parsed['performance']['avg_ttft_ms'])
            if parsed['performance']['avg_speed_tok_per_sec'] > 0:
                all_speeds.append(parsed['performance']['avg_speed_tok_per_sec'])
        except Exception:
            pass

    avg_ttft = sum(all_ttfts) / len(all_ttfts) if all_ttfts else 0
    avg_speed = sum(all_speeds) / len(all_speeds) if all_speeds else 0

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
            'model': s['model'],
            'created_at': s['created_at'],
        } for s in top_10],
    }


def get_models_table():
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
            }
        m = models[model_key]
        m['sessions'] += 1
        m['total_input'] += s['input_tokens']
        m['total_output'] += s['output_tokens']
        m['total_turns'] += s['turns']
        m['total_tools'] += s['tool_calls']

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
    sessions = get_session_list()
    weekly = {}
    providers = set()
    for s in sessions:
        wk = _week_key(s['created_at'])
        if not wk:
            continue
        if wk not in weekly:
            weekly[wk] = {'week': wk, 'sessions': 0, 'input_tokens': 0,
                          'output_tokens': 0, 'providers': {}}
        w = weekly[wk]
        w['sessions'] += 1
        w['input_tokens'] += s['input_tokens']
        w['output_tokens'] += s['output_tokens']
        prov = s['provider'] or 'unknown'
        providers.add(prov)
        w['providers'][prov] = w['providers'].get(prov, 0) + 1

    result = sorted(weekly.values(), key=lambda x: x['week'])
    return {'weeks': result, 'providers': sorted(providers)}


def get_skills_chart():
    sessions = get_session_list()
    weekly = {}
    skill_names = set()
    for s in sessions:
        path = find_session_file(s['filename'])
        if not path:
            continue
        try:
            with open(path, 'r') as f:
                parsed = parse_session_data(json.load(f))
        except Exception:
            continue
        wk = _week_key(s['created_at'])
        if not wk:
            continue
        for name, info in parsed['skills'].items():
            skill_names.add(name)
            if wk not in weekly:
                weekly[wk] = {}
            weekly[wk][name] = weekly[wk].get(name, 0) + info['loads']

    weeks_sorted = sorted(weekly.keys())
    return {
        'week_keys': weeks_sorted,
        'weeks': [weekly[wk] for wk in weeks_sorted],
        'skills': sorted(skill_names),
    }


def get_turn_data(session_path, msg_id):
    with open(session_path, 'r') as f:
        data = json.load(f)
    messages = data.get('messages', [])
    idx = None
    for i, m in enumerate(messages):
        if m.get('id') == msg_id:
            idx = i
            break
    if idx is None:
        return {'error': f'message {msg_id} not found'}

    start = idx
    while start > 0 and messages[start - 1].get('role') != 'user':
        start -= 1
    end = idx
    while end + 1 < len(messages) and messages[end + 1].get('role') != 'user':
        end += 1

    prev_user = None
    for i in range(start - 1, -1, -1):
        if messages[i].get('role') == 'user':
            prev_user = messages[i].get('id')
            break
    next_user = None
    for i in range(end + 1, len(messages)):
        if messages[i].get('role') == 'user':
            next_user = messages[i].get('id')
            break

    return {
        'messages': messages[start:end + 1],
        'turn_start_msg_id': messages[start].get('id'),
        'prev_msg_id': prev_user,
        'next_msg_id': next_user,
    }


class AnalyticsHandler(SimpleHTTPRequestHandler):
    def do_GET(self):
        raw = self.path
        path = raw.split('?')[0]
        query = raw.split('?', 1)[1] if '?' in raw else ''

        if path == '/api/sessions':
            return self.json_response(get_session_list())

        if path == '/api/dashboard/summary':
            return self.json_response(get_dashboard_summary())

        if path == '/api/dashboard/models':
            return self.json_response(get_models_table())

        if path == '/api/dashboard/activity':
            return self.json_response(get_activity_chart())

        if path == '/api/dashboard/skills':
            return self.json_response(get_skills_chart())

        m = re.match(r'^/api/sessions/(.+)/(tally|timeline|performance|tools|skills|files|general)$', path)
        if m:
            ref, endpoint = m.group(1), m.group(2)
            session_path = find_session_file(ref)
            if not session_path:
                return self.json_response({'error': 'session not found'}, status=404)
            try:
                with open(session_path, 'r') as f:
                    parsed = parse_session_data(json.load(f))
                filename = os.path.basename(os.path.dirname(session_path))

                if endpoint == 'tally':
                    return self.json_response({'filename': filename, **parsed['tokens'], **parsed['counts']})
                elif endpoint == 'timeline':
                    return self.json_response({'filename': filename, 'timeline': parsed['timeline']})
                elif endpoint == 'performance':
                    return self.json_response({'filename': filename,
                                               'timeline': parsed['performance_timeline'],
                                               **parsed['performance']})
                elif endpoint == 'tools':
                    return self.json_response({'filename': filename, 'tools': parsed['tools']})
                elif endpoint == 'skills':
                    return self.json_response({'filename': filename,
                                               'skills': parsed['skills'],
                                               'timeline': parsed['skill_timeline']})
                elif endpoint == 'files':
                    return self.json_response({'filename': filename, 'files': parsed['files']})
                elif endpoint == 'general':
                    return self.json_response({'filename': filename,
                                               **parsed['session_info'],
                                               **parsed['general'],
                                               **parsed['counts'],
                                               **parsed['performance']})
            except Exception as e:
                return self.json_response({'error': str(e)}, status=500)

        m = re.match(r'^/api/sessions/(.+)/turn$', path)
        if m:
            ref = m.group(1)
            session_path = find_session_file(ref)
            if not session_path:
                return self.json_response({'error': 'session not found'}, status=404)
            qs = parse_qs(query)
            msg_id = (qs.get('msg_id') or [''])[0]
            if not msg_id:
                return self.json_response({'error': 'msg_id required'}, status=400)
            try:
                return self.json_response(get_turn_data(session_path, msg_id))
            except Exception as e:
                return self.json_response({'error': str(e)}, status=500)

        m = re.match(r'^/api/sessions/(.+)$', path)
        if m:
            session_path = find_session_file(m.group(1))
            if session_path:
                try:
                    with open(session_path, 'r') as f:
                        return self.json_response(json.load(f))
                except Exception as e:
                    return self.json_response({'error': str(e)}, status=500)
            return self.json_response({'error': 'session not found'}, status=404)

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

        asset_path = os.path.join(ASSETS_DIR, path.lstrip('/'))
        if os.path.isfile(asset_path):
            super().do_GET()
            return
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
        pass


def main():
    port = 17771

    if not os.path.isdir(SESSIONS_DIR):
        print(f"Error: Sessions directory not found: {SESSIONS_DIR}", file=sys.stderr)
        print("Create a symlink: ln -s ~/.config/squid-os/sessions ./sessions", file=sys.stderr)
        sys.exit(1)

    count = len(glob.glob(os.path.join(SESSIONS_DIR, "*", "chat.json")))
    print(f"Chat Analytics Server")
    print(f"Sessions directory: {SESSIONS_DIR}")
    print(f"Sessions found: {count}")

    server = HTTPServer(('localhost', port), AnalyticsHandler)
    print(f"Serving on http://localhost:{port}")
    server.serve_forever()


if __name__ == '__main__':
    main()
