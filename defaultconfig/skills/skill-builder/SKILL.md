---
name: skill-builder
description: Generates compliant squid-os skills by interviewing the user, building incrementally via a deterministic Python CLI, validating scripts, and ensuring strict structural compliance.
allowed-tools: bash read_file write_file
---

## Overview
A skill is a reusable capability that teaches the agent how to perform a specific task.
The builder generates compliant skills by interviewing the user, building incrementally via a deterministic Python CLI, validating scripts, and ensuring strict structural compliance.

## Variables
- `<skill-folder>` — directory containing the active skill's `SKILL.md`
- `<working-dir>` — active working directory and project root for the current session
- `<global-skills-dir>` — Squid OS-provided directory where global skills are installed
- `<session_mode>` — Squid OS- current session mode

## Instructions

1. **Interview and Discover the Skill**

   First determine whether the current run is interactive or autonomous.

   ### Interactive Mode

   Understand the workflow before collecting metadata such as the skill name, description, scope, or allowed tools.

   Interview only until additional answers would materially change the generated skill.

   Ask the fewest questions necessary. Infer anything that can be safely derived from the request, current context, project files, or established conventions.

   Explore only the areas needed:

   * Problem
   * Current workflow
   * Trigger
   * Inputs
   * Process
   * Output
   * Success criteria
   * Constraints
   * Deterministic logic
   * Implementation strategy
     * Prompt instructions — reasoning, writing, summarization, or judgment.
     * Scripts — deterministic computation, parsing, validation, formatting, file generation, or repeatable transformations.
   * Required resources
   * Scope (Global or Project-Local)

   Prefer one focused question at a time.

   For simple skills, ask zero or one question when sufficient context already exists.

   ### Autonomous Mode

   Never ask questions or pause for confirmation.

   Infer reasonable defaults from the request, current context, project files, and established conventions.

   If essential information is missing and safe construction is impossible, stop with a clear error instead of requesting input.

   ### Final Skill Definition

   Once enough information has been collected or inferred, derive:

   * `name` — kebab-case, lowercase, hyphens only.
   * `description` — maximum 1024 characters describing the user intent that triggers the skill.
   * `overview` — one-paragraph summary.
   * `allowed-tools` — only the tools required by the workflow.
   * `scope` — one of:

     * **Global:** The skill lives in `<global-skills-dir>/<name>` and may be used from any working directory.
     * **Project-Local:** The skill lives in `<working-dir>/.squid-os/skills/<name>` and is available only within that project.

     If both exist with the same name, the project-local skill takes precedence.

     Path conventions:

     * Skill implementation files live under `<skill-folder>`.
     * Internal project state and supporting artifacts live under `<working-dir>/.squid-os/<skill-name>/`.
     * User-requested project files are created or modified in their natural location under `<working-dir>`.
     * Never place project output inside `<skill-folder>`.

   In interactive mode, present the derived definition only when confirmation would materially reduce the risk of building the wrong skill.

   In autonomous mode, proceed directly to the build.

2. **Determine the target directory**:

   * **Global skill:** `<global-skills-dir>/<name>`
   * **Project-Local skill:** `<working-dir>/.squid-os/skills/<name>`

3. **Init**: Create the skill skeleton.
   ```bash
   python3 <skill-folder>/scripts/build_skill.py init --name "<name>" --dir "<target-dir>" --description "<description>" --overview "<overview>" --allowed-tools "<allowed-tools>"
   ```

4. **Add Variables**: Define the contextual tags available to the generated skill.

   Every generated skill always receives `<skill-folder>`.

   Add `<working-dir>` only when the skill reads, creates, modifies, or stores files tied to the active project.

   For skills that only need access to their own files:

   ```bash
   python3 <skill-folder>/scripts/build_skill.py add-variables --dir "<target-dir>" --text "- \`<skill-folder>\` — directory containing the generated skill's SKILL.md"
   ```

   For skills that also operate on project files:

   ```bash
   python3 <skill-folder>/scripts/build_skill.py add-variables --dir "<target-dir>" --text "- \`<skill-folder>\` — directory containing the generated skill's SKILL.md\n- \`<working-dir>\` — active working directory and project root for the current session"
   ```

   Path conventions:

   - Skill implementation files live under `<skill-folder>`.
   - Internal project-scoped state and supporting artifacts live under `<working-dir>/.squid-os/<skill-name>/`.
   - User-requested project files are created or modified in their natural location under `<working-dir>`.
   - Never hardcode absolute paths.

5. **Add Instructions**: Build instructions incrementally. Each call appends.
   ```bash
   python3 <skill-folder>/scripts/build_skill.py add-instructions --dir "<target-dir>" --text "<instruction block>"
   ```
   Ensure instructions use contextual tags (`<skill-folder>`, `<working-dir>`) for paths — never embed absolute paths.

   ### Capability References

   Generated skills may instruct the assistant to use capabilities available in the current environment:

   - `@skill:name` — load and follow another skill.
   - `@agent:name` — delegate suitable work to an agent.
   - `@tool:name` — invoke a specific tool.

   Use a capability reference only when the dependency is intentional and expected to be available. Place it next to a clear instruction; the annotation communicates invocation intent but does not replace arguments or task context.

   Examples:

   ```text
   Use @skill:browser-use to inspect the page.
   Ask @agent:trader to evaluate the market setup.
   Use @tool:read_file to read the generated configuration.
   ```

   Preserve qualified capability references exactly when generating or validating a skill. Never invent capability names. Prefer describing the desired outcome when a specific low-level tool is not required.

6. **Add Rules**: Build rules incrementally. Each call appends.
   ```bash
   python3 <skill-folder>/scripts/build_skill.py add-rules --dir "<target-dir>" --text "<rule block>"
   ```

7. **Add Output Format**: Set the output format section.
   ```bash
   python3 <skill-folder>/scripts/build_skill.py add-output-format --dir "<target-dir>" --text "<output format template>"
   ```

8. **Add Examples**: Set the examples section.
   ```bash
   python3 <skill-folder>/scripts/build_skill.py add-examples --dir "<target-dir>" --text "<examples>"
   ```

9. **Add Scripts**: Add each executable script individually. For short scripts use `--content`, for long ones write to a temp file and use `--from-file`.
   ```bash
   python3 <skill-folder>/scripts/build_skill.py add-script --dir "<target-dir>" --name "<filename>" --content "<content>"
   ```

10. **Add Assets**: Add each template or resource file individually.
    ```bash
    python3 <skill-folder>/scripts/build_skill.py add-asset --dir "<target-dir>" --name "<filename>" --content "<content>"
    ```

11. **Add References**: Add each supplementary doc file individually.
    ```bash
    python3 <skill-folder>/scripts/build_skill.py add-reference --dir "<target-dir>" --name "<filename>" --content "<content>"
    ```

12. **Optional metadata**: Set version, license, or extra metadata if needed.
    ```bash
    python3 <skill-folder>/scripts/build_skill.py set-version --dir "<target-dir>" --version "1.0.0"
    python3 <skill-folder>/scripts/build_skill.py set-license --dir "<target-dir>" --license "MIT"
    python3 <skill-folder>/scripts/build_skill.py add-metadata --dir "<target-dir>" --key "author" --value "me"
    ```

13. **Finalize**: Export SKILL.md, write all files, remove the state file.
    ```bash
    python3 <skill-folder>/scripts/build_skill.py finalize --dir "<target-dir>"
    ```

14. **Script Validation**:
    - Read the generated `SKILL.md` to verify structure.
    - If scripts were generated, run each with a stub/test input to ensure it executes without errors.
    - If a script fails, edit it with `edit_file` and re-test.
    - Revert any temporary data or side-effects generated during testing.

15. **Structure Verification**:
    - Ensure SKILL.md contains only: Frontmatter, `## Overview`, `## Variables`, `## Instructions`, `## Rules`, `## Output Format` (code block), `## Resources`, `## Examples`.
    - Ensure `## Output Format` is inside a code block.
    - Ensure `## Variables` exists with tag definitions (e.g. `<skill-folder>`, `<working-dir>`).
    - Ensure `references/` contains only context, never output templates.
    - Ensure all paths use contextual tags — no hardcoded absolute paths.
    - If SKILL.md violates the structure, use `edit_file` to correct it.

16. **Version Control for Global Skills**

    This step applies only to global skills.

    - Check whether `<global-skills-dir>/.git` exists.
    - If it does not exist, initialize a Git repository in `<global-skills-dir>`.
    - Stage only the generated skill directory:
      ```bash
      git -C "<global-skills-dir>" add "<name>"
      ```
    - Commit the skill:
      ```bash
      git -C "<global-skills-dir>" commit -m "feat(skill): add <name>"
      ```
    - If there are no changes to commit, continue without error.

    Project-local skills do not initialize repositories or create commits.
    
17. **Final Review**: Show the user the final `SKILL.md` structure and confirm.

## Rules
- **Deterministic Logic**: Prefer scripts for deterministic logic over prompt instructions. (Avoid complex conditional instructions.)
- **Script First**: If a task can be implemented deterministically, implement it as a script rather than prompt instructions. Reserve prompt instructions for reasoning, interpretation, and communication.
- **JSON data**: If the skill requires state creation, prefer JSON storage and using scripts to generate it so it is deterministic.
- **Ambiguity Handling**: In interactive mode, ask only when ambiguity would materially change the implementation. In autonomous mode, infer conservative defaults and stop only when safe or valid construction is impossible.
- **Strict Template Separation**: SKILL.md must only contain the official squid-os sections.
- **Scripts for Strictness**: Use scripts/ for validation, formatting, or computational tasks.
- **References for Context Only**: references/ must never contain output templates or examples.
- **Test Everything**: If a script is generated, it must be tested. If it fails, fix it.
- **Output Format**: The output_format must be the exact Markdown template the skill should produce, wrapped in a code block.
- **No Nested Headers**: Never use `##` headers inside code blocks in Output Format or Examples.
- **Path Hygiene**: Global skills live in `<global-skills-dir>`. Project-local skills live in `<working-dir>/.squid-os/skills/`.
- **No Hardcoded Paths**: Generated skills must use contextual tags (`<skill-folder>`, `<working-dir>`) in instructions — never embed absolute paths.
- **Script Path Injection**: All scripts must accept paths as CLI arguments or environment variables. Never hardcode absolute paths in scripts — the AI substitutes SKILL.md placeholders at runtime, but scripts are blind to them and must receive paths explicitly.
- **Variables are Tag Definitions**: The `add-variables` step defines contextual tags (e.g. `<skill-folder>`, `<working-dir>`). These are placeholders the AI substitutes — NOT environment variables passed to scripts.
- **No Shell Variable Indirection**: Instructions should reference tags directly (e.g. `<skill-folder>/scripts/build.py`) rather than going through shell variables like `$SKILL_SCRIPTS`.
- **Incremental Build**: Build the skill piece by piece using the CLI commands. Do not try to compose everything at once.
- **Output Convention**: Skills that produce project-scoped output should place it under `<working-dir>/.squid-os/<skill-name>/`. Use `<working-dir>` directly only for top-level project files.

## Output Format
The generated SKILL.md must contain these sections in order: Frontmatter, Overview, Variables, Instructions, Rules, Output Format (code block), Resources (with Scripts/References/Assets sub-sections), Examples.

## Resources
### Scripts
- [build_skill.py](scripts/build_skill.py) — Deterministic builder that maintains .skill.json state, exports SKILL.md, writes scripts/assets/references, and cleans up.

## Examples
**Input:** User wants a global skill named "yaml-validator" that checks YAML syntax using a shell script.
**Output:**
1. `python3 <skill-folder>/scripts/build_skill.py init --name yaml-validator --dir "<global-skills-dir>/yaml-validator" --description "Validates YAML files" --overview "Checks YAML syntax using a script" --allowed-tools "bash read_file"`
2. `python3 <skill-folder>/scripts/build_skill.py add-variables --dir "<global-skills-dir>/yaml-validator" --text "- \`<skill-folder>\` — directory containing this SKILL.md"`
3. `python3 <skill-folder>/scripts/build_skill.py add-instructions --dir "<global-skills-dir>/yaml-validator" --text "1. Read the YAML file. 2. Run python3 <skill-folder>/scripts/validate.sh --file <path>"`
4. `python3 <skill-folder>/scripts/build_skill.py add-script --dir "<global-skills-dir>/yaml-validator" --name validate.sh --content "#!/bin/bash\npython3 -c 'import yaml; yaml.safe_load(open(sys.argv[1]))'"`
5. `python3 <skill-folder>/scripts/build_skill.py add-rules --dir "<global-skills-dir>/yaml-validator" --text "Always validate before parsing."`
6. `python3 <skill-folder>/scripts/build_skill.py add-output-format --dir "<global-skills-dir>/yaml-validator" --text "Valid: true/false\nErrors: list of issues"`
7. `python3 <skill-folder>/scripts/build_skill.py add-examples --dir "<global-skills-dir>/yaml-validator" --text "Input: config.yml\nOutput: Valid: true"`
8. `python3 <skill-folder>/scripts/build_skill.py finalize --dir "<global-skills-dir>/yaml-validator"`
9. Run the script with test input to validate.
10. Verify SKILL.md structure.
11. Commit the generated skill in `<global-skills-dir>`.
