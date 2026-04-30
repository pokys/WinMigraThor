package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/pokys/winmigrathor/internal/jobs"
	"github.com/pokys/winmigrathor/internal/logging"
	"github.com/pokys/winmigrathor/internal/meta"
)

// RestoreOptions are passed from CLI/TUI to the restore command.
type RestoreOptions struct {
	Source           string
	UserMapping      map[string]string // source username -> target path
	JobNames         []string
	SelectedFolders  []string // subset of folders to restore (nil = all)
	SelectedBrowsers []string // subset of browsers to restore (nil = all)
	DryRun           bool
	ConflictStrategy string
	InstallApps      bool
}

// RestoreResult holds the aggregate restore result.
type RestoreResult struct {
	Results    []jobs.Result
	Duration   time.Duration
	LogDir     string
	SourceMeta meta.Metadata
	Cancelled  bool
	Error      error
}

// RunRestore performs the restore operation.
// If progressCh is non-nil, RunRestore closes it when done.
// If ctx is nil, context.Background() is used.
func RunRestore(ctx context.Context, opts RestoreOptions, allJobs []jobs.Job, progressCh chan jobs.Progress) (*RestoreResult, error) {
	if progressCh != nil {
		defer close(progressCh)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	start := time.Now()

	// Load metadata from backup
	sourceMeta, err := meta.Load(opts.Source)
	if err != nil {
		return nil, fmt.Errorf("load backup metadata: %w", err)
	}

	logDir := filepath.Join(opts.Source, "logs")
	logger, err := logging.Setup(logDir)
	if err != nil {
		return nil, fmt.Errorf("setup logging: %w", err)
	}
	defer logger.Close()

	log := logger.Main
	log.Info("restore started", "source", opts.Source, "jobs", opts.JobNames)

	var allResults []jobs.Result

	activeJobs := filterJobs(allJobs, opts.JobNames)

	jobOpts := jobs.Options{
		Ctx:              ctx,
		DryRun:           opts.DryRun,
		ConflictStrategy: opts.ConflictStrategy,
		LogDir:           logDir,
		ProgressCh:       progressCh,
		SelectedFolders:  opts.SelectedFolders,
		SelectedBrowsers: opts.SelectedBrowsers,
	}

	cancelled := false
userLoop:
	// Process each user mapping
	for srcUsername, targetUserPath := range opts.UserMapping {
		log.Info("restoring user", "source_user", srcUsername, "target_path", targetUserPath)

		totalJobs := int64(len(activeJobs))
		for ji, j := range activeJobs {
			if ctx.Err() != nil {
				cancelled = true
				log.Info("cancelled, stopping restore loop", "before", j.Name())
				break userLoop
			}
			log.Info("running restore job", "job", j.Name())

			// Notify UI that this job is starting
			if progressCh != nil {
				progressCh <- jobs.Progress{JobName: j.Name(), Current: int64(ji), Total: totalJobs}
			}

			result, err := j.Restore(opts.Source, targetUserPath, jobOpts)
			if err != nil {
				log.Error("restore job failed", "job", j.Name(), "error", err)
				if result.Status == "" {
					result.Status = "error"
					result.Errors = append(result.Errors, err.Error())
				}
			}

			allResults = append(allResults, result)

			for _, w := range result.Warnings {
				log.Warn("restore warning", "job", j.Name(), "warning", w)
			}
			for _, e := range result.Errors {
				log.Error("restore error", "job", j.Name(), "error", e)
			}
		}
	}

	duration := time.Since(start)
	if cancelled {
		log.Info("restore cancelled", "duration", duration)
	} else {
		log.Info("restore complete", "duration", duration)
	}

	return &RestoreResult{
		Results:    allResults,
		Duration:   duration,
		LogDir:     logDir,
		SourceMeta: sourceMeta,
		Cancelled:  cancelled,
	}, nil
}

// ValidateBackup checks that a given directory is a valid backup.
// A non-nil Metadata may be returned alongside a non-nil error if the backup
// is parseable but flagged (e.g. cancelled mid-run); callers can decide
// whether to proceed.
func ValidateBackup(dir string) (meta.Metadata, error) {
	if !meta.Exists(dir) {
		return meta.Metadata{}, fmt.Errorf("no metadata.json found in %s\n\nThis directory does not appear to be a valid MigraThor backup.", dir)
	}
	m, err := meta.Load(dir)
	if err != nil {
		return m, fmt.Errorf("corrupt metadata.json: %w", err)
	}
	if m.Cancelled {
		return m, fmt.Errorf("this backup was cancelled mid-run and may be incomplete")
	}
	return m, nil
}
