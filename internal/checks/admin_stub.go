//go:build !windows

package checks

func IsAdmin() bool {
	return true
}

func CheckAdmin() error {
	return nil
}

func EnsureAdminRelaunch(argv []string) (bool, error) {
	return false, nil
}
