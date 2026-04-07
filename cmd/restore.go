package cmd

import (
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
	DryRun           bool
	ConflictStrategy string
	InstallApps      bool
}

// RestoreResult holds the aggregate restore result.
type RestoreResult struct {
	Results     []jobs.Result
	Duration    time.Duration
	LogDir      string
	SourceMeta  meta.Metadata
	Error       error
}

// RunRestore performs the restore operation.
func RunRestore(opts RestoreOptions, allJobs []jobs.Job, progressCh chan<- jobs.Progress) (*RestoreResult, error) {
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
		DryRun:           opts.DryRun,
		ConflictStrategy: opts.ConflictStrategy,
		LogDir:           logDir,
		ProgressCh:       progressCh,
	}

	// Process each user mapping
	for srcUsername, targetUserPath := range opts.UserMapping {
		log.Info("restoring user", "source_user", srcUsername, "target_path", targetUserPath)

		for _, j := range activeJobs {
			log.Info("running restore job", "job", j.Name())

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
	log.Info("restore complete", "duration", duration)

	return &RestoreResult{
		Results:    allResults,
		Duration:   duration,
		LogDir:     logDir,
		SourceMeta: sourceMeta,
	}, nil
}

// ValidateBackup checks that a given directory is a valid backup.
func ValidateBackup(dir string) (meta.Metadata, error) {
	if !meta.Exists(dir) {
		return meta.Metadata{}, fmt.Errorf("no metadata.json found in %s\n\nThis directory does not appear to be a valid migrator backup.", dir)
	}
	m, err := meta.Load(dir)
	if err != nil {
		return m, fmt.Errorf("corrupt metadata.json: %w", err)
	}
	return m, nil
}
