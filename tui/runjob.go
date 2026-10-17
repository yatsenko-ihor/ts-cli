package tui

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os/exec"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Streaming execution of a remote job.
//
// A runJob owns one ssh process. Two reader goroutines turn stdout and stderr
// into lines on a shared channel; when both readers are done and the process
// has exited, the channel is closed and the result is published on done.
//
// The Bubbletea side pulls events with waitRunEvent, which returns either a
// runLinesMsg (one or more lines) or a runDoneMsg. Because lines is closed
// only after every line was sent, no output is lost or reordered relative to
// the completion message.

// runJob is the handle of a running (or finished) remote job.
type runJob struct {
	cmd     *exec.Cmd
	cleanup func()
	lines   chan string
	done    chan runResult
	killed  bool
	killMu  sync.Mutex
}

// runResult is the outcome of a job.
type runResult struct {
	err      error
	failure  sshFailure
	exitCode int
	killed   bool
}

// runLinesMsg carries newly read output lines.
type runLinesMsg struct {
	job   *runJob
	lines []string
}

// runDoneMsg signals that the job's process exited.
type runDoneMsg struct {
	job *runJob
	res runResult
}

// runTickMsg refreshes the elapsed-time indicator while a job runs.
type runTickMsg struct{ job *runJob }

// maxOutputLines caps the in-memory output buffer.
const maxOutputLines = 20000

// startRunJob starts cmd and begins streaming its combined output.
// stdin, when non-nil, is fed to the process (used to ship a script).
// cleanup is called once the process has exited.
func startRunJob(cmd *exec.Cmd, stdin []byte, cleanup func()) (*runJob, error) {
	if cleanup == nil {
		cleanup = func() {}
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, err
	}

	job := &runJob{
		cmd:     cmd,
		cleanup: cleanup,
		lines:   make(chan string, 256),
		done:    make(chan runResult, 1),
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go job.readLines(stdout, &wg)
	go job.readLines(stderr, &wg)

	go func() {
		wg.Wait()
		err := cmd.Wait()
		cleanup()
		close(job.lines)

		res := runResult{err: err, failure: classifySSHError(err)}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.exitCode = exitErr.ExitCode()
		}
		job.killMu.Lock()
		res.killed = job.killed
		job.killMu.Unlock()
		if res.killed {
			res.failure = sshFailureRemoteExit
			res.exitCode = -1
		}
		job.done <- res
	}()

	return job, nil
}

func (j *runJob) readLines(r io.Reader, wg *sync.WaitGroup) {
	defer wg.Done()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		j.lines <- sc.Text()
	}
}

// kill terminates the process. The job then completes through the normal
// done path with killed=true.
func (j *runJob) kill() {
	if j == nil || j.cmd == nil || j.cmd.Process == nil {
		return
	}
	j.killMu.Lock()
	j.killed = true
	j.killMu.Unlock()
	_ = j.cmd.Process.Kill()
}

// waitRunEvent returns a Cmd that delivers the next batch of output lines or
// the completion result. Call it again after each runLinesMsg.
func waitRunEvent(job *runJob) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-job.lines
		if !ok {
			return runDoneMsg{job: job, res: <-job.done}
		}
		batch := []string{line}
		// Coalesce whatever is already buffered so a chatty command does not
		// cost one Update per line.
		for len(batch) < 200 {
			select {
			case more, ok := <-job.lines:
				if !ok {
					return runLinesMsg{job: job, lines: batch}
				}
				batch = append(batch, more)
			default:
				return runLinesMsg{job: job, lines: batch}
			}
		}
		return runLinesMsg{job: job, lines: batch}
	}
}

// tickRun schedules an elapsed-time refresh while job runs.
func tickRun(job *runJob) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return runTickMsg{job: job} })
}
