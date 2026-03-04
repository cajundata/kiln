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
	"strings"
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

// loadTasks reads and parses a tasks.yaml file, returning the task list.
func loadTasks(path string) ([]Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read tasks file: %w", err)
	}

	var tasks []Task
	if err := yaml.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("failed to parse tasks file: %w", err)
	}

	if len(tasks) == 0 {
		return nil, fmt.Errorf("no tasks found in %s", path)
	}

	return tasks, nil
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

	// Ensure log directory exists
	logDir := ".kiln/logs"
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return 0, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file
	logPath := filepath.Join(logDir, *taskID+".json")
	logFile, err := os.Create(logPath)
	if err != nil {
		return 0, fmt.Errorf("failed to create log file %s: %w", logPath, err)
	}
	defer logFile.Close()

	model := resolveModel(*modelFlag, taskModel)
	maxAttempts := 1 + *retriesFlag

	var lastErr error
	var lastCode int

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Write per-attempt header to log
		attemptEntry := map[string]interface{}{
			"type":    "kiln_attempt",
			"attempt": attempt,
			"of":      maxAttempts,
			"task_id": *taskID,
		}
		if b, jsonErr := json.Marshal(attemptEntry); jsonErr == nil {
			fmt.Fprintf(logFile, "%s\n", b)
		}

		code, err := execOnce(*taskID, prompt, model, timeout, logFile, stdout)
		if err == nil {
			// Write done marker only on complete (code 2)
			if code == 2 {
				doneDir := ".kiln/done"
				if mkErr := os.MkdirAll(doneDir, 0o755); mkErr == nil {
					donePath := filepath.Join(doneDir, *taskID+".done")
					os.WriteFile(donePath, nil, 0o644)
				}
			}
			return code, nil
		}

		lastErr = err
		lastCode = code

		if !isRetryable(err) {
			return lastCode, lastErr
		}

		if attempt < maxAttempts && retryBackoff > 0 {
			sleepFn(retryBackoff)
		}
	}

	return lastCode, lastErr
}

// execOnce runs a single claude invocation attempt and returns its exit code and any error.
func execOnce(taskID, prompt, model string, timeout time.Duration, logFile io.Writer, stdout io.Writer) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := commandBuilder(ctx, prompt, model)

	var captured bytes.Buffer
	out := io.MultiWriter(stdout, logFile, &captured)
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			entry := map[string]string{
				"type":    "kiln_timeout",
				"task_id": taskID,
				"message": fmt.Sprintf("process killed after %s timeout", timeout),
			}
			if b, jsonErr := json.Marshal(entry); jsonErr == nil {
				fmt.Fprintf(logFile, "\n%s\n", b)
			}
			return 0, &timeoutError{taskID: taskID, timeout: timeout}
		}
		return 0, &claudeExitError{err: err}
	}

	// Parse the kiln JSON footer from the captured output.
	status, footerTaskID, ok := parseFooter(captured.String())
	if !ok {
		return 0, &footerError{msg: fmt.Sprintf(
			"missing or invalid footer in claude output\n"+
				`expected format: {"kiln":{"status":"complete|not_complete|blocked","task_id":"%s"}}`,
			taskID,
		)}
	}

	if footerTaskID != taskID {
		fmt.Fprintf(stdout, "warning: footer task_id %q does not match expected %q\n", footerTaskID, taskID)
	}

	switch status {
	case "complete":
		return 2, nil
	case "not_complete", "blocked":
		return 0, nil
	default:
		return 0, &footerError{msg: fmt.Sprintf(
			"invalid footer status %q; expected complete, not_complete, or blocked", status,
		)}
	}
}
