# Requirements: Tailscale daemon check, `commands/tailscale_check.go` (REQ-TSC)

These functions decide whether to warn the user that the local Tailscale is not running and try to start it. They run the local `tailscale` / `open` programs; the tests substitute them on a private PATH and replace the two-second wait (`sleepFn`) and the operating system (`goos`).

- **REQ-TSC-001** `CheckTailscaleStatus` shall return (true, "") when `tailscale status` succeeds and its output does not contain "logged out".
- **REQ-TSC-002** `CheckTailscaleStatus` shall return false with the operating-system specific "not running" message when `tailscale` is missing or `tailscale status` exits with an error.
- **REQ-TSC-003** `CheckTailscaleStatus` shall return false with the message "Tailscale daemon is running but you're logged out.\nRun: tailscale up" when a successful `tailscale status` prints "logged out" in any letter case; near misses ("logged-out", "loggedout", "log out") shall not count.
- **REQ-TSC-004** `buildTailscaleNotRunningMessage` shall begin "Tailscale doesn't appear to be running." and give the instructions of the current system only: darwin, linux, windows, or a generic text for any other.
- **REQ-TSC-005** `attemptToStartTailscale` shall, on linux, print the manual commands and on any system other than darwin or linux print that automatic start is unsupported, return false, execute nothing and not wait.
- **REQ-TSC-006** On darwin `attemptToStartTailscale` shall run `open -a Tailscale`; when it fails it shall print the failure (and its output, when there is any) and return false without waiting; when it succeeds it shall wait two seconds, check the status again and return true if Tailscale is running, otherwise print that the service is not yet active and return false.
- **REQ-TSC-007** `WarnIfTailscaleNotRunning` shall print nothing and start nothing when Tailscale is running; otherwise it shall print the warning with the check's message and the note that the API does not need the local daemon, try to start it, and print "Please start Tailscale manually and try again." only when the start did not succeed.

## MC/DC

`tailscale_check.go` has no compound boolean decision. Single-condition decisions, each exercised both ways: `err != nil` (status), `strings.Contains(lower(output), "logged out")` (REQ-TSC-003), `err != nil` and `len(output) > 0` after `open` (`TestDO178CAttemptStartOnDarwin`: open fails with output, open missing without output), `isRunning` (started / not yet active), `!isRunning` and `!started` in `WarnIfTailscaleNotRunning` (`TestDO178CWarnIfTailscaleNotRunning`); the `switch goos` cases darwin / linux / windows / default in `buildTailscaleNotRunningMessage` and `attemptToStartTailscale`.

## Derived requirements / unreachable code

Derived requirements:

- On linux the program never runs `sudo` or `systemctl` itself; it only prints instructions (REQ-TSC-005); this is a security decision and the tests verify that nothing is executed.
- The two-second wait before re-checking is a heuristic (REQ-TSC-006).
- Code changes made for the tests (default behaviour identical): `runtime.GOOS` replaced by the `goos` variable; `time.Sleep` replaced by the variable `sleepFn`.

Unreachable code: none. Every statement of `tailscale_check.go` is executed by the tests (100 % statement coverage).
