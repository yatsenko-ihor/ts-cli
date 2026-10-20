//go:build !windows

package tui

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// makePasswordFIFO creates a private directory (0700) holding a named pipe
// (0600).
func makePasswordFIFO() (fifo, dir string, err error) {
	dir, err = os.MkdirTemp("", "ts-cli-pw-")
	if err != nil {
		return "", "", err
	}
	fifo = filepath.Join(dir, "pw")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", "", err
	}
	return fifo, dir, nil
}

// feedPasswordFIFO writes password and a newline to fifo as soon as a reader
// has opened it, then removes dir. A non-blocking open fails with ENXIO while
// nobody reads, so the wait is a poll that gives up after timeout and never
// blocks on a pane that did not start.
func feedPasswordFIFO(fifo, dir, password string, timeout time.Duration) {
	defer os.RemoveAll(dir)
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		f, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			if errors.Is(err, syscall.ENXIO) {
				continue
			}
			return // the pipe is gone: the pane removed it after reading
		}
		_, _ = io.WriteString(f, password+"\n")
		_ = f.Close()
		return
	}
}
