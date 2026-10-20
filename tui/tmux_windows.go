//go:build windows

package tui

import (
	"errors"
	"time"
)

// tmux splits are not used on Windows (tmuxSplitActive is always false).

func makePasswordFIFO() (fifo, dir string, err error) {
	return "", "", errors.New("named pipes are not supported on Windows")
}

func feedPasswordFIFO(fifo, dir, password string, timeout time.Duration) {}
