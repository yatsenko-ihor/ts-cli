# Requirements: browsing a machine (REQ-RBR) - tui/remotebrowse.go

In the remote path prompts of upload (destination directory) and download (source), the Browse button or Ctrl+O lists directories on the machine over ssh. There is no Tab completion for remote paths.

- **REQ-RBR-001** The remote directory shall be entered with a shell command that quotes every path (`cd -- '…'`), expands a leading `~/` through `"$HOME"`, goes home for an empty path or `~`, never lets a path act as an option or command, and prints the real directory (`pwd -P`) followed by `ls -1ApL`; an unenterable directory shall exit with status 3.
- **REQ-RBR-002** The first listing shall start at the typed text: home for empty text, `.`, `~` or a bare name; the text itself when it ends in `/`; otherwise its directory part. The reply shall be parsed as an absolute directory line followed by entry lines (CRLF and blank lines tolerated); anything else shall be an error.
- **REQ-RBR-003** A listing shall show `.` and `..` first, then directories before files, case-insensitively, hide dot entries unless toggled, offer only directories in directory mode, show no sizes, and put the cursor on the first real entry.
- **REQ-RBR-004** A listing shall run with the machine's saved username and password (password only through sshpass fd 3) or keys, in BatchMode, and never ask anything; it shall refuse a device without address, a bad username or address and a path with control characters before running ssh; it shall report a failing start, exit status 3 as "cannot open the directory", status 255 and a rejected password as advice to connect once with `s`, other failures as they are, an unexpected reply, and a machine that does not answer within the timeout (the process is killed).
- **REQ-RBR-005** Ctrl+O, or Enter on the focused button, in a remote prompt shall open the browser in a loading state and request the listing in the background; the browser shall show the machine's short name and the loading state; destination prompts shall browse directories only.
- **REQ-RBR-006** Opening shall be refused with a message when the machine belongs to another account than the one this computer uses or no username is saved; a saved password shall be used.
- **REQ-RBR-007** Descending, going up (the root is its own parent), `~` (home) and the `..` row shall request a listing; keys other than Esc/q/Ctrl+C shall be ignored while one is loading; `.` shall re-filter the received listing without a request; `.` row, Space and Enter on a file shall choose the directory or file path (absolute) and return it to the prompt; Esc shall close the browser, also while loading.
- **REQ-RBR-008** When the first listing fails and the typed directory was not home, the browser shall retry once at home; any other failure shall keep the listing (if any) and show the message; answers to older requests, answers after the browser was closed and answers while a local browser is open shall be ignored.
- **REQ-RBR-009** The upload destination prompt shall browse directories of the first target machine, the download source prompt shall browse the highlighted machine, and the download destination shall stay a local directory.
- **REQ-RBR-010** The filter of the browser shall work on a machine's listing too and be cleared by every new listing.

## Derived requirements / unreachable code

- Names containing a newline cannot be shown correctly (the listing is line based).
- Symlinks to directories are followed (`ls -L`), so they behave as directories; broken symlinks are listed as files without an error.
- When the machine prints something before the directory line (a banner on stdout), the reply is refused as unexpected.
