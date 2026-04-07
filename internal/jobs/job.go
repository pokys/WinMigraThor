package jobs

// Options holds per-run configuration passed to jobs.
type Options struct {
	DryRun           bool
	PasswordMode     string // skip, assisted, experimental
	ConflictStrategy string // ask, overwrite, skip, rename
	Compress         bool
	LogDir           string
	ProgressCh       chan<- Progress
	SelectedFolders  []string // subset of standard folders (nil = all)
	SelectedBrowsers []string // subset of browsers (nil = all)
}

// Progress is sent from a running job to the UI.
type Progress struct {
	JobName    string
	Current    int64
	Total      int64
	CurrentFile string
	Done        bool
	Err        error
	Warning    string
}

// ScanItem represents a single discoverable item within a job.
type ScanItem struct {
	Label     string
	Path      string
	SizeBytes int64
	Details   string
	Selected  bool
}

// ScanResult is the output of Job.Scan().
type ScanResult struct {
	Items          []ScanItem
	TotalSizeBytes int64
}

// Result holds the outcome of a Backup or Restore operation.
type Result struct {
	JobName    string
	Status     string // success, warning, error, skipped
	SizeBytes  int64
	FilesCount int
	Warnings   []string
	Errors     []string
	Duration   string
}

// Job is the interface that every data category must implement.
type Job interface {
	Name() string
	Description() string
	Scan(userPath string) (ScanResult, error)
	Backup(userPath, target string, opts Options) (Result, error)
	Restore(source, userPath string, opts Options) (Result, error)
}
