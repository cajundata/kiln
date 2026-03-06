# Task: prompt-chaining — Inject completed task context into downstream prompts

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
prompt-chaining

## SCOPE
Implement ONLY the prompt chaining feature described below. Do not work on other backlog items (UNIFY closure, recovery UX, validation hooks, TUI, error taxonomy, etc.), even if you notice related opportunities.

## CONTEXT
- This project uses the AgentRun workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.
- All source lives in `cmd/kiln/main.go` (single-package CLI).
- Kiln uses fresh Claude Code invocations per task to prevent context rot. However, downstream tasks often need selective memory of prior work — what changed, what decisions were made, what artifacts were produced.
- Prompt chaining is the mechanism that injects completed task context into downstream task prompts automatically.
- The primary injection sources are:
  1. **UNIFY closure artifacts** (`.kiln/unify/<task-id>.md`) — high-signal semantic summaries of what happened in completed tasks (if the UNIFY feature exists).
  2. **Research artifacts** (`.kiln/artifacts/research/<task-id>.md`) — outputs from tasks with `kind: research` (if the richer schema feature exists).
  3. **Execution logs** (`.kiln/logs/<task-id>.json`) — structured logs from completed tasks (always available after `kiln exec`).
- Prompt chaining should be resilient: if UNIFY artifacts or research artifacts don't exist (because those features aren't implemented yet), fall back gracefully to execution logs or skip injection entirely.
- The injection happens at `kiln exec` time: before invoking Claude, the prompt is augmented with context from completed dependency tasks.

## REQUIREMENTS

1. **Context gathering for dependency tasks** — When `kiln exec` runs a task, gather context from all completed dependency tasks (those listed in the task's `needs` field):
   - For each dependency task ID, attempt to read context sources in priority order:
     1. `.kiln/unify/<dep-id>.md` — UNIFY closure artifact (highest signal, preferred)
     2. `.kiln/artifacts/research/<dep-id>.md` — research artifact (for `kind: research` tasks)
     3. `.kiln/logs/<dep-id>.json` — execution log (fallback, always available)
   - Use the first source that exists for each dependency. If none exist, skip that dependency (no error).
   - Only gather context from dependencies that have a `.kiln/done/<dep-id>.done` marker (confirmed complete).

2. **Execution log summarization** — When falling back to execution logs as context source:
   - Parse the `execRunLog` JSON from `.kiln/logs/<dep-id>.json`.
   - Extract a minimal summary: task ID, status, model used, duration, and the footer notes (if present).
   - Do NOT include raw event lines — they are too verbose and would pollute the downstream prompt.

3. **Prompt augmentation** — Inject gathered context into the task prompt before Claude invocation:
   - Prepend a `## Context from Completed Dependencies` section before the original prompt content.
   - For each dependency with available context, include a subsection:
     ```
     ### Dependency: <dep-id> (source: unify|research|log)
     <context content>
     ```
   - If no dependencies have available context, do not add the section (keep the prompt unchanged).
   - The augmented prompt replaces the original prompt string passed to `commandBuilder` — no changes to `commandBuilder` itself.

4. **Size guard** — Prevent prompt bloat from large dependency contexts:
   - Add a `--max-context-bytes` flag to `kiln exec` (default: 50000 bytes, ~50KB).
   - If total injected context exceeds this limit, truncate the oldest (first-listed) dependency contexts first, appending `\n[truncated — exceeded context budget]\n` to each truncated section.
   - The size guard applies only to the injected context, not the original prompt.

5. **Opt-out mechanism** — Allow disabling prompt chaining:
   - Add a `--no-chain` flag to `kiln exec` (default: false).
   - When `--no-chain` is set, skip all context gathering and prompt augmentation.

6. **Integration with `execOnce`** — The prompt augmentation must happen before `execOnce` is called:
   - In `runExec`, after reading the prompt file and before the retry loop, call a new function (e.g., `augmentPromptWithDeps`) that returns the augmented prompt string.
   - `execOnce` receives the already-augmented prompt — no changes to its signature or behavior.

7. **New function: `augmentPromptWithDeps`** — Create a pure-ish function with this signature (or similar):
   - Inputs: original prompt string, list of dependency task IDs, max context bytes, base directory (for resolving `.kiln/` paths)
   - Output: augmented prompt string
   - This function should be testable in isolation (no side effects beyond file reads).

8. **Directory conventions** — Ensure `.kiln/artifacts/research/` directory is created by `kiln gen-make` if not already present (alongside `.kiln/done/`, `.kiln/logs/`, `.kiln/unify/`).

## Tests

- `augmentPromptWithDeps` returns original prompt unchanged when no dependencies are provided
- `augmentPromptWithDeps` returns original prompt unchanged when no context files exist for dependencies
- `augmentPromptWithDeps` prefers UNIFY artifact over research artifact over execution log
- `augmentPromptWithDeps` falls back to research artifact when UNIFY artifact is missing
- `augmentPromptWithDeps` falls back to execution log when both UNIFY and research artifacts are missing
- `augmentPromptWithDeps` skips dependencies without `.done` markers
- `augmentPromptWithDeps` includes correct source label (unify, research, log) in section headers
- `augmentPromptWithDeps` truncates context when total exceeds `maxContextBytes`
- `augmentPromptWithDeps` truncates oldest dependencies first
- `augmentPromptWithDeps` appends truncation notice to truncated sections
- Execution log summarization extracts task_id, status, model, duration_ms, and footer notes
- Execution log summarization does NOT include raw event lines
- `--no-chain` flag skips all context gathering
- `--max-context-bytes` flag controls the context budget
- `kiln exec` with dependencies produces an augmented prompt containing dependency context
- `kiln gen-make` creates `.kiln/artifacts/research/` directory
- Existing tests continue to pass (`go test ./cmd/kiln -v`)

## ACCEPTANCE CRITERIA
- `augmentPromptWithDeps` function exists and is testable in isolation
- Context is gathered from completed dependency tasks in priority order: UNIFY > research > log
- Graceful fallback when UNIFY or research artifacts don't exist
- Prompt is augmented with a `## Context from Completed Dependencies` section containing per-dependency subsections
- Prompt remains unchanged when no dependencies or no context is available
- `--max-context-bytes` flag limits injected context size with truncation
- `--no-chain` flag disables prompt chaining entirely
- Execution log fallback extracts a minimal summary (no raw events)
- `kiln gen-make` creates `.kiln/artifacts/research/` directory
- All new and existing tests pass (`go test ./cmd/kiln -v`)
- No large refactors unrelated to prompt chaining

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"prompt-chaining"}}

Allowed values for status:
- "complete"     (all acceptance criteria met)
- "not_complete" (work attempted but acceptance criteria not met)
- "blocked"      (cannot proceed due to missing info, permissions, dependencies, or unclear requirements)

STRICT RULES FOR THE JSON FOOTER
- The JSON object MUST be the final line of your response.
- Output EXACTLY one JSON object.
- No extra text after it.
- No code fences around it.
- The task_id must exactly match the TASK ID above.
- If you are unsure, choose "not_complete" or "blocked" rather than "complete".

If you finish successfully, the correct final line is:
{"kiln":{"status":"complete","task_id":"prompt-chaining"}}
