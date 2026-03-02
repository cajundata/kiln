package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

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
			return 1
		}
		return 0
	case "gen-make":
		fmt.Fprintln(stderr, "gen-make not implemented yet")
		return 1
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 1
	}
}

// commandBuilder creates an *exec.Cmd for the claude invocation.
// Swappable in tests to avoid calling the real claude binary.
var commandBuilder = func(prompt string) *exec.Cmd {
	return exec.Command("claude", "--output-format", "stream-json", "-p", prompt)
}

func runExec(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)

	taskID := fs.String("task-id", "", "task identifier")
	promptFile := fs.String("prompt-file", "", "path to prompt file")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *taskID == "" {
		return fmt.Errorf("--task-id is required")
	}
	if *promptFile == "" {
		return fmt.Errorf("--prompt-file is required")
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

	// Invoke claude CLI with stream-json output
	cmd := commandBuilder(prompt)

	// Tee combined output to both stdout and the log file
	out := io.MultiWriter(stdout, logFile)
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude invocation failed: %w", err)
	}

	return nil
}