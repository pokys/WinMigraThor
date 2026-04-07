package jobs

// AllJobs returns all registered backup/restore jobs.
// On non-Windows platforms this returns an empty list (stub).
var allJobs []Job

// AllJobs returns the full list of available jobs.
func AllJobs() []Job {
	return allJobs
}
