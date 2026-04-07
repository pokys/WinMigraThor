package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/pokys/winmigrathor/internal/config"
	"github.com/pokys/winmigrathor/internal/jobs"
	"github.com/pokys/winmigrathor/internal/logging"
	"github.com/pokys/winmigrathor/internal/meta"
)

// BackupOptions are passed from CLI/TUI to the backup command.
type BackupOptions struct {
	Target           string
	Users            []string // user paths to back up
	JobNames         []string // e.g. ["userdata","browsers","wifi"]
	SelectedFolders  []string // subset of standard folders for userdata job (nil = all)
	SelectedBrowsers []string // subset of browsers (nil = all)
	DryRun           bool
	Compress         bool
	PasswordMode     string
	ConflictStrategy string
}

// BackupResult holds the aggregate result.
type BackupResult struct {
	Results  []jobs.Result
	Duration time.Duration
	LogDir   string
	Error    error
}

// RunBackup performs the backup operation.
// The caller is responsible for creating progressCh; RunBackup closes it when done.
func RunBackup(opts BackupOptions, allJobs []jobs.Job, progressCh chan jobs.Progress) (*BackupResult, error) {
	if progressCh != nil {
		defer close(progressCh)
	}

	start := time.Now()

	// Setup log dir inside target
	logDir := filepath.Join(opts.Target, "logs")
	logger, err := logging.Setup(logDir)
	if err != nil {
		return nil, fmt.Errorf("setup logging: %w", err)
	}
	defer logger.Close()

	log := logger.Main
	log.Info("backup started", "target", opts.Target, "users", opts.Users, "jobs", opts.JobNames)

	// Create metadata
	hostname, _ := os.Hostname()
	m := meta.New(hostname, "", "")
	m.Users = opts.Users

	var allResults []jobs.Result

	// Filter jobs by name
	activeJobs := filterJobs(allJobs, opts.JobNames)

	jobOpts := jobs.Options{
		DryRun:           opts.DryRun,
		PasswordMode:     opts.PasswordMode,
		ConflictStrategy: opts.ConflictStrategy,
		Compress:         opts.Compress,
		LogDir:           logDir,
		ProgressCh:       progressCh,
		SelectedFolders:  opts.SelectedFolders,
		SelectedBrowsers: opts.SelectedBrowsers,
	}

	for _, userPath := range opts.Users {
		username := filepath.Base(userPath)
		log.Info("processing user", "user", username)

		for _, j := range activeJobs {
			log.Info("running job", "job", j.Name(), "user", username)

			// Notify UI that this job is starting
			if progressCh != nil {
				progressCh <- jobs.Progress{JobName: j.Name(), Current: 0, Total: 1}
			}

			result, err := j.Backup(userPath, opts.Target, jobOpts)
			if err != nil {
				log.Error("job failed", "job", j.Name(), "error", err)
				if result.Status == "" {
					result.Status = "error"
					result.Errors = append(result.Errors, err.Error())
				}
			}

			allResults = append(allResults, result)

			for _, w := range result.Warnings {
				log.Warn("job warning", "job", j.Name(), "warning", w)
			}
			for _, e := range result.Errors {
				log.Error("job error", "job", j.Name(), "error", e)
			}

			// Update metadata
			m.Jobs = append(m.Jobs, meta.JobMeta{
				Name:       result.JobName,
				Status:     result.Status,
				SizeBytes:  result.SizeBytes,
				FilesCount: result.FilesCount,
				Warnings:   len(result.Warnings),
				Errors:     len(result.Errors),
				Duration:   result.Duration,
			})
		}
	}

	duration := time.Since(start)
	m.Duration = duration.Round(time.Second).String()

	// Calculate total size
	for _, r := range allResults {
		m.TotalSize += r.SizeBytes
	}

	// Save metadata
	if err := meta.Save(m, opts.Target); err != nil {
		log.Error("save metadata", "error", err)
	}

	// Save config
	cfg := config.Default()
	cfg.Users = userPathsToNames(opts.Users)
	cfg.Jobs = opts.JobNames
	if err := config.Save(cfg, opts.Target); err != nil {
		log.Error("save config", "error", err)
	}

	log.Info("backup complete", "duration", duration, "total_size", m.TotalSize)

	return &BackupResult{
		Results:  allResults,
		Duration: duration,
		LogDir:   logDir,
	}, nil
}

func filterJobs(all []jobs.Job, names []string) []jobs.Job {
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	var filtered []jobs.Job
	for _, j := range all {
		if nameSet[j.Name()] {
			filtered = append(filtered, j)
		}
	}
	return filtered
}

func userPathsToNames(paths []string) []string {
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
	}
	return names
}

// ScanJobs runs Scan on each job for the given user and returns a summary.
func ScanJobs(allJobs []jobs.Job, userPath string) map[string]jobs.ScanResult {
	results := make(map[string]jobs.ScanResult)
	for _, j := range allJobs {
		sr, err := j.Scan(userPath)
		if err != nil {
			slog.Warn("scan failed", "job", j.Name(), "error", err)
			continue
		}
		results[j.Name()] = sr
	}
	return results
}
