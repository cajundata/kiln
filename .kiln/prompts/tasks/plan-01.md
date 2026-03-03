# Task: Finalize `make plan` → `.kiln/tasks.yaml` workflow

## Role

You are an assistant developer working inside an existing Go / Make–based CLI project named `kiln`. Your job is to refine **prompts** and **workflow wiring**, not to introduce unnecessary complexity.

In this task, you will:

- Review and, if needed, update the `.kiln/prompts/00_extract_tasks.md` prompt
- Make sure `make plan` reliably generates a valid `.kiln/tasks.yaml` file
- Validate that the YAML matches the agreed minimal schema

You are allowed to:

- Read and edit files in this repo
- Run shell commands like `ls`, `cat`, `go test`, `make plan`
- Create or update `.kiln/tasks.yaml` as needed

You are **not** implementing new Go code in this task. Focus on prompt and workflow behavior.

---

## Context

`kiln` is a personal developer productivity CLI inspired by the “Ralph Wiggum” workflow for Claude Code. The high‑level loop we are building is:

1. `make plan`  
   - Uses `kiln exec` with `.kiln/prompts/00_extract_tasks.md`
   - Claude reads `PRD.md` and **writes** `.kiln/tasks.yaml`

2. `make graph`  
   - Will call `kiln gen-make` to convert `.kiln/tasks.yaml` into `.kiln/targets.mk`  
   - (Implemented in a later task)

3. `make all`  
   - Will eventually run all tasks from the generated targets

For this task, we care only about **step 1**: making sure `make plan` produces a clean `tasks.yaml` based on the current PRD.

### Desired `tasks.yaml` schema (MVP)

`tasks.yaml` should be a list of tasks, each with:

- `id` (string, required)  
  - Short, CLI-friendly identifier (e.g., `exec-01`, `genmake-01`, `timeout-01`)
- `prompt` (string, required)  
  - Path to the task prompt file, relative to repo root (e.g., `.kiln/prompts/tasks/exec-01.md`)
- `needs` (optional list of strings)  
  - Other task IDs that must run before this one

Example:

```yaml
- id: exec-01
  prompt: .kiln/prompts/tasks/exec-01.md
  needs: []

- id: genmake-01
  prompt: .kiln/prompts/tasks/genmake-01.md
  needs:
    - exec-01
```

Notes:

- Keep the schema **minimal**: `id`, `prompt`, optional `needs`.
- No extra top‑level wrapper; `tasks.yaml` is just a YAML sequence.
- Do **not** embed long notes in `tasks.yaml`. Commentary belongs in PRD or separate docs.

---

## Requirements

### 1. Review and adjust `00_extract_tasks.md` prompt

Open and examine:

- `.kiln/prompts/00_extract_tasks.md`

Your goals:

1. Ensure the prompt:
   - Clearly explains the PRD → `tasks.yaml` transformation
   - Describes the **exact schema** above
   - Instructs Claude to:
     - Read `PRD.md`
     - Identify sensible task units for kiln / Claude Code work
     - Map each task to an `id`, `prompt`, and `needs`
     - Write the resulting YAML to `.kiln/tasks.yaml` in the repo root

2. Ensure the prompt **strongly discourages hallucination**:
   - Only create dependencies when there is a clear ordering requirement
   - Prefer independent tasks when unsure
   - Use conservative `needs` edges

3. Ensure the prompt:
   - Requires **pure YAML only** (no prose, no markdown fences)
   - Requires valid, strictly formatted YAML (no comments needed)
   - Makes it clear that invalid YAML is a hard failure

You may refactor the wording of `00_extract_tasks.md` for clarity and robustness, but keep it concise and practical.

### 2. Verify `make plan` produces `.kiln/tasks.yaml`

With the updated prompt:

1. From the project root, run:

   ```bash
   make plan
   ```

2. Confirm that:
   - `.kiln/tasks.yaml` exists
   - It is non‑empty
   - It is valid YAML matching the schema

You can use tooling like:

- `cat .kiln/tasks.yaml`
- A small Go one‑liner or ad‑hoc script, if you want, but avoid over‑engineering

If anything fails (e.g., Claude writes commentary, invalid YAML, or wrong shape), adjust `00_extract_tasks.md` and re‑run `make plan` until it works reliably.

### 3. Sanity‑check the task graph

Read `.kiln/tasks.yaml` and perform a quick sanity pass:

- Ensure every `id` is:
  - Unique
  - CLI‑friendly (no spaces, no weird characters)
- Ensure every `prompt`:
  - Points to a real file in `.kiln/prompts/tasks/`
  - Uses a consistent naming convention (e.g., `exec-01`, `genmake-01`, etc.)
- Ensure every `needs` only references **existing** task IDs

If you need to tweak task IDs or `needs` to make the graph sane for the current PRD, you can:

- Update `tasks.yaml` **and**
- Update the PRD (briefly) or related task prompt filenames if they’re out of sync

Keep changes minimal: prefer renaming a prompt file and updating the reference over rewriting large chunks of PRD.

---

## Acceptance Criteria

This task is complete when:

1. `.kiln/prompts/00_extract_tasks.md`:
   - Clearly defines the target `tasks.yaml` schema
   - Explicitly instructs Claude to output **only** YAML with that schema
   - Clearly describes how to derive tasks from `PRD.md`
   - Emphasizes conservative dependencies and failure on invalid YAML

2. Running:

   ```bash
   make plan
   ```

   from the repo root:

   - Completes successfully (exit code 0)
   - Produces `.kiln/tasks.yaml` at the expected location

3. `.kiln/tasks.yaml`:

   - Is valid YAML
   - Is a list of objects, each with `id`, `prompt`, and optional `needs`
   - Uses prompt paths that actually exist under `.kiln/prompts/tasks/`
   - Has no `needs` that reference missing task IDs

4. You have done at least **one** full generate → inspect → adjust iteration, rather than trusting the first attempt blindly.

---

## Final JSON Status Footer

At the end of your work, emit exactly one JSON object on a single line:

**Successful completion:**

```json
{"kiln":{"status":"complete","task_id":"plan-01","notes":"make plan now generates a valid .kiln/tasks.yaml matching the agreed schema"}}
```

**Blocked:**

```json
{"kiln":{"status":"blocked","task_id":"plan-01","notes":"<brief explanation of what is blocking you>"}}
```