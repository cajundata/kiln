# Task: profile-strategy — Profile Strategy (speed vs reliable)

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
profile-strategy

## SCOPE
Implement ONLY the profile strategy feature described below. Do not work on other backlog items (TUI, engine abstraction, git automation, etc.), even if you notice related opportunities.

## CONTEXT
- This project uses the AgentRun workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.
- All source lives in `cmd/kiln/main.go` (single-package CLI).
- Kiln currently has no concept of workflow profiles. Every run uses the same defaults for parallelism, retry behavior, and gate enforcement.
- Different projects and development phases need different tradeoffs: a prototype sprint prioritizes speed; a production release prioritizes correctness gates.
- Profiles are default-value presets — individual task overrides still take precedence.
- If features referenced by profile settings (e.g., UNIFY closure, validation hooks, concurrency limits) are not yet implemented, the profile should still define the setting and document that it takes effect once the corresponding feature lands. Store the value in config; skip enforcement for features that don't exist yet.

## REQUIREMENTS

1. **Profile configuration file** — Introduce `.kiln/config.yaml` as the project-level configuration file:
   - Top-level `profile` key selects the active profile (string, e.g., `speed`, `reliable`).
   - If `.kiln/config.yaml` does not exist or `profile` is omitted, default to `speed`.
   - Parse with `gopkg.in/yaml.v3` (already used for `tasks.yaml`) or the YAML library already in use.
   - Validate that the profile value is one of the known profiles; error on unknown profile names.

2. **Built-in profile definitions** — Define exactly two profiles as Go structs/constants (not in YAML — these are compiled-in defaults):
   - **`speed`** profile (default):
     - `require_unify`: `false` (UNIFY closure is optional)
     - `require_verify_gates`: `false` (verify gates do not block `.done` creation)
     - `parallelism_limit`: `0` (unlimited — defers to Make's `-jN`)
     - `retry_max`: `2` (fewer retries for faster iteration)
     - `retry_backoff_base`: `5s`
   - **`reliable`** profile:
     - `require_unify`: `true` (UNIFY closure required before marking complete)
     - `require_verify_gates`: `true` (verify gates must pass before `.done` is written)
     - `parallelism_limit`: `2` (conservative parallelism)
     - `retry_max`: `4` (more retries for reliability)
     - `retry_backoff_base`: `10s`

3. **Profile struct and loading** — Implement a `Profile` struct and loader:
   - `type Profile struct` with fields: `RequireUnify bool`, `RequireVerifyGates bool`, `ParallelismLimit int`, `RetryMax int`, `RetryBackoffBase time.Duration`.
   - `func loadProfile(configPath string) (*Profile, error)` — reads `.kiln/config.yaml`, resolves the profile name, returns the populated Profile struct.
   - If the config file doesn't exist, return the `speed` profile (no error).
   - If the config file exists but is malformed YAML, return an error.

4. **Profile overrides in config** — Allow `.kiln/config.yaml` to override individual profile settings:
   - Example:
     ```yaml
     profile: reliable
     overrides:
       retry_max: 6
       parallelism_limit: 3
     ```
   - Overrides are applied on top of the selected profile's defaults.
   - Only known fields can be overridden; unknown fields produce a validation error.

5. **Integrate with `kiln exec`** — Wire the profile into the exec flow:
   - Load the profile at startup (after flag parsing, before execution).
   - Use `profile.RetryMax` as the default for `--retries` (flag still overrides if explicitly set).
   - Use `profile.RetryBackoffBase` as the default backoff base duration.
   - `require_unify` and `require_verify_gates`: if the corresponding features are not implemented yet, log a debug-level message (e.g., `"profile requires UNIFY but UNIFY is not yet implemented; skipping enforcement"`) and proceed. Do NOT error on unimplemented features — the settings are forward-compatible.
   - `parallelism_limit`: store on the profile but do not enforce in `kiln exec` (parallelism is controlled by Make). Document in comments that this value is consumed by `kiln gen-make` for `-jN` capping.

6. **Integrate with `kiln gen-make`** — Wire the parallelism limit:
   - If `profile.ParallelismLimit > 0`, add a `.NOTPARALLEL` guard or a `MAKEFLAGS += -j<N>` line to `.kiln/targets.mk` to cap parallelism.
   - If `parallelism_limit` is `0`, do not emit any parallelism directive (unlimited).

7. **`--profile` flag** — Add a `--profile` flag to `kiln exec` and `kiln gen-make`:
   - Overrides the profile from `.kiln/config.yaml`.
   - Validates against known profile names.
   - Takes precedence over config file but individual flags (e.g., `--retries`) still override profile defaults.

8. **`kiln profile` subcommand** (informational only):
   - `kiln profile` — prints the active profile name and its resolved settings (after overrides) to stdout.
   - Output format: human-readable key-value pairs (one per line).
   - Useful for debugging which settings are in effect.

## Tests

- `loadProfile` returns `speed` profile when config file doesn't exist
- `loadProfile` returns `reliable` profile when config specifies `profile: reliable`
- `loadProfile` returns error for unknown profile name
- `loadProfile` returns error for malformed YAML
- `loadProfile` applies overrides on top of base profile
- `loadProfile` rejects unknown override fields
- `speed` profile has correct default values (require_unify=false, retry_max=2, etc.)
- `reliable` profile has correct default values (require_unify=true, retry_max=4, etc.)
- `--profile` flag overrides config file profile
- `--retries` flag overrides profile's retry_max
- `kiln gen-make` emits parallelism cap when `parallelism_limit > 0`
- `kiln gen-make` omits parallelism directive when `parallelism_limit == 0`
- `kiln profile` subcommand prints resolved settings
- `go test ./...` passes with no regressions

## ACCEPTANCE CRITERIA
- `Profile` struct exists with all five fields (RequireUnify, RequireVerifyGates, ParallelismLimit, RetryMax, RetryBackoffBase)
- Two built-in profiles (`speed`, `reliable`) are defined with the specified defaults
- `.kiln/config.yaml` is parsed to select profile and apply overrides
- Missing config file defaults to `speed` profile without error
- Unknown profile names and unknown override fields produce validation errors
- `--profile` flag works on `kiln exec` and `kiln gen-make`, overriding config file
- `kiln exec` uses profile's RetryMax and RetryBackoffBase as defaults (flags still override)
- `kiln gen-make` respects `parallelism_limit` when generating `.kiln/targets.mk`
- `kiln profile` subcommand prints resolved active profile settings
- Forward-compatible: unimplemented features (UNIFY, verify gates) log a message but do not error
- `go test ./...` passes
- No large refactors unrelated to profile strategy

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"profile-strategy"}}

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
{"kiln":{"status":"complete","task_id":"profile-strategy"}}
