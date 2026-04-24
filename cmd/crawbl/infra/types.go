package infra

import "time"

// const declarations

// doctlForceFlag is the doctl flag that skips confirmation prompts on destructive operations.
const doctlForceFlag = "--force"

// appSyncPollInterval is how often to check ArgoCD application sync status.
const appSyncPollInterval = 15 * time.Second

// type declarations

// updateOpts groups the parameters for runUpdate.
// ctx is always passed separately as the first argument.
type updateOpts struct {
	env            string
	region         string
	autoApprove    bool
	saveKubeconfig bool
	clusterName    string
	syncTimeout    time.Duration
}

// planOutput is the JSON structure for --json output. CI parses this
// instead of scraping human-readable text.
type planOutput struct {
	Creates   int    `json:"creates"`
	Updates   int    `json:"updates"`
	Deletes   int    `json:"deletes"`
	Unchanged int    `json:"unchanged"`
	HasDrift  bool   `json:"hasDrift"`
	Error     string `json:"error,omitempty"`
}
