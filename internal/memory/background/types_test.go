package background

import (
	"testing"
	"time"
)

func TestProcessArgs_Kind(t *testing.T) {
	if got := (ProcessArgs{}).Kind(); got != "memory_process" {
		t.Errorf("ProcessArgs.Kind() = %q, want %q", got, "memory_process")
	}
}

func TestMaintainArgs_Kind(t *testing.T) {
	if got := (MaintainArgs{}).Kind(); got != "memory_maintain" {
		t.Errorf("MaintainArgs.Kind() = %q, want %q", got, "memory_maintain")
	}
}

func TestProcessArgs_InsertOpts(t *testing.T) {
	opts := (ProcessArgs{}).InsertOpts()
	if opts.Queue != QueueMemoryProcess {
		t.Errorf("ProcessArgs.InsertOpts().Queue = %q, want %q", opts.Queue, QueueMemoryProcess)
	}
	if opts.UniqueOpts.ByPeriod <= 0 {
		t.Errorf("ProcessArgs.InsertOpts().UniqueOpts.ByPeriod = %v, want > 0", opts.UniqueOpts.ByPeriod)
	}
	if want := 60 * time.Second; opts.UniqueOpts.ByPeriod != want {
		t.Errorf("ProcessArgs.InsertOpts().UniqueOpts.ByPeriod = %v, want %v", opts.UniqueOpts.ByPeriod, want)
	}
}

func TestMaintainArgs_InsertOpts(t *testing.T) {
	opts := (MaintainArgs{}).InsertOpts()
	if opts.Queue != QueueMemoryMaintain {
		t.Errorf("MaintainArgs.InsertOpts().Queue = %q, want %q", opts.Queue, QueueMemoryMaintain)
	}
	if opts.UniqueOpts.ByPeriod <= 0 {
		t.Errorf("MaintainArgs.InsertOpts().UniqueOpts.ByPeriod = %v, want > 0", opts.UniqueOpts.ByPeriod)
	}
	if want := time.Hour; opts.UniqueOpts.ByPeriod != want {
		t.Errorf("MaintainArgs.InsertOpts().UniqueOpts.ByPeriod = %v, want %v", opts.UniqueOpts.ByPeriod, want)
	}
}
