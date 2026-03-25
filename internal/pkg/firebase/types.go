package firebase

// Config holds Firebase authentication configuration.
// One of CredentialsFile or CredentialsJSON must be provided.
type Config struct {
	// CredentialsFile is the path to a Firebase service account JSON file.
	CredentialsFile string
	// CredentialsJSON is the raw JSON content of a Firebase service account.
	CredentialsJSON string
}

// Claims represents the extracted user information from a Firebase ID token.
type Claims struct {
	// UID is the Firebase user ID.
	UID string
	// Email is the user's email address.
	Email string
	// Name is the user's display name.
	Name string
}

// Enabled returns true if Firebase credentials are configured.
// At least one of CredentialsFile or CredentialsJSON must be non-empty.
func (c Config) Enabled() bool {
	return c.CredentialsFile != "" || c.CredentialsJSON != ""
}
