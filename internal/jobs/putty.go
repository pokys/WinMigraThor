//go:build windows

package jobs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/registry"
)

// PuTTYJob backs up and restores PuTTY session definitions and host keys
// from HKCU\Software\SimonTatham\PuTTY. Everything PuTTY needs is in this
// one registry subtree — no DPAPI, no DLLs, no service state.
type PuTTYJob struct{}

const puttyRegPath = `Software\SimonTatham\PuTTY`

func (j *PuTTYJob) Name() string        { return "putty" }
func (j *PuTTYJob) Description() string { return "PuTTY sessions and SSH host keys" }

func (j *PuTTYJob) Scan(userPath string) (ScanResult, error) {
	// We can only check the live HKCU here — registry hives of other users
	// would need explicit hive loading. Backup runs as the current user, so
	// this is the right scope.
	k, err := registry.OpenKey(registry.CURRENT_USER, puttyRegPath, registry.QUERY_VALUE)
	if err != nil {
		return ScanResult{}, nil
	}
	k.Close()

	sessions := countPuttySubkeys(`Software\SimonTatham\PuTTY\Sessions`)
	hostKeys := countPuttySubkeys(`Software\SimonTatham\PuTTY\SshHostKeys`)

	return ScanResult{
		Items: []ScanItem{
			{
				Label:    "PuTTY",
				Details:  fmt.Sprintf("%d sessions, %d host keys", sessions, hostKeys),
				Selected: false,
			},
		},
		TotalSizeBytes: 4096,
	}, nil
}

func (j *PuTTYJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	k, err := registry.OpenKey(registry.CURRENT_USER, puttyRegPath, registry.QUERY_VALUE)
	if err != nil {
		result.Status = "skipped"
		return result, nil
	}
	k.Close()

	dstDir := filepath.Join(target, "putty")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	if opts.DryRun {
		result.Warnings = append(result.Warnings, "[dry-run] would export HKCU\\Software\\SimonTatham\\PuTTY")
		result.Status = "success"
		if opts.ProgressCh != nil {
			opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
		}
		return result, nil
	}

	regFile := filepath.Join(dstDir, "putty.reg")
	cmd := exec.Command("reg.exe", "export", `HKCU\`+puttyRegPath, regFile, "/y")
	if out, err := cmd.CombinedOutput(); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, fmt.Sprintf("reg export: %v (%s)", err, out))
		return result, err
	}

	if info, err := os.Stat(regFile); err == nil {
		result.SizeBytes = info.Size()
		result.FilesCount = 1
	}

	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

func (j *PuTTYJob) Restore(source, userPath string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	regFile := filepath.Join(source, "putty", "putty.reg")
	if _, err := os.Stat(regFile); os.IsNotExist(err) {
		result.Status = "skipped"
		return result, nil
	}

	if opts.DryRun {
		result.Warnings = append(result.Warnings, "[dry-run] would import "+regFile)
		result.Status = "success"
		if opts.ProgressCh != nil {
			opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
		}
		return result, nil
	}

	cmd := exec.Command("reg.exe", "import", regFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, fmt.Sprintf("reg import: %v (%s)", err, out))
		return result, err
	}

	if info, err := os.Stat(regFile); err == nil {
		result.SizeBytes = info.Size()
		result.FilesCount = 1
	}

	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

func countPuttySubkeys(path string) int {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return 0
	}
	defer k.Close()
	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return 0
	}
	return len(names)
}
