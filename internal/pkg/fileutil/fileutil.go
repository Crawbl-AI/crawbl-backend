// Package fileutil provides safe file I/O operations for use across the platform.
package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteAtomically writes data to path using a temp file + rename pattern.
// This prevents partial writes from corrupting the file if the process
// is killed mid-write (e.g. OOMKill, SIGKILL, pod eviction).
//
// The temp file is created in the same directory as path to ensure the
// rename is an atomic filesystem operation (same mount point).
func WriteAtomically(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp to target: %w", err)
	}
	return nil
}
