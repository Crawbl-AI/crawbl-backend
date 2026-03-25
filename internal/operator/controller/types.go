package controller

import "strings"

type RuntimeVaultConfig struct {
	Enabled            bool
	AuthPath           string
	Role               string
	PrePopulateOnly    bool
	SecretPath         string
	SecretKey          string
	FileName           string
	AgentCPURequest    string
	AgentMemoryRequest string
	AgentCPULimit      string
	AgentMemoryLimit   string
}

func (c RuntimeVaultConfig) EnabledForRuntime() bool {
	return c.Enabled &&
		strings.TrimSpace(c.AuthPath) != "" &&
		strings.TrimSpace(c.Role) != "" &&
		strings.TrimSpace(c.SecretPath) != "" &&
		strings.TrimSpace(c.SecretKey) != "" &&
		strings.TrimSpace(c.FileName) != ""
}
