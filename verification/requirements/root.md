# Requirements: root command, `commands/root.go` (REQ-ROOT)

`root.go` builds the command tree and makes the interactive mode the default when no subcommand is given. Tests build the real tree, replace the exit function (`osExit`) and capture standard error; no terminal UI is started because every default-mode test runs without an account or with a corrupt configuration.

- **REQ-ROOT-001** The root command shall be named `ts-cli` and report the version constant (`MAJOR.MINOR.PATCH`) for `--version`.
- **REQ-ROOT-002** The root command shall register exactly the subcommands login, list, interactive (aliases `i`, `tui`), ssh, up, account, install, uninstall and secrets (plus cobra's own help and completion), and every call shall build an independent tree.
- **REQ-ROOT-003** Without a subcommand the root command shall run the interactive command; when that returns an error the message shall be printed to standard error as `Error: <message>` (nothing on standard output) and the process shall exit with status 1.
- **REQ-ROOT-004** The error for no configured account shall tell the user to run `ts-cli login`, and an unreadable configuration shall be reported as "failed to load configuration" without being modified.
- **REQ-ROOT-005** A known subcommand shall run instead of the default; an unknown word or flag shall be a command-line error and shall not start interactive mode or exit through the default handler.

## MC/DC

`root.go` contains no compound boolean decision. Its single condition `err != nil` (default mode) is true in `TestROOTNoSubcommandRunsInteractiveAndReportsErrors`.

## Derived requirements / unreachable code

- `osExit` is a package variable only so tests can observe the exit status; production behaviour is `os.Exit`.
- The `err == nil` outcome of the default mode (the interactive TUI runs and quits normally) needs a terminal and is not exercised; it executes no statement of `root.go` that is not already covered (the branch has no body), so statement coverage is 100%.
- Unreachable statements: none.
