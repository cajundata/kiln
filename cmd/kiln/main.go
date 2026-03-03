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
		if err := runExec(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "exec: %v\n", err)
			var te *timeoutError
			if errors.As(err, &te) {
				return 20
			}
			return 1
		}
		return 0
	case "gen-make":
		if err := runGenMake(args[1:]); err != nil {
			fmt.Fprintf(stderr, "gen-make: %v\n", err)
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
	ID     string   `yaml:"id"`
	Prompt string   `yaml:"prompt"`
	Needs  []string `yaml:"needs"`
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

	data, err := os.ReadFile(*tasksFile)
	if err != nil {
		return fmt.Errorf("failed to read tasks file: %w", err)
	}

	var tasks []Task
	if err := yaml.Unmarshal(data, &tasks); err != nil {
		return fmt.Errorf("failed to parse tasks file: %w", err)
	}

	if len(tasks) == 0 {
		return fmt.Errorf("no tasks found in %s", *tasksFile)
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
		buf.WriteString("\t$(KILN) exec --task-id " + t.ID + " --prompt-file " + t.Prompt + " && touch $@\n")
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

// defaultModel is the built-in fallback when neither --model nor KILN_MODEL is set.
const defaultModel = "claude-3-7-sonnet-latest"

// resolveModel returns the effective model name using the precedence:
// 1. flag value (if non-empty)
// 2. KILN_MODEL env var (if non-empty)
// 3. defaultModel constant
func resolveModel(flagValue string) string {
	if flagValue != "" {
		return flagValue
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

func runExec(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)

	taskID := fs.String("task-id", "", "task identifier")
	promptFile := fs.String("prompt-file", "", "path to prompt file")
	modelFlag := fs.String("model", "", "claude model to use (overrides KILN_MODEL env var)")
	timeoutStr := fs.String("timeout", "5m", "maximum duration for the claude invocation")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *taskID == "" {
		return fmt.Errorf("--task-id is required")
	}
	if *promptFile == "" {
		return fmt.Errorf("--prompt-file is required")
	}

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil {
		return fmt.Errorf("invalid --timeout value: %w", err)
	}

	data, err := os.ReadFile(*promptFile)
	if err != nil {
		return fmt.Errorf("failed to read prompt file: %w", err)
	}
	prompt := string(data)

	// Ensure log directory exists
	logDir := ".kiln/logs"
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file
	logPath := filepath.Join(logDir, *taskID+".json")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("failed to create log file %s: %w", logPath, err)
	}
	defer logFile.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Resolve effective model
	model := resolveModel(*modelFlag)

	// Invoke claude CLI with stream-json output
	cmd := commandBuilder(ctx, prompt, model)

	// Tee combined output to both stdout and the log file
	out := io.MultiWriter(stdout, logFile)
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			// Write timeout entry to log file
			entry := map[string]string{
				"type":    "kiln_timeout",
				"task_id": *taskID,
				"message": fmt.Sprintf("process killed after %s timeout", timeout),
			}
			if jsonData, jsonErr := json.Marshal(entry); jsonErr == nil {
				fmt.Fprintf(logFile, "\n%s\n", jsonData)
			}
			return &timeoutError{taskID: *taskID, timeout: timeout}
		}
		return fmt.Errorf("claude invocation failed: %w", err)
	}

	return nil
}