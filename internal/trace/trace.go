// Package trace writes a timestamped log of a run, for auditing.
package trace

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// T is a trace file. A nil *T discards everything.
type T struct {
	mu   sync.Mutex
	f    *os.File
	Path string
}

// Open opens the trace for a command run on a VM. EXE_NEW_TRACE=<file> picks the
// file; EXE_NEW_TRACE set but empty turns tracing off. The default is
// ~/Library/Logs/exe-dev/<cmd>-<name>-<time>.log.
func Open(cmd, name string) *T {
	path, set := os.LookupEnv("EXE_NEW_TRACE")
	if set && path == "" {
		return nil
	}
	if !set {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		dir := filepath.Join(home, "Library", "Logs", "exe-dev")
		if os.MkdirAll(dir, 0o755) != nil {
			return nil
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%s-%s.log", cmd, name, time.Now().Format("20060102-150405")))
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	return &T{f: f, Path: path}
}

// Printf appends one line, prefixed with Unix time to the hundredth of a second.
func (t *T) Printf(format string, args ...any) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := float64(time.Now().UnixNano()) / 1e9
	fmt.Fprintf(t.f, "%.2f %s\n", now, fmt.Sprintf(format, args...))
}

// Close closes the file.
func (t *T) Close() {
	if t != nil {
		t.f.Close()
	}
}
