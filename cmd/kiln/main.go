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
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

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
type footerError struct{ msg string }

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

// execRunLog is the structured JSON log written for each kiln exec attempt.
type execRunLog struct {
	TaskID      string              `json:"task_id"`
	StartedAt   time.Time           `json:"started_at"`
	EndedAt     time.Time           `json:"ended_at"`
	DurationMs  int64               `json:"duration_ms"`
	Model       string              `json:"model"`
	PromptFile  string              `json:"prompt_file"`
	ExitCode    int                 `json:"exit_code"`
	Status      string              `json:"status"` // "complete","not_complete","blocked","timeout","error"
	Footer      *kilnFooterEnvelope `json:"footer,omitempty"`
	FooterValid bool                `json:"footer_valid"`
	Events      []logEvent          `json:"events"`
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
		if err := runGenMake(args[1:]); err != nil {
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
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 1
	}
}

// Task represents a single entry in the tasks.yaml file.
type Task struct {
	ID          string            `yaml:"id"`
	Prompt      string            `yaml:"prompt"`
	Needs       []string          `yaml:"needs"`
	Timeout     string            `yaml:"timeout,omitempty"`
	Model       string            `yaml:"model,omitempty"`
	Description string            `yaml:"description,omitempty"`
	Kind        string            `yaml:"kind,omitempty"`
	Tags        []string          `yaml:"tags,omitempty"`
	Retries     int               `yaml:"retries,omitempty"`
	Validation  []string          `yaml:"validation,omitempty"`
	Engine      string            `yaml:"engine,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	DevPhase    int               `yaml:"dev-phase,omitempty"`
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
	timeoutStr := fs.String("timeout", "15m", "maximum duration for the claude invocation")

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

func runGenMake(args []string) error {
	fs := flag.NewFlagSet("gen-make", flag.ContinueOnError)
	tasksFile := fs.String("tasks", "", "path to tasks.yaml")
	outFile := fs.String("out", "", "output path for targets.mk")
	devPhase := fs.Int("dev-phase", 0, "filter tasks to a specific dev-phase (0 = all)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *tasksFile == "" {
		return fmt.Errorf("--tasks is required")
	}
	if *outFile == "" {
		return fmt.Errorf("--out is required")
	}

	tasks, err := loadTasks(*tasksFile)
	if err != nil {
		return err
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

	// Individual targets in definition order
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
		buf.WriteString("\t" + recipe + "\n")
		buf.WriteString("\n")
	}

	if err := os.MkdirAll(filepath.Dir(*outFile), 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if err := os.WriteFile(*outFile, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	return nil
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

// classifyError returns an error classification string for state tracking.
func classifyError(err error) string {
	var te *timeoutError
	if errors.As(err, &te) {
		return "timeout"
	}
	var ce *claudeExitError
	if errors.As(err, &ce) {
		return "claude_exit"
	}
	var fe *footerError
	if errors.As(err, &fe) {
		return "footer_invalid"
	}
	return "permanent"
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

func runStatus(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
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

	// Build done set by checking for done markers
	doneSet := make(map[string]bool)
	for _, t := range tasks {
		donePath := filepath.Join(".kiln", "done", t.ID+".done")
		if _, err := os.Stat(donePath); err == nil {
			doneSet[t.ID] = true
		}
	}

	// Load state.json for additional per-task info (attempts, last error).
	// Ignore errors — treat as empty state.
	state, _ := loadState(".kiln/state.json")
	if state == nil {
		state = &StateManifest{Tasks: make(map[string]*TaskState)}
	}

	// Classify and print
	var doneCount, runnableCount int
	fmt.Fprintf(stdout, "%-30s %-10s %-8s %-40s %s\n", "TASK", "STATUS", "ATTEMPTS", "LAST ERROR", "NEEDS")
	fmt.Fprintf(stdout, "%-30s %-10s %-8s %-40s %s\n", "----", "------", "--------", "----------", "-----")

	for _, t := range tasks {
		var status string
		if doneSet[t.ID] {
			status = "done"
			doneCount++
		} else if hasUnfinishedDeps(t.Needs, doneSet) {
			status = "blocked"
		} else {
			status = "runnable"
			runnableCount++
		}
		needs := strings.Join(t.Needs, ", ")
		if needs == "" {
			needs = "-"
		}
		var attempts int
		lastErr := "-"
		if ts := state.Tasks[t.ID]; ts != nil {
			attempts = ts.Attempts
			if ts.LastError != "" {
				lastErr = ts.LastError
				if len(lastErr) > 38 {
					lastErr = lastErr[:37] + "…"
				}
			}
		}
		fmt.Fprintf(stdout, "%-30s %-10s %-8d %-40s %s\n", t.ID, status, attempts, lastErr, needs)
	}

	fmt.Fprintf(stdout, "\n%d/%d tasks done, %d runnable\n", doneCount, len(tasks), runnableCount)
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

func runGenPrompts(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("gen-prompts", flag.ContinueOnError)
	tasksFile := fs.String("tasks", ".kiln/tasks.yaml", "path to tasks.yaml")
	prdFile := fs.String("prd", "PRD.md", "path to PRD file")
	templateFile := fs.String("template", ".kiln/templates/<id>.md", "path to prompt template")
	modelFlag := fs.String("model", "", "model override")
	timeoutStr := fs.String("timeout", "15m", "timeout per Claude invocation")
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

func runExec(args []string, stdout io.Writer) (int, error) {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)

	taskID := fs.String("task-id", "", "task identifier")
	promptFile := fs.String("prompt-file", "", "path to prompt file")
	tasksFlag := fs.String("tasks", ".kiln/tasks.yaml", "path to tasks.yaml (resolves prompt and model from task definition)")
	modelFlag := fs.String("model", "", "claude model to use (overrides KILN_MODEL env var)")
	timeoutStr := fs.String("timeout", "15m", "maximum duration for the claude invocation")
	retriesFlag := fs.Int("retries", 0, "number of additional attempts on retryable failures")
	retryBackoffStr := fs.String("retry-backoff", "0s", "sleep duration between retry attempts")
	backoffFlag := fs.String("backoff", "fixed", "backoff strategy between retries: fixed or exponential")

	if err := fs.Parse(args); err != nil {
		return 0, err
	}

	if *taskID == "" {
		return 0, fmt.Errorf("--task-id is required")
	}

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

	retryBackoff, err := time.ParseDuration(*retryBackoffStr)
	if err != nil {
		return 0, fmt.Errorf("invalid --retry-backoff value: %w", err)
	}

	if *backoffFlag != "fixed" && *backoffFlag != "exponential" {
		return 0, fmt.Errorf("invalid --backoff value %q: must be fixed or exponential", *backoffFlag)
	}

	data, err := os.ReadFile(*promptFile)
	if err != nil {
		return 0, fmt.Errorf("failed to read prompt file: %w", err)
	}
	prompt := string(data)

	model := resolveModel(*modelFlag, taskModel)
	retries := *retriesFlag
	if taskRetries > 0 {
		retries = taskRetries
	}
	maxAttempts := 1 + retries
	logDir := ".kiln/logs"
	stateFile := ".kiln/state.json"

	// Load state before execution; ignore errors (treat as empty state).
	state, stateErr := loadState(stateFile)
	if stateErr != nil {
		fmt.Fprintf(stdout, "warning: failed to load state: %v\n", stateErr)
		state = &StateManifest{Tasks: make(map[string]*TaskState)}
	}
	if state.Tasks[*taskID] == nil {
		state.Tasks[*taskID] = &TaskState{Status: "pending"}
	}
	ts := state.Tasks[*taskID]

	var lastErr error
	var lastLog execRunLog

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Update state to "running" before each attempt.
		ts.Status = "running"
		ts.Attempts++
		ts.LastAttemptAt = time.Now()
		if saveErr := saveState(stateFile, state); saveErr != nil {
			fmt.Fprintf(stdout, "warning: failed to save state: %v\n", saveErr)
		}

		log, err := execOnce(*taskID, prompt, model, *promptFile, timeout, stdout, taskEnv)

		// Always write the log, even on error.
		if writeErr := writeExecLog(logDir, *taskID, log); writeErr != nil {
			fmt.Fprintf(stdout, "warning: failed to write exec log: %v\n", writeErr)
		}

		if err == nil {
			if log.FooterValid {
				// Task completed successfully.
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
					fmt.Fprintf(stdout, "warning: failed to save state: %v\n", saveErr)
				}
				doneDir := ".kiln/done"
				if mkErr := os.MkdirAll(doneDir, 0o755); mkErr == nil {
					donePath := filepath.Join(doneDir, *taskID+".done")
					os.WriteFile(donePath, nil, 0o644)
				}
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
				fmt.Fprintf(stdout, "warning: failed to save state: %v\n", saveErr)
			}
			return 0, nil
		}

		lastErr = err
		lastLog = log

		// Update state to "failed" after an error attempt.
		ts.Status = "failed"
		ts.LastError = err.Error()
		ts.LastErrorClass = classifyError(err)
		if saveErr := saveState(stateFile, state); saveErr != nil {
			fmt.Fprintf(stdout, "warning: failed to save state: %v\n", saveErr)
		}

		if !isRetryable(err) {
			return lastLog.ExitCode, lastErr
		}

		if attempt < maxAttempts && retryBackoff > 0 {
			sleepFn(computeBackoff(*backoffFlag, retryBackoff, attempt))
		}
	}

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
			return entry, &timeoutError{taskID: taskID, timeout: timeout}
		}
		entry.Status = "error"
		entry.ExitCode = 1
		return entry, &claudeExitError{err: runErr}
	}

	footerStatus, footerTaskID, ok := parseFooter(capturedBuf.String())
	if !ok {
		entry.Status = "error"
		entry.ExitCode = 10
		return entry, &footerError{msg: fmt.Sprintf(
			"missing or invalid footer in claude output\n"+
				`expected format: {"kiln":{"status":"complete|not_complete|blocked","task_id":"%s"}}`,
			taskID,
		)}
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
		return entry, &footerError{msg: fmt.Sprintf(
			"invalid footer status %q; expected complete, not_complete, or blocked", footerStatus,
		)}
	}
}
