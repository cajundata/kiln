# Task: exec-retry-backoff — exponential backoff with jitter

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## Goal
Upgrade the retry backoff strategy from fixed interval to exponential backoff with jitter. This prevents thundering herd when running `make -jN` with multiple tasks retrying simultaneously.

## Context
Current behavior: `--retry-backoff <duration>` sleeps a fixed duration between retries. The PRD specifies `--backoff exponential` with jitter.

## Requirements

1. Add `--backoff` flag with values: `fixed` (default, current behavior) and `exponential`
2. Exponential backoff formula: `min(base * 2^(attempt-1), maxBackoff) + jitter`
   - Base: value of `--retry-backoff` (e.g. 1s)
   - Max backoff cap: 5 minutes (constant)
   - Jitter: random 0–50% of computed delay
3. Keep `sleepFn` injectable for testing
4. When `--backoff fixed`: behave exactly as today (backward compat)
5. When `--backoff exponential`:
   - Attempt 1→2: sleep ~base
   - Attempt 2→3: sleep ~base*2
   - Attempt 3→4: sleep ~base*4
   - etc., capped at 5m

## Tests
- Fixed backoff: same duration each time (existing behavior preserved)
- Exponential backoff: durations increase between attempts
- Exponential backoff: durations capped at max
- Jitter: delays are not exactly powers of 2 (has randomness)
- Invalid `--backoff` value → error
- Default `--backoff` is "fixed"

## Acceptance criteria
- `go test ./...` passes
- `--retries 3 --retry-backoff 1s --backoff exponential` produces increasing delays
- Existing tests pass without modification (fixed is default)

## Final JSON Status Footer
{"kiln":{"status":"complete","task_id":"exec-retry-backoff","notes":"Implemented exponential backoff with jitter for retry delays."}}
