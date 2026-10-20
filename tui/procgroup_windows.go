//go:build windows

package tui

import "os/exec"

// Windows has no process groups in the POSIX sense; only the process itself
// is killed.

func setProcessGroup(cmd *exec.Cmd) {}

func killProcessTree(cmd *exec.Cmd) error { return cmd.Process.Kill() }
