//go:build !windows

package tui

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// gone reports whether pid no longer runs (a zombie waiting to be reaped by
// init counts as gone).
func gone(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return true
	}
	if data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		if i := strings.LastIndex(string(data), ") "); i >= 0 && strings.HasPrefix(string(data)[i+2:], "Z") {
			return true
		}
	}
	return false
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if gone(pid) {
			return
		}
	}
	t.Fatalf("process %d is still running", pid)
}

// Verifies: REQ-JOB-016
func TestKillStopsChildrenOfTheJobProcess(t *testing.T) {
	// sh starts a background sleep that holds the output pipe, like ssh under
	// sshpass. Killing only sh would leave the job waiting for the sleep.
	cmd := exec.Command("sh", "-c", "sleep 60 & echo $!; wait")
	job, err := startRunJob(cmd, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	msg := waitRunEvent(job)().(runLinesMsg)
	childPID, err := strconv.Atoi(strings.TrimSpace(msg.lines[0]))
	if err != nil {
		t.Fatalf("child pid %q: %v", msg.lines[0], err)
	}
	if gone(childPID) {
		t.Fatal("the child must be running before the kill")
	}

	started := time.Now()
	job.kill()
	for {
		ev := waitRunEvent(job)()
		if done, ok := ev.(runDoneMsg); ok {
			if !done.res.killed || done.res.exitCode != -1 {
				t.Fatalf("result = %+v", done.res)
			}
			break
		}
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("the job must finish promptly after kill, not when the child exits")
	}
	waitGone(t, childPID)
}

// Verifies: REQ-JOB-017
func TestKillFallsBackToTheProcessWhenItLeadsNoGroup(t *testing.T) {
	// A command started without setProcessGroup has no group of its own:
	// the group kill finds nothing and the process itself is killed.
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	job := &runJob{cmd: cmd}
	job.kill()
	_ = cmd.Wait()
	if !job.killed || !gone(cmd.Process.Pid) {
		t.Fatal("the process must be killed through the fallback")
	}
}
