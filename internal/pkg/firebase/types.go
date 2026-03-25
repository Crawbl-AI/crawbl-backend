package firebase

type Config struct {
	CredentialsFile string
	CredentialsJSON string
}

type Claims struct {
	UID   string
	Email string
	Name  string
}

func (c Config) Enabled() bool {
	return c.CredentialsFile != "" || c.CredentialsJSON != ""
}
