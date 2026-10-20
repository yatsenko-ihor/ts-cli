package tui

// Requirements-based tests for tui/runjob.go (see verification/requirements/runjob.md).

import (
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func needSh(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
}

// drainJobTimeout is like drainJob but fails instead of hanging.
func drainJobTimeout(t *testing.T, job *runJob) (lines []string, res runResult) {
	t.Helper()
	type out struct {
		lines []string
		res   runResult
	}
	ch := make(chan out, 1)
	go func() {
		var o out
		for {
			switch msg := waitRunEvent(job)().(type) {
			case runLinesMsg:
				o.lines = append(o.lines, msg.lines...)
			case runDoneMsg:
				o.res = msg.res
				ch <- o
				return
			}
		}
	}()
	select {
	case o := <-ch:
		return o.lines, o.res
	case <-time.After(20 * time.Second):
		t.Fatal("job did not complete")
		return nil, runResult{}
	}
}

func shJob(t *testing.T, script string, stdin []byte, cleanup func()) *runJob {
	t.Helper()
	needSh(t)
	job, err := startRunJob(exec.Command("sh", "-c", script), stdin, cleanup)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

// Verifies: REQ-JOB-001, REQ-JOB-002, REQ-JOB-003
func TestDO178CRunJobStreamsOutput(t *testing.T) {
	t.Run("stdout and stderr lines, unterminated tail, empty lines", func(t *testing.T) {
		job := shJob(t, `echo one; echo two 1>&2; echo; printf 'three'`, nil, nil)
		lines, res := drainJobTimeout(t, job)
		if len(lines) != 4 {
			t.Fatalf("lines = %q", lines)
		}
		got := map[string]bool{}
		for _, l := range lines {
			got[l] = true
		}
		if !got["one"] || !got["two"] || !got["three"] || !got[""] {
			t.Fatalf("lines = %q", lines)
		}
		if res.err != nil || res.exitCode != 0 || res.failure != sshFailureNone || res.killed {
			t.Fatalf("res = %+v", res)
		}
	})
	t.Run("no output at all", func(t *testing.T) {
		lines, res := drainJobTimeout(t, shJob(t, `true`, nil, nil))
		if len(lines) != 0 || res.err != nil {
			t.Fatalf("lines=%q res=%+v", lines, res)
		}
	})
	t.Run("no line is lost or reordered before the completion message", func(t *testing.T) {
		const n = 5000
		job := shJob(t, `i=0; while [ $i -lt 5000 ]; do echo "line $i"; i=$((i+1)); done`, nil, nil)
		lines, res := drainJobTimeout(t, job)
		if len(lines) != n || lines[0] != "line 0" || lines[n-1] != "line 4999" || res.err != nil {
			t.Fatalf("got %d lines (first %q last %q), res=%+v", len(lines), lines[0], lines[len(lines)-1], res)
		}
		for i, l := range lines {
			if l != "line "+itoaN(i) {
				t.Fatalf("line %d = %q", i, l)
			}
		}
	})
	t.Run("long line below the limit is intact", func(t *testing.T) {
		job := shJob(t, `head -c 300000 /dev/zero | tr '\0' a; echo; echo next`, nil, nil)
		lines, _ := drainJobTimeout(t, job)
		if len(lines) != 2 || len(lines[0]) != 300000 || lines[1] != "next" {
			t.Fatalf("got %d lines, first len %d", len(lines), len(lines[0]))
		}
	})
}

func itoaN(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}

// Verifies: REQ-JOB-004
func TestDO178CRunJobOverlongLineDoesNotHangTheProcess(t *testing.T) {
	// A single 5 MB line exceeds the reader's 4 MiB limit. Its content is
	// discarded, but the process must still be able to finish.
	job := shJob(t, `head -c 5000000 /dev/zero | tr '\0' a; echo; echo after`, nil, nil)
	lines, res := drainJobTimeout(t, job)
	if len(lines) != 0 {
		t.Fatalf("over-long line must not be delivered, got %d lines", len(lines))
	}
	if res.err != nil || res.exitCode != 0 {
		t.Fatalf("res = %+v", res)
	}
}

// Verifies: REQ-JOB-005
func TestDO178CRunJobStdin(t *testing.T) {
	t.Run("payload delivered", func(t *testing.T) {
		lines, _ := drainJobTimeout(t, shJob(t, `cat`, []byte("a\nb 'c' $(x)\n"), nil))
		if len(lines) != 2 || lines[0] != "a" || lines[1] != "b 'c' $(x)" {
			t.Fatalf("lines = %q", lines)
		}
	})
	t.Run("empty non-nil payload gives immediate EOF", func(t *testing.T) {
		lines, res := drainJobTimeout(t, shJob(t, `cat; echo done`, []byte{}, nil))
		if len(lines) != 1 || lines[0] != "done" || res.err != nil {
			t.Fatalf("lines=%q res=%+v", lines, res)
		}
	})
	t.Run("nil payload leaves stdin untouched (null device)", func(t *testing.T) {
		lines, _ := drainJobTimeout(t, shJob(t, `cat; echo done`, nil, nil))
		if len(lines) != 1 || lines[0] != "done" {
			t.Fatalf("lines = %q", lines)
		}
	})
}

// Verifies: REQ-JOB-006, REQ-JOB-007
func TestDO178CRunJobResultClassification(t *testing.T) {
	cases := []struct {
		name     string
		script   string
		code     int
		failure  sshFailure
		wantErr  bool
		killedOK bool
	}{
		{"exit 0", `exit 0`, 0, sshFailureNone, false, false},
		{"exit 1", `exit 1`, 1, sshFailureRemoteExit, true, false},
		{"exit 3", `exit 3`, 3, sshFailureRemoteExit, true, false},
		{"exit 5 is a bad password", `exit 5`, 5, sshFailureBadPassword, true, false},
		{"exit 255 is a connection failure", `exit 255`, 255, sshFailureConnection, true, false},
		{"exit 254 boundary", `exit 254`, 254, sshFailureRemoteExit, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, res := drainJobTimeout(t, shJob(t, c.script, nil, nil))
			if res.exitCode != c.code || res.failure != c.failure || (res.err != nil) != c.wantErr || res.killed {
				t.Fatalf("res = %+v", res)
			}
		})
	}
}

// Verifies: REQ-JOB-008
func TestDO178CRunJobCleanupRunsOnceAfterExit(t *testing.T) {
	var calls atomic.Int32
	job := shJob(t, `echo hi; exit 2`, nil, func() { calls.Add(1) })
	_, res := drainJobTimeout(t, job)
	if calls.Load() != 1 || res.exitCode != 2 {
		t.Fatalf("cleanup calls = %d, res = %+v", calls.Load(), res)
	}

	t.Run("nil cleanup is tolerated", func(t *testing.T) {
		if _, res := drainJobTimeout(t, shJob(t, `true`, nil, nil)); res.err != nil {
			t.Fatalf("res = %+v", res)
		}
	})
}

// Verifies: REQ-JOB-009
func TestDO178CRunJobKill(t *testing.T) {
	var calls atomic.Int32
	job := shJob(t, `echo started; exec sleep 30`, nil, func() { calls.Add(1) })
	// wait until the process is demonstrably running
	if msg, ok := waitRunEvent(job)().(runLinesMsg); !ok || msg.lines[0] != "started" {
		t.Fatalf("msg = %#v", msg)
	}
	start := time.Now()
	job.kill()
	_, res := drainJobTimeout(t, job)
	if time.Since(start) > 10*time.Second {
		t.Fatal("kill did not stop the process promptly")
	}
	if !res.killed || res.failure != sshFailureRemoteExit || res.exitCode != -1 || res.err == nil {
		t.Fatalf("res = %+v", res)
	}
	if calls.Load() != 1 {
		t.Fatalf("cleanup calls = %d", calls.Load())
	}
	// A second kill after exit is harmless.
	job.kill()
}

// Verifies: REQ-JOB-010
func TestDO178CRunJobKillIsNilSafe(t *testing.T) {
	var nilJob *runJob
	nilJob.kill()
	(&runJob{}).kill()                          // no cmd
	(&runJob{cmd: exec.Command("true")}).kill() // never started: no Process
	j := &runJob{cmd: exec.Command("true")}
	j.kill()
	if j.killed {
		t.Fatal("a job that was never started must not be marked killed")
	}
}

// Verifies: REQ-JOB-011
func TestDO178CStartRunJobFailures(t *testing.T) {
	needSh(t)
	cases := []struct {
		name string
		cmd  func() *exec.Cmd
	}{
		{"stdout already set", func() *exec.Cmd { c := exec.Command("sh", "-c", "true"); c.Stdout = os.Stdout; return c }},
		{"stderr already set", func() *exec.Cmd { c := exec.Command("sh", "-c", "true"); c.Stderr = os.Stderr; return c }},
		{"binary does not exist", func() *exec.Cmd { return exec.Command("/nonexistent/ts-cli-test-binary") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var calls atomic.Int32
			job, err := startRunJob(c.cmd(), nil, func() { calls.Add(1) })
			if err == nil || job != nil {
				t.Fatalf("job=%v err=%v", job, err)
			}
			if calls.Load() != 1 {
				t.Fatalf("cleanup must run exactly once on failure, ran %d times", calls.Load())
			}
		})
	}
	t.Run("failure with nil cleanup does not panic", func(t *testing.T) {
		if _, err := startRunJob(exec.Command("/nonexistent/ts-cli-test-binary"), nil, nil); err == nil {
			t.Fatal("expected error")
		}
	})
}

// Verifies: REQ-JOB-012, REQ-JOB-013, REQ-JOB-014
func TestDO178CWaitRunEventBatching(t *testing.T) {
	mk := func(n int, closeIt bool) *runJob {
		j := &runJob{lines: make(chan string, n+1), done: make(chan runResult, 1)}
		for i := 0; i < n; i++ {
			j.lines <- "l" + itoaN(i)
		}
		if closeIt {
			close(j.lines)
		}
		j.done <- runResult{exitCode: 7}
		return j
	}
	count := func(t *testing.T, j *runJob) (sizes []int, res runResult) {
		t.Helper()
		for {
			switch msg := waitRunEvent(j)().(type) {
			case runLinesMsg:
				if msg.job != j {
					t.Fatal("message carries wrong job")
				}
				sizes = append(sizes, len(msg.lines))
			case runDoneMsg:
				if msg.job != j {
					t.Fatal("message carries wrong job")
				}
				return sizes, msg.res
			}
		}
	}
	t.Run("one buffered line and channel open gives a single-line batch", func(t *testing.T) {
		j := mk(1, false)
		msg := waitRunEvent(j)().(runLinesMsg)
		if len(msg.lines) != 1 || msg.lines[0] != "l0" {
			t.Fatalf("msg = %#v", msg)
		}
	})
	t.Run("batch size is capped at 200", func(t *testing.T) {
		for n, want := range map[int][]int{199: {199}, 200: {200}, 201: {200, 1}, 450: {200, 200, 50}} {
			sizes, res := count(t, mk(n, true))
			if len(sizes) != len(want) {
				t.Fatalf("n=%d sizes=%v want %v", n, sizes, want)
			}
			for i := range want {
				if sizes[i] != want[i] {
					t.Fatalf("n=%d sizes=%v want %v", n, sizes, want)
				}
			}
			if res.exitCode != 7 {
				t.Fatalf("res = %+v", res)
			}
		}
	})
	t.Run("closed channel with nothing buffered gives completion", func(t *testing.T) {
		sizes, res := count(t, mk(0, true))
		if len(sizes) != 0 || res.exitCode != 7 {
			t.Fatalf("sizes=%v res=%+v", sizes, res)
		}
	})
	t.Run("order is preserved inside and across batches", func(t *testing.T) {
		j := mk(450, true)
		next := 0
		for {
			switch msg := waitRunEvent(j)().(type) {
			case runLinesMsg:
				for _, l := range msg.lines {
					if l != "l"+itoaN(next) {
						t.Fatalf("line %d = %q", next, l)
					}
					next++
				}
			case runDoneMsg:
				if next != 450 {
					t.Fatalf("got %d lines", next)
				}
				return
			}
		}
	})
}

// Verifies: REQ-JOB-015
func TestDO178CTickRun(t *testing.T) {
	job := &runJob{}
	start := time.Now()
	msg, ok := tickRun(job)().(runTickMsg)
	if !ok || msg.job != job {
		t.Fatalf("msg = %#v", msg)
	}
	if d := time.Since(start); d < 900*time.Millisecond || d > 5*time.Second {
		t.Fatalf("tick after %v, want about 1s", d)
	}
}
