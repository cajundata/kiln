package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

// version is set at build time via ldflags. Defaults to "dev" for untagged builds.
var version = "dev"

// timeoutError is returned when the claude process exceeds the configured timeout.
type timeoutError struct {
	taskID  string
	timeout time.Duration
}

func (e *timeoutError) Error() string {
	return fmt.Sprintf("task %s timed out after %s", e.taskID, e.timeout)
}

// claudeExitError is returned when the claude process exits with a non-zero code.
type claudeExitError struct{ err error }

func (e *claudeExitError) Error() string { return "claude invocation failed: " + e.err.Error() }
func (e *claudeExitError) Unwrap() error { return e.err }

// footerError is returned when the claude output is missing a valid kiln JSON footer.
// isValidation is true when the footer was parsed but contained invalid/unexpected values;
// false when the footer could not be found or parsed at all.
type footerError struct {
	msg          string
	isValidation bool
}

func (e *footerError) Error() string { return e.msg }

// isRetryable returns true for errors that should trigger a retry attempt.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var te *timeoutError
	if errors.As(err, &te) {
		return true
	}
	var ce *claudeExitError
	return errors.As(err, &ce)
}

// Canonical error class constants for structured error reporting.
const (
	ErrClassTimeout          = "timeout"
	ErrClassClaudeExit       = "claude_exit"
	ErrClassFooterParse      = "footer_parse"
	ErrClassFooterValidation = "footer_validation"
	ErrClassLockConflict     = "lock_conflict"
	ErrClassSchemaValidation = "schema_validation"
	ErrClassUnknown          = "unknown"
)

// classify returns the canonical error class and retryability for the given error.
// timeout and claude_exit are retryable; all others are not.
func classify(err error) (errorClass string, retryable bool) {
	if err == nil {
		return "", false
	}
	var te *timeoutError
	if errors.As(err, &te) {
		return ErrClassTimeout, true
	}
	var ce *claudeExitError
	if errors.As(err, &ce) {
		return ErrClassClaudeExit, true
	}
	var fe *footerError
	if errors.As(err, &fe) {
		if fe.isValidation {
			return ErrClassFooterValidation, false
		}
		return ErrClassFooterParse, false
	}
	var lce *lockConflictError
	if errors.As(err, &lce) {
		return ErrClassLockConflict, false
	}
	return ErrClassUnknown, false
}

// lockInfo holds diagnostics written to a task's lock file.
type lockInfo struct {
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
	Hostname  string `json:"hostname"`
}

// lockConflictError is returned when a task lock is already held by another process.
type lockConflictError struct {
	TaskID   string
	LockPath string
	Holder   lockInfo
}

func (e *lockConflictError) Error() string {
	return fmt.Sprintf(
		"task %s is already locked by PID %d (started %s on %s). Use --force-unlock if the process is no longer running.",
		e.TaskID, e.Holder.PID, e.Holder.StartedAt, e.Holder.Hostname,
	)
}

// acquireLock atomically creates a lock file for the given task ID under locksDir.
// On success, returns an idempotent cleanup function that removes the lock file.
// If the lock file already exists, returns a lockConflictError with the holder's diagnostics.
func acquireLock(locksDir string, taskID string) (func(), error) {
	if err := os.MkdirAll(locksDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create locks directory: %w", err)
	}
	lockPath := filepath.Join(locksDir, taskID+".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			data, readErr := os.ReadFile(lockPath)
			if readErr != nil {
				return nil, fmt.Errorf("task %s is already locked (could not read lock file: %v). Use --force-unlock if the process is no longer running.", taskID, readErr)
			}
			var info lockInfo
			if jsonErr := json.Unmarshal(data, &info); jsonErr != nil {
				return nil, fmt.Errorf("task %s is already locked (could not parse lock file). Use --force-unlock if the process is no longer running.", taskID)
			}
			return nil, &lockConflictError{TaskID: taskID, LockPath: lockPath, Holder: info}
		}
		return nil, fmt.Errorf("failed to create lock file: %w", err)
	}
	hostname, _ := os.Hostname()
	info := lockInfo{
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Hostname:  hostname,
	}
	data, _ := json.Marshal(info)
	_, _ = f.Write(data)
	_ = f.Close()

	var once sync.Once
	cleanup := func() {
		once.Do(func() { _ = os.Remove(lockPath) })
	}
	return cleanup, nil
}

// kilnFooterPayload holds the inner fields of the kiln JSON footer.
type kilnFooterPayload struct {
	Status string `json:"status"`
	TaskID string `json:"task_id"`
	Notes  string `json:"notes,omitempty"`
}

// logEvent is a single captured output line with a timestamp and stream type.
type logEvent struct {
	TS   time.Time `json:"ts"`
	Type string    `json:"type"` // "stdout" or "stderr"
	Line string    `json:"line"`
}

// gateResult holds the outcome of a single verify gate execution.
type gateResult struct {
	Name       string `json:"name"`
	Cmd        string `json:"cmd"`
	Passed     bool   `json:"passed"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Stderr     string `json:"stderr,omitempty"` // combined stdout+stderr, truncated to 2000 chars
}

// verifyResults holds the aggregate outcome of all verify gates for a task run.
type verifyResults struct {
	Gates     []gateResult `json:"gates"`
	AllPassed bool         `json:"all_passed"`
	Skipped   bool         `json:"skipped"` // true when --skip-verify was used
}

// execRunLog is the structured JSON log written for each kiln exec attempt.
type execRunLog struct {
	TaskID       string              `json:"task_id"`
	StartedAt    time.Time           `json:"started_at"`
	EndedAt      time.Time           `json:"ended_at"`
	DurationMs   int64               `json:"duration_ms"`
	Model        string              `json:"model"`
	PromptFile   string              `json:"prompt_file"`
	ExitCode     int                 `json:"exit_code"`
	Status       string              `json:"status"` // "complete","not_complete","blocked","timeout","error"
	Footer       *kilnFooterEnvelope `json:"footer,omitempty"`
	FooterValid  bool                `json:"footer_valid"`
	ErrorClass   string              `json:"error_class,omitempty"`   // canonical error class (see ErrClass* constants)
	ErrorMessage string              `json:"error_message,omitempty"` // human-readable error description
	Retryable    bool                `json:"retryable,omitempty"`     // whether this error is retryable
	Events       []logEvent          `json:"events"`
	Verify       *verifyResults      `json:"verify,omitempty"` // nil when no gates configured
}

// lineCapture is an io.Writer that forwards writes to an optional passthrough
// writer and records each complete line as a logEvent.
type lineCapture struct {
	out    io.Writer
	evType string
	mu     *sync.Mutex
	events *[]logEvent
	buf    []byte
}

func newLineCapture(out io.Writer, evType string, mu *sync.Mutex, events *[]logEvent) *lineCapture {
	return &lineCapture{out: out, evType: evType, mu: mu, events: events}
}

func (lc *lineCapture) Write(p []byte) (int, error) {
	n := len(p)
	var writeErr error
	if lc.out != nil {
		_, writeErr = lc.out.Write(p)
	}
	lc.buf = append(lc.buf, p...)
	for {
		idx := bytes.IndexByte(lc.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(lc.buf[:idx])
		ts := time.Now()
		lc.mu.Lock()
		*lc.events = append(*lc.events, logEvent{TS: ts, Type: lc.evType, Line: line})
		lc.mu.Unlock()
		lc.buf = lc.buf[idx+1:]
	}
	return n, writeErr
}

func (lc *lineCapture) flush() {
	if len(lc.buf) > 0 {
		ts := time.Now()
		lc.mu.Lock()
		*lc.events = append(*lc.events, logEvent{TS: ts, Type: lc.evType, Line: string(lc.buf)})
		lc.mu.Unlock()
		lc.buf = nil
	}
}

// writeExecLog atomically writes entry as JSON to .kiln/logs/<taskID>.json.
func writeExecLog(logDir, taskID string, entry execRunLog) error {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}
	logPath := filepath.Join(logDir, taskID+".json")
	tmpPath := logPath + ".tmp"
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal log: %w", err)
	}
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write log: %w", err)
	}
	return os.Rename(tmpPath, logPath)
}

// kilnFooterEnvelope is the top-level JSON wrapper for the footer.
type kilnFooterEnvelope struct {
	Kiln kilnFooterPayload `json:"kiln"`
}

// parseFooter scans output lines (last-first) for a valid kiln JSON footer.
// Returns status, task_id, and true when found; empty strings and false otherwise.
// Handles both standalone footer lines and footers embedded within stream-json output.
func parseFooter(output string) (status, taskID string, ok bool) {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.Contains(line, `"kiln"`) && !strings.Contains(line, `\"kiln\"`) {
			continue
		}
		// Try parsing the whole line as a bare footer.
		var env kilnFooterEnvelope
		if err := json.Unmarshal([]byte(line), &env); err == nil && env.Kiln.Status != "" {
			return env.Kiln.Status, env.Kiln.TaskID, true
		}
		// Footer may be embedded inside a stream-json text field.
		// Extract string values and check each for the footer.
		for _, text := range extractStreamJSONTexts(line) {
			if s, id, found := tryParseFooterInText(text); found {
				return s, id, true
			}
		}
	}
	return "", "", false
}

// extractStreamJSONTexts extracts text content from known stream-json message structures.
func extractStreamJSONTexts(line string) []string {
	var msg struct {
		Result  string `json:"result"`
		Message struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal([]byte(line), &msg) != nil {
		return nil
	}
	var texts []string
	if msg.Result != "" {
		texts = append(texts, msg.Result)
	}
	for _, c := range msg.Message.Content {
		if c.Text != "" {
			texts = append(texts, c.Text)
		}
	}
	return texts
}

// tryParseFooterInText searches a text string for an embedded kiln footer JSON object.
func tryParseFooterInText(text string) (status, taskID string, ok bool) {
	for idx := 0; idx < len(text); idx++ {
		pos := strings.Index(text[idx:], `{"kiln":`)
		if pos < 0 {
			break
		}
		candidate := text[idx+pos:]
		var env kilnFooterEnvelope
		if err := json.Unmarshal([]byte(candidate), &env); err == nil && env.Kiln.Status != "" {
			return env.Kiln.Status, env.Kiln.TaskID, true
		}
		// Try truncating at each '}' from the end in case of trailing text.
		for j := len(candidate) - 1; j >= 0; j-- {
			if candidate[j] == '}' {
				sub := candidate[:j+1]
				if err := json.Unmarshal([]byte(sub), &env); err == nil && env.Kiln.Status != "" {
					return env.Kiln.Status, env.Kiln.TaskID, true
				}
			}
		}
		idx += pos + 1
	}
	return "", "", false
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: kiln <command> [flags]")
		return 1
	}

	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "exec":
		code, err := runExec(args[1:], stdout)
		if err != nil {
			fmt.Fprintf(stderr, "exec: %v\n", err)
			var te *timeoutError
			if errors.As(err, &te) {
				return 20
			}
			var fe *footerError
			if errors.As(err, &fe) {
				return 10
			}
			var lce *lockConflictError
			if errors.As(err, &lce) {
				return 10
			}
			return 1
		}
		return code
	case "plan":
		if err := runPlan(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "plan: %v\n", err)
			return 1
		}
		return 0
	case "gen-make":
		if err := runGenMake(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "gen-make: %v\n", err)
			return 1
		}
		return 0
	case "status":
		if err := runStatus(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "status: %v\n", err)
			return 1
		}
		return 0
	case "validate-schema":
		if err := runValidateSchema(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "validate-schema: %v\n", err)
			return 1
		}
		return 0
	case "validate-cycles":
		if err := runValidateCycles(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "validate-cycles: %v\n", err)
			return 1
		}
		return 0
	case "gen-prompts":
		if err := runGenPrompts(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "gen-prompts: %v\n", err)
			return 1
		}
		return 0
	case "report":
		if err := runReport(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "report: %v\n", err)
			return 1
		}
		return 0
	case "unify":
		code, err := runUnify(args[1:], stdout)
		if err != nil {
			fmt.Fprintf(stderr, "unify: %v\n", err)
		}
		return code
	case "retry":
		if err := runRetry(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "retry: %v\n", err)
			return 1
		}
		return 0
	case "reset":
		if err := runReset(args[1:], os.Stdin, stdout); err != nil {
			fmt.Fprintf(stderr, "reset: %v\n", err)
			return 1
		}
		return 0
	case "resume":
		if err := runResume(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "resume: %v\n", err)
			return 1
		}
		return 0
	case "verify-plan":
		code, err := runVerifyPlan(args[1:], stdout)
		if err != nil {
			fmt.Fprintf(stderr, "verify-plan: %v\n", err)
		}
		return code
	case "init":
		if err := runInit(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "init: %v\n", err)
			return 1
		}
		return 0
	case "profile":
		if err := runProfile(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "profile: %v\n", err)
			return 1
		}
		return 0
	case "tui":
		return runTUI(args[1:])
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 1
	}
}

// VerifyGate is a post-completion validation gate in a task's verify list.
// Each gate runs a shell command; the task is not marked done until all gates pass.
type VerifyGate struct {
	// Cmd is the shell command to run (required).
	Cmd string `yaml:"cmd"`
	// Name is a human-readable label for the gate (optional).
	Name string `yaml:"name,omitempty"`
	// Expect describes the expected outcome (optional); only "exit_code_zero" is currently supported.
	Expect string `yaml:"expect,omitempty"`
}

// Task represents a single entry in the tasks.yaml file.
type Task struct {
	ID          string   `yaml:"id"`
	Prompt      string   `yaml:"prompt"`
	Needs       []string `yaml:"needs"`
	Timeout     string   `yaml:"timeout,omitempty"`
	Model       string   `yaml:"model,omitempty"`
	Description string   `yaml:"description,omitempty"`
	// Kind classifies the task type (e.g. "feature", "fix", "research", "docs").
	// Convention: tasks with kind "research" are expected to produce artifacts at
	// .kiln/artifacts/research/<id>.md — this is a naming convention only and is
	// not enforced by kiln.
	Kind       string            `yaml:"kind,omitempty"`
	Tags       []string          `yaml:"tags,omitempty"`
	Retries    int               `yaml:"retries,omitempty"`
	Validation []string          `yaml:"validation,omitempty"`
	Engine     string            `yaml:"engine,omitempty"`
	Env        map[string]string `yaml:"env,omitempty"`
	DevPhase   int               `yaml:"dev-phase,omitempty"`
	// Phase is a human-oriented lifecycle phase (e.g. "plan", "build", "verify", "docs").
	// Free-form but must be non-whitespace-only if present.
	Phase string `yaml:"phase,omitempty"`
	// Milestone is a project milestone grouping (e.g. "m1-auth", "m2-payments").
	// Must be kebab-case if present.
	Milestone string `yaml:"milestone,omitempty"`
	// Acceptance lists acceptance criteria (Given/When/Then or bullet AC).
	// Each entry must be non-empty.
	Acceptance []string `yaml:"acceptance,omitempty"`
	// Verify lists validation gates to run post-completion before the .done marker is written.
	// Set to [] to opt out of project-level defaults.
	Verify []VerifyGate `yaml:"verify,omitempty"`
	// Lane is a concurrency grouping identifier. Tasks in the same lane run serially.
	// Must be kebab-case if present.
	Lane string `yaml:"lane,omitempty"`
	// Exclusive, if true, means this task must run with no other tasks in parallel.
	Exclusive bool `yaml:"exclusive,omitempty"`
}

// taskIDRegexp is the valid pattern for task IDs (kebab-case).
var taskIDRegexp = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// envVarKeyRegexp is the valid pattern for environment variable key names.
var envVarKeyRegexp = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// loadTasks reads, parses, and validates a tasks.yaml file.
// Unknown fields are rejected (strict schema).
func loadTasks(path string) ([]Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read tasks file: %w", err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var tasks []Task
	if decErr := dec.Decode(&tasks); decErr != nil {
		if decErr == io.EOF {
			return nil, fmt.Errorf("no tasks found in %s", path)
		}
		return nil, fmt.Errorf("failed to parse tasks file: %w", decErr)
	}

	if len(tasks) == 0 {
		return nil, fmt.Errorf("no tasks found in %s", path)
	}

	seen := make(map[string]bool)
	for i, t := range tasks {
		if t.ID == "" {
			return nil, fmt.Errorf("task at index %d: id is required", i)
		}
		if !taskIDRegexp.MatchString(t.ID) {
			return nil, fmt.Errorf("task %q: id must be kebab-case", t.ID)
		}
		if seen[t.ID] {
			return nil, fmt.Errorf("duplicate task id %q", t.ID)
		}
		seen[t.ID] = true
		if t.Prompt == "" {
			return nil, fmt.Errorf("task %q: prompt is required", t.ID)
		}
		if filepath.IsAbs(t.Prompt) {
			return nil, fmt.Errorf("task %q: prompt must be a relative path, got %q", t.ID, t.Prompt)
		}
		for j, dep := range t.Needs {
			if dep == "" {
				return nil, fmt.Errorf("task %q: needs[%d] must not be empty", t.ID, j)
			}
		}
		if t.Retries < 0 {
			return nil, fmt.Errorf("task %q: retries must be >= 0, got %d", t.ID, t.Retries)
		}
		if t.Kind != "" && strings.TrimSpace(t.Kind) == "" {
			return nil, fmt.Errorf("task %q: kind must not be whitespace-only", t.ID)
		}
		for j, tag := range t.Tags {
			if tag == "" || strings.ContainsAny(tag, " \t\n\r\f\v") {
				return nil, fmt.Errorf("task %q: tags[%d] must be non-empty and contain no whitespace", t.ID, j)
			}
		}
		for k := range t.Env {
			if !envVarKeyRegexp.MatchString(k) {
				return nil, fmt.Errorf("task %q: env key %q is not a valid environment variable name", t.ID, k)
			}
		}
		if t.Phase != "" && strings.TrimSpace(t.Phase) == "" {
			return nil, fmt.Errorf("task %q: phase must not be whitespace-only", t.ID)
		}
		if t.Milestone != "" && !taskIDRegexp.MatchString(t.Milestone) {
			return nil, fmt.Errorf("task %q: milestone must be kebab-case, got %q", t.ID, t.Milestone)
		}
		for j, ac := range t.Acceptance {
			if ac == "" {
				return nil, fmt.Errorf("task %q: acceptance[%d] must not be empty", t.ID, j)
			}
		}
		for j, g := range t.Verify {
			if g.Cmd == "" {
				return nil, fmt.Errorf("task %q: verify[%d].cmd must not be empty", t.ID, j)
			}
			if g.Expect != "" && g.Expect != "exit_code_zero" {
				return nil, fmt.Errorf("task %q: verify[%d].expect %q is not supported; use exit_code_zero", t.ID, j, g.Expect)
			}
		}
		if t.Lane != "" && !taskIDRegexp.MatchString(t.Lane) {
			return nil, fmt.Errorf("task %q: lane must be kebab-case, got %q", t.ID, t.Lane)
		}
	}

	return tasks, nil
}

func runValidateCycles(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("validate-cycles", flag.ContinueOnError)
	tasksFile := fs.String("tasks", "", "path to tasks.yaml")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *tasksFile == "" {
		return fmt.Errorf("--tasks is required")
	}

	tasks, err := loadTasks(*tasksFile)
	if err != nil {
		return err
	}

	// Build ID set for existence check and adjacency list in definition order.
	idSet := make(map[string]bool, len(tasks))
	adj := make(map[string][]string, len(tasks))
	order := make([]string, 0, len(tasks))
	for _, t := range tasks {
		idSet[t.ID] = true
		adj[t.ID] = t.Needs
		order = append(order, t.ID)
	}

	// 1. Validate all dependency references exist.
	for _, t := range tasks {
		for _, dep := range t.Needs {
			if !idSet[dep] {
				return fmt.Errorf("task %q: unknown dependency %q", t.ID, dep)
			}
		}
	}

	// 2. Cycle detection via DFS with color marking.
	// Colors: 0=white (unvisited), 1=gray (in current path), 2=black (fully visited).
	color := make(map[string]int, len(tasks))
	parent := make(map[string]string, len(tasks))

	var cycleErr error

	var dfs func(id string) bool
	dfs = func(id string) bool {
		color[id] = 1 // gray: on current DFS path
		for _, dep := range adj[id] {
			if color[dep] == 1 {
				// Back edge found: dep is an ancestor on the current path.
				// Reconstruct cycle: dep -> ... -> id -> dep
				path := []string{}
				cur := id
				for cur != dep {
					path = append(path, cur)
					cur = parent[cur]
				}
				// path is [id, ..., child_of_dep] in reverse; reverse it.
				for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
					path[i], path[j] = path[j], path[i]
				}
				fullPath := append([]string{dep}, path...)
				fullPath = append(fullPath, dep)
				cycleErr = fmt.Errorf("cycle detected: %s", strings.Join(fullPath, " -> "))
				return true
			}
			if color[dep] == 0 {
				parent[dep] = id
				if dfs(dep) {
					return true
				}
			}
		}
		color[id] = 2 // black: fully explored
		return false
	}

	for _, id := range order {
		if color[id] == 0 {
			if dfs(id) {
				return cycleErr
			}
		}
	}

	fmt.Fprintln(stdout, "validate-cycles: OK")
	return nil
}

func runValidateSchema(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("validate-schema", flag.ContinueOnError)
	tasksFile := fs.String("tasks", "", "path to tasks.yaml")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *tasksFile == "" {
		return fmt.Errorf("--tasks is required")
	}

	tasks, err := loadTasks(*tasksFile)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "validate-schema: OK (%d tasks)\n", len(tasks))
	return nil
}

func runPlan(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	prdFile := fs.String("prd", "PRD.md", "path to PRD file")
	promptFile := fs.String("prompt", ".kiln/prompts/00_extract_tasks.md", "path to extraction prompt")
	outFile := fs.String("out", ".kiln/tasks.yaml", "output path for tasks.yaml")
	modelFlag := fs.String("model", "", "claude model to use (overrides KILN_MODEL env var)")
	timeoutStr := fs.String("timeout", "60m", "maximum duration for the claude invocation")

	if err := fs.Parse(args); err != nil {
		return err
	}

	prdData, err := os.ReadFile(*prdFile)
	if err != nil {
		return fmt.Errorf("failed to read PRD file %q: %w", *prdFile, err)
	}

	promptData, err := os.ReadFile(*promptFile)
	if err != nil {
		return fmt.Errorf("failed to read prompt file %q: %w", *promptFile, err)
	}

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil {
		return fmt.Errorf("invalid --timeout value: %w", err)
	}

	// Build combined prompt: inject PRD content and output path so Claude
	// does not need to read from disk and writes to the correct location.
	combined := fmt.Sprintf(
		"%s\n\nOVERRIDE: Write the output YAML to %q (not any hardcoded path).\n\nPRD CONTENT:\n%s",
		string(promptData), *outFile, string(prdData),
	)

	model := resolveModel(*modelFlag, planDefaultModel)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := commandBuilder(ctx, combined, model)
	cmd.Stdout = stdout
	cmd.Stderr = stdout

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("plan timed out after %s", timeout)
		}
		return fmt.Errorf("claude invocation failed: %w", err)
	}

	if _, err := loadTasks(*outFile); err != nil {
		return fmt.Errorf("generated %s is invalid: %w", *outFile, err)
	}

	fmt.Fprintf(stdout, "plan: wrote %s\n", *outFile)
	return nil
}

func runGenMake(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("gen-make", flag.ContinueOnError)
	tasksFile := fs.String("tasks", "", "path to tasks.yaml")
	outFile := fs.String("out", "", "output path for targets.mk")
	devPhase := fs.Int("dev-phase", 0, "filter tasks to a specific dev-phase (0 = all)")
	format := fs.String("format", "text", "output format: text or json")
	profileFlag := fs.String("profile", "", "workflow profile: speed or reliable (overrides .kiln/config.yaml)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *tasksFile == "" {
		return fmt.Errorf("--tasks is required")
	}
	if *outFile == "" {
		return fmt.Errorf("--out is required")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("invalid --format value %q: must be text or json", *format)
	}

	tasks, err := loadTasks(*tasksFile)
	if err != nil {
		return err
	}

	// Load workflow profile for parallelism_limit.
	profile, profileErr := loadProfile(".kiln/config.yaml", *profileFlag)
	if profileErr != nil {
		return fmt.Errorf("failed to load profile: %w", profileErr)
	}

	if *devPhase > 0 {
		filtered := make([]Task, 0, len(tasks))
		for _, t := range tasks {
			if t.DevPhase == *devPhase {
				filtered = append(filtered, t)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("no tasks found for dev-phase %d", *devPhase)
		}
		tasks = filtered
	}

	var buf bytes.Buffer

	// Emit parallelism cap from profile if parallelism_limit > 0.
	// parallelism_limit = 0 means unlimited (no directive emitted).
	if profile.ParallelismLimit > 0 {
		fmt.Fprintf(&buf, "# Parallelism cap from profile (parallelism_limit=%d)\n", profile.ParallelismLimit)
		fmt.Fprintf(&buf, "MAKEFLAGS += -j%d\n\n", profile.ParallelismLimit)
	}

	// Collect all done targets
	var allDone []string
	for _, t := range tasks {
		allDone = append(allDone, ".kiln/done/"+t.ID+".done")
	}

	// Phony all target
	buf.WriteString(".PHONY: all\n")
	buf.WriteString("all:")
	for _, d := range allDone {
		buf.WriteString(" " + d)
	}
	buf.WriteString("\n\n")

	// Individual targets in definition order.
	// Note: tasks with kind "research" are expected to produce artifacts at
	// .kiln/artifacts/research/<id>.md (convention only, not enforced by kiln).
	for _, t := range tasks {
		target := ".kiln/done/" + t.ID + ".done"
		buf.WriteString(target + ":")
		for _, dep := range t.Needs {
			buf.WriteString(" .kiln/done/" + dep + ".done")
		}
		buf.WriteString("\n")
		recipe := "$(KILN) exec --task-id " + t.ID
		if t.Timeout != "" {
			recipe += " --timeout " + t.Timeout
		}
		if t.Model != "" {
			recipe += " --model " + t.Model
		}
		buf.WriteString("\t" + recipe + "\n")
		buf.WriteString("\n")
	}

	// Collect unique phase values in sorted order and generate .PHONY phase targets.
	phaseMap := make(map[string][]string) // phase -> list of done markers
	for _, t := range tasks {
		if t.Phase != "" {
			phaseMap[t.Phase] = append(phaseMap[t.Phase], ".kiln/done/"+t.ID+".done")
		}
	}
	if len(phaseMap) > 0 {
		phases := sortedKeys(phaseMap)
		for _, phase := range phases {
			buf.WriteString(".PHONY: phase-" + phase + "\n")
			buf.WriteString("phase-" + phase + ":")
			for _, d := range phaseMap[phase] {
				buf.WriteString(" " + d)
			}
			buf.WriteString("\n\n")
		}
	}

	// Collect unique milestone values in sorted order and generate .PHONY milestone targets.
	milestoneMap := make(map[string][]string) // milestone -> list of done markers
	for _, t := range tasks {
		if t.Milestone != "" {
			milestoneMap[t.Milestone] = append(milestoneMap[t.Milestone], ".kiln/done/"+t.ID+".done")
		}
	}
	if len(milestoneMap) > 0 {
		milestones := sortedKeys(milestoneMap)
		for _, ms := range milestones {
			buf.WriteString(".PHONY: milestone-" + ms + "\n")
			buf.WriteString("milestone-" + ms + ":")
			for _, d := range milestoneMap[ms] {
				buf.WriteString(" " + d)
			}
			buf.WriteString("\n\n")
		}
	}

	kilnDir := filepath.Dir(*outFile)
	if err := os.MkdirAll(kilnDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create runtime subdirectories alongside the output file.
	for _, subdir := range []string{"done", "logs", "locks", "unify", filepath.Join("artifacts", "research")} {
		if err := os.MkdirAll(filepath.Join(kilnDir, subdir), 0o755); err != nil {
			return fmt.Errorf("failed to create %s directory: %w", subdir, err)
		}
	}

	if err := os.WriteFile(*outFile, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	if *format == "json" {
		type genMakeTargetInfo struct {
			TaskID    string   `json:"task_id"`
			Target    string   `json:"target"`
			DependsOn []string `json:"depends_on"`
		}
		type genMakeJSONResult struct {
			TasksCount int                 `json:"tasks_count"`
			OutputFile string              `json:"output_file"`
			Targets    []genMakeTargetInfo `json:"targets"`
		}
		targets := make([]genMakeTargetInfo, 0, len(tasks))
		for _, t := range tasks {
			deps := make([]string, 0, len(t.Needs))
			for _, dep := range t.Needs {
				deps = append(deps, ".kiln/done/"+dep+".done")
			}
			targets = append(targets, genMakeTargetInfo{
				TaskID:    t.ID,
				Target:    ".kiln/done/" + t.ID + ".done",
				DependsOn: deps,
			})
		}
		result := genMakeJSONResult{
			TasksCount: len(tasks),
			OutputFile: *outFile,
			Targets:    targets,
		}
		data, _ := json.Marshal(result)
		fmt.Fprintln(stdout, string(data))
	}

	return nil
}

// sortedKeys returns the keys of m in sorted order.
func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple insertion sort — maps are small.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// StateManifest is the top-level state file written to .kiln/state.json.
type StateManifest struct {
	Tasks       map[string]*TaskState `json:"tasks"`
	LastUpdated time.Time             `json:"last_updated"`
}

// TaskState holds per-task execution state persisted across kiln exec invocations.
type TaskState struct {
	Status         string    `json:"status"`
	Attempts       int       `json:"attempts"`
	LastAttemptAt  time.Time `json:"last_attempt_at,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
	LastErrorClass string    `json:"last_error_class,omitempty"`
	CompletedAt    time.Time `json:"completed_at,omitempty"`
	DurationMs     int64     `json:"duration_ms,omitempty"`
	Model          string    `json:"model,omitempty"`
	Notes          string    `json:"notes,omitempty"`
}

// loadState reads and unmarshals .kiln/state.json.
// Returns an empty manifest (not error) if the file does not exist.
func loadState(path string) (*StateManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &StateManifest{Tasks: make(map[string]*TaskState)}, nil
		}
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}
	var s StateManifest
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}
	if s.Tasks == nil {
		s.Tasks = make(map[string]*TaskState)
	}
	return &s, nil
}

// saveState atomically writes state to path (write to .tmp, then rename).
// It sets LastUpdated before writing.
func saveState(path string, state *StateManifest) error {
	state.LastUpdated = time.Now()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write state: %w", err)
	}
	return os.Rename(tmpPath, path)
}

// hasUnfinishedDeps returns true if any of the given dependency IDs are not in the doneSet.
func hasUnfinishedDeps(needs []string, doneSet map[string]bool) bool {
	for _, dep := range needs {
		if !doneSet[dep] {
			return true
		}
	}
	return false
}

// taskStatusInfo holds the derived display state for a single task.
type taskStatusInfo struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Attempts int    `json:"attempts"`
	LastErr  string `json:"last_error,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Phase    string `json:"phase,omitempty"`
}

// deriveTaskStatus returns the effective display status for a task by consulting
// state.json, done markers, log files, and dep graph in that priority order.
func deriveTaskStatus(t Task, state *StateManifest, logDir, doneDir string, doneSet map[string]bool) taskStatusInfo {
	info := taskStatusInfo{ID: t.ID, Kind: t.Kind, Phase: t.Phase}

	// Priority 1: state.json per-task entry.
	if ts := state.Tasks[t.ID]; ts != nil && ts.Status != "" {
		status := ts.Status
		if status == "completed" {
			status = "complete"
		}
		info.Status = status
		info.Attempts = ts.Attempts
		if ts.LastErrorClass != "" {
			info.LastErr = ts.LastErrorClass
		}
		if ts.LastError != "" {
			info.LastErr = ts.LastError
		}
		return info
	}

	// Priority 2: done marker.
	if doneSet[t.ID] {
		info.Status = "complete"
		info.Attempts = 1
		return info
	}

	// Priority 3: log file.
	logPath := filepath.Join(logDir, t.ID+".json")
	if data, readErr := os.ReadFile(logPath); readErr == nil {
		var entry execRunLog
		if json.Unmarshal(data, &entry) == nil {
			status := entry.Status
			if status == "timeout" || status == "error" {
				status = "failed"
			}
			info.Status = status
			info.Attempts = 1
			if entry.ErrorClass != "" {
				info.LastErr = entry.ErrorClass
			} else if entry.ErrorMessage != "" {
				info.LastErr = entry.ErrorMessage
			} else {
				info.LastErr = entry.Status
			}
			return info
		}
	}

	// Priority 4: dep-based blocking; fallback to pending.
	if hasUnfinishedDeps(t.Needs, doneSet) {
		info.Status = "blocked"
	} else {
		info.Status = "pending"
	}
	return info
}

func runStatus(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	tasksFile := fs.String("tasks", "", "path to tasks.yaml")
	format := fs.String("format", "", "output format (json)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *tasksFile == "" {
		return fmt.Errorf("--tasks is required")
	}

	tasks, err := loadTasks(*tasksFile)
	if err != nil {
		return err
	}

	doneDir := ".kiln/done"
	logDir := ".kiln/logs"

	// Build done set by checking for done markers.
	doneSet := make(map[string]bool)
	for _, t := range tasks {
		if _, statErr := os.Stat(filepath.Join(doneDir, t.ID+".done")); statErr == nil {
			doneSet[t.ID] = true
		}
	}

	// Load state.json; ignore errors (treat as empty).
	state, _ := loadState(".kiln/state.json")
	if state == nil {
		state = &StateManifest{Tasks: make(map[string]*TaskState)}
	}

	// Derive display info for each task.
	infos := make([]taskStatusInfo, 0, len(tasks))
	counts := map[string]int{
		"complete": 0, "failed": 0, "not_complete": 0,
		"blocked": 0, "pending": 0, "running": 0,
	}
	for _, t := range tasks {
		info := deriveTaskStatus(t, state, logDir, doneDir, doneSet)
		if info.LastErr == "" {
			info.LastErr = "-"
		}
		counts[info.Status]++
		infos = append(infos, info)
	}

	// JSON output.
	if *format == "json" {
		type jsonSummary struct {
			Total       int `json:"total"`
			Complete    int `json:"complete"`
			Failed      int `json:"failed"`
			NotComplete int `json:"not_complete"`
			Blocked     int `json:"blocked"`
			Pending     int `json:"pending"`
			Running     int `json:"running"`
		}
		type jsonOutput struct {
			Tasks   []taskStatusInfo `json:"tasks"`
			Summary jsonSummary      `json:"summary"`
		}
		out := jsonOutput{
			Tasks: infos,
			Summary: jsonSummary{
				Total:       len(tasks),
				Complete:    counts["complete"],
				Failed:      counts["failed"],
				NotComplete: counts["not_complete"],
				Blocked:     counts["blocked"],
				Pending:     counts["pending"],
				Running:     counts["running"],
			},
		}
		data, _ := json.Marshal(out)
		fmt.Fprintln(stdout, string(data))
		return nil
	}

	// Human-readable table.
	fmt.Fprintf(stdout, "%-30s %-12s %-8s %-10s %-10s %-40s %s\n", "TASK", "STATUS", "ATTEMPTS", "KIND", "PHASE", "LAST ERROR", "NEEDS")
	fmt.Fprintf(stdout, "%-30s %-12s %-8s %-10s %-10s %-40s %s\n", "----", "------", "--------", "----", "-----", "----------", "-----")

	for i, info := range infos {
		t := tasks[i]
		needs := strings.Join(t.Needs, ", ")
		if needs == "" {
			needs = "-"
		}
		kind := info.Kind
		if kind == "" {
			kind = "-"
		}
		if len(kind) > 10 {
			kind = kind[:9] + "…"
		}
		phase := info.Phase
		if phase == "" {
			phase = "-"
		}
		if len(phase) > 10 {
			phase = phase[:9] + "…"
		}
		lastErr := info.LastErr
		if lastErr != "-" && len(lastErr) > 38 {
			lastErr = lastErr[:37] + "…"
		}
		fmt.Fprintf(stdout, "%-30s %-12s %-8d %-10s %-10s %-40s %s\n",
			info.ID, info.Status, info.Attempts, kind, phase, lastErr, needs)
	}

	// Backward-compat summary: "pending" tasks are those ready to run (was "runnable").
	doneCount := counts["complete"]
	runnableCount := counts["pending"]
	fmt.Fprintf(stdout, "\n%d/%d tasks done, %d runnable\n", doneCount, len(tasks), runnableCount)

	// Spec summary line.
	fmt.Fprintf(stdout, "\nSummary\n-------\nTotal: %d | Complete: %d | Failed: %d | Not Complete: %d | Pending: %d | Blocked: %d\n",
		len(tasks), counts["complete"], counts["failed"], counts["not_complete"], counts["pending"], counts["blocked"])

	return nil
}

// defaultModel is the built-in fallback when neither --model nor KILN_MODEL is set.
const defaultModel = "claude-sonnet-4-6"

// planDefaultModel is the default model for the plan command.
const planDefaultModel = "claude-opus-4-6"

// genPromptsDefaultModel is the default model for the gen-prompts command.
const genPromptsDefaultModel = "claude-opus-4-6"

// resolveModel returns the effective model name using the precedence:
// 1. flag value (if non-empty)
// 2. task model from tasks.yaml (if non-empty)
// 3. KILN_MODEL env var (if non-empty)
// 4. defaultModel constant
func resolveModel(flagValue, taskModel string) string {
	if flagValue != "" {
		return flagValue
	}
	if taskModel != "" {
		return taskModel
	}
	if v := os.Getenv("KILN_MODEL"); v != "" {
		return v
	}
	return defaultModel
}

// commandBuilder creates an *exec.Cmd for the claude invocation.
// Swappable in tests to avoid calling the real claude binary.
var commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
	return exec.CommandContext(ctx, "claude", "--model", model, "--dangerously-skip-permissions", "--verbose", "--output-format", "stream-json", "-p", prompt)
}

// sleepFn is used to sleep between retry attempts. Swappable in tests.
var sleepFn = time.Sleep

// execProgressWriter is the writer used for claude process output and warning messages
// in kiln exec --format json mode. When non-nil, it overrides the stdout writer for
// progress/warnings so that stdout carries only the final JSON result.
// Defaults to nil (use the caller-provided stdout). Override in tests to capture output.
var execProgressWriter io.Writer

func runGenPrompts(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("gen-prompts", flag.ContinueOnError)
	tasksFile := fs.String("tasks", ".kiln/tasks.yaml", "path to tasks.yaml")
	prdFile := fs.String("prd", "PRD.md", "path to PRD file")
	templateFile := fs.String("template", ".kiln/templates/<id>.md", "path to prompt template")
	modelFlag := fs.String("model", "", "model override")
	timeoutStr := fs.String("timeout", "60m", "timeout per Claude invocation")
	overwrite := fs.Bool("overwrite", false, "regenerate prompts even when file already exists")

	if err := fs.Parse(args); err != nil {
		return err
	}

	tasks, err := loadTasks(*tasksFile)
	if err != nil {
		return err
	}

	prdData, err := os.ReadFile(*prdFile)
	if err != nil {
		return fmt.Errorf("failed to read PRD file %q: %w", *prdFile, err)
	}

	templateData, err := os.ReadFile(*templateFile)
	if err != nil {
		return fmt.Errorf("failed to read template file %q: %w", *templateFile, err)
	}

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil {
		return fmt.Errorf("invalid --timeout value: %w", err)
	}

	model := resolveModel(*modelFlag, genPromptsDefaultModel)

	var generated, skipped int
	for _, task := range tasks {
		promptPath := task.Prompt

		if _, statErr := os.Stat(promptPath); statErr == nil && !*overwrite {
			skipped++
			continue
		}

		metaPrompt := buildGenPromptsMetaPrompt(string(templateData), string(prdData), task.ID, promptPath)

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		cmd := commandBuilder(ctx, metaPrompt, model)
		cmd.Stdout = stdout
		cmd.Stderr = stdout

		runErr := cmd.Run()
		cancel()

		if runErr != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("task %q timed out after %s", task.ID, timeout)
			}
			return fmt.Errorf("task %q: claude invocation failed: %w", task.ID, runErr)
		}

		if _, statErr := os.Stat(promptPath); statErr != nil {
			return fmt.Errorf("task %q: prompt file %q was not created", task.ID, promptPath)
		}

		generated++
	}

	fmt.Fprintf(stdout, "gen-prompts: generated %d, skipped %d\n", generated, skipped)
	return nil
}

// buildGenPromptsMetaPrompt constructs the meta-prompt sent to Claude for generating a task prompt file.
func buildGenPromptsMetaPrompt(template, prd, taskID, outputPath string) string {
	return fmt.Sprintf(`You are generating a task prompt file for the kiln CLI tool.

TEMPLATE STRUCTURE:
%s

PRD CONTENT:
%s

TASK ID: %s

INSTRUCTIONS:
1. Fill in the template above for task ID "%s".
2. Replace <task-id> with the actual task ID: %s
3. Write specific, actionable task instructions derived from the PRD for this task.
4. Include concrete acceptance criteria relevant to this task.
5. The JSON footer MUST use {"kiln":{"status":"complete","task_id":"%s"}} NOT {"agentrun":...}.
6. Write the complete filled-in prompt file to: %s

The output file must follow the template structure exactly with:
- TASK ID section containing: %s
- SCOPE focused on this specific task only
- REQUIREMENTS specific to what this task implements
- ACCEPTANCE CRITERIA with testable criteria for this task
- OUTPUT FORMAT CONTRACT using the kiln JSON footer with task_id "%s"
`, template, prd, taskID, taskID, taskID, taskID, outputPath, taskID, taskID)
}

// ProjectDefaults holds project-wide default settings applied to all tasks.
type ProjectDefaults struct {
	Verify []VerifyGate `yaml:"verify,omitempty"`
}

// =============================================================================
// --- Profile system ---
// =============================================================================

// Profile defines workflow defaults for speed vs reliability tradeoffs.
// These are compiled-in defaults; individual flag values and task settings
// take precedence over profile defaults.
type Profile struct {
	// RequireUnify, if true, means UNIFY closure must be generated before marking a task complete.
	// Forward-compatible: auto-enforcement in kiln exec is not yet implemented.
	// When true and UNIFY enforcement is absent, a debug message is logged and execution proceeds.
	RequireUnify bool
	// RequireVerifyGates, if true, means verify gates must pass before the .done marker is written.
	// Forward-compatible: full enforcement semantics may evolve; current behavior always enforces
	// configured gates regardless of this setting.
	RequireVerifyGates bool
	// ParallelismLimit, if > 0, caps the number of Make jobs via MAKEFLAGS in targets.mk.
	// 0 means unlimited (deferring to Make's -jN flag).
	// This value is consumed by gen-make, not by exec.
	ParallelismLimit int
	// RetryMax is the default number of additional retry attempts on retryable failures.
	RetryMax int
	// RetryBackoffBase is the default base duration for backoff between retry attempts.
	RetryBackoffBase time.Duration
}

// Known workflow profile name constants.
const (
	WorkflowProfileSpeed    = "speed"
	WorkflowProfileReliable = "reliable"
)

// speedProfile is the default workflow profile, optimized for fast iteration.
var speedProfile = Profile{
	RequireUnify:       false,
	RequireVerifyGates: false,
	ParallelismLimit:   0,
	RetryMax:           2,
	RetryBackoffBase:   5 * time.Second,
}

// reliableProfile is the conservative workflow profile, optimized for correctness.
var reliableProfile = Profile{
	RequireUnify:       true,
	RequireVerifyGates: true,
	ParallelismLimit:   2,
	RetryMax:           4,
	RetryBackoffBase:   10 * time.Second,
}

// workflowProfiles is the registry of built-in workflow profiles.
var workflowProfiles = map[string]Profile{
	WorkflowProfileSpeed:    speedProfile,
	WorkflowProfileReliable: reliableProfile,
}

// kilnConfigOverrides allows overriding individual profile settings in .kiln/config.yaml.
// Only the fields listed here are valid override keys; unknown keys produce a parse error
// when using strict YAML decoding.
type kilnConfigOverrides struct {
	RequireUnify       *bool   `yaml:"require_unify,omitempty"`
	RequireVerifyGates *bool   `yaml:"require_verify_gates,omitempty"`
	ParallelismLimit   *int    `yaml:"parallelism_limit,omitempty"`
	RetryMax           *int    `yaml:"retry_max,omitempty"`
	RetryBackoffBase   *string `yaml:"retry_backoff_base,omitempty"`
}

// ProjectConfig holds project-wide configuration loaded from .kiln/config.yaml.
type ProjectConfig struct {
	Defaults  ProjectDefaults     `yaml:"defaults"`
	Profile   string              `yaml:"profile,omitempty"`
	Overrides kilnConfigOverrides `yaml:"overrides,omitempty"`
}

// loadProjectConfig reads path and returns the parsed config.
// Returns an empty config (not an error) if the file does not exist.
// Unknown top-level fields and unknown override fields produce a validation error.
func loadProjectConfig(path string) (ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ProjectConfig{}, nil
		}
		return ProjectConfig{}, fmt.Errorf("failed to read config file: %w", err)
	}
	var cfg ProjectConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // reject unknown fields (including unknown override keys)
	if err := dec.Decode(&cfg); err != nil {
		if err == io.EOF {
			return ProjectConfig{}, nil // empty file is fine
		}
		return ProjectConfig{}, fmt.Errorf("failed to parse config file: %w", err)
	}
	return cfg, nil
}

// loadProfile reads .kiln/config.yaml and returns the active Profile.
// If the file does not exist, the speed profile is returned without error.
// profileOverride, if non-empty, takes precedence over the config file's profile field.
func loadProfile(configPath string, profileOverride string) (*Profile, error) {
	cfg, err := loadProjectConfig(configPath)
	if err != nil {
		return nil, err
	}

	// Determine effective profile name: flag > config > default.
	profileName := cfg.Profile
	if profileOverride != "" {
		profileName = profileOverride
	}
	if profileName == "" {
		profileName = WorkflowProfileSpeed
	}

	base, ok := workflowProfiles[profileName]
	if !ok {
		return nil, fmt.Errorf("unknown profile %q; known profiles: speed, reliable", profileName)
	}

	// Apply overrides on top of the base profile.
	if cfg.Overrides.RequireUnify != nil {
		base.RequireUnify = *cfg.Overrides.RequireUnify
	}
	if cfg.Overrides.RequireVerifyGates != nil {
		base.RequireVerifyGates = *cfg.Overrides.RequireVerifyGates
	}
	if cfg.Overrides.ParallelismLimit != nil {
		base.ParallelismLimit = *cfg.Overrides.ParallelismLimit
	}
	if cfg.Overrides.RetryMax != nil {
		base.RetryMax = *cfg.Overrides.RetryMax
	}
	if cfg.Overrides.RetryBackoffBase != nil {
		dur, parseErr := time.ParseDuration(*cfg.Overrides.RetryBackoffBase)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid overrides.retry_backoff_base %q: %w", *cfg.Overrides.RetryBackoffBase, parseErr)
		}
		base.RetryBackoffBase = dur
	}

	return &base, nil
}

// mergeGates computes the final gate list by merging project defaults with task-level gates.
// If taskLoaded is false, only project defaults are used.
// If taskVerify is nil (no verify field in task), project defaults are used.
// If taskVerify is empty (verify: []), project defaults are NOT used (opt-out).
// If taskVerify has gates, they are appended to project defaults.
func mergeGates(projectDefaults []VerifyGate, taskVerify []VerifyGate, taskLoaded bool) []VerifyGate {
	if !taskLoaded {
		return append([]VerifyGate{}, projectDefaults...)
	}
	if taskVerify == nil {
		return append([]VerifyGate{}, projectDefaults...)
	}
	if len(taskVerify) == 0 {
		return nil // explicit opt-out
	}
	combined := make([]VerifyGate, 0, len(projectDefaults)+len(taskVerify))
	combined = append(combined, projectDefaults...)
	combined = append(combined, taskVerify...)
	return combined
}

// defaultVerifyGateTimeout is the per-gate timeout when running verify gates.
const defaultVerifyGateTimeout = 60 * time.Minute

// runVerifyGates runs gates sequentially and returns the results.
// It stops at the first failure (fail-fast). workDir sets the working directory
// for gate commands; empty string means the current directory.
// gateTimeout is applied per gate.
func runVerifyGates(gates []VerifyGate, taskID string, workDir string, gateTimeout time.Duration) ([]gateResult, error) {
	if len(gates) == 0 {
		return nil, nil
	}
	results := make([]gateResult, 0, len(gates))
	for _, gate := range gates {
		name := gate.Name
		if name == "" {
			name = gate.Cmd
		}
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), gateTimeout)
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", gate.Cmd)
		if workDir != "" {
			cmd.Dir = workDir
		}
		// Run in its own process group so that all children (e.g. sleep spawned by sh)
		// are killed together when the context deadline is exceeded.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			if cmd.Process != nil {
				return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			return nil
		}
		var combinedOut bytes.Buffer
		cmd.Stdout = &combinedOut
		cmd.Stderr = &combinedOut
		runErr := cmd.Run()
		cancel()
		durMs := time.Since(start).Milliseconds()

		exitCode := 0
		if runErr != nil {
			var exitErr *exec.ExitError
			if errors.As(runErr, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}

		output := combinedOut.String()
		if len(output) > 2000 {
			output = output[:2000]
		}

		passed := runErr == nil
		results = append(results, gateResult{
			Name:       name,
			Cmd:        gate.Cmd,
			Passed:     passed,
			ExitCode:   exitCode,
			DurationMs: durMs,
			Stderr:     output,
		})

		if !passed {
			reason := runErr.Error()
			if ctx.Err() == context.DeadlineExceeded {
				reason = fmt.Sprintf("timed out after %s", gateTimeout)
			}
			return results, fmt.Errorf("verify gate %q failed: %s", name, reason)
		}
	}
	return results, nil
}

// maxBackoffDuration is the upper cap for exponential backoff delays.
const maxBackoffDuration = 5 * time.Minute

// computeBackoff returns the sleep duration before the next retry attempt.
// strategy must be "fixed" or "exponential".
// attempt is 1-indexed (attempt=1 means sleeping before the 2nd try).
func computeBackoff(strategy string, base time.Duration, attempt int) time.Duration {
	if strategy != "exponential" {
		return base
	}
	// delay = base * 2^(attempt-1), capped at maxBackoffDuration
	multiplier := math.Pow(2, float64(attempt-1))
	scaled := float64(base) * multiplier
	var delay time.Duration
	if scaled >= float64(maxBackoffDuration) {
		delay = maxBackoffDuration
	} else {
		delay = time.Duration(scaled)
	}
	// Jitter: add 0–50% of delay
	if delay > 0 {
		jitter := time.Duration(rand.Int63n(int64(delay/2) + 1))
		delay += jitter
	}
	return delay
}

// execJSONResult is the JSON output schema for kiln exec --format json.
type execJSONResult struct {
	TaskID      string              `json:"task_id"`
	Status      string              `json:"status"`
	ExitCode    int                 `json:"exit_code"`
	Model       string              `json:"model"`
	PromptFile  string              `json:"prompt_file"`
	StartedAt   string              `json:"started_at"`
	EndedAt     string              `json:"ended_at"`
	DurationMs  int64               `json:"duration_ms"`
	Attempts    int                 `json:"attempts"`
	Footer      *kilnFooterEnvelope `json:"footer"`
	FooterValid bool                `json:"footer_valid"`
	Error       *string             `json:"error"`
}

// writeExecJSONResult marshals and writes an execJSONResult to w as a single compact JSON line.
func writeExecJSONResult(w io.Writer, log execRunLog, attempts int, execErr error) {
	result := execJSONResult{
		TaskID:      log.TaskID,
		Status:      log.Status,
		ExitCode:    log.ExitCode,
		Model:       log.Model,
		PromptFile:  log.PromptFile,
		StartedAt:   log.StartedAt.UTC().Format(time.RFC3339),
		EndedAt:     log.EndedAt.UTC().Format(time.RFC3339),
		DurationMs:  log.DurationMs,
		Attempts:    attempts,
		Footer:      log.Footer,
		FooterValid: log.FooterValid,
	}
	if execErr != nil {
		msg := execErr.Error()
		result.Error = &msg
	}
	data, _ := json.Marshal(result)
	fmt.Fprintln(w, string(data))
}

func runExec(args []string, stdout io.Writer) (int, error) {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)

	taskID := fs.String("task-id", "", "task identifier")
	promptFile := fs.String("prompt-file", "", "path to prompt file")
	tasksFlag := fs.String("tasks", ".kiln/tasks.yaml", "path to tasks.yaml (resolves prompt and model from task definition)")
	modelFlag := fs.String("model", "", "claude model to use (overrides KILN_MODEL env var)")
	timeoutStr := fs.String("timeout", "60m", "maximum duration for the claude invocation")
	retriesFlag := fs.Int("retries", 0, "number of additional attempts on retryable failures (default from profile)")
	retryBackoffStr := fs.String("retry-backoff", "0s", "sleep duration between retry attempts (default from profile)")
	backoffFlag := fs.String("backoff", "fixed", "backoff strategy between retries: fixed or exponential")
	forceUnlock := fs.Bool("force-unlock", false, "remove existing lock file before acquiring (for stale locks)")
	skipVerify := fs.Bool("skip-verify", false, "skip all verify gates (useful for debugging; logs a warning)")
	noChain := fs.Bool("no-chain", false, "disable prompt chaining (skip dependency context injection)")
	maxContextBytes := fs.Int("max-context-bytes", 50000, "maximum bytes of injected dependency context (~50KB default)")
	profileFlag := fs.String("profile", "", "workflow profile: speed or reliable (overrides .kiln/config.yaml)")
	formatFlag := fs.String("format", "text", "output format: text or json")

	if err := fs.Parse(args); err != nil {
		return 0, err
	}

	// Track which flags were explicitly set by the user (for profile default precedence).
	retriesExplicit := false
	retryBackoffExplicit := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "retries":
			retriesExplicit = true
		case "retry-backoff":
			retryBackoffExplicit = true
		}
	})

	if *taskID == "" {
		return 0, fmt.Errorf("--task-id is required")
	}

	if *formatFlag != "text" && *formatFlag != "json" {
		return 0, fmt.Errorf("invalid --format value %q: must be text or json", *formatFlag)
	}

	// Determine the writer for progress output (claude process output, warnings).
	// In json mode, progress goes to stderr so stdout carries only the final JSON result.
	progressOut := io.Writer(stdout)
	if *formatFlag == "json" {
		if execProgressWriter != nil {
			progressOut = execProgressWriter
		} else {
			progressOut = os.Stderr
		}
	}

	// Acquire task-level lock to prevent concurrent execution of the same task.
	locksDir := ".kiln/locks"
	if *forceUnlock {
		lockPath := filepath.Join(locksDir, *taskID+".lock")
		if _, statErr := os.Stat(lockPath); statErr == nil {
			fmt.Fprintf(progressOut, "warning: force-unlocking stale lock for task %s\n", *taskID)
			_ = os.Remove(lockPath)
		}
	}
	unlock, lockErr := acquireLock(locksDir, *taskID)
	if lockErr != nil {
		return 10, lockErr
	}

	// Register signal handler so the lock is released on SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig, ok := <-sigCh
		if ok && sig != nil {
			unlock()
			os.Exit(130)
		}
	}()
	defer func() {
		signal.Stop(sigCh)
		close(sigCh)
		unlock()
	}()

	// Determine whether --tasks was explicitly provided by the user.
	tasksExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "tasks" {
			tasksExplicit = true
		}
	})

	// Resolve prompt and model from tasks.yaml.
	// Load tasks.yaml when explicitly provided or when --prompt-file is absent (use default).
	var taskModel string
	var taskRetries int
	var taskEnv map[string]string
	var taskNeeds []string
	var taskVerify []VerifyGate // nil = not set in tasks.yaml; []VerifyGate{} = explicitly empty (opt-out)
	var taskVerifyLoaded bool   // true if the task definition was loaded from tasks.yaml
	if tasksExplicit || *promptFile == "" {
		tasks, err := loadTasks(*tasksFlag)
		if err != nil {
			return 0, err
		}
		var found *Task
		for i := range tasks {
			if tasks[i].ID == *taskID {
				found = &tasks[i]
				break
			}
		}
		if found == nil {
			return 0, fmt.Errorf("task %q not found in %s", *taskID, *tasksFlag)
		}
		taskModel = found.Model
		taskRetries = found.Retries
		taskEnv = found.Env
		taskNeeds = found.Needs
		taskVerify = found.Verify
		taskVerifyLoaded = true
		if *promptFile == "" {
			if found.Prompt == "" {
				return 0, fmt.Errorf("task %q has no prompt field", *taskID)
			}
			*promptFile = found.Prompt
		}
	}

	if *promptFile == "" {
		return 0, fmt.Errorf("no prompt file: provide --prompt-file or ensure task has a prompt field in tasks.yaml")
	}

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil {
		return 0, fmt.Errorf("invalid --timeout value: %w", err)
	}

	if *backoffFlag != "fixed" && *backoffFlag != "exponential" {
		return 0, fmt.Errorf("invalid --backoff value %q: must be fixed or exponential", *backoffFlag)
	}

	data, err := os.ReadFile(*promptFile)
	if err != nil {
		return 0, fmt.Errorf("failed to read prompt file: %w", err)
	}
	prompt := string(data)

	// Augment prompt with context from completed dependency tasks (prompt chaining).
	if !*noChain && len(taskNeeds) > 0 {
		kilnDir := filepath.Dir(*tasksFlag)
		prompt = augmentPromptWithDeps(prompt, taskNeeds, *maxContextBytes, kilnDir)
	}

	// Load project-wide config (best-effort; ignore errors and proceed with no defaults).
	projectCfg, cfgErr := loadProjectConfig(".kiln/config.yaml")
	if cfgErr != nil {
		fmt.Fprintf(progressOut, "warning: failed to load .kiln/config.yaml: %v\n", cfgErr)
	}

	// Determine if a profile was explicitly configured (--profile flag or config file).
	// Profile retry defaults only apply when explicitly configured; otherwise defaults
	// remain at 0 for backward compatibility.
	profileExplicit := *profileFlag != "" || projectCfg.Profile != ""

	// Load workflow profile for settings (RequireUnify, parallelism, retry defaults).
	// Errors here are non-fatal (fall back to speed profile).
	profile, profileErr := loadProfile(".kiln/config.yaml", *profileFlag)
	if profileErr != nil {
		fmt.Fprintf(progressOut, "warning: failed to load profile: %v; using speed defaults\n", profileErr)
		p := speedProfile
		profile = &p
	}

	// Log forward-compatible profile features that are not yet enforced.
	if profile.RequireUnify {
		fmt.Fprintf(progressOut, "debug: profile requires UNIFY but UNIFY enforcement in exec is not yet implemented; skipping\n")
	}
	// require_verify_gates is forward-compatible; current behavior always enforces configured gates.

	model := resolveModel(*modelFlag, taskModel)

	// Compute effective retries: explicit flag > task-level > profile (if explicitly configured) > 0.
	retries := 0
	retriesFromProfile := false
	if profileExplicit {
		retries = profile.RetryMax
		retriesFromProfile = true
	}
	if taskRetries > 0 {
		retries = taskRetries
		retriesFromProfile = false
	}
	if retriesExplicit {
		retries = *retriesFlag
		retriesFromProfile = false
	}

	// Compute effective backoff: explicit flag > profile (when retries from profile) > 0s.
	// When retries come from explicit flag or task field, default backoff is 0s for
	// backward compatibility. Profile backoff only applies when retries also come from profile.
	retryBackoff, err := time.ParseDuration(*retryBackoffStr)
	if err != nil {
		return 0, fmt.Errorf("invalid --retry-backoff value: %w", err)
	}
	var effectiveBackoff time.Duration
	if retryBackoffExplicit {
		effectiveBackoff = retryBackoff
	} else if retriesFromProfile {
		effectiveBackoff = profile.RetryBackoffBase
	}

	maxAttempts := 1 + retries
	logDir := ".kiln/logs"
	stateFile := ".kiln/state.json"

	// Load state before execution; ignore errors (treat as empty state).
	state, stateErr := loadState(stateFile)
	if stateErr != nil {
		fmt.Fprintf(progressOut, "warning: failed to load state: %v\n", stateErr)
		state = &StateManifest{Tasks: make(map[string]*TaskState)}
	}
	if state.Tasks[*taskID] == nil {
		state.Tasks[*taskID] = &TaskState{Status: "pending"}
	}
	ts := state.Tasks[*taskID]

	var lastErr error
	var lastLog execRunLog
	var totalAttempts int

	// emitIfJSON writes the final JSON result to stdout when --format json is active.
	emitIfJSON := func(log execRunLog, attempts int, execErr error) {
		if *formatFlag == "json" {
			writeExecJSONResult(stdout, log, attempts, execErr)
		}
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		totalAttempts = attempt
		// Update state to "running" before each attempt.
		ts.Status = "running"
		ts.Attempts++
		ts.LastAttemptAt = time.Now()
		if saveErr := saveState(stateFile, state); saveErr != nil {
			fmt.Fprintf(progressOut, "warning: failed to save state: %v\n", saveErr)
		}

		log, err := execOnce(*taskID, prompt, model, *promptFile, timeout, progressOut, taskEnv)
		lastLog = log

		// For successful completions, run verify gates before writing the log.
		if err == nil && log.FooterValid {
			finalGates := mergeGates(projectCfg.Defaults.Verify, taskVerify, taskVerifyLoaded)
			if *skipVerify && len(finalGates) > 0 {
				fmt.Fprintf(progressOut, "warning: --skip-verify is set; skipping %d verify gate(s) for task %s\n", len(finalGates), *taskID)
				log.Verify = &verifyResults{Skipped: true, AllPassed: true, Gates: nil}
				lastLog = log
			} else if len(finalGates) > 0 {
				gateRes, gateErr := runVerifyGates(finalGates, *taskID, "", defaultVerifyGateTimeout)
				allPassed := gateErr == nil
				log.Verify = &verifyResults{Gates: gateRes, AllPassed: allPassed, Skipped: false}
				lastLog = log
				if !allPassed {
					fmt.Fprintf(progressOut, "verify: gate failure for task %s: %v\n", *taskID, gateErr)
				}
			}
		}

		// Always write the log, even on error (now includes verify results for successes).
		if writeErr := writeExecLog(logDir, *taskID, log); writeErr != nil {
			fmt.Fprintf(progressOut, "warning: failed to write exec log: %v\n", writeErr)
		}

		if err == nil {
			if log.FooterValid {
				verifyPassed := log.Verify == nil || log.Verify.AllPassed || log.Verify.Skipped
				if !verifyPassed {
					// Verify gate failed: don't write .done marker; treat as not_complete.
					ts.Status = "not_complete"
					ts.DurationMs = log.DurationMs
					ts.Model = log.Model
					if saveErr := saveState(stateFile, state); saveErr != nil {
						fmt.Fprintf(progressOut, "warning: failed to save state: %v\n", saveErr)
					}
					emitIfJSON(lastLog, totalAttempts, nil)
					return 2, nil
				}
				// Task completed and all verify gates passed (or no gates, or skipped).
				ts.Status = "completed"
				ts.CompletedAt = log.EndedAt
				ts.DurationMs = log.DurationMs
				ts.Model = log.Model
				ts.LastError = ""
				ts.LastErrorClass = ""
				if log.Footer != nil {
					ts.Notes = log.Footer.Kiln.Notes
				}
				if saveErr := saveState(stateFile, state); saveErr != nil {
					fmt.Fprintf(progressOut, "warning: failed to save state: %v\n", saveErr)
				}
				doneDir := ".kiln/done"
				if mkErr := os.MkdirAll(doneDir, 0o755); mkErr == nil {
					donePath := filepath.Join(doneDir, *taskID+".done")
					os.WriteFile(donePath, nil, 0o644)
				}
				emitIfJSON(lastLog, totalAttempts, nil)
				return 2, nil
			}
			// not_complete or blocked.
			taskStatus := "failed"
			if log.Status == "blocked" {
				taskStatus = "blocked"
			}
			ts.Status = taskStatus
			ts.DurationMs = log.DurationMs
			ts.Model = log.Model
			if saveErr := saveState(stateFile, state); saveErr != nil {
				fmt.Fprintf(progressOut, "warning: failed to save state: %v\n", saveErr)
			}
			emitIfJSON(lastLog, totalAttempts, nil)
			return 0, nil
		}

		lastErr = err

		// Update state to "failed" after an error attempt.
		ts.Status = "failed"
		ts.LastError = err.Error()
		errClass, _ := classify(err)
		ts.LastErrorClass = errClass
		if saveErr := saveState(stateFile, state); saveErr != nil {
			fmt.Fprintf(progressOut, "warning: failed to save state: %v\n", saveErr)
		}

		if !isRetryable(err) {
			emitIfJSON(lastLog, totalAttempts, lastErr)
			return lastLog.ExitCode, lastErr
		}

		if attempt < maxAttempts && effectiveBackoff > 0 {
			sleepFn(computeBackoff(*backoffFlag, effectiveBackoff, attempt))
		}
	}

	emitIfJSON(lastLog, totalAttempts, lastErr)
	return lastLog.ExitCode, lastErr
}

// execOnce runs a single claude invocation attempt and returns a structured log entry and any error.
func execOnce(taskID, prompt, model, promptFile string, timeout time.Duration, stdout io.Writer, taskEnv map[string]string) (execRunLog, error) {
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var events []logEvent
	var mu sync.Mutex
	var capturedBuf bytes.Buffer

	// stdout is captured both for passthrough and footer parsing; stderr only passes through.
	stdoutCapture := newLineCapture(io.MultiWriter(stdout, &capturedBuf), "stdout", &mu, &events)
	stderrCapture := newLineCapture(stdout, "stderr", &mu, &events)

	cmd := commandBuilder(ctx, prompt, model)
	if len(taskEnv) > 0 {
		baseEnv := cmd.Env
		if baseEnv == nil {
			baseEnv = os.Environ()
		}
		envMap := make(map[string]string, len(baseEnv))
		for _, e := range baseEnv {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				envMap[parts[0]] = parts[1]
			}
		}
		for k, v := range taskEnv {
			envMap[k] = v
		}
		newEnv := make([]string, 0, len(envMap))
		for k, v := range envMap {
			newEnv = append(newEnv, k+"="+v)
		}
		cmd.Env = newEnv
	}
	cmd.Stdout = stdoutCapture
	cmd.Stderr = stderrCapture

	runErr := cmd.Run()
	stdoutCapture.flush()
	stderrCapture.flush()

	endedAt := time.Now()
	entry := execRunLog{
		TaskID:     taskID,
		StartedAt:  startedAt,
		EndedAt:    endedAt,
		DurationMs: endedAt.Sub(startedAt).Milliseconds(),
		Model:      model,
		PromptFile: promptFile,
		Events:     events,
	}

	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			entry.Status = "timeout"
			entry.ExitCode = 20
			te := &timeoutError{taskID: taskID, timeout: timeout}
			entry.ErrorClass = ErrClassTimeout
			entry.ErrorMessage = te.Error()
			entry.Retryable = true
			return entry, te
		}
		entry.Status = "error"
		entry.ExitCode = 1
		ce := &claudeExitError{err: runErr}
		entry.ErrorClass = ErrClassClaudeExit
		entry.ErrorMessage = ce.Error()
		entry.Retryable = true
		return entry, ce
	}

	footerStatus, footerTaskID, ok := parseFooter(capturedBuf.String())
	if !ok {
		entry.Status = "error"
		entry.ExitCode = 10
		fe := &footerError{msg: fmt.Sprintf(
			"missing or invalid footer in claude output\n"+
				`expected format: {"kiln":{"status":"complete|not_complete|blocked","task_id":"%s"}}`,
			taskID,
		)}
		entry.ErrorClass = ErrClassFooterParse
		entry.ErrorMessage = fe.Error()
		return entry, fe
	}

	entry.Footer = &kilnFooterEnvelope{Kiln: kilnFooterPayload{Status: footerStatus, TaskID: footerTaskID}}

	switch footerStatus {
	case "complete":
		if footerTaskID == taskID {
			entry.FooterValid = true
			entry.Status = "complete"
			entry.ExitCode = 0
		} else {
			fmt.Fprintf(stdout, "warning: footer task_id %q does not match expected %q\n", footerTaskID, taskID)
			entry.FooterValid = false
			entry.Status = "complete"
			entry.ExitCode = 0
		}
		return entry, nil
	case "not_complete", "blocked":
		entry.FooterValid = false
		entry.Status = footerStatus
		entry.ExitCode = 0
		return entry, nil
	default:
		entry.Status = "error"
		entry.ExitCode = 10
		fe := &footerError{
			msg: fmt.Sprintf(
				"invalid footer status %q; expected complete, not_complete, or blocked", footerStatus,
			),
			isValidation: true,
		}
		entry.ErrorClass = ErrClassFooterValidation
		entry.ErrorMessage = fe.Error()
		return entry, fe
	}
}

// decisionLedgerEntry is a JSON Lines record appended to .kiln/decisions.log.
type decisionLedgerEntry struct {
	TaskID       string `json:"task_id"`
	Timestamp    string `json:"timestamp"`
	ArtifactPath string `json:"artifact_path"`
	Model        string `json:"model"`
}

// appendDecisionLedger appends a JSON Lines entry to the decisions log file.
func appendDecisionLedger(ledgerPath string, entry decisionLedgerEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal decision ledger entry: %w", err)
	}
	f, err := os.OpenFile(ledgerPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open decision ledger: %w", err)
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s\n", data)
	return err
}

// stripFooter removes the last kiln JSON footer line from the output string.
// Returns the content before the footer, with trailing blank lines trimmed.
func stripFooter(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.Contains(line, `"kiln"`) {
			continue
		}
		var env kilnFooterEnvelope
		if json.Unmarshal([]byte(line), &env) == nil && env.Kiln.Status != "" {
			prefix := lines[:i]
			for len(prefix) > 0 && strings.TrimSpace(prefix[len(prefix)-1]) == "" {
				prefix = prefix[:len(prefix)-1]
			}
			return strings.Join(prefix, "\n")
		}
	}
	return strings.TrimRight(output, "\n")
}

// readLogSummary reads an execRunLog file and returns a human-readable one-line summary.
func readLogSummary(logPath string) string {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return "(no execution log found)"
	}
	var entry execRunLog
	if err := json.Unmarshal(data, &entry); err != nil {
		return "(failed to parse execution log)"
	}
	return fmt.Sprintf("status=%s duration_ms=%d model=%s exit_code=%d",
		entry.Status, entry.DurationMs, entry.Model, entry.ExitCode)
}

// buildUnifyPrompt constructs the structured prompt for UNIFY closure artifact generation.
func buildUnifyPrompt(taskID, taskPromptContent, logSummary string) string {
	return fmt.Sprintf(`You are generating a closure artifact for kiln task "%s".

A closure artifact is a semantic summary of what actually happened during task execution,
bridging the gap between "task ran" and "task is fully reconciled".

## Original Task Prompt

%s

## Execution Log Summary

%s

## Your Job

Inspect the current repository state (run git status, git log --oneline -10, and
git diff HEAD~1 or similar to understand what changed during this task), then produce
a structured closure summary with these sections:

### What Changed
List files modified, functions added/removed, and key code changes made during this task.

### What's Incomplete or Deferred
List any known gaps, TODOs left in code, or work explicitly deferred.

### Decisions Made
List key design decisions made during this task and their rationale.

### Handoff Notes
What does the next developer (or downstream task) need to know?
Include any important context, caveats, or gotchas.

### Acceptance Criteria Coverage
If the original task prompt contains acceptance criteria, assess each criterion:
- MET: <criterion>
- UNMET: <criterion>

## Output Format

Write your closure summary in Markdown. Be specific and concrete.
End your response with EXACTLY this JSON footer on its own line:

{"kiln":{"status":"complete","task_id":"%s"}}

If you cannot produce a meaningful closure (e.g. cannot access git history), use:
{"kiln":{"status":"not_complete","task_id":"%s"}}`, taskID, taskPromptContent, logSummary, taskID, taskID)
}

// runUnify implements the `kiln unify` subcommand.
// It generates a closure artifact for a completed task via a single-shot Claude invocation.
func runUnify(args []string, stdout io.Writer) (int, error) {
	fs := flag.NewFlagSet("unify", flag.ContinueOnError)
	taskID := fs.String("task-id", "", "task identifier (kebab-case)")
	modelFlag := fs.String("model", "", "claude model to use (overrides KILN_MODEL env var)")
	timeoutStr := fs.String("timeout", "60m", "maximum duration for the claude invocation")

	if err := fs.Parse(args); err != nil {
		return 1, err
	}

	if *taskID == "" {
		return 1, fmt.Errorf("--task-id is required")
	}
	if !taskIDRegexp.MatchString(*taskID) {
		return 1, fmt.Errorf("--task-id %q must be kebab-case", *taskID)
	}

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil {
		return 1, fmt.Errorf("invalid --timeout value: %w", err)
	}

	// Check task completion: try state.json first, then .done marker.
	stateFile := ".kiln/state.json"
	state, _ := loadState(stateFile)

	taskCompleted := false
	if state != nil {
		if ts := state.Tasks[*taskID]; ts != nil && ts.Status == "completed" {
			taskCompleted = true
		}
	}
	if !taskCompleted {
		donePath := filepath.Join(".kiln", "done", *taskID+".done")
		if _, statErr := os.Stat(donePath); statErr == nil {
			taskCompleted = true
		}
	}
	if !taskCompleted {
		return 2, fmt.Errorf("task %q is not completed: check .kiln/done/%s.done or state.json", *taskID, *taskID)
	}

	// Read task prompt file (convention: .kiln/prompts/tasks/<task-id>.md).
	promptFilePath := filepath.Join(".kiln", "prompts", "tasks", *taskID+".md")
	promptBytes, readErr := os.ReadFile(promptFilePath)
	var taskPromptContent string
	if readErr != nil {
		taskPromptContent = fmt.Sprintf("(prompt file not found at %s)", promptFilePath)
	} else {
		taskPromptContent = string(promptBytes)
	}

	// Read execution log summary.
	logPath := filepath.Join(".kiln", "logs", *taskID+".json")
	logSummary := readLogSummary(logPath)

	model := resolveModel(*modelFlag, "")

	// Build the unify prompt.
	unifyPrompt := buildUnifyPrompt(*taskID, taskPromptContent, logSummary)

	// Single-shot Claude invocation (no retry loop).
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var capturedBuf bytes.Buffer
	cmd := commandBuilder(ctx, unifyPrompt, model)
	cmd.Stdout = io.MultiWriter(stdout, &capturedBuf)
	cmd.Stderr = stdout

	if runErr := cmd.Run(); runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return 2, fmt.Errorf("timed out after %s", timeout)
		}
		return 1, fmt.Errorf("claude invocation failed: %w", runErr)
	}

	output := capturedBuf.String()

	// Parse footer using existing parseFooter function.
	footerStatus, _, ok := parseFooter(output)
	if !ok {
		fmt.Fprintf(stdout, "unify: raw output logged above\n")
		return 10, &footerError{msg: fmt.Sprintf(
			"missing or invalid footer in claude output for task %q\n"+
				`expected: {"kiln":{"status":"complete","task_id":"%s"}}`,
			*taskID, *taskID,
		)}
	}

	if footerStatus != "complete" {
		return 2, fmt.Errorf("claude returned status %q for task %q (expected 'complete')", footerStatus, *taskID)
	}

	// Write closure artifact to .kiln/unify/<task-id>.md.
	unifyDir := ".kiln/unify"
	if err := os.MkdirAll(unifyDir, 0o755); err != nil {
		return 1, fmt.Errorf("failed to create unify directory: %w", err)
	}

	artifactPath := filepath.Join(unifyDir, *taskID+".md")
	artifactContent := stripFooter(output)
	if err := os.WriteFile(artifactPath, []byte(artifactContent), 0o644); err != nil {
		return 1, fmt.Errorf("failed to write closure artifact: %w", err)
	}

	// Append entry to decision ledger (.kiln/decisions.log).
	ledgerPath := ".kiln/decisions.log"
	ledgerEntry := decisionLedgerEntry{
		TaskID:       *taskID,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		ArtifactPath: artifactPath,
		Model:        model,
	}
	if ledgerErr := appendDecisionLedger(ledgerPath, ledgerEntry); ledgerErr != nil {
		fmt.Fprintf(stdout, "warning: failed to append decision ledger: %v\n", ledgerErr)
	}

	fmt.Fprintf(stdout, "unify: closure artifact written to %s\n", artifactPath)
	return 0, nil
}

// --- kiln report ---

// taskReportEntry holds per-task data for the report command.
type taskReportEntry struct {
	TaskID         string `json:"task_id"`
	Status         string `json:"status"`
	Attempts       int    `json:"attempts"`
	LastErrorClass string `json:"last_error_class,omitempty"`
	LastError      string `json:"last_error,omitempty"`
}

// reportSummary holds aggregate statistics for the report command.
type reportSummary struct {
	Total       int            `json:"total"`
	Complete    int            `json:"complete"`
	Failed      int            `json:"failed"`
	NotComplete int            `json:"not_complete"`
	Blocked     int            `json:"blocked"`
	Attempts    int            `json:"total_attempts"`
	TopErrors   map[string]int `json:"top_errors,omitempty"`
}

// reportData is the full structured output for the report command.
type reportData struct {
	Tasks   []taskReportEntry `json:"tasks"`
	Summary reportSummary     `json:"summary"`
}

// logStatusToDisplayStatus maps an execRunLog status to a report-facing display status.
func logStatusToDisplayStatus(status string) string {
	switch status {
	case "complete":
		return "complete"
	case "not_complete":
		return "not_complete"
	case "blocked":
		return "blocked"
	default:
		// "timeout", "error", and any unknown status are all displayed as "failed".
		return "failed"
	}
}

func runReport(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	format := fs.String("format", "table", "output format: table or json")
	logDir := fs.String("log-dir", ".kiln/logs", "path to logs directory")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *format != "table" && *format != "json" {
		return fmt.Errorf("invalid --format value %q: must be table or json", *format)
	}

	// Read all log files.
	dirEntries, err := os.ReadDir(*logDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(stdout, "No execution logs found in .kiln/logs/")
			return nil
		}
		return fmt.Errorf("failed to read log directory: %w", err)
	}

	var logs []execRunLog
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(*logDir, de.Name()))
		if readErr != nil {
			continue
		}
		var entry execRunLog
		if jsonErr := json.Unmarshal(data, &entry); jsonErr != nil {
			continue
		}
		logs = append(logs, entry)
	}

	if len(logs) == 0 {
		fmt.Fprintln(stdout, "No execution logs found in .kiln/logs/")
		return nil
	}

	// Load state for attempt counts (best-effort; ignore errors).
	state, _ := loadState(".kiln/state.json")
	if state == nil {
		state = &StateManifest{Tasks: make(map[string]*TaskState)}
	}

	// Build report.
	summary := reportSummary{TopErrors: make(map[string]int)}
	tasks := make([]taskReportEntry, 0, len(logs))

	for _, log := range logs {
		displayStatus := logStatusToDisplayStatus(log.Status)
		attempts := 0
		if ts := state.Tasks[log.TaskID]; ts != nil {
			attempts = ts.Attempts
		}

		tasks = append(tasks, taskReportEntry{
			TaskID:         log.TaskID,
			Status:         displayStatus,
			Attempts:       attempts,
			LastErrorClass: log.ErrorClass,
			LastError:      log.ErrorMessage,
		})

		summary.Total++
		summary.Attempts += attempts
		switch displayStatus {
		case "complete":
			summary.Complete++
		case "failed":
			summary.Failed++
		case "not_complete":
			summary.NotComplete++
		case "blocked":
			summary.Blocked++
		}
		if log.ErrorClass != "" {
			summary.TopErrors[log.ErrorClass]++
		}
	}

	if len(summary.TopErrors) == 0 {
		summary.TopErrors = nil
	}

	report := reportData{Tasks: tasks, Summary: summary}

	if *format == "json" {
		data, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			return fmt.Errorf("failed to marshal report: %w", marshalErr)
		}
		fmt.Fprintln(stdout, string(data))
		return nil
	}

	printReport(stdout, report)
	return nil
}

// printReport writes a human-readable table report to w.
func printReport(w io.Writer, r reportData) {
	fmt.Fprintf(w, "%-20s %-13s %-9s %-18s %s\n", "Task", "Status", "Attempts", "Last Error Class", "Last Error")
	fmt.Fprintf(w, "%-20s %-13s %-9s %-18s %s\n", "----", "------", "--------", "----------------", "----------")

	for _, t := range r.Tasks {
		errClass := t.LastErrorClass
		if errClass == "" {
			errClass = "-"
		}
		lastErr := t.LastError
		if lastErr == "" {
			lastErr = "-"
		}
		if len(lastErr) > 40 {
			lastErr = lastErr[:39] + "…"
		}
		fmt.Fprintf(w, "%-20s %-13s %-9d %-18s %s\n", t.TaskID, t.Status, t.Attempts, errClass, lastErr)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Summary")
	fmt.Fprintln(w, "-------")
	fmt.Fprintf(w, "Total: %d | Complete: %d | Failed: %d | Not Complete: %d | Blocked: %d\n",
		r.Summary.Total, r.Summary.Complete, r.Summary.Failed, r.Summary.NotComplete, r.Summary.Blocked)
	fmt.Fprintf(w, "Attempts: %d\n", r.Summary.Attempts)

	if len(r.Summary.TopErrors) > 0 {
		parts := make([]string, 0, len(r.Summary.TopErrors))
		for cls, count := range r.Summary.TopErrors {
			parts = append(parts, fmt.Sprintf("%s (%d)", cls, count))
		}
		sort.Strings(parts)
		fmt.Fprintf(w, "Top errors: %s\n", strings.Join(parts, ", "))
	}
}

// --- kiln retry ---

func runRetry(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("retry", flag.ContinueOnError)
	tasksFile := fs.String("tasks", ".kiln/tasks.yaml", "path to tasks.yaml")
	taskID := fs.String("task-id", "", "retry a specific task by ID")
	failedOnly := fs.Bool("failed", false, "retry only tasks with failed status")
	transientOnly := fs.Bool("transient-only", false, "with --failed, retry only retryable errors (requires error_class in log)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	tasks, err := loadTasks(*tasksFile)
	if err != nil {
		return err
	}

	doneDir := ".kiln/done"
	logDir := ".kiln/logs"

	doneSet := make(map[string]bool)
	for _, t := range tasks {
		if _, statErr := os.Stat(filepath.Join(doneDir, t.ID+".done")); statErr == nil {
			doneSet[t.ID] = true
		}
	}

	state, _ := loadState(".kiln/state.json")
	if state == nil {
		state = &StateManifest{Tasks: make(map[string]*TaskState)}
	}

	// Determine which tasks to retry.
	var toRetry []Task
	for _, t := range tasks {
		// If a specific task ID is requested, only consider that one.
		if *taskID != "" && t.ID != *taskID {
			continue
		}

		info := deriveTaskStatus(t, state, logDir, doneDir, doneSet)

		// Skip complete and pending tasks (only retry tasks that attempted but didn't complete).
		if info.Status == "complete" || info.Status == "pending" {
			continue
		}

		if *failedOnly && info.Status != "failed" {
			continue
		}

		if *transientOnly {
			if *failedOnly {
				// Check if the error is retryable via log file error_class.
				logPath := filepath.Join(logDir, t.ID+".json")
				data, readErr := os.ReadFile(logPath)
				if readErr != nil {
					// No log file to check retryability; warn and skip.
					fmt.Fprintf(stdout, "warning: cannot check retryability for task %s (no log file); skipping\n", t.ID)
					continue
				}
				var entry execRunLog
				if json.Unmarshal(data, &entry) != nil {
					fmt.Fprintf(stdout, "warning: cannot parse log for task %s; skipping\n", t.ID)
					continue
				}
				if !entry.Retryable {
					continue
				}
			} else {
				fmt.Fprintf(stdout, "warning: --transient-only has no effect without --failed\n")
			}
		}

		toRetry = append(toRetry, t)
	}

	if len(toRetry) == 0 {
		fmt.Fprintln(stdout, "No tasks match retry criteria.")
		return nil
	}

	// Print which tasks will be retried.
	ids := make([]string, len(toRetry))
	for i, t := range toRetry {
		ids[i] = t.ID
	}
	fmt.Fprintf(stdout, "Retrying %d task(s): %s\n", len(toRetry), strings.Join(ids, ", "))

	// Retry each task.
	for _, t := range toRetry {
		// Remove done marker so Make (and exec) don't skip.
		donePath := filepath.Join(doneDir, t.ID+".done")
		_ = os.Remove(donePath)

		execArgs := []string{"--task-id", t.ID, "--tasks", *tasksFile}
		if _, execErr := runExec(execArgs, stdout); execErr != nil {
			fmt.Fprintf(stdout, "retry: task %s failed: %v\n", t.ID, execErr)
		}
	}

	return nil
}

// --- kiln reset ---

func runReset(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("reset", flag.ContinueOnError)
	taskID := fs.String("task-id", "", "task ID to reset")
	all := fs.Bool("all", false, "reset all tasks (requires confirmation)")
	tasksFile := fs.String("tasks", ".kiln/tasks.yaml", "path to tasks.yaml")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *taskID == "" && !*all {
		return fmt.Errorf("--task-id or --all is required")
	}

	tasks, err := loadTasks(*tasksFile)
	if err != nil {
		return err
	}

	taskIndex := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		taskIndex[t.ID] = true
	}

	if *all {
		// Prompt for confirmation.
		fmt.Fprintf(stdout, "Reset all %d tasks? [y/N] ", len(tasks))
		buf := make([]byte, 8)
		n, _ := stdin.Read(buf)
		answer := strings.TrimSpace(strings.ToLower(string(buf[:n])))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(stdout, "Aborted.")
			return nil
		}
		for _, t := range tasks {
			if err := resetTask(t.ID, stdout); err != nil {
				fmt.Fprintf(stdout, "warning: reset task %s: %v\n", t.ID, err)
			}
		}
		// Clear state.json entirely.
		clearAllState()
		return nil
	}

	// Single task reset.
	if !taskIndex[*taskID] {
		fmt.Fprintf(stdout, "Unknown task: %s\n", *taskID)
		return fmt.Errorf("unknown task: %s", *taskID)
	}
	return resetTask(*taskID, stdout)
}

// resetTask removes the done marker and archives the log for a single task,
// and removes the task's entry from state.json.
func resetTask(id string, stdout io.Writer) error {
	doneDir := ".kiln/done"
	logDir := ".kiln/logs"
	stateFile := ".kiln/state.json"

	// Remove done marker.
	donePath := filepath.Join(doneDir, id+".done")
	_ = os.Remove(donePath)

	// Archive log file.
	logPath := filepath.Join(logDir, id+".json")
	bakPath := logPath + ".bak"
	if _, err := os.Stat(logPath); err == nil {
		if renameErr := os.Rename(logPath, bakPath); renameErr != nil {
			// Try copy+delete as fallback.
			if data, readErr := os.ReadFile(logPath); readErr == nil {
				_ = os.WriteFile(bakPath, data, 0o644)
				_ = os.Remove(logPath)
			}
		}
	}

	// Remove task entry from state.json.
	state, _ := loadState(stateFile)
	if state != nil {
		delete(state.Tasks, id)
		_ = saveState(stateFile, state)
	}

	fmt.Fprintf(stdout, "Reset task: %s (done marker removed, logs archived)\n", id)
	return nil
}

// clearAllState removes all task entries from state.json.
func clearAllState() {
	stateFile := ".kiln/state.json"
	state, _ := loadState(stateFile)
	if state != nil {
		state.Tasks = make(map[string]*TaskState)
		_ = saveState(stateFile, state)
	}
}

// --- prompt chaining ---

// truncationNotice is appended to context sections that have been truncated due to budget limits.
const truncationNotice = "\n[truncated — exceeded context budget]\n"

// depContext holds gathered context for a single dependency task.
type depContext struct {
	id      string
	source  string // "unify", "research", or "log"
	content string
}

// summarizeExecLog parses an execRunLog JSON blob and returns a minimal summary.
// It omits raw event lines to avoid polluting downstream prompts.
func summarizeExecLog(data []byte) string {
	var entry execRunLog
	if err := json.Unmarshal(data, &entry); err != nil {
		return "(failed to parse execution log)"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "task_id: %s\n", entry.TaskID)
	fmt.Fprintf(&sb, "status: %s\n", entry.Status)
	fmt.Fprintf(&sb, "model: %s\n", entry.Model)
	fmt.Fprintf(&sb, "duration_ms: %d\n", entry.DurationMs)
	if entry.Footer != nil && entry.Footer.Kiln.Notes != "" {
		fmt.Fprintf(&sb, "notes: %s\n", entry.Footer.Kiln.Notes)
	}
	return sb.String()
}

// augmentPromptWithDeps gathers context from completed dependency tasks and prepends
// a "## Context from Completed Dependencies" section to the original prompt.
// Context sources are checked in priority order: UNIFY artifact > research artifact > execution log.
// Only dependencies with a .done marker are included.
// If no context is available, the original prompt is returned unchanged.
func augmentPromptWithDeps(prompt string, depIDs []string, maxContextBytes int, kilnDir string) string {
	if len(depIDs) == 0 {
		return prompt
	}

	var contexts []depContext
	for _, depID := range depIDs {
		// Only gather context from confirmed-complete deps (done marker must exist).
		donePath := filepath.Join(kilnDir, "done", depID+".done")
		if _, err := os.Stat(donePath); err != nil {
			continue
		}

		// Priority 1: UNIFY closure artifact.
		unifyPath := filepath.Join(kilnDir, "unify", depID+".md")
		if data, err := os.ReadFile(unifyPath); err == nil {
			contexts = append(contexts, depContext{id: depID, source: "unify", content: string(data)})
			continue
		}

		// Priority 2: Research artifact.
		researchPath := filepath.Join(kilnDir, "artifacts", "research", depID+".md")
		if data, err := os.ReadFile(researchPath); err == nil {
			contexts = append(contexts, depContext{id: depID, source: "research", content: string(data)})
			continue
		}

		// Priority 3: Execution log (fallback).
		logPath := filepath.Join(kilnDir, "logs", depID+".json")
		if data, err := os.ReadFile(logPath); err == nil {
			summary := summarizeExecLog(data)
			contexts = append(contexts, depContext{id: depID, source: "log", content: summary})
			continue
		}
	}

	if len(contexts) == 0 {
		return prompt
	}

	// Apply size guard: truncate oldest (first-listed) contexts to fit within budget.
	if maxContextBytes > 0 {
		total := 0
		for _, c := range contexts {
			total += len(c.content)
		}
		if total > maxContextBytes {
			excess := total - maxContextBytes
			for i := range contexts {
				if excess <= 0 {
					break
				}
				contentLen := len(contexts[i].content)
				if contentLen <= excess {
					excess -= contentLen
					contexts[i].content = truncationNotice
				} else {
					keepLen := contentLen - excess
					contexts[i].content = contexts[i].content[:keepLen] + truncationNotice
					excess = 0
				}
			}
		}
	}

	// Build augmented prompt.
	var sb strings.Builder
	sb.WriteString("## Context from Completed Dependencies\n\n")
	for _, c := range contexts {
		fmt.Fprintf(&sb, "### Dependency: %s (source: %s)\n", c.id, c.source)
		sb.WriteString(c.content)
		if !strings.HasSuffix(c.content, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	sb.WriteString(prompt)
	return sb.String()
}

// --- kiln resume ---

func runResume(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	taskID := fs.String("task-id", "", "task ID to resume")
	tasksFile := fs.String("tasks", ".kiln/tasks.yaml", "path to tasks.yaml")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *taskID == "" {
		return fmt.Errorf("--task-id is required")
	}

	tasks, err := loadTasks(*tasksFile)
	if err != nil {
		return err
	}

	// Find the task.
	var found *Task
	for i := range tasks {
		if tasks[i].ID == *taskID {
			found = &tasks[i]
			break
		}
	}
	if found == nil {
		fmt.Fprintf(stdout, "Unknown task: %s\n", *taskID)
		return fmt.Errorf("unknown task: %s", *taskID)
	}

	// Load state for attempt count and last error.
	state, _ := loadState(".kiln/state.json")
	if state == nil {
		state = &StateManifest{Tasks: make(map[string]*TaskState)}
	}

	logDir := ".kiln/logs"
	logPath := filepath.Join(logDir, *taskID+".json")

	// Determine attempt count and last error.
	var attemptCount int
	var lastStatus, lastError string

	if ts := state.Tasks[*taskID]; ts != nil {
		attemptCount = ts.Attempts
		lastStatus = ts.Status
		lastError = ts.LastError
	} else {
		// Fall back to log file.
		if data, readErr := os.ReadFile(logPath); readErr == nil {
			var entry execRunLog
			if json.Unmarshal(data, &entry) == nil {
				attemptCount = 1
				lastStatus = entry.Status
				lastError = entry.ErrorMessage
			}
		}
	}

	if attemptCount == 0 {
		fmt.Fprintf(stdout, "No prior attempts found for task: %s. Use 'kiln exec' instead.\n", *taskID)
		return nil
	}

	// Read original prompt.
	promptData, err := os.ReadFile(found.Prompt)
	if err != nil {
		return fmt.Errorf("failed to read prompt file %q: %w", found.Prompt, err)
	}

	// Build resume output.
	var buf strings.Builder

	buf.WriteString("# RESUME CONTEXT\n\n")
	fmt.Fprintf(&buf, "Task ID: %s\n", *taskID)
	fmt.Fprintf(&buf, "Prior attempts: %d\n", attemptCount)
	fmt.Fprintf(&buf, "Last status: %s\n", lastStatus)
	if lastError != "" {
		fmt.Fprintf(&buf, "Last error: %s\n", lastError)
	}
	buf.WriteString("\n")

	// Include closure artifact if available.
	unifyPath := filepath.Join(".kiln", "unify", *taskID+".md")
	if data, readErr := os.ReadFile(unifyPath); readErr == nil {
		buf.WriteString("# PREVIOUS CLOSURE SUMMARY\n\n")
		buf.Write(data)
		buf.WriteString("\n\n")
	}

	buf.WriteString("# ORIGINAL TASK PROMPT\n\n")
	buf.Write(promptData)

	fmt.Fprint(stdout, buf.String())
	return nil
}

// =============================================================================
// --- verify-plan subcommand ---
// =============================================================================

// verifyIssue represents a single coverage or executability issue found by verify-plan.
type verifyIssue struct {
	TaskID  string `json:"task_id"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// verifyPlanSummary holds aggregate counts for the verify-plan report.
type verifyPlanSummary struct {
	TotalTasks int `json:"total_tasks"`
	Covered    int `json:"covered"`
	Uncovered  int `json:"uncovered"`
	Warnings   int `json:"warnings"`
}

// verifyPlanResult is the full JSON-serializable report produced by verify-plan.
type verifyPlanResult struct {
	Summary verifyPlanSummary `json:"summary"`
	Issues  []verifyIssue     `json:"issues"`
	Pass    bool              `json:"pass"`
}

// runVerifyPlan implements the verify-plan subcommand.
// It analyzes the task graph for coverage gaps between acceptance criteria and
// verify gates, and checks that gate executables are plausibly runnable.
// Returns exit code and any fatal error.
func runVerifyPlan(args []string, stdout io.Writer) (int, error) {
	fs := flag.NewFlagSet("verify-plan", flag.ContinueOnError)
	tasksFile := fs.String("tasks", ".kiln/tasks.yaml", "path to tasks.yaml")
	configFile := fs.String("config", ".kiln/config.yaml", "path to config.yaml")
	strict := fs.Bool("strict", false, "treat warnings as errors (CI mode)")
	format := fs.String("format", "text", "output format: text or json")

	if err := fs.Parse(args); err != nil {
		return 1, err
	}

	cfg, err := loadProjectConfig(*configFile)
	if err != nil {
		return 1, fmt.Errorf("failed to load config: %w", err)
	}

	tasks, loadErr := loadTasks(*tasksFile)
	if loadErr != nil {
		// An empty tasks file ([] or missing entries) is not an error for verify-plan.
		if strings.Contains(loadErr.Error(), "no tasks found") {
			printVerifyPlanReport(stdout, *format, verifyPlanResult{
				Summary: verifyPlanSummary{},
				Issues:  []verifyIssue{},
				Pass:    true,
			}, 0)
			return 0, nil
		}
		return 1, loadErr
	}

	issues := []verifyIssue{}
	covered := 0
	uncovered := 0

	for _, t := range tasks {
		// Compute effective gates by merging project defaults with task-level gates.
		// t.Verify == nil means "inherit defaults"; len == 0 means explicit opt-out.
		effectiveGates := mergeGates(cfg.Defaults.Verify, t.Verify, true)
		hasAcceptance := len(t.Acceptance) > 0
		hasGates := len(effectiveGates) > 0

		if !hasAcceptance && !hasGates {
			continue // neither: skip silently
		}

		if hasAcceptance && !hasGates {
			uncovered++
			issues = append(issues, verifyIssue{
				TaskID:  t.ID,
				Type:    "UNCOVERED",
				Message: fmt.Sprintf("Task has %d acceptance criteria but no verify gates", len(t.Acceptance)),
			})
			continue
		}

		if !hasAcceptance && hasGates {
			issues = append(issues, verifyIssue{
				TaskID:  t.ID,
				Type:    "UNANCHORED",
				Message: "Task has verify gates but no acceptance criteria",
			})
		}

		if hasAcceptance && hasGates {
			covered++
		}

		// Check each gate's executability.
		for _, gate := range effectiveGates {
			if strings.TrimSpace(gate.Cmd) == "" {
				issues = append(issues, verifyIssue{
					TaskID:  t.ID,
					Type:    "EMPTY_CMD",
					Message: "Verify gate has empty or blank command",
				})
				continue
			}
			parts := strings.Fields(gate.Cmd)
			if len(parts) > 0 {
				if _, lookErr := exec.LookPath(parts[0]); lookErr != nil {
					issues = append(issues, verifyIssue{
						TaskID:  t.ID,
						Type:    "CMD_NOT_FOUND",
						Message: fmt.Sprintf("Executable %q not found on PATH", parts[0]),
					})
				}
			}
		}
	}

	// Tally errors vs warnings based on --strict flag.
	errorCount := 0
	warningCount := 0
	for _, issue := range issues {
		switch issue.Type {
		case "UNCOVERED", "EMPTY_CMD":
			errorCount++
		case "UNANCHORED", "CMD_NOT_FOUND":
			if *strict {
				errorCount++
			} else {
				warningCount++
			}
		}
	}

	pass := errorCount == 0
	result := verifyPlanResult{
		Summary: verifyPlanSummary{
			TotalTasks: len(tasks),
			Covered:    covered,
			Uncovered:  uncovered,
			Warnings:   warningCount,
		},
		Issues: issues,
		Pass:   pass,
	}

	printVerifyPlanReport(stdout, *format, result, errorCount)
	if !pass {
		return 1, nil
	}
	return 0, nil
}

// printVerifyPlanReport writes the verify-plan report to stdout in the requested format.
func printVerifyPlanReport(stdout io.Writer, format string, result verifyPlanResult, errorCount int) {
	if format == "json" {
		data, _ := json.Marshal(result)
		fmt.Fprintln(stdout, string(data))
		return
	}

	// Text format.
	s := result.Summary
	fmt.Fprintf(stdout, "verify-plan: %d tasks | %d covered | %d uncovered | %d warnings\n",
		s.TotalTasks, s.Covered, s.Uncovered, s.Warnings)

	if len(result.Issues) > 0 {
		fmt.Fprintln(stdout)
		for _, issue := range result.Issues {
			fmt.Fprintf(stdout, "[%s] %s: %s\n", issue.Type, issue.TaskID, issue.Message)
		}
		fmt.Fprintln(stdout)
	}

	if result.Pass {
		fmt.Fprintln(stdout, "All tasks covered")
	} else {
		fmt.Fprintf(stdout, "%d issue(s) found\n", errorCount)
	}
}

// =============================================================================
// --- init subcommand ---
// =============================================================================

// validProfiles is the set of allowed --profile values for kiln init.
var validProfiles = map[string]bool{
	"go":      true,
	"python":  true,
	"node":    true,
	"generic": true,
}

// initScaffoldFile represents a file to be written during kiln init.
type initScaffoldFile struct {
	path    string
	content string
}

// runInit implements the `kiln init` subcommand.
// It scaffolds a complete .kiln/ directory structure and supporting files.
func runInit(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	profile := fs.String("profile", "generic", "project profile: go, python, node, generic")
	force := fs.Bool("force", false, "overwrite existing files")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !validProfiles[*profile] {
		return fmt.Errorf("invalid profile %q: must be one of go, python, node, generic", *profile)
	}

	// Determine base directory (current working directory).
	baseDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to determine working directory: %w", err)
	}

	return runInitInDir(baseDir, *profile, *force, stdout)
}

// runInitInDir performs the actual scaffolding in the given base directory.
// Separated for testability.
func runInitInDir(baseDir, profile string, force bool, stdout io.Writer) error {
	// Directories to create.
	dirs := []string{
		filepath.Join(baseDir, ".kiln"),
		filepath.Join(baseDir, ".kiln", "prompts", "tasks"),
		filepath.Join(baseDir, ".kiln", "logs"),
		filepath.Join(baseDir, ".kiln", "done"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", d, err)
		}
	}

	// Files to scaffold.
	files := []initScaffoldFile{
		{
			path:    filepath.Join(baseDir, ".kiln", "tasks.yaml"),
			content: initTasksYAML(),
		},
		{
			path:    filepath.Join(baseDir, ".kiln", "prompts", "tasks", "hello-world.md"),
			content: initHelloWorldPrompt(profile),
		},
		{
			path:    filepath.Join(baseDir, ".kiln", "prompts", "tasks", "follow-up.md"),
			content: initFollowUpPrompt(profile),
		},
		{
			path:    filepath.Join(baseDir, "Makefile"),
			content: initMakefile(profile),
		},
		{
			path:    filepath.Join(baseDir, "PRD.md"),
			content: initPRD(),
		},
	}

	var created, skipped []string
	for _, f := range files {
		if !force {
			if _, err := os.Stat(f.path); err == nil {
				skipped = append(skipped, f.path)
				continue
			}
		}
		if err := os.WriteFile(f.path, []byte(f.content), 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", f.path, err)
		}
		created = append(created, f.path)
	}

	// Print summary.
	for _, p := range created {
		fmt.Fprintf(stdout, "created: %s\n", p)
	}
	for _, p := range skipped {
		fmt.Fprintf(stdout, "skipped: %s (already exists; use --force to overwrite)\n", p)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Next steps:")
	fmt.Fprintln(stdout, "  1. Edit PRD.md to describe your project goals and tasks.")
	fmt.Fprintln(stdout, "  2. Run `make plan` to generate .kiln/tasks.yaml from your PRD.")
	fmt.Fprintln(stdout, "  3. Run `make all` to execute all tasks.")

	return nil
}

// initTasksYAML returns the starter tasks.yaml content.
func initTasksYAML() string {
	return `# tasks.yaml — Kiln task graph
#
# Each task has the following fields:
#   id:      (required) Unique kebab-case identifier for the task.
#   prompt:  (required) Path to the prompt file for this task.
#   needs:   (optional) List of task IDs that must complete before this task runs.
#   timeout: (optional) Maximum duration for the task (e.g. "15m", "1h"). Default: 15m.
#
# Tasks are executed by kiln exec and orchestrated by Make via kiln gen-make.

- id: hello-world
  prompt: .kiln/prompts/tasks/hello-world.md
  needs: []

- id: follow-up
  prompt: .kiln/prompts/tasks/follow-up.md
  needs:
    - hello-world
`
}

// initHelloWorldPrompt returns the starter hello-world prompt content for the given profile.
func initHelloWorldPrompt(profile string) string {
	var langInstruction string
	switch profile {
	case "go":
		langInstruction = "Create a file named `hello.go` in the project root that prints \"Hello, World!\" when run with `go run hello.go`."
	case "python":
		langInstruction = "Create a file named `hello.py` in the project root that prints \"Hello, World!\" when run with `python hello.py`."
	case "node":
		langInstruction = "Create a file named `hello.js` in the project root that prints \"Hello, World!\" when run with `node hello.js`."
	default:
		langInstruction = "Create a file named `hello.txt` in the project root containing the text \"Hello, World!\"."
	}

	return fmt.Sprintf(`# Task: hello-world

%s

When you are done, output the following JSON footer as the very last line of your response:

{"kiln":{"status":"complete","task_id":"hello-world"}}

If you cannot complete the task, use:

{"kiln":{"status":"not_complete","task_id":"hello-world"}}
`, langInstruction)
}

// initFollowUpPrompt returns the starter follow-up prompt content for the given profile.
func initFollowUpPrompt(profile string) string {
	var fileName string
	switch profile {
	case "go":
		fileName = "hello.go"
	case "python":
		fileName = "hello.py"
	case "node":
		fileName = "hello.js"
	default:
		fileName = "hello.txt"
	}

	return fmt.Sprintf(`# Task: follow-up

Verify that the file %s exists in the project root.
If it exists, add a comment at the top of the file explaining what it does.
If it does not exist, report that it is missing.

When you are done, output the following JSON footer as the very last line of your response:

{"kiln":{"status":"complete","task_id":"follow-up"}}

If you cannot complete the task, use:

{"kiln":{"status":"not_complete","task_id":"follow-up"}}
`, fileName)
}

// initMakefile returns the starter Makefile content for the given profile.
func initMakefile(profile string) string {
	var profileTargets string
	switch profile {
	case "go":
		profileTargets = `
build:
	go build ./...

test:
	go test ./...
`
	case "python":
		profileTargets = `
test:
	pytest
`
	case "node":
		profileTargets = `
test:
	npm test
`
	default:
		profileTargets = ""
	}

	return fmt.Sprintf(`# Makefile — Kiln project
#
# Workflow:
#   make plan   — (manual step) Edit PRD.md then run kiln/claude to generate tasks.yaml
#   make graph  — Generate .kiln/targets.mk from .kiln/tasks.yaml
#   make all    — Run all tasks in the dependency graph

-include .kiln/targets.mk

.PHONY: plan graph all

# plan: Placeholder — edit PRD.md and use your preferred method to generate .kiln/tasks.yaml.
plan:
	@echo "Edit PRD.md to describe your project, then run kiln gen-make to generate targets."

# graph: Regenerate Make targets from .kiln/tasks.yaml.
graph:
	kiln gen-make

# all: Run graph first, then execute all generated targets.
all: graph
	$(MAKE) -f .kiln/targets.mk
%s`, profileTargets)
}

// initPRD returns the starter PRD.md content.
func initPRD() string {
	return `# Project Requirements Document (PRD)

## Project Name

<!-- Replace with your project name -->
My Project

## Goals

<!-- Describe what this project aims to accomplish -->
- Goal 1: ...
- Goal 2: ...
- Goal 3: ...

## Tasks

<!-- Describe the discrete tasks that need to be implemented.
     Each task here should correspond to an entry in .kiln/tasks.yaml.
     Tasks can depend on each other — list dependencies explicitly. -->

### Task: hello-world
- **Goal:** Create a hello-world entry point for the project.
- **Depends on:** (none)

### Task: follow-up
- **Goal:** Verify and annotate the hello-world file.
- **Depends on:** hello-world

## Acceptance Criteria

<!-- List the conditions that must be true for this project to be considered complete -->
- [ ] All tasks complete successfully
- [ ] All verify gates pass
`
}

// =============================================================================
// --- profile subcommand ---
// =============================================================================

// runProfile implements the `kiln profile` subcommand.
// It prints the active profile name and its resolved settings (after overrides).
func runProfile(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("profile", flag.ContinueOnError)
	profileFlag := fs.String("profile", "", "workflow profile override: speed or reliable")
	configFlag := fs.String("config", ".kiln/config.yaml", "path to config.yaml")

	if err := fs.Parse(args); err != nil {
		return err
	}

	p, err := loadProfile(*configFlag, *profileFlag)
	if err != nil {
		return err
	}

	// Determine the active profile name for display.
	profileName := *profileFlag
	if profileName == "" {
		cfg, cfgErr := loadProjectConfig(*configFlag)
		if cfgErr == nil && cfg.Profile != "" {
			profileName = cfg.Profile
		} else {
			profileName = WorkflowProfileSpeed
		}
	}

	fmt.Fprintf(stdout, "profile: %s\n", profileName)
	fmt.Fprintf(stdout, "require_unify: %v\n", p.RequireUnify)
	fmt.Fprintf(stdout, "require_verify_gates: %v\n", p.RequireVerifyGates)
	fmt.Fprintf(stdout, "parallelism_limit: %d\n", p.ParallelismLimit)
	fmt.Fprintf(stdout, "retry_max: %d\n", p.RetryMax)
	fmt.Fprintf(stdout, "retry_backoff_base: %s\n", p.RetryBackoffBase)
	return nil
}
