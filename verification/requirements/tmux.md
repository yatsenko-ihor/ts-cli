# Requirements: ssh sessions in a tmux split (REQ-TMX) - tui/tmux.go, tui/tmux_unix.go

When ts-cli runs inside tmux, `s` opens the ssh session in a pane next to the list. The pane is a real terminal; ts-cli cannot observe how the session ends, so credentials are proven first.

- **REQ-TMX-001** A session shall open in a tmux pane only when the "SSH in a tmux split" option is on, ts-cli runs inside tmux ($TMUX set), the `tmux` executable is found, and either no password is needed or `sshpass` is available.
- **REQ-TMX-002** The tmux command shall be `split-window -h` (list left, session right), targeted at ts-cli's own pane (`-t $TMUX_PANE`) when that is known, with the script wrapped in `sh -c '…'` so it does not depend on the user's default shell.
- **REQ-TMX-003** Without a password the pane shall run `ssh` with `StrictHostKeyChecking=accept-new`, `ConnectTimeout=15` and the validated target as one argument; after a non-zero exit it shall print the status and wait for Enter so the error can be read, and after a zero exit it shall close silently.
- **REQ-TMX-004** With a password the script shall open the named pipe as file descriptor 3, remove the pipe's directory, and run `sshpass -d 3 ssh …`; every path shall be shell-quoted.
- **REQ-TMX-005** A password shall reach the pane only through the named pipe: it shall not appear in the arguments of tmux, sshpass or ssh, nor in the pane's output.
- **REQ-TMX-006** The password pipe's directory shall be removed once the pane has read it (or the writer gave up).
- **REQ-TMX-007** Without a password no pipe shall be created and `sshpass` shall not be used.
- **REQ-TMX-008** openInTmux shall reject a device without addresses, an invalid username and an invalid address before running tmux, and shall remove the pipe and report the error when tmux itself fails.
- **REQ-TMX-009** The password pipe shall be a 0600 named pipe in a 0700 directory; the writer shall wait for a reader without blocking (non-blocking open polled until a timeout), remove the directory when it gives up, and stop at once when the pipe has disappeared.
- **REQ-TMX-010** The result of opening the pane shall be shown: an info line on success (clearing a previous error), the error otherwise.
- **REQ-TMX-011** In tmux mode a session attempt shall first run the non-interactive probe (key auth, typed password or saved password); when it succeeds the credentials shall be saved, the pending attempt cleared and the pane opened instead of an in-process session; a failing probe shall follow the normal failure handling (ask for a password, forget a rejected saved password).
- **REQ-TMX-012** When the tmux conditions of REQ-TMX-001 are not met the session flow shall be exactly the one without tmux.

## Derived requirements / unreachable code

- `makePasswordFIFO`'s `Mkfifo` failure branch (file system without named pipes) is not exercised by a test: it cannot be provoked portably, so `tui/tmux_unix.go` is not in the 100 % coverage gate; `tui/tmux.go` is.
- A saved password that is wrong is detected by the probe before the pane opens, so the "forget a rejected saved password" behaviour is preserved in tmux mode.
- Windows: tmux splits are never active (`tmuxSplitActive` is false); the Windows stubs exist only to compile.
