package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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
func parseFooter(output string) (status, taskID string, ok bool) {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.Contains(line, `"kiln"`) {
			continue
		}
		var env kilnFooterEnvelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		if env.Kiln.Status != "" {
			return env.Kiln.Status, env.Kiln.TaskID, true
		}
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
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 1
	}
}

// Task represents a single entry in the tasks.yaml file.
type Task struct {
	ID      string   `yaml:"id"`
	Prompt  string   `yaml:"prompt"`
	Needs   []string `yaml:"needs"`
	Timeout string   `yaml:"timeout,omitempty"`
	Model   string   `yaml:"model,omitempty"`
}

// taskIDRegexp is the valid pattern for task IDs (kebab-case).
var taskIDRegexp = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

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

func runGenMake(args []string) error {
	fs := flag.NewFlagSet("gen-make", flag.ContinueOnError)
	tasksFile := fs.String("tasks", "", "path to tasks.yaml")
	outFile := fs.String("out", "", "output path for targets.mk")

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
		recipe := "$(KILN) exec --task-id " + t.ID + " --tasks " + *tasksFile
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

	// Classify and print
	var doneCount, runnableCount int
	fmt.Fprintf(stdout, "%-30s %-10s %s\n", "TASK", "STATUS", "NEEDS")
	fmt.Fprintf(stdout, "%-30s %-10s %s\n", "----", "------", "-----")

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
		fmt.Fprintf(stdout, "%-30s %-10s %s\n", t.ID, status, needs)
	}

	fmt.Fprintf(stdout, "\n%d/%d tasks done, %d runnable\n", doneCount, len(tasks), runnableCount)
	return nil
}

// defaultModel is the built-in fallback when neither --model nor KILN_MODEL is set.
const defaultModel = "claude-sonnet-4-6"

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

func runExec(args []string, stdout io.Writer) (int, error) {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)

	taskID := fs.String("task-id", "", "task identifier")
	promptFile := fs.String("prompt-file", "", "path to prompt file")
	tasksFlag := fs.String("tasks", "", "path to tasks.yaml (resolves prompt and model from task definition)")
	modelFlag := fs.String("model", "", "claude model to use (overrides KILN_MODEL env var)")
	timeoutStr := fs.String("timeout", "15m", "maximum duration for the claude invocation")
	retriesFlag := fs.Int("retries", 0, "number of additional attempts on retryable failures")
	retryBackoffStr := fs.String("retry-backoff", "0s", "sleep duration between retry attempts")

	if err := fs.Parse(args); err != nil {
		return 0, err
	}

	if *taskID == "" {
		return 0, fmt.Errorf("--task-id is required")
	}

	// Resolve prompt and model from tasks.yaml when --tasks is provided
	var taskModel string
	if *tasksFlag != "" {
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
		if *promptFile == "" {
			if found.Prompt == "" {
				return 0, fmt.Errorf("task %q has no prompt field", *taskID)
			}
			*promptFile = found.Prompt
		}
	}

	if *promptFile == "" {
		return 0, fmt.Errorf("no prompt file: provide --prompt-file or --tasks to resolve from tasks.yaml")
	}

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil {
		return 0, fmt.Errorf("invalid --timeout value: %w", err)
	}

	retryBackoff, err := time.ParseDuration(*retryBackoffStr)
	if err != nil {
		return 0, fmt.Errorf("invalid --retry-backoff value: %w", err)
	}

	data, err := os.ReadFile(*promptFile)
	if err != nil {
		return 0, fmt.Errorf("failed to read prompt file: %w", err)
	}
	prompt := string(data)

	model := resolveModel(*modelFlag, taskModel)
	maxAttempts := 1 + *retriesFlag
	logDir := ".kiln/logs"

	var lastErr error
	var lastLog execRunLog

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		log, err := execOnce(*taskID, prompt, model, *promptFile, timeout, stdout)

		// Always write the log, even on error.
		if writeErr := writeExecLog(logDir, *taskID, log); writeErr != nil {
			fmt.Fprintf(stdout, "warning: failed to write exec log: %v\n", writeErr)
		}

		if err == nil {
			if log.FooterValid {
				doneDir := ".kiln/done"
				if mkErr := os.MkdirAll(doneDir, 0o755); mkErr == nil {
					donePath := filepath.Join(doneDir, *taskID+".done")
					os.WriteFile(donePath, nil, 0o644)
				}
				return 2, nil
			}
			return 0, nil
		}

		lastErr = err
		lastLog = log

		if !isRetryable(err) {
			return lastLog.ExitCode, lastErr
		}

		if attempt < maxAttempts && retryBackoff > 0 {
			sleepFn(retryBackoff)
		}
	}

	return lastLog.ExitCode, lastErr
}

// execOnce runs a single claude invocation attempt and returns a structured log entry and any error.
func execOnce(taskID, prompt, model, promptFile string, timeout time.Duration, stdout io.Writer) (execRunLog, error) {
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
