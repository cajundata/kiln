# Task: engine-abstraction — Engine Abstraction & Multi-Engine Support

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
engine-abstraction

## SCOPE
Implement ONLY the engine abstraction and multi-engine parsing feature described below. Do not work on other backlog items (TUI, git automation, profile strategy, etc.), even if you notice related opportunities.

## CONTEXT
- This project uses the AgentRun workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.
- All source lives in `cmd/kiln/main.go` (single-package CLI).
- Kiln MVP is Claude Code only. The execution path is tightly coupled to Claude Code specifics:
  - `commandBuilder` builds a `claude` CLI invocation with `--model`, `--dangerously-skip-permissions`, `--verbose`, `--output-format stream-json`, and `-p` flags.
  - `parseFooter` expects a `{"kiln":{"status":"...","task_id":"..."}}` JSON footer, with logic to extract it from Claude's `stream-json` wrapper format.
  - Error classification (`timeoutError`, `claudeExitError`, `footerError`) and `isRetryable()` are Claude-specific assumptions.
  - `execOnce` orchestrates command building, output capture, error classification, and footer parsing in a single function.
- The `Task` struct already has an `Engine` field (`yaml:"engine,omitempty"`), but it is not used anywhere in the execution path.
- The `KILN_MODEL` env var and `--model` flag are Claude-specific concepts (other engines may use different model naming).

## REQUIREMENTS

1. **Define an `Engine` interface** — Create an interface that encapsulates the engine-specific concerns currently hardcoded for Claude Code:
   ```go
   type Engine interface {
       // Name returns the engine identifier (e.g., "claude", "codex", "custom").
       Name() string
       // BuildCommand creates an *exec.Cmd for this engine given a context, prompt text, and model name.
       BuildCommand(ctx context.Context, prompt, model string) *exec.Cmd
       // ParseFooter extracts (status, taskID, ok) from the engine's raw output.
       ParseFooter(output string) (status, taskID string, ok bool)
       // ClassifyError determines if a run error is retryable, given the run error and context error (for timeout detection).
       ClassifyError(runErr error, ctxErr error) error
       // DefaultModel returns the default model name for this engine.
       DefaultModel() string
   }
   ```
   - The interface must live in `cmd/kiln/main.go` alongside existing types.
   - Keep the interface minimal — only abstract what differs between engines.

2. **Implement `claudeEngine`** — Extract current Claude-specific logic into a struct that satisfies the `Engine` interface:
   - `Name()` returns `"claude"`.
   - `BuildCommand()` contains the current `commandBuilder` logic (builds `claude` CLI invocation with `--model`, `--dangerously-skip-permissions`, `--verbose`, `--output-format stream-json`, `-p`).
   - `ParseFooter()` delegates to the existing `parseFooter` function (and its helpers `extractStreamJSONTexts`, `tryParseFooterInText`).
   - `ClassifyError()` wraps the current timeout/exit-error classification logic: returns `*timeoutError` for deadline exceeded, `*claudeExitError` for non-zero exit, or nil.
   - `DefaultModel()` returns `"claude-sonnet-4-6"`.
   - The existing `commandBuilder` package-level var must remain for test injection purposes but should delegate to the active engine's `BuildCommand` by default.

3. **Engine registry** — Implement a simple registry for looking up engines by name:
   - `var engineRegistry = map[string]Engine{"claude": &claudeEngine{}}` (or a function-based registry).
   - `func getEngine(name string) (Engine, error)` — returns the engine or an error if the name is unknown.
   - The registry must be extensible: adding a new engine means adding one map entry and one struct.

4. **Wire `--engine` flag into `kiln exec`** — Update the exec subcommand:
   - Add `--engine` flag (string, default `"claude"`).
   - The `Task.Engine` field from `tasks.yaml` is used when the flag is not explicitly provided (task-level override).
   - Resolution order: `--engine` flag (if explicitly set) > `Task.Engine` field > default `"claude"`.
   - Look up the engine from the registry. If unknown, return a clear error: `"unknown engine %q; available: claude"`.
   - Pass the resolved `Engine` to `execOnce` (update its signature).

5. **Refactor `execOnce` to use the `Engine` interface** — Update the execution function:
   - Add an `Engine` parameter to `execOnce` (or pass it via a struct/options pattern).
   - Replace the direct `commandBuilder(ctx, prompt, model)` call with `engine.BuildCommand(ctx, prompt, model)`.
   - Replace the direct `parseFooter(output)` call with `engine.ParseFooter(output)`.
   - Replace the inline timeout/error classification with `engine.ClassifyError(runErr, ctx.Err())`.
   - The function signature change should be minimal — prefer adding `engine Engine` as a parameter.

6. **Update `resolveModel` to respect engine defaults** — The current `resolveModel` falls back to `KILN_MODEL` env var then to a hardcoded `"claude-sonnet-4-6"`. Update the fallback chain:
   - `--model` flag > `Task.Model` field > `KILN_MODEL` env var > `engine.DefaultModel()`.
   - The hardcoded `"claude-sonnet-4-6"` string should only appear inside `claudeEngine.DefaultModel()`.

7. **Update `gen-prompts` command** — The `runGenPrompts` function also uses `commandBuilder` directly to invoke Claude for prompt generation. This is intentionally NOT abstracted behind the Engine interface since prompt generation is always a Claude Code task. Leave `runGenPrompts` using `commandBuilder` directly, but document this decision with a comment.

8. **Do NOT implement additional engines** — This task only introduces the abstraction and migrates the existing Claude Code engine. A second engine (e.g., Codex) is future work. Do not add stub/placeholder engines.

## Tests

- `claudeEngine` satisfies the `Engine` interface (compile-time check: `var _ Engine = (*claudeEngine)(nil)`)
- `claudeEngine.Name()` returns `"claude"`
- `claudeEngine.DefaultModel()` returns `"claude-sonnet-4-6"`
- `claudeEngine.BuildCommand()` produces a command with the expected `claude` binary and arguments
- `claudeEngine.ParseFooter()` correctly extracts status and task_id from valid footer output
- `claudeEngine.ParseFooter()` returns `ok=false` for output with no footer
- `claudeEngine.ClassifyError()` returns `*timeoutError` when context deadline exceeded
- `claudeEngine.ClassifyError()` returns `*claudeExitError` for non-zero exit errors
- `claudeEngine.ClassifyError()` returns nil for nil runErr
- `getEngine("claude")` returns the claude engine
- `getEngine("unknown")` returns an error listing available engines
- `--engine` flag defaults to `"claude"` and resolves from task definition when not explicitly set
- Engine resolution order: explicit flag > task field > default "claude"
- `resolveModel` uses `engine.DefaultModel()` as final fallback instead of hardcoded string
- Existing `kiln exec` tests pass without modification (backward compatibility)
- `go test ./cmd/kiln -v` passes with no regressions

## ACCEPTANCE CRITERIA
- An `Engine` interface is defined with `Name()`, `BuildCommand()`, `ParseFooter()`, `ClassifyError()`, and `DefaultModel()` methods
- A `claudeEngine` struct implements the `Engine` interface, encapsulating all Claude Code-specific logic
- An engine registry exists with `getEngine()` lookup function
- `kiln exec --engine <name>` flag exists and resolves engine from flag > task field > default
- `execOnce` uses the `Engine` interface for command building, footer parsing, and error classification
- `resolveModel` fallback chain ends with `engine.DefaultModel()` instead of a hardcoded string
- The `commandBuilder` package-level var remains functional for test injection
- All existing tests pass without modification (backward-compatible refactor)
- New tests cover the engine interface, registry, and resolution logic
- Adding a new engine in the future requires only: (a) a new struct implementing `Engine`, (b) one registry entry

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"engine-abstraction"}}

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
{"kiln":{"status":"complete","task_id":"engine-abstraction"}}
