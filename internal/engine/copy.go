//go:build windows

package engine

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ExcludeDirs are directories always excluded from robocopy.
var ExcludeDirs = []string{
	"Cache",
	"Code Cache",
	"GPUCache",
	"Temp",
	"node_modules",
	"__pycache__",
	"ShaderCache",
	"Service Worker",
	".cache",
}

// CopyOptions configures a robocopy operation.
type CopyOptions struct {
	Source       string
	Destination  string
	LogFile      string
	ExtraFlags   []string
	ExcludeFiles []string
	ProgressCh   chan<- CopyProgress
}

// CopyProgress is sent during a copy operation.
type CopyProgress struct {
	CurrentFile string
	BytesCopied int64
	Done        bool
	Err         error
	Warning     string
	ExitCode    int
}

// RobocopyResult holds the outcome of a robocopy operation.
type RobocopyResult struct {
	ExitCode    int
	BytesCopied int64
	FilesCopied int
	Warnings    []string
	Duration    time.Duration
}

// Copy runs robocopy from src to dst with the standard flags.
func Copy(opts CopyOptions) (RobocopyResult, error) {
	start := time.Now()

	if err := os.MkdirAll(opts.Destination, 0o755); err != nil {
		return RobocopyResult{}, fmt.Errorf("create destination: %w", err)
	}

	args := buildArgs(opts)
	cmd := exec.Command("robocopy.exe", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return RobocopyResult{}, err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return RobocopyResult{}, fmt.Errorf("start robocopy: %w", err)
	}

	var result RobocopyResult
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		parseRobocopyLine(line, &result, opts.ProgressCh)
	}

	err = cmd.Wait()
	result.Duration = time.Since(start)

	// Robocopy exit codes: 0-7 = success/informational, >=8 = error
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		return result, fmt.Errorf("robocopy: %w", err)
	}
	result.ExitCode = exitCode

	if exitCode >= 8 {
		return result, fmt.Errorf("robocopy failed with exit code %d", exitCode)
	}

	if opts.ProgressCh != nil {
		opts.ProgressCh <- CopyProgress{Done: true, ExitCode: exitCode}
	}

	return result, nil
}

func buildArgs(opts CopyOptions) []string {
	args := []string{
		opts.Source,
		opts.Destination,
		"/E",    // include subdirectories including empty
		"/Z",    // restartable mode
		"/R:3",  // 3 retries
		"/W:5",  // 5 second wait between retries
		"/MT:16",// 16 threads
		"/NP",   // no percentage progress in output
		"/NDL",  // no directory list
		"/NFL",  // no file list (we parse our own)
		"/256",  // long path support
	}

	// Add log file
	if opts.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(opts.LogFile), 0o755); err == nil {
			args = append(args, "/LOG+:"+opts.LogFile)
		}
	}

	// Exclude dirs
	if len(ExcludeDirs) > 0 {
		args = append(args, "/XD")
		args = append(args, ExcludeDirs...)
	}

	// Exclude files
	if len(opts.ExcludeFiles) > 0 {
		args = append(args, "/XF")
		args = append(args, opts.ExcludeFiles...)
	}

	// Extra flags
	args = append(args, opts.ExtraFlags...)

	return args
}

func parseRobocopyLine(line string, result *RobocopyResult, ch chan<- CopyProgress) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	// Look for byte count lines
	if strings.Contains(line, "Bytes :") {
		parts := strings.Fields(line)
		for i, p := range parts {
			if p == ":" && i+1 < len(parts) {
				if n, err := strconv.ParseInt(parts[i+1], 10, 64); err == nil {
					result.BytesCopied += n
				}
			}
		}
	}

	// Look for file count
	if strings.Contains(line, "Files :") {
		parts := strings.Fields(line)
		for i, p := range parts {
			if p == ":" && i+1 < len(parts) {
				if n, err := strconv.Atoi(parts[i+1]); err == nil {
					result.FilesCopied += n
				}
			}
		}
	}

	// Detect warnings
	lower := strings.ToLower(line)
	if strings.Contains(lower, "error") || strings.Contains(lower, "skipped") || strings.Contains(lower, "locked") {
		result.Warnings = append(result.Warnings, line)
		if ch != nil {
			ch <- CopyProgress{Warning: line}
		}
		return
	}

	// Send current file to progress channel
	if ch != nil && !strings.HasPrefix(line, "---") {
		ch <- CopyProgress{CurrentFile: line}
	}
}

// CopyFile copies a single file using robocopy.
func CopyFile(src, dstDir, logFile string) error {
	srcDir := filepath.Dir(src)
	fileName := filepath.Base(src)
	args := []string{
		srcDir, dstDir, fileName,
		"/R:3", "/W:5", "/NP", "/NFL", "/NDL",
	}
	if logFile != "" {
		args = append(args, "/LOG+:"+logFile)
	}
	cmd := exec.Command("robocopy.exe", args...)
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() < 8 {
			return nil
		}
		return fmt.Errorf("robocopy exit code %d copying %s", exitErr.ExitCode(), fileName)
	}
	return err
}
