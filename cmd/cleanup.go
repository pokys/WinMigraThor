package cmd

import (
	"fmt"

	"github.com/pokys/winmigrathor/internal/cleanup"
)

// CleanupResult holds what was found and removed.
type CleanupResult struct {
	Found   []cleanup.Item
	Removed []cleanup.Item
	Errors  []string
}

// RunCleanupScan scans for temporary files without removing them.
func RunCleanupScan() ([]cleanup.Item, error) {
	return cleanup.Scan()
}

// RunCleanupDelete removes the given items.
func RunCleanupDelete(items []cleanup.Item) CleanupResult {
	result := CleanupResult{Found: items}
	err := cleanup.Delete(items)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	result.Removed = items
	return result
}

// RunCleanupFull scans and removes all found temp files.
func RunCleanupFull() (CleanupResult, error) {
	items, err := cleanup.Scan()
	if err != nil {
		return CleanupResult{}, fmt.Errorf("scan: %w", err)
	}
	result := RunCleanupDelete(items)
	return result, nil
}
