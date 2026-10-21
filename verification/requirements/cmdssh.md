# Requirements: `ts-cli ssh` command, `commands/ssh.go` (REQ-CSH)

The `ssh` subcommand finds a device in the tailnet(s) and runs the system `ssh` to its first address. The device address comes from the API and the user name from the command line or from `config.json`, so both are untrusted: neither may become an ssh option or shell text. (The TUI equivalent is `tui/ssh.go`, REQ-SSH; this command applies the same validation rules.) Tests use a fake Tailscale API (`client.SetBaseURL`), a temporary configuration and fake `ssh` / `tailscale` programs on a private PATH.

- **REQ-CSH-001** The command shall require exactly one argument, and shall reject an empty device name before any configuration is read or any request is made (an empty name would match a device with an empty name, hostname or ID).
- **REQ-CSH-002** The command shall return "failed to load configuration" when the configuration is unreadable, and "no accounts configured" with the `ts-cli login` hint when no account is stored (also when explicit credentials are given), without any request or process.
- **REQ-CSH-003** When both `--api-key` and `--tailnet` are given the command shall list only that tailnet with that key and ignore the stored accounts; when only one of them is given it shall be ignored and the stored accounts used; an API failure shall return "failed to list devices".
- **REQ-CSH-004** Without explicit credentials the command shall search the devices of all stored accounts (an account that fails shall only produce a warning), and shall return "no devices found in any of your configured accounts" when there are none.
- **REQ-CSH-005** The device shall be the first one whose name, hostname or ID equals the argument exactly (case-sensitive, no trimming, no partial match); otherwise the command shall return "device '<name>' not found in tailnet" and run nothing.
- **REQ-CSH-006** The command shall return "device '<name>' has no IP addresses" for a device without addresses and shall use the first address of a device that has several.
- **REQ-CSH-007** When the local Tailscale is not running the command shall print the warning, wait three seconds and still attempt the connection; when it is running it shall print no warning and not wait.
- **REQ-CSH-008** The user name shall be, in order of precedence: `--user` (when non-empty), the `username` of the machine credential stored under the device ID (the hostname when the device has no ID) when non-empty, the default `ssh_username` of the configuration, and otherwise none (ssh decides).
- **REQ-CSH-009** `buildSSHTarget` and the command shall reject, with an error and without starting ssh, an address that is not a literal IP address (hostname, empty, option-like text, spaces, newline, out-of-range octet) and a user name rejected by `util.ValidateSSHUsername` (leading hyphen, space, `;`, `$(...)`, `@`, newline, more than 32 characters, non-ASCII), whether it comes from `--user`, the machine credential or the configuration; it shall return the bare address for an empty user name and `user@address` otherwise (IPv4 and IPv6, 32-character names accepted).
- **REQ-CSH-010** The command shall print "Connecting to <name> (<address>)...", run `ssh <target>` with exactly one argument (the target), connect the command's input to ssh's input, and return nil when ssh succeeds.
- **REQ-CSH-011** The command shall return "SSH connection failed: <cause>" when ssh exits with an error or cannot be started.
- **REQ-CSH-012** The command shall be named `ssh [device-name-or-hostname]` and offer the string flags `--api-key`, `--tailnet`, `--user` (default empty).

## MC/DC

1. `apiKey != "" && tailnet != ""` (explicit credentials): `TestDO178CSSHExplicitCredentialsOverrideStoredAccounts` first call (both: true) vs the loop with only `--api-key` (false) and with only `--tailnet` (false).
2. `device.Name == x || device.Hostname == x || device.ID == x` (`TestDO178CSSHDeviceLookup`): by name only (`second.ts.net`: T,F,F), by hostname only (`Third`: F,T,F), by ID only (`id-2`: F,F,T), each against `nope` (F,F,F) which is not found.
3. `ok && cred.Username != ""` (machine credential): `TestDO178CSSHUserSelectionPrecedence/machine credential by ID beats config` (true,true) vs `.../credential without username falls through to config` (true,false) vs `.../config default user` (no credential: false,-).
4. `user == "" && cfg.SSHUsername != ""` (default user): `.../config default user` (true,true) vs `.../--user beats everything` (user set: false) vs `.../no user anywhere` (user empty, default empty: true,false).
5. `user == ""` guarding the machine credential lookup: `.../--user beats everything` (credential ignored) vs `.../machine credential by ID beats config`.
6. `key == ""` (ID or hostname as key): `.../machine credential by hostname when the device has no ID` vs `.../credential stored under the hostname is ignored when the device has an ID`.
7. `buildSSHTarget`: `net.ParseIP(address) == nil` and `username == ""` are single conditions: `TestDO178CBuildSSHTarget` covers both outcomes of each.
8. Single-condition decisions, each exercised both ways: `deviceName == ""`, `len(cfg.Accounts) == 0`, `len(devices) == 0`, `targetDevice == nil`, `len(Addresses) == 0`, `!isRunning`, every `err != nil`.

## Derived requirements / unreachable code

Derived requirements:

- The first address of the device is used (REQ-CSH-006).
- The password stored for a machine is not used by this command: ssh asks interactively (the TUI path uses sshpass); not changed.

Defects found and fixed while writing the tests (code changes in `ssh.go`):

- The target was built with `fmt.Sprintf("%s@%s", user, address)` without validation: a user name such as `-oProxyCommand=...` from `--user` or from `config.json`, or a non-IP "address" from the API, reached the ssh argument list as an option. Now `buildSSHTarget` (same rules as `tui/ssh.go`) validates both (REQ-CSH-009).
- An empty device name matched a device whose name, hostname or ID is empty and connected to it; it is now rejected (REQ-CSH-001).
- Test seams (default behaviour identical): the pause uses `sleepFn` (declared in `tailscale_check.go`); the standard input of ssh is `cmd.InOrStdin()`.

Unreachable code: none. Every statement of `ssh.go` is executed by the tests (100 % statement coverage).
