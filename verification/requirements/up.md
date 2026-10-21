# Requirements: `ts-cli up`, `commands/up.go` (REQ-UP)

`up` runs the local `tailscale up` with flags. Flag values (hostname, exit node) come from the user and must reach tailscale as single arguments, never through a shell. The tests put a fake `tailscale` that records its argv and stdin on a private PATH.

- **REQ-UP-001** When `tailscale` is not on PATH, `up` shall return an error containing "tailscale command not found." and the operating-system specific installation instructions, and shall start nothing.
- **REQ-UP-002** `up` shall run `tailscale up` followed by: `--accept-routes` when requested, `--accept-dns` (default) or `--accept-dns=false` when disabled, `--exit-node=<name>` when non-empty and `--hostname=<name>` when non-empty, in that order.
- **REQ-UP-003** Each flag value shall be passed as exactly one argument, unmodified, whatever it contains (spaces, shell metacharacters, a leading dash, quotes, non-ASCII).
- **REQ-UP-004** `up` shall print "Starting Tailscale...", the command line "Running: tailscale <args>", let the output of tailscale through and, on success, print "Tailscale is now connected!" and the follow-up hints.
- **REQ-UP-005** When `tailscale up` exits with an error, `up` shall return "failed to start Tailscale: <cause>" and shall not print the success message.
- **REQ-UP-006** `up` shall connect the command's input stream to the standard input of `tailscale` (login questions are interactive).
- **REQ-UP-007** The command shall be named `up` and offer the flags `--accept-routes` (false), `--accept-dns` (true), `--exit-node` and `--hostname` (empty).
- **REQ-UP-008** `getInstallInstructions` shall return text specific to darwin (brew), linux (download page and install script), windows, and a generic text for every other system.
- **REQ-UP-009** `joinArgs` shall join arguments with single spaces (empty list: empty string).
- **REQ-UP-010** `isTailscaleInstalled` shall be true only when an executable `tailscale` is found on PATH.

## MC/DC

`up.go` contains no compound boolean decision: every condition (`acceptRoutes`, `acceptDNS`, `exitNode != ""`, `hostname != ""`, `!isTailscaleInstalled()`, `err != nil`) is a single condition, and `TestDO178CUpBuildsArguments` exercises each of them both true and false (including the combination of all four).

## Derived requirements / unreachable code

Derived requirements:

- Disabling DNS is passed as `--accept-dns=false`. Before the tests were written the flag was simply omitted, so `--accept-dns=false` had no effect (REQ-UP-002). Code change in `up.go`.
- Hostname and exit node are embedded as `--name=value` so a value starting with `-` cannot become a separate option (REQ-UP-003).
- Code changes made for the tests (default behaviour identical): `runtime.GOOS` replaced by the `goos` variable; the standard input of tailscale is `cmd.InOrStdin()` (is `os.Stdin` for a real run).

Unreachable code: none. Every statement of `up.go` is executed by the tests (100 % statement coverage).
