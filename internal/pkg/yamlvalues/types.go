package yamlvalues

// stackConfigFile represents the structure of Pulumi.<env>.yaml.
type stackConfigFile struct {
	Config map[string]any `yaml:"config"`
}
