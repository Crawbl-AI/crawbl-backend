package test

import (
	"os/exec"
	"time"
)

// const declarations

// dialTimeout is the per-attempt TCP dial timeout used by waitForPort.
const dialTimeout = 500 * time.Millisecond

// portPollInterval is how often waitForPort retries between dial attempts.
const portPollInterval = 300 * time.Millisecond

const gotestfmtModule = "github.com/gotesttools/gotestfmt/v2/cmd/gotestfmt@v2.5.0"

// type declarations

// portForward represents a running kubectl port-forward subprocess.
type portForward struct {
	cmd       *exec.Cmd
	localPort int
	label     string
}
