# Task: Implement `kiln exec` to call Claude Code and log raw output

## Role

You are an assistant developer working inside an existing Go codebase
for a CLI tool named `kiln`. Your job is to implement and modify code
directly in this repository to fulfill the task below. You should make
focused, minimal changes and keep the codebase clean and maintainable.

You are allowed to: - Read existing files - Create new files and
directories when appropriate - Modify existing files - Run shell
commands like `go build` or `go test` if needed

You are working in a Git-tracked project. Be careful to keep changes
coherent and localized to this task.

------------------------------------------------------------------------

## Context

`kiln` is a personal developer productivity CLI inspired by the "Ralph
Wiggum" workflow for Claude Code.

Right now, `kiln exec` is just a stub that:

-   Parses flags like `--task-id` and `--prompt-file`
-   Reads the prompt file
-   Prints some debug output
-   Exits with status code 0

We want `kiln exec` to become a thin, reliable wrapper around the Claude
Code CLI, with the following behavior:

-   Read the task prompt from a file
-   Invoke the `claude` CLI with `--output-format stream-json`
-   Stream the AI's output to stdout (so the caller / Make can see it)
-   Write the full raw stream to a log file under `.kiln/logs/`
-   Exit with a non-zero status code if the Claude invocation fails

This is the first real implementation step for `kiln exec`. We are not
yet adding timeouts, retries, or JSON-footer parsing in this task. Those
will be separate tasks.

------------------------------------------------------------------------

## Requirements

### 1. CLI interface (exec subcommand)

-   The `exec` subcommand already exists and accepts flags:
    -   `--task-id` (string, required)
    -   `--prompt-file` (string, required)

Keep this interface intact.

If either flag is missing or empty: - Print a helpful error message to
`stderr` - Exit with status code 1

### 2. Prompt loading

-   Read the prompt file specified by `--prompt-file`
-   Treat the file contents as the full prompt to send to Claude
-   If the file cannot be read:
    -   Print an error to `stderr`
    -   Exit with status code 1

### 3. Claude Code invocation

Implement logic that:

-   Uses `os/exec` to run the `claude` CLI

-   Invokes it approximately as:

    claude --output-format stream-json -p "`<prompt contents>`{=html}"

-   Captures both stdout and stderr from the `claude` process

-   Streams the combined output to:

    -   `os.Stdout`
    -   A log file (see next section)

If the `claude` binary is not found, or exits with a non-zero status
code: - Print a clear error message to `stderr` - Exit with status code
1

Assume the `claude` CLI is available on the PATH.

### 4. Logging

-   Log directory: `.kiln/logs/`
    -   Ensure this directory exists before writing logs
    -   Create it if necessary
-   Log file naming convention:
    -   `.kiln/logs/<task-id>.json`
-   Log contents:
    -   Write the full raw stream from the Claude process to this file
    -   Do not parse or interpret it yet

If the log file cannot be written: - Print an error to `stderr` - Exit
with status code 1

### 5. Exit codes

`kiln exec` should use exit codes as follows:

-   0 --- Claude CLI ran successfully and log was written
-   1 --- Any failure (missing flags, file read error, Claude error,
    logging failure)

------------------------------------------------------------------------

## Acceptance Criteria

Running:

    ./kiln exec       --task-id exec-01       --prompt-file .kiln/prompts/00_extract_tasks.md

Should:

1.  Read the prompt file without error
2.  Invoke Claude with stream-json output
3.  Stream Claude's raw output to stdout
4.  Produce a log file at `.kiln/logs/exec-01.json`
5.  Exit with 0 on success, 1 on failure

The code must build successfully with:

    go build -o kiln ./cmd/kiln

------------------------------------------------------------------------

## Final JSON Status Footer

At the end of your work, emit exactly one JSON object on a single line:

Successful completion:

{"kiln":{"status":"complete","task_id":"exec-01","notes":"implemented
exec to call claude, stream output, and log to
.kiln/logs/exec-01.json"}}

Blocked:

{"kiln":{"status":"blocked","task_id":"exec-01","notes":"`<brief explanation>`{=html}"}}
