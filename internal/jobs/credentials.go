//go:build windows

package jobs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CredentialsJob exports a read-only inventory of stored credentials so the
// user knows which apps need a fresh sign-in on the new machine. We do NOT
// copy the encrypted vault data — DPAPI master keys are derived from the
// user's password, SID and machine, so the blobs are unusable on any other
// machine and restoring the Protect\ folder can corrupt the target user's
// credential state.
type CredentialsJob struct{}

func (j *CredentialsJob) Name() string        { return "credentials" }
func (j *CredentialsJob) Description() string { return "Credential Manager inventory (re-login checklist, no secrets)" }

func (j *CredentialsJob) Scan(userPath string) (ScanResult, error) {
	count := 0
	if out, err := exec.Command("cmdkey.exe", "/list").CombinedOutput(); err == nil {
		count = countCmdkeyTargets(string(out))
	}
	if count == 0 {
		return ScanResult{}, nil
	}
	return ScanResult{
		Items: []ScanItem{
			{
				Label:    "Credential inventory",
				Details:  fmt.Sprintf("%d stored credentials (names only, no passwords)", count),
				Selected: false,
			},
		},
		TotalSizeBytes: 4096,
	}, nil
}

func (j *CredentialsJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	dstDir := filepath.Join(target, "credentials")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	if opts.DryRun {
		result.Warnings = append(result.Warnings, "[dry-run] would write credential inventory")
		result.Status = "success"
		if opts.ProgressCh != nil {
			opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
		}
		return result, nil
	}

	out, err := exec.Command("cmdkey.exe", "/list").CombinedOutput()
	if err != nil {
		result.Warnings = append(result.Warnings, "cmdkey /list: "+err.Error())
	}

	listPath := filepath.Join(dstDir, "cmdkey_list.txt")
	if writeErr := os.WriteFile(listPath, out, 0o644); writeErr != nil {
		result.Errors = append(result.Errors, "write inventory: "+writeErr.Error())
	} else {
		result.SizeBytes = int64(len(out))
		result.FilesCount = 1
	}

	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

func (j *CredentialsJob) Restore(source, userPath string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	listPath := filepath.Join(source, "credentials", "cmdkey_list.txt")
	data, err := os.ReadFile(listPath)
	if err != nil {
		result.Status = "skipped"
		return result, nil
	}

	count := countCmdkeyTargets(string(data))
	result.Warnings = append(result.Warnings,
		fmt.Sprintf("Credential Manager has %d entries to re-create. See %s for the list — Windows DPAPI prevents migrating the actual secrets across machines.", count, listPath))
	result.SizeBytes = int64(len(data))
	result.FilesCount = 1
	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = "warning"

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

func countCmdkeyTargets(output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Target:") {
			count++
		}
	}
	return count
}
