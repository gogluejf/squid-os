#!/usr/bin/env python3
"""
Rich HTML Plan Renderer.
Loads CSS and Prism.js from assets and glues them into the report.
"""
import sys
import os
import html as html_module

def get_assets_path():
    return os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "assets")

def load_asset(name):
    path = os.path.join(get_assets_path(), name)
    if os.path.exists(path):
        with open(path, "r") as f:
            return f.read()
    return ""

EXT_MAP = {
    ".go": "language-go", ".js": "language-js", ".ts": "language-typescript", ".tsx": "language-typescript",
    ".py": "language-python", ".sh": "language-bash", ".sql": "language-sql",
    ".yaml": "language-yaml", ".yml": "language-yaml", ".json": "language-json",
    ".md": "language-markdown", ".html": "language-markup", ".htm": "language-markup",
    ".css": "language-css", ".rs": "language-rust", ".java": "language-java",
    ".c": "language-c", ".cpp": "language-cpp", ".rb": "language-ruby", ".php": "language-php",
    ".tf": "language-hcl", ".xml": "language-xml", ".proto": "language-protobuf",
    ".dockerfile": "language-docker", ".docker": "language-docker", ".env": "language-bash"
}

def detect_language(snippet, files):
    # 1. Check file extensions for strong signals
    for f in files:
        for ext, lang in EXT_MAP.items():
            if f.endswith(ext):
                return lang
    
    # 2. Fall back to content heuristics
    s = snippet.lower()
    if "func " in s or "type " in s or "package " in s: return "language-go"
    if "import " in s and ".go" in s: return "language-go"
    if "def " in s or "import " in s or "class " in s: return "language-python"
    if "interface " in s or "const " in s or "function " in s or "import " in s: return "language-typescript"
    if "import " in s and ("require" in s or ".js" in s or ".jsx" in s): return "language-js"
    if "SELECT " in s or "INSERT " in s or "CREATE " in s: return "language-sql"
    if "if [" in s or "while " in s or "export " in s: return "language-bash"
    return "language-unknown"

def parse_plan(content):
    epic = {"name": "", "why": "", "outcomes": ""}
    milestones = []
    current_milestone = None
    
    lines = content.split('\n')
    for line in lines:
        line = line.strip()
        if not line: continue

        if line.startswith("# EPIC:"):
            epic["name"] = line[7:].strip()
            epic["title"] = line[7:].strip()
        elif line.startswith("Why:") and not current_milestone:
            epic["why"] = line[4:].strip()
        elif line.startswith("Outcomes:") and not current_milestone:
            epic["outcomes"] = line[10:].strip()
            
        elif line.startswith("## MILESTONE:"):
            if current_milestone:
                milestones.append(current_milestone)
            raw = line[14:].strip()
            parts = raw.split("-", 1)
            num = parts[0].strip()
            name = parts[1].strip() if len(parts) > 1 else raw
            current_milestone = {
                "number": num,
                "name": name,
                "pattern": [], 
                "objective": "", 
                "success": "", 
                "diagram": "", 
                "tasks": []
            }
        elif current_milestone:
            # If we're inside a multi-line diagram, accumulate continuation lines
            if current_milestone.get("_in_diagram", False) and not line.startswith(("Pattern:", "Objective:", "Success:", "Diagram:", "### TASK:")):
                current_milestone["diagram"] += "\n" + line
                continue
            if line.startswith("Pattern:"):
                current_milestone["pattern"].append(line[8:].strip())
                current_milestone["_in_diagram"] = False
            elif line.startswith("Objective:"):
                current_milestone["objective"] = line[10:].strip()
                current_milestone["_in_diagram"] = False
            elif line.startswith("Success:"):
                current_milestone["success"] = line[8:].strip()
                current_milestone["_in_diagram"] = False
            elif line.startswith("Diagram:"):
                current_milestone["diagram"] = line[8:].strip()
                current_milestone["_in_diagram"] = True
            elif line.startswith("### TASK:"):
                current_milestone["_in_diagram"] = False
                raw = line[10:].strip()
                parts = raw.split("-", 1)
                num = parts[0].strip()
                name = parts[1].strip() if len(parts) > 1 else raw
                current_milestone["tasks"].append({
                    "number": num,
                    "name": name,
                    "type": "",
                    "what": "",
                    "why": "",
                    "files": [],
                    "snippet": [],
                    "acceptance": [],
                    "verify": []
                })
            else:
                if current_milestone["tasks"]:
                    t = current_milestone["tasks"][-1]
                    if line.startswith("What:"): t["what"] = line[5:].strip()
                    elif line.startswith("Why:"): t["why"] = line[4:].strip()
                    elif line.startswith("Type:"): t["type"] = line[5:].strip()
                    elif line.startswith("Files:"): t["files"].append(line[6:].strip())
                    elif line.startswith("Snippet:"): t["snippet"].append(line[8:].strip())
                    elif line.startswith("Acceptance:"): t["acceptance"].append(line[11:].strip())
                    elif line.startswith("Verification:"): t["verify"].append(line[14:].strip())
                    
    if current_milestone:
        milestones.append(current_milestone)
        
    return {"epic": epic, "milestones": milestones}

MERMAID_DIRECTIVES = (
    "graph", "flowchart", "sequencediagram", "statediagram",
    "classdiagram", "erdiagram", "gantt", "pie", "journey",
    "gitgraph", "c4context", "c4container", "c4component", "c4rel",
)

def is_mermaid(diagram_text):
    """Check if diagram text starts with a known Mermaid directive."""
    first_line = diagram_text.strip().split("\n")[0].strip().lower()
    for d in MERMAID_DIRECTIVES:
        if first_line.startswith(d):
            return True
    return False

def parse_flow(diagram_text):
    """Parse diagram text. If Mermaid syntax detected, render as Mermaid.
    Otherwise fall back to legacy arrow-split flow steps."""
    if is_mermaid(diagram_text):
        # Do NOT HTML-escape Mermaid content — it needs literal syntax
        return '<pre class="mermaid">{}</pre>'.format(diagram_text)

    # Legacy: split on arrows into flow steps
    text = diagram_text.replace("->", "||").replace("→", "||").replace("=>", "||")
    parts = [p.strip() for p in text.split("||")]
    if len(parts) < 2:
        return '<div class="diagram-content">{}</div>'.format(html_module.escape(diagram_text))

    h = '<div class="flow-diagram">'
    for i, p in enumerate(parts):
        if i > 0:
            h += '<span class="flow-arrow">&rarr;</span>'
        h += '<div class="flow-step">{}</div>'.format(html_module.escape(p))
    h += '</div>'
    return h

def generate_html(data, output_file, plan_name):
    # Load assets
    css_content = load_asset("plan-style.css")
    prism_css = load_asset("prism-tomorrow.min.css")
    prism_js = load_asset("prism.min.js")
    prism_go = load_asset("prism-go.min.js")
    prism_js_lib = load_asset("prism-js.min.js")
    prism_bash = load_asset("prism-bash.min.js")
    prism_py = load_asset("prism-python.min.js")
    prism_ts = load_asset("prism-typescript.min.js")
    prism_yaml = load_asset("prism-yaml.min.js")
    prism_sql = load_asset("prism-sql.min.js")
    prism_json = load_asset("prism-json.min.js")
    prism_markup = load_asset("prism-markup.min.js")
    mermaid_js = load_asset("mermaid.min.js")
    
    # 1. Navigation
    nav_items = ''
    for m in data["milestones"]:
        anchor = m["name"].replace(" ", "-").lower()
        nav_items += '<a href="#{}" class="nav-item">{}</a>'.format(anchor, html_module.escape(m["name"]))
    nav_links = '<a href="#epic-overview" class="nav-item home always-visible">🏠 Epic</a><div class="nav-items" id="nav-items">' + nav_items + '</div><button class="hamburger" id="hamburger" aria-label="Toggle menu"><span></span><span></span><span></span></button>'
    
    # 2. Epic Home Section
    ms_cards = []
    for m in data["milestones"]:
        anchor = m["name"].replace(" ", "-").lower()
        preview = ""
        if m["tasks"]:
            items = "".join('<li>{} - {}</li>'.format(html_module.escape(t["number"]), html_module.escape(t["name"])) for t in m["tasks"])
            preview = '<ul class="ms-task-list">{}</ul>'.format(items)
        
        pattern_tags = ""
        for p in m["pattern"]:
            pattern_tags += '<span class="pattern-tag">{}</span> '.format(html_module.escape(p))
        
        ms_cards.append('''
        <a href="#{}" class="milestone-card-link">
            <div class="milestone-card">
                <div class="ms-header">
                    <span class="ms-num">{}</span>
                    <span class="ms-name">{}</span>
                </div>
                {}
                <p class="ms-count">{} Task(s)</p>
                {}
            </div>
        </a>'''.format(
            anchor, 
            html_module.escape(m["number"]), 
            html_module.escape(m["name"]), 
            pattern_tags,
            len(m["tasks"]), 
            preview
        ))
        
    epic_html = '''
    <section id="epic-overview" class="epic-header">
        <h1>{}</h1>
        <div class="epic-meta">
            <div class="meta-box">
                <span class="meta-label">Why</span>
                <div class="meta-value">{}</div>
            </div>
            <div class="meta-box">
                <span class="meta-label">Outcomes</span>
                <div class="meta-value">{}</div>
            </div>
        </div>
        <div class="milestone-grid">
            {}
        </div>
    </section>
    '''.format(
        html_module.escape(data["epic"]["name"]), 
        html_module.escape(data["epic"]["why"]), 
        html_module.escape(data["epic"]["outcomes"]), 
        ''.join(ms_cards)
    )
    
    # 3. Milestone Sections
    sections_html = ""
    for m in data["milestones"]:
        anchor = m["name"].replace(" ", "-").lower()
        
        pattern_badges = ""
        for p in m["pattern"]:
            pattern_badges += '<span class="pattern-badge">{}</span> '.format(html_module.escape(p))
        
        diagram_card = ""
        if m["diagram"]:
            flow_html = parse_flow(m["diagram"])
            diagram_card = '''
            <div class="diagram-canvas">
                <div class="diagram-header">
                    <span class="icon">📐</span>
                    <span class="label">Architecture & Flow</span>
                </div>
                {}
            </div>'''.format(flow_html)
        
        objective_box = ""
        if m["objective"]:
            objective_box = '''
            <div class="ms-overview-box">
                <span class="cell-label">Objective</span>
                <div class="cell-value">{}</div>
            </div>'''.format(html_module.escape(m["objective"]))
            
        success_box = ""
        if m["success"]:
            success_box = '''
            <div class="ms-success-box">
                <span class="cell-label success">Success Criteria</span>
                <div class="cell-value">{}</div>
            </div>'''.format(html_module.escape(m["success"]))

        task_cards = ""
        for t in m["tasks"]:
            type_color = {
                "feature": "#569cd6",
                "refactor": "#c586c0",
                "bug": "#f44747",
                "test": "#4ec9b0",
                "doc": "#dcdcaa",
                "chore": "#888"
            }.get(t["type"], "#888")

            type_badge = '<span class="layer-badge" style="background:{}">{}</span>'.format(type_color, html_module.escape(t["type"]))
            
            verify_badges = ""
            for v in t["verify"]:
                verify_badges += '<span class="task-verify">✔ {}</span> '.format(html_module.escape(v))
            
            snippet_blocks = ""
            for s in t["snippet"]:
                s = s.replace("\\n", "\n").replace("\\t", "\t")
                # Detect language based on files first, then content
                lang_class = detect_language(s, t["files"])
                snippet_blocks += '''
                <div class="task-cell full-width snippet-block">
                    <span class="cell-label">Snippet</span>
                    <pre><code class="{}">{}</code></pre>
                </div>'''.format(lang_class, html_module.escape(s))

            acceptance_list = ""
            if t["acceptance"]:
                items = ""
                for a in t["acceptance"]:
                    items += '<li>{}</li>'.format(html_module.escape(a))
                acceptance_list = '''
                <div class="task-cell full-width">
                    <span class="cell-label">Acceptance Criteria</span>
                    <ul class="acceptance-list">{}</ul>
                </div>'''.format(items)

            files_html = ""
            if t["files"]:
                file_items = ""
                for f in t["files"]:
                    f_esc = html_module.escape(f)
                    if f.startswith("+"): file_items += '<li class="file-add"><span class="action-sym">+</span> {}</li>'.format(f_esc[1:])
                    elif f.startswith("~"): file_items += '<li class="file-mod"><span class="action-sym">~</span> {}</li>'.format(f_esc[1:])
                    elif f.startswith("-"): file_items += '<li class="file-rm"><span class="action-sym">-</span> {}</li>'.format(f_esc[1:])
                    else: file_items += '<li class="file-plain">{}</li>'.format(f_esc)
                files_html = '''
                <div class="task-cell full-width">
                    <span class="cell-label">Files</span>
                    <ul class="files-list">{}</ul>
                </div>'''.format(file_items)

            task_cards += '''
            <div class="task-card">
                <div class="task-header">
                    <div class="task-title-row">
                        <span class="task-num"># {}</span>
                        <span class="task-name">{}</span>
                        {}
                    </div>
                </div>
                <div class="task-grid">
                    <div class="task-cell">
                        <span class="cell-label">What</span>
                        <span class="cell-value">{}</span>
                    </div>
                    <div class="task-cell">
                        <span class="cell-label">Why</span>
                        <span class="cell-value">{}</span>
                    </div>
                    {}
                    {}
                    {}
                </div>
                <div class="task-footer">{}</div>
            </div>'''.format(
                html_module.escape(t["number"]),
                html_module.escape(t["name"]),
                type_badge,
                html_module.escape(t["what"]),
                html_module.escape(t["why"]),
                files_html,
                snippet_blocks,
                acceptance_list,
                verify_badges
            )
        
        sections_html += '''
        <section id="{}" class="milestone-section">
            <div class="section-header">
                <span class="section-num">{}</span>
                <h2>{}</h2>
                {}
            </div>
            {}
            {}
            {}
            <div class="task-list">{}</div>
        </section>'''.format(
            anchor,
            html_module.escape(m["number"]),
            html_module.escape(m["name"]),
            pattern_badges,
            objective_box,
            success_box,
            diagram_card,
            task_cards
        )

    html_content = """<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"><title>{}</title>
<style>{}</style>
<style>{}</style>
</head><body>
    <nav>{}</nav>
    <div class="container">
        {}
        {}
    </div>
<script>{}</script>
<script>{}</script>
<script>{}</script>
<script>{}</script>
<script>{}</script>
<script>{}</script>
<script>{}</script>
<script>{}</script>
<script>{}</script>
<script>mermaid.initialize({{startOnLoad:true,securityLevel:'loose',theme:'dark'}})</script>
<script>document.getElementById("hamburger").addEventListener("click",function(){{document.getElementById("nav-items").classList.toggle("open")}});document.querySelectorAll("#nav-items .nav-item").forEach(function(a){{a.addEventListener("click",function(){{document.getElementById("nav-items").classList.remove("open")}})}});</script>
</body></html>""".format(
        plan_name,
        css_content,
        prism_css,
        nav_links,
        epic_html,
        sections_html,
        prism_js,
        prism_go,
        prism_js_lib,
        prism_bash,
        prism_py,
        prism_ts,
        prism_yaml,
        prism_sql,
        mermaid_js
    )
    
    with open(output_file, 'w') as f:
        f.write(html_content)

def main():
    if len(sys.argv) < 4:
        print("Usage: render_plan.py <input.md> <output.html> <plan-name>")
        sys.exit(1)
    input_file = sys.argv[1]
    output_file = sys.argv[2]
    plan_name = sys.argv[3]
    if not os.path.exists(input_file):
        print("Error: {} not found.".format(input_file))
        sys.exit(1)
    with open(input_file, 'r') as f:
        content = f.read()
    data = parse_plan(content)
    generate_html(data, output_file, plan_name)
    print("Rendered HTML to {}".format(output_file))

if __name__ == "__main__":
    main()