# Requirements: `ts-cli ssh-config`, `commands/sshconfig.go` (REQ-SSC)

`ssh-config` turns the machines of the tailnet into `Host` blocks for the ssh client, so ssh, scp, rsync, git and editors reach a machine by its short name. The text goes into a config file, so every value in it is validated; passwords are never written.

## Content

- **REQ-SSC-001** Each machine with a usable alias and a valid first address shall become `Host <alias>` with `HostName <address>`; a machine with no address, an invalid address or an empty alias shall be skipped.
- **REQ-SSC-002** The alias shall be the first label of a MagicDNS name (or the whole name), with every character other than letters, digits, `.`, `-`, `_` replaced by `-` and leading or trailing `-` and `.` removed, so that no value can start a new config line or option.
- **REQ-SSC-003** A duplicate alias shall get the account name appended, and then a counter (`-2`, `-3`, ...), so that every alias in the file is unique.
- **REQ-SSC-004** `User` shall be the username saved for the machine (keyed by its ID, else its hostname), else `--user`, else the default username; a value that is not a valid SSH username shall be dropped, and no `User` line written when none remains.

## Output

- **REQ-SSC-005** Without `--write` and `--install` the text shall be printed; with `--user` invalid, or when the configuration or the device fetch fails, nothing shall be printed or written and the error shall be returned.
- **REQ-SSC-006** `--write` shall write the text to `ssh_config` in the config directory with mode 0600 through a temporary file, and print the `Include` line to add; the filters shall apply as for `list`.
- **REQ-SSC-007** `--install` shall also put `Include "<file>"` at the top of `~/.ssh/config` (creating it with mode 0600 when absent), keep the rest of the file, and leave it untouched when the line is already there.
