//go:build !windows

package tui

import (
	"os/exec"
	"syscall"
)

// A job's ssh process runs in its own process group so that stopping it
// stops everything it started: sshpass runs ssh as a child, and a remote
// command's local helpers, would otherwise survive a kill of just the parent
// and keep the output pipes open (the job would not finish until they exit).

// setProcessGroup makes cmd the leader of a new process group.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessTree kills the whole process group led by cmd's process. When
// there is no such group (the process was not started with setProcessGroup)
// it kills the process alone.
func killProcessTree(cmd *exec.Cmd) error {
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return cmd.Process.Kill()
}
