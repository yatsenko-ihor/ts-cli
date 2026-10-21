# Requirements: Tailscale SSH (REQ-TSS) - tui/tailscalessh.go

A machine that runs the Tailscale SSH server advertises SSH host keys in `tailscale status --json`. With the option on, `s` runs `tailscale ssh [user@]address` for such machines.

- **REQ-TSS-001** Tailscale SSH shall be used for a session only when the option is on, the tailscale CLI is installed, this computer is logged in to the machine's account, and the status shows that the machine offers it.
- **REQ-TSS-002** For such a machine an interactive session shall not ask for a username, password or key and shall not save credentials; it shall run `tailscale ssh` with the known username (else only the address) in ts-cli's terminal and remember the machine for the next start; commands and scripts shall keep the normal flow.
- **REQ-TSS-003** Inside tmux (with the tmux split on) the session shall open in a pane; a device without address, an invalid username or a failing tmux shall be reported as errors.
- **REQ-TSS-004** The end of an in-terminal Tailscale SSH session shall be shown as information (never as an error); a clean end shall show nothing.
- **REQ-TSS-005** The option shall be off by default, be read from the stored settings and be toggled in the settings window.

## Derived requirements / unreachable code

- If the ACL does not allow the connection, `tailscale ssh` fails and nothing falls back to plain ssh; the message names the option to switch off.
