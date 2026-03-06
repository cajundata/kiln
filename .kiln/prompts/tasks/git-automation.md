# Task: git-automation — Optional Git Automation

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
git-automation

## SCOPE
Implement ONLY the optional git automation feature described below. Do not work on other backlog items (engine abstraction, TUI, profile strategy, etc.), even if you notice related opportunities.

## CONTEXT
- This project uses the AgentRun workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.
- All source lives in `cmd/kiln/main.go` (single-package CLI).
- Kiln currently has no git awareness — task execution does not verify or interact with version control.
- Git automation is explicitly opt-in. All features MUST be behind flags or config; nothing changes default behavior.
- If validation hooks (#3) exist in the codebase, git verification can integrate as a hook type. If not, implement git checks as standalone post-exec verification within `kiln exec`.

## REQUIREMENTS

1. **Commit verification gate** — Verify that a git commit occurred during task execution:
   - Add a `--verify-commit` flag to `kiln exec`.
   - When enabled, after a successful task execution (status=complete), check whether the current HEAD commit differs from the HEAD captured before execution started.
   - If no new commit was made, treat the task as `not_complete` (exit code 2) and log a warning: `"verify-commit: no new commit detected after task completion"`.
   - Capture the pre-execution HEAD SHA and post-execution HEAD SHA in the task's JSON log entry (fields: `pre_commit_sha`, `post_commit_sha`).
   - If git is not available or the working directory is not a git repo, log a warning and skip verification (do not fail).

2. **Auto-commit with templated messages** — Optional automatic commit after successful task execution:
   - Add a `--auto-commit` flag to `kiln exec`.
   - When enabled, after a successful task execution (status=complete), stage all changes (`git add -A`) and commit with a templated message.
   - Default commit message template: `kiln: complete task {task_id}`.
   - Add a `--commit-template` flag to override the template. Support `{task_id}` as a placeholder.
   - Auto-commit runs AFTER any validation hooks (if they exist) have passed. If validation fails, do not auto-commit.
   - `--auto-commit` and `--verify-commit` are mutually exclusive (auto-commit implies a commit will be created, so verification is redundant). If both are provided, exit with an error.
   - If there are no staged/unstaged changes after execution, skip the commit and log: `"auto-commit: no changes to commit"`.

3. **Branch-per-task mode** — Optional task isolation via git branches:
   - Add a `--branch-per-task` flag to `kiln exec`.
   - When enabled, before execution begins:
     - Record the current branch name (for return).
     - Create and checkout a new branch named `kiln/<task-id>` from the current HEAD.
     - If the branch already exists, check it out (resume mode).
   - After execution completes (regardless of success/failure):
     - Remain on the task branch (do NOT auto-merge back).
     - Log the branch name in the task's JSON log entry (field: `task_branch`).
   - The user is responsible for merging the branch back manually or via PR.

4. **Git helper functions** — Implement reusable internal functions:
   - `gitHeadSHA() (string, error)` — Returns the current HEAD commit SHA. Uses `git rev-parse HEAD`.
   - `gitCurrentBranch() (string, error)` — Returns the current branch name. Uses `git rev-parse --abbrev-ref HEAD`.
   - `gitHasChanges() (bool, error)` — Returns true if there are staged or unstaged changes. Uses `git status --porcelain`.
   - `gitCommit(message string) error` — Stages all changes and commits. Uses `git add -A` then `git commit -m <message>`.
   - `gitCreateBranch(name string) error` — Creates and checks out a branch. Uses `git checkout -b <name>` (or `git checkout <name>` if it already exists).
   - All git functions should use `exec.Command` and capture stderr for error reporting.
   - All git functions should be assigned to package-level `var` for testability (same pattern as `commandBuilder` and `sleepFn`).

5. **Task schema integration** — If the richer task schema (#7) is present:
   - Support optional per-task git config fields in `tasks.yaml`:
     - `verify_commit: true|false` (equivalent to `--verify-commit`)
     - `auto_commit: true|false` (equivalent to `--auto-commit`)
     - `branch_per_task: true|false` (equivalent to `--branch-per-task`)
     - `commit_template: "string"` (equivalent to `--commit-template`)
   - CLI flags override task-level config.
   - If the richer schema is not yet implemented, skip this requirement and document in a code comment: `// TODO: per-task git config (backlog #7)`.

6. **Logging integration** — Add git-related fields to the JSON log entry:
   - `pre_commit_sha` (string, optional): HEAD SHA before execution.
   - `post_commit_sha` (string, optional): HEAD SHA after execution.
   - `task_branch` (string, optional): Branch name if `--branch-per-task` was used.
   - `auto_committed` (bool, optional): Whether an auto-commit was performed.

## Tests

- `gitHeadSHA` returns a valid SHA in a git repo
- `gitHeadSHA` returns an error in a non-git directory
- `gitCurrentBranch` returns the current branch name
- `gitHasChanges` returns false in a clean repo
- `gitHasChanges` returns true after modifying a tracked file
- `gitCommit` stages and commits changes with the provided message
- `gitCreateBranch` creates and checks out a new branch
- `gitCreateBranch` checks out an existing branch without error
- `--verify-commit` passes when a new commit is detected
- `--verify-commit` fails (exit 2) when no new commit is detected
- `--verify-commit` skips gracefully when not in a git repo
- `--auto-commit` creates a commit with the default template after successful execution
- `--auto-commit` with `--commit-template` uses the custom template with `{task_id}` substitution
- `--auto-commit` skips when there are no changes
- `--auto-commit` and `--verify-commit` together produce an error
- `--branch-per-task` creates and checks out `kiln/<task-id>` branch
- `--branch-per-task` resumes an existing `kiln/<task-id>` branch
- Branch name is logged in the JSON log entry
- Pre/post commit SHAs are logged in the JSON log entry
- Git helper functions are injectable via package-level vars (testability)
- `go test ./...` passes with no regressions

## ACCEPTANCE CRITERIA
- `--verify-commit` flag detects whether a new commit was made during execution and fails the task if none was made
- `--auto-commit` flag stages all changes and commits with a templated message after successful execution
- `--commit-template` flag allows customizing the auto-commit message with `{task_id}` placeholder
- `--auto-commit` and `--verify-commit` are mutually exclusive with a clear error
- `--branch-per-task` creates/resumes a `kiln/<task-id>` branch for isolated execution
- Git helper functions are testable via package-level var injection
- Pre/post commit SHAs, branch name, and auto-commit status are captured in JSON log entries
- All git features are opt-in; default `kiln exec` behavior is unchanged
- Graceful degradation when git is unavailable or directory is not a repo
- `go test ./...` passes
- No large refactors unrelated to git automation

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"git-automation"}}

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
{"kiln":{"status":"complete","task_id":"git-automation"}}
