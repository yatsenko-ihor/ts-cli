# Requirements: streamed job execution (REQ-JOB) - tui/runjob.go

Scope: running one process, turning its output into lines, delivering lines and completion to the UI in order, and
killing it. Intent: no output is lost or reordered, a finished process always produces exactly one result, cleanup
(which releases the password pipe) always runs, and a runaway output cannot hang the job.

- **REQ-JOB-001** startRunJob shall deliver every stdout and stderr line of the process as a separate string without its line terminator, including empty lines and a final line without a newline.
- **REQ-JOB-002** startRunJob shall complete with no lines and a nil error for a process that prints nothing.
- **REQ-JOB-003** All lines shall be delivered, in order, before the completion message (5000 lines checked), and a single line of 300 000 bytes (well above the default 64 KiB scanner buffer, below the 4 MiB limit) shall arrive intact.
- **REQ-JOB-004** A line longer than 4 MiB shall not be delivered, and the rest of that stream shall be discarded, but the process shall still be able to finish and the job shall complete normally.
- **REQ-JOB-005** When stdin is non-nil (also when empty) it shall be the process standard input and the process sees EOF after it; when stdin is nil the process stdin is left unset (null device).
- **REQ-JOB-006** The result shall classify the exit with classifySSHError: exit 0 gives sshFailureNone and a nil error, exit 5 sshFailureBadPassword, exit 255 sshFailureConnection, any other non-zero code (for example 1, 3, 254) sshFailureRemoteExit.
- **REQ-JOB-007** The result shall carry the process exit code for every exit that has one (0, 1, 3, 5, 254, 255) and killed = false for a process that ended on its own.
- **REQ-JOB-008** The cleanup function shall be called exactly once, after the process has exited; a nil cleanup shall be accepted.
- **REQ-JOB-009** kill shall terminate a running process promptly; the job then completes with killed = true, failure sshFailureRemoteExit, exit code -1 and the process error, cleanup runs once, and a second kill after the exit is harmless.
- **REQ-JOB-010** kill shall do nothing, and shall not mark the job as killed, for a nil job, a job without command and a command that was never started.
- **REQ-JOB-011** startRunJob shall return an error and no job when stdout is already set, stderr is already set, or the process cannot be started, and shall call cleanup exactly once in each case (also when no cleanup was given).
- **REQ-JOB-012** waitRunEvent shall return the next line, together with every further line already buffered, as one runLinesMsg that names the job; with a single buffered line and an open channel it shall return a one-line batch without blocking for more.
- **REQ-JOB-013** waitRunEvent shall return at most 200 lines per message; the remainder is delivered by the following calls (199, 200, 201 and 450 buffered lines checked).
- **REQ-JOB-014** When the line channel is closed and drained, waitRunEvent shall return a runDoneMsg naming the job and carrying the result; lines buffered before the close shall still be delivered first, in order.
- **REQ-JOB-015** tickRun shall deliver a runTickMsg naming the job after about one second.

## MC/DC

Compound decisions:

1. runJob.kill: `j == nil || j.cmd == nil || j.cmd.Process == nil` (outcome true = return without killing). Base case: started job, all conditions false (`TestDO178CRunJobKill`).
   - `j == nil`: `TestDO178CRunJobKillIsNilSafe` (nil job) vs `TestDO178CRunJobKill`.
   - `j.cmd == nil`: `TestDO178CRunJobKillIsNilSafe` (`&runJob{}`) vs `TestDO178CRunJobKill`.
   - `j.cmd.Process == nil`: `TestDO178CRunJobKillIsNilSafe` (command never started) vs `TestDO178CRunJobKill`.
2. Single-condition decisions covered both ways: `cleanup == nil`, `stdin != nil`, each `err != nil` in startRunJob (`TestDO178CStartRunJobFailures` cases and `TestDO178CRunJobStreamsOutput`), `errors.As(err, &exitErr)` (exit versus clean run), `res.killed` (`TestDO178CRunJobKill` versus `TestDO178CRunJobResultClassification`), `!ok` on the closed channel in waitRunEvent (both the first read and the read inside the select: `TestDO178CWaitRunEventBatching`), `len(batch) < 200` (cap case 450 versus 199), the `select` default (open channel with one line versus buffered lines).

## Derived requirements / unreachable code

Derived behaviour:

- Bug fixed while writing REQ-JOB-004: when a single line exceeded the 4 MiB scanner limit, readLines stopped reading; a process that kept writing then blocked on the full pipe forever, so wg.Wait never returned and the job hung. readLines now drains and discards the remainder of the stream (`io.Copy(io.Discard, r)`), so the job completes. The over-long line itself, and anything after it on that stream, is dropped.
- The line channel holds 256 entries and the job result channel one entry; the output buffer cap `maxOutputLines` is applied by the caller (runpanel.go), not here.
- A kill that arrives after the process already ended on its own still sets `killed`; if the completion has not been published yet the result is reported as killed. Harmless to credentials (a killed job counts as connected).

Known limitation, not tested: kill sends SIGKILL to the direct child only. If the child has children that keep the stdout pipe open (for example `sh -c 'a; sleep 30'`, or sshpass leaving its ssh child), the job completes only when they exit. The tests kill a process that has no children (`exec sleep`). Changing this (process groups) was judged out of scope for a test-only change.

Unreachable code: none. Every statement of tui/runjob.go is executed (100 % statement coverage, every condition true and false under gobco).
