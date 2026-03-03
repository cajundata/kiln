# Task: Add model selection to `kiln exec`

## Role

You are an assistant developer working inside an existing Go codebase
for a CLI tool named `kiln`. Your job is to implement focused, minimal
changes to add **Claude model selection** while keeping the current
behavior working.

You are allowed to:
- Read existing files
- Create new files and directories when appropriate
- Modify existing files
- Run Go build and tests

You are working in a Git-tracked project. Keep changes coherent,
incremental, and well‑tested.

---

## Context

`kiln exec` currently:

- Accepts `--task-id` and `--prompt-file`
- Reads the prompt file
- Invokes the `claude` CLI roughly as:

  claude --output-format stream-json -p "<prompt>"

- Streams the Claude output to stdout
- Logs the raw stream to `.kiln/logs/<task-id>.json`
- Returns exit code 0 on success, 1 on any failure

Right now, the underlying Claude CLI uses its default model
(e.g., latest Opus), which is **expensive and slower** for many tasks.
We want Kiln to be able to choose **which Claude model to use for
each invocation**, so we can use a heavier model for planning
and a lighter model for most coding work.

We do **not** need per‑task overrides in `tasks.yaml` yet.
We just need a clean mechanism so callers (Makefile, shell, CI)
can select the model.

---

## Requirements

### 1. New CLI flag: `--model`

Update the `exec` subcommand to accept an optional flag:

- `--model` (string, optional)
  - Example usage:

    ./kiln exec \
      --task-id exec-01 \
      --prompt-file .kiln/prompts/tasks/exec-01.md \
      --model claude-3-7-sonnet-latest

Behavior:

- If `--model` is provided and non‑empty, `kiln exec` must pass the
  equivalent model selection argument through to the `claude` CLI.
- If `--model` is omitted, Kiln should fall back to other mechanisms
  (see below).

Do **not** change the existing required flags or their behavior:
- `--task-id` (string, required)
- `--prompt-file` (string, required)

### 2. Environment variable: `KILN_MODEL`

Add support for a project‑level default via environment variable:

- If `--model` is **not** provided, but `KILN_MODEL` is set in the
  environment, use that as the model name.
- Example usage from shell / Make:

  KILN_MODEL=claude-3-7-sonnet-latest make all

Precedence rules (from highest to lowest):

1. `--model` flag (if present and non‑empty)
2. `KILN_MODEL` environment variable (if non‑empty)
3. Built‑in default (see next section)

### 3. Built‑in default model

Define a **single** built‑in default used when neither the flag
nor `KILN_MODEL` are provided.

- Choose a **sensible mid‑tier model** name string (e.g. a Sonnet‑class
  model) and define it as a constant in Go, e.g.:

  const defaultModel = "claude-3-7-sonnet-latest"

- The exact string can be updated later; for now, hard‑code a reasonable
  default so Kiln works out of the box without any configuration.

Behavior summary:

- Effective model = first non‑empty of:

  1. `--model` flag
  2. `KILN_MODEL` env var
  3. `defaultModel` constant

### 4. Wiring into the Claude CLI invocation

Update the place where `commandBuilder` (or equivalent) constructs
the `exec.Cmd` for the `claude` process.

Current behavior is roughly:

- `claude --output-format stream-json -p "<prompt>"`

Required behavior:

- Insert the **effective model** argument so the final command is
  equivalent to:

  claude \
    --model <effective-model> \
    --output-format stream-json \
    -p "<prompt>"

Implementation notes:

- Keep `commandBuilder` (or an equivalent abstraction) testable.
- Do not break existing tests that use the fake helper process.
- You may extend the `commandBuilder` signature if needed
  (e.g. accept both prompt and model), but keep the design simple
  and focused on this feature.

### 5. Error handling & messages

- If the user passes `--model ""` (an empty string), treat it as “not
  provided” and fall back to env/default as usual.
- If the `claude` binary fails because of an invalid model name, Kiln
  should surface this the same way as other Claude errors:
  - Keep using the existing `claude invocation failed: ...` error path.
  - Do not try to pre‑validate model names in Kiln for now.

### 6. Tests

Add or update tests in `cmd/kiln` to cover:

1. **Flag vs env precedence**

   - When `--model` is provided, it overrides `KILN_MODEL`.
   - When `--model` is omitted but `KILN_MODEL` is set, that value is used.
   - When neither is set, the built‑in `defaultModel` is used.

2. **Command wiring**

   - Use the existing `fakeCommandBuilder` helper approach to capture
     the arguments passed to the `claude` command.
   - Assert that the correct `--model <value>` pair is present
     in `cmd.Args` for the three cases above.

3. **No regression of current behavior**

   - Existing tests for `runExec` and `run` should still pass.
   - You may add small assertions in one of the success‑path tests
     to confirm that some model is always included.

Do **not** add any dependencies beyond the Go standard library.

---

## Acceptance Criteria

- `go build -o kiln ./cmd/kiln` succeeds.
- `go test ./cmd/kiln/...` passes.
- Running with an explicit model:

  ./kiln exec \
    --task-id exec-01 \
    --prompt-file .kiln/prompts/tasks/exec-01.md \
    --model claude-3-7-sonnet-latest

  results in a `claude` process that includes:

  --model claude-3-7-sonnet-latest

- Running without `--model` but with env var:

  KILN_MODEL=claude-3-5-haiku-latest ./kiln exec ...

  results in a `claude` process that includes:

  --model claude-3-5-haiku-latest

- Running without `--model` and without `KILN_MODEL`
  results in a `claude` process that includes the `defaultModel`
  constant.

- No change to the existing logging, exit codes, or prompt
  loading behavior of `kiln exec`.

---

## Final JSON Status Footer

At the end of your work, emit exactly one JSON object on a single line:

Successful completion:

{"kiln":{"status":"complete","task_id":"exec-model","notes":"added --model flag, KILN_MODEL env support, and default model wiring for claude CLI invocations"}}

Blocked:

{"kiln":{"status":"blocked","task_id":"exec-model","notes":"<brief explanation>"}}
