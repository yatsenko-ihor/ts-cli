# Requirements: SSH process construction (REQ-SSH) - tui/ssh.go

Scope: how the ssh / sshpass+ssh process is built, how the password reaches it, how targets are validated and how
process outcomes are classified. Security intent: a password must only travel through a private pipe (fd 3), never
through argv or the environment; usernames and addresses come from disk or the API and must not be able to inject
ssh options or shell text.

## Failure classification

- **REQ-SSH-001** classifySSHError shall return sshFailureNone for a nil error.
- **REQ-SSH-002** classifySSHError shall return sshFailureExec for any error that is not an `*exec.ExitError` (the process could not be started), including such an error wrapped with `%w`.
- **REQ-SSH-003** classifySSHError shall return sshFailureBadPassword for a process that exited with code 5 (sshpass: wrong password), also when the error is wrapped.
- **REQ-SSH-004** classifySSHError shall return sshFailureConnection for a process that exited with code 255 (ssh: connection or authentication error), also when the error is wrapped.
- **REQ-SSH-005** classifySSHError shall return sshFailureRemoteExit for every other non-zero exit code (for example 1, 4, 6, 127, 130, 254), so an ordinary failing remote command is never taken for a credential failure.

## Target validation

- **REQ-SSH-006** buildSSHTarget shall reject, with an error and an empty result, any address that is not a literal IP address (hostnames, empty string, text with separators, spaces, newlines, option-like text, CIDR notation, IPv6 zone identifiers), regardless of the username.
- **REQ-SSH-007** buildSSHTarget shall return the bare address when the username is empty.
- **REQ-SSH-008** buildSSHTarget shall return `user@address` for a valid username and IPv4 or IPv6 address, accepting usernames of 1 to 32 characters.
- **REQ-SSH-009** buildSSHTarget shall reject a username that util.ValidateSSHUsername rejects (leading hyphen such as `-oProxyCommand=x`, a lone `-`, `@`, spaces, `;`, newlines, 33 or more characters) with an error and an empty result.

## Command construction

- **REQ-SSH-010** newSSHCommand shall build `ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 <target> <remoteArgs...>` with the remote arguments after the target, in order and unmodified, when no sshpass is used.
- **REQ-SSH-011** When the password is non-empty and sshpass is on PATH, newSSHCommand shall build `sshpass -d 3 ssh <options> <target> <remoteArgs...>` with exactly one extra file (fd 3) that carries the password followed by a newline, and the password shall appear in neither argv nor the environment of sshpass or ssh (checked for ordinary, 1-character, quote/`$()`/option-like, space-padded and non-ASCII passwords).
- **REQ-SSH-012** newSSHCommand shall never use sshpass when the password is empty, even if sshpass is installed.
- **REQ-SSH-013** newSSHCommand shall add `-o BatchMode=yes` before the target when batch is requested and sshpass is not used, so ssh fails fast instead of prompting on the TUI terminal.
- **REQ-SSH-014** newSSHCommand shall not add BatchMode when batch is not requested; when a password is given but sshpass is not installed it shall fall back to plain ssh, still honouring batch, and shall never place the password on the command line.
- **REQ-SSH-015** newSSHCommand shall return an error mentioning the cause, and neither a command nor a cleanup function, when the password pipe cannot be created.
- **REQ-SSH-016** newSSHCommand shall return an error, and neither a command nor a cleanup function, when the password cannot be written to the pipe, and shall close both pipe ends in that case.
- **REQ-SSH-017** The cleanup function returned with an sshpass command shall close the read end of the password pipe; the cleanup function of a plain ssh command shall be a harmless no-op that can be called repeatedly.

## Interactive banner

- **REQ-SSH-018** bannerExec.Run shall write the banner to the configured stdout, then run the wrapped command and return its error unchanged; the SetStdin/SetStdout/SetStderr methods shall connect the corresponding streams of the wrapped command.
- **REQ-SSH-019** bannerExec.Run shall write no banner when the banner text is empty or no stdout was set, and shall still run the command.

## Interactive session

- **REQ-SSH-020** sshToDevice shall, for a device without addresses, return a message with an error and sshFailureExec and start nothing.
- **REQ-SSH-021** sshToDevice shall, when the first address or the username is invalid (hostile address, option-injecting username), return a message with an error and sshFailureExec and start nothing; only the first address of the device is used.
- **REQ-SSH-022** sshToDevice shall, when the process cannot be built (for example the password pipe cannot be created), return a message with an error and sshFailureExec and start nothing.
- **REQ-SSH-023** sshToDevice shall request BatchMode only when the password is empty and sshpass is installed; with a password, or without sshpass, ssh is left free to prompt on the terminal.
- **REQ-SSH-024** sshToDevice shall hand the process to the terminal with the banner `\nConnection to <name> (<target>)\n`, using the device Name or, when empty, its Hostname.
- **REQ-SSH-025** The completion callback of sshToDevice shall release the password pipe.
- **REQ-SSH-026** The completion callback of sshToDevice shall return an sshMsg carrying the error and its classification (nil gives an empty message; exit 5, 255, other, non-exit errors give the matching failure).

## Credential probe

- **REQ-SSH-027** probeSSH shall return a probeMsg with an error and sshFailureExec for a device without addresses or with an invalid address or username, without running anything.
- **REQ-SSH-028** probeSSH shall return a probeMsg with an error and sshFailureExec when the process cannot be built.
- **REQ-SSH-029** probeSSH shall run `true` on the target non-interactively (BatchMode when no password is used) and report success as sshFailureNone with a nil error; a password shall reach the process only through fd 3.
- **REQ-SSH-030** probeSSH shall classify the outcome of the process with classifySSHError (exit 5, 255, other non-zero).
- **REQ-SSH-031** probeSSH shall release the password pipe when the probe has finished and shall report an ssh binary that cannot be started as sshFailureExec.

## Streamed remote jobs

- **REQ-SSH-032** startRemoteJob shall return an error "no IP addresses" and no job for a device without addresses.
- **REQ-SSH-033** startRemoteJob shall return an error and no job when the address or the username is invalid.
- **REQ-SSH-034** startRemoteJob shall return an error and no job when the script action has no script ("no script selected") or the action is neither command nor script ("nothing to run"), including the interactive-session and none actions.
- **REQ-SSH-035** startRemoteJob shall return an error and no job when the ssh process cannot be built or cannot be started ("could not start ssh").
- **REQ-SSH-036** startRemoteJob shall pass a command to ssh as exactly one trailing argument, byte for byte (quotes, `$()`, newlines and option-like text stay data), with BatchMode.
- **REQ-SSH-037** startRemoteJob shall run a script by passing Script.RemoteCommand(args) as the single remote argument and sending the script content on the process stdin; a password shall reach the process only through fd 3.

## Remote path resolution

- **REQ-SSH-038** resolveRemoteOutputPath shall return an error for a device without addresses, an invalid address or username, a pipe failure, a failing ssh (non-zero exit) and an empty remote working directory.
- **REQ-SSH-039** resolveRemoteOutputPath shall run `pwd` on the device, trim the output and return path.Clean(path.Join(cwd, entry)) (relative entries, `./`, `..` and empty entry resolved; an absolute entry is treated as relative to the cwd).
- **REQ-SSH-040** resolveRemoteOutputPath shall use the username saved for the machine (or the default) and the saved or in-progress password, delivered through fd 3 and with BatchMode when no password is used.

## Clipboard command

- **REQ-SSH-041** copySSHCommand shall report failure with empty text when the device has no addresses.
- **REQ-SSH-042** copySSHCommand shall report failure with empty text, and copy nothing, when the address or username is invalid.
- **REQ-SSH-043** copySSHCommand shall copy exactly `ssh <target>` (with `user@` only when a username is known) and never any password.

## MC/DC

Decisions with more than one condition (a nested `if` pair is treated as one compound decision). Each independence pair names two test cases that differ in that condition only and flip the outcome. Test names are `TestFunc/subtest`.

1. newSSHCommand: `password != "" && sshpass found` (nested ifs; outcome = sshpass used).
   - password != "": `TestDO178CNewSSHCommandSSHPassUsesPrivatePipe/s3cret-pass` (sshpass used) vs `TestDO178CNewSSHCommandEmptyPasswordNeverUsesSSHPass` (plain ssh).
   - sshpass found: `TestDO178CNewSSHCommandSSHPassUsesPrivatePipe/s3cret-pass` vs `TestDO178CNewSSHCommandPlainSSH/password without sshpass is never put on the command line`.
2. newSSHCommand: `batch` (simple condition, listed for completeness): `TestDO178CNewSSHCommandPlainSSH/batch adds BatchMode` vs `.../not batch has no BatchMode`.
3. bannerExec.Run: `b.stdout != nil && b.banner != ""`.
   - stdout != nil: `TestDO178CBannerExec/banner then command output, wiring through setters` (banner printed) vs `.../no stdout set prints no banner and does not panic`.
   - banner != "": `.../banner then command output, wiring through setters` vs `.../empty banner prints nothing extra`.
4. sshToDevice: `batch := password == "" && sshpassAvailable()` (`TestDO178CSSHToDeviceBatchModeNeedsSSHPassAndNoPassword`).
   - password == "": `empty password, sshpass present` (BatchMode) vs `password, sshpass present` (none).
   - sshpassAvailable(): `empty password, sshpass present` vs `empty password, sshpass absent`.
5. Single-condition decisions (no MC/DC pair needed, covered both ways by the tests of the cited requirements): `len(device.Addresses) == 0` in sshToDevice, probeSSH, startRemoteJob, resolveRemoteOutputPath, copySSHCommand; every `err != nil` after buildSSHTarget/newSSHCommand/startRunJob/Output; `username == ""` and `net.ParseIP(address) == nil` in buildSSHTarget (REQ-SSH-006..009); `spec.script == nil`; `remoteCwd == ""`; `!errors.As(err, &exitErr)` and the exit-code switch in classifySSHError (REQ-SSH-001..005).

## Derived requirements / unreachable code

Derived requirements (behaviour no user need asked for; each is covered by a test above):

- `StrictHostKeyChecking=accept-new` (trust on first use) and `ConnectTimeout=15` are design decisions, REQ-SSH-010.
- The meaning of exit codes 5 (sshpass wrong password) and 255 (ssh error) comes from the sshpass and ssh manuals, REQ-SSH-003/004.
- A non-empty password with no sshpass installed is silently not used (ssh prompts or fails); REQ-SSH-014.
- resolveRemoteOutputPath lets `..` climb above the remote cwd and treats an absolute entry as relative; the result is only used to copy a path, REQ-SSH-039.
- sshToDevice only uses the first address of a device, REQ-SSH-021.

Known limitations, deliberately not tested:

- The password is written to the pipe before the process starts. A password larger than the pipe buffer (64 KiB on Linux) would block forever because the child has not started to read. Real passwords are far below that; no test is possible without hanging.
- startRemoteJob wraps start failures as "could not start ssh: ..." and finishSSHAttempt wraps again, so the user sees the phrase twice. Cosmetic; behaviour kept.

Test seams added to non-test code (behaviour identical): `osPipe` (os.Pipe) and `teaExec` (tea.Exec), package-level variables in tui/ssh.go, used to provoke pipe failures and to capture the interactive process without a terminal.

Unreachable code: none. Every statement of tui/ssh.go is executed by the tests (100 % statement coverage, every condition true and false under gobco).
