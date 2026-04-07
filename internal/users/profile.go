package users

// Profile represents a local Windows user profile.
type Profile struct {
	Username  string
	Path      string
	SizeBytes int64
	IsCurrent bool
}
