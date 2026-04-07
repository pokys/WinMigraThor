//go:build !windows

package jobs

func DetectedBrowserNames(userPath string) []string {
	return nil
}
