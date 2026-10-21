# Requirements: file copy with scp (REQ-CPY) - tui/copy.go

`f` uploads a file or directory to the selected machines, `F` downloads from the highlighted one. A copy is an ordinary job (credential flow, parallel runs, output, stop).

- **REQ-CPY-001** A copy shall be labelled `upload <name> → <remote>` or `download <remote> → <dir>`; an IPv6 address in a target shall be bracketed (`user@[addr]`) as scp requires, IPv4 and already bracketed targets unchanged.
- **REQ-CPY-002** The scp command shall use the same connection options and BatchMode as ssh jobs, `-r`, then `--`, the local path made absolute (so it cannot be read as an option or as `host:path`) and `user@host:remote`; downloads shall put the remote first and the local directory last.
- **REQ-CPY-003** A password shall be passed like for ssh: through the private pipe to `sshpass -d 3 scp`, never in the arguments.
- **REQ-CPY-004** An empty local or remote path, a remote path containing a newline, carriage return or NUL, and a local path that cannot be resolved shall be rejected before anything runs.
- **REQ-CPY-005** A leading `~` in a path typed by the user shall expand to the home directory; other paths shall be trimmed and otherwise unchanged.
- **REQ-CPY-006** Upload shall ask for the local path (which must exist) and the remote path (empty means the home directory) and then run the copy on every target machine, one job per machine.
- **REQ-CPY-007** Download shall ask for the remote path and a local directory (empty means the current one; it must exist and be a directory) and then run the copy for the highlighted machine.
- **REQ-CPY-008** Neither key shall do anything without a target machine; download shall refuse when several machines are marked.

## Derived requirements / unreachable code

- A remote path is passed to scp as is: shell metacharacters are interpreted by the remote scp (older protocol) or taken literally (SFTP mode, OpenSSH 9+), depending on the remote scp; only control characters are refused.
- Copy jobs are not recorded in the Recent history.
