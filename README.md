# Tailscale CLI (ts-cli)

A CLI tool for managing Tailscale devices via the Tailscale REST API. Supports multi-account configurations, an interactive TUI with split-screen layout, remote command execution, per-device command history, and clipboard integration.

## Features

- **Multi-Account Management**: Store and switch between multiple Tailscale accounts/tailnets
- **Interactive TUI**: Browse devices with a keyboard-driven split-screen terminal interface
- **Device Search**: Vim-style incremental search by name, hostname, OS, or IP address
- **SSH Integration**: Connect to devices via SSH directly from the TUI or CLI
- **Run Panel**: Execute local scripts (`~/.ts-cli/scripts/`), saved commands with `{{placeholders}}`, or one-off commands on the selected machine
- **Streaming Output**: Output appears line by line while the job runs, with exit status and elapsed time; stop a job with `x`
- **Recent**: Re-run anything that ran on a machine before, or save it as a named command
- **Multi-machine runs**: mark machines with `Space` (or `*` for all), then run a script or command on all of them in parallel; the output is split into one section per machine
- **Clipboard Integration**: Copy SSH commands (`c`) or paste text (`ctrl+v` / `cmd+v`) in any input field
- **Profile Filtering**: Filter the device list by account or view all accounts at once
- **Shell Completion**: Built-in support for bash, zsh, fish, and PowerShell

## Prerequisites

- Go 1.24 or later
- A Tailscale account with API access
- A valid Tailscale API key (get one from https://login.tailscale.com/admin/settings/keys)
- Optional: `sshpass` for saved-password SSH logins (`brew install hudochenkov/sshpass/sshpass` on macOS, `apt install sshpass` on Debian/Ubuntu). Without it ts-cli falls back to plain `ssh`.

## Installation

### Build from source

```bash
git clone https://github.com/yatsenko-ihor/ts-cli.git
cd ts-cli
go mod download
go build -o ts-cli .
```

### Install globally

```bash
go install github.com/yatsenko-ihor/ts-cli@latest
```

This installs `ts-cli` to your `$GOPATH/bin` directory.

## Usage

### Authentication

Authenticate with your Tailscale API key:

```bash
# Using an environment variable
export TAILSCALE_API_KEY=tskey-api-xxxxx
./ts-cli login --tailnet=example.com

# Or provide the key directly
./ts-cli login --api-key=tskey-api-xxxxx --tailnet=example.com
```

Replace `example.com` with your actual tailnet name (e.g. `mycompany.com` or `user@example.com`).

The login command validates your API key and stores it in `~/.ts-cli/config.json`.

Prefer the environment variable over `--api-key`: command-line flags end up in your shell history and are visible to other local users in the process list while the command runs.

### Multi-Account Setup

Run `login` multiple times with different tailnet/API key pairs. Each account is stored separately and you can switch between them, or view all devices across all accounts using the **All Accounts** profile in the TUI.

### List Devices

```bash
./ts-cli list
```

Output (default table format):

```
NAME                HOSTNAME    ADDRESS       OS     LAST SEEN    AUTHORIZED
laptop.example.com  laptop      100.64.0.1    linux  2 hours ago  Yes
phone.example.com   phone       100.64.0.2    iOS    just now     Yes

Total devices: 2
```

JSON output:

```bash
./ts-cli list --format=json
```

### SSH

Connect directly from the CLI:

```bash
ts-cli ssh laptop.example.com
ts-cli ssh laptop.example.com --user=admin
```

### Interactive TUI

```bash
ts-cli              # defaults to interactive mode
ts-cli interactive
ts-cli i            # short alias
ts-cli tui          # alternative alias
```

#### TUI Keyboard Shortcuts

| Key         | Action                                            |
| ----------- | ------------------------------------------------- |
| `↑` / `k`   | Move cursor up                                    |
| `↓` / `j`   | Move cursor down                                  |
| `/`         | Enter search mode                                 |
| `s`         | SSH to selected device                            |
| `c`         | Copy SSH command to clipboard                     |
| `r`         | Reload devices                                    |
| `p`         | Switch profile (account filter)                   |
| `m`         | Manage accounts                                   |
| `u`         | Tailscale up                                      |
| `o`         | Options menu (manage credentials)                 |
| `a`         | About (version, author info)                      |
| `d`         | Clear default SSH username                        |
| `x`         | Dismiss install tip                               |
| `:`         | Run a one-off command on the selected machine(s)  |
| `Space`     | Mark/unmark the machine for a multi-machine run   |
| `*`         | Mark all visible machines (again to clear)        |
| `Esc`       | Clear marks / close the right panels              |
| `Tab`       | Cycle panel focus: machines → run → output        |
| `Shift+Tab` | Reverse cycle panel focus                         |
| `1`         | Jump focus to device list panel                   |
| `2`         | Jump focus to the Run panel                       |
| `3`         | Jump focus to the Output panel                    |
| `Esc`       | Exit current mode / return to list focus          |
| `q`         | Quit                                              |

#### Input Mode Shortcuts (search / command / username prompts)

| Key                | Action                 |
| ------------------ | ---------------------- |
| `ctrl+v` / `cmd+v` | Paste from clipboard   |
| `Backspace`        | Delete last character  |
| `Enter`            | Confirm input          |
| `Esc` / `ctrl+c`   | Cancel and clear input |

#### Three-Panel Layout

When the terminal is wide enough, the TUI shows three panels:

- **Left — Device List**: Searchable, filterable device list with online/offline status icons (🟢 / 🔴)
- **Top-Right — Run**: what you can execute on the selected machine, in three tabs (`[` / `]` to switch)
  - **Scripts**: local files from `~/.ts-cli/scripts/`, streamed over one SSH connection and executed there
  - **Commands**: saved named commands from `config.json`; `{{placeholders}}` are asked for at run time (`n` new, `e` edit, `d` delete)
  - **Recent**: what ran on this machine before (`Enter` run again, `s` save as command, `d` forget)
  - `:` opens an ad-hoc command line from any tab
- **Bottom-Right — Output**: streams while the job runs; the title shows `● running`, then `✓ exit 0` or `✗ exit N`. `c` copies the selected line as an absolute remote path, `n` only the file name, `x` stops the job.

Focus cycles with `Tab` / `Shift+Tab`, or jump directly with `1`, `2`, `3`.

#### Scripts

Any executable file in `~/.ts-cli/scripts/` shows up in the Scripts tab. The interpreter comes from the shebang (`sh` when there is none). On `Enter` the file is sent over ssh's stdin into a private temp file on the machine, executed with its arguments, and removed; the exit status is propagated. Optional metadata in a comment header:

```bash
#!/usr/bin/env bash
# ts-cli: name=Disk usage
# ts-cli: desc=Largest directories under a path
# ts-cli: os=linux,darwin
# ts-cli: args=path
du -xh --max-depth=1 "${1:-/}" | sort -h | tail -n 15
```

`os=` hides the script on machines with another OS; `args=` lists positional arguments the TUI asks for before running.

#### Running on several machines

Mark machines in the list with `Space` (`*` marks every visible one, `Esc` clears). While marks exist, the Run panel and `:` target the whole set:

- Jobs run in parallel, up to 8 at a time, in list order.
- No prompts: each machine uses its saved username and password, or the default username. Machines that are offline, have no username, belong to another Tailscale account, or do not match the script's `os=` are listed as **skipped** with the reason. A machine whose key or saved password is rejected is listed as **failed** with a hint to connect once via `s` so the credentials get saved.
- The Output panel shows one section per machine, headed by its status (`● running`, `✓ exit 0 · 1.2s`, `✗ exit 1`, `⊘ skipped`, `✗ failed`). `{` / `}` jump between machines, `c` copies the selected line as an absolute path on that machine, `x` stops everything still running.
- The panel title summarises the run: `3/4 done · 1 failed · 1 skipped · 4.8s`.

Every machine's result is recorded in its own Recent history.

### Running In tmux

```bash
# Split horizontally
tmux split-window -h -c "#{pane_current_path}" "ts-cli"

# Split vertically
tmux split-window -v -c "#{pane_current_path}" "ts-cli"
```

## Configuration

Configuration is stored in `~/.ts-cli/config.json` (override the directory with `TS_CLI_CONFIG_DIR`) and contains all saved accounts (API key + tailnet per account), the active account selection, the default SSH username and per-machine SSH credentials. The file is created with permissions `0600`; the directory with `0700`.

The API keys grant whatever access they were created with in the Tailscale admin console. They are stored encrypted (see **SSH Credential Management → Security**), but treat `~/.ts-cli/` like `~/.ssh/` anyway: do not sync it to shared folders or commit it anywhere.

You can always override stored values using CLI flags (`--api-key`, `--tailnet`).

### SSH Credential Management

ts-cli stores SSH credentials **per-machine** — each device has its own username/password pair.

**Connection flow** (same for interactive sessions and remote commands):
1. **Saved password for this machine** → connect directly with it, no questions asked
2. **No saved password** → try once without a password (key-based auth). The username is the one saved for the machine, else the default username, else you are asked for it
3. **That attempt fails to authenticate** → you are asked for the password and the connection is retried with `sshpass`
4. **Connection succeeds** → the username and, if you entered one, the encrypted password are saved for this machine. Nothing is written to disk before success
5. **Saved password rejected** (sshpass exit 5) → it is forgotten and you are asked for the current one
6. **Still failing** (ssh exit 255 after a password, or `sshpass` not installed) → retry menu: change user / password / both
7. **Remote shell exits non-zero** → shown as information only; nothing is deleted

Credentials are keyed by the device's stable Tailscale ID, so renaming a machine keeps its saved login. Entries saved by older versions (keyed by name) are migrated automatically on first start.

**Security:**
- Encryption: AES-256-GCM (authenticated encryption) with a random nonce per secret. Both SSH passwords and Tailscale API keys are stored encrypted in `~/.ts-cli/config.json` (permissions 0600); passwords and keys saved by older versions are migrated automatically.
- Master key: a random 32-byte key generated on first use. Where it lives is up to you, see **Where the encryption key lives** below.
- The password is handed to `sshpass` over a private file descriptor, never on the command line, so it is not visible in `ps`.
- Requires `sshpass` in PATH; without it ts-cli falls back to plain `ssh` (key-based or interactive password).

### Where the encryption key lives

```bash
ts-cli secrets status          # current store, keychain availability
ts-cli secrets use keychain    # move the key into the OS credential store
ts-cli secrets use file        # move it back to ~/.ts-cli/key
```

The same switch is available in the TUI under `o` → "Encryption key".

| Store | Where | Protects against | Caveats |
|---|---|---|---|
| `file` (default) | `~/.ts-cli/key`, mode 0600 | a leaked `config.json` on its own | anyone who can read your home directory has both files |
| `keychain` | macOS Keychain, Linux Secret Service (D-Bus), Windows Credential Manager | a copied or backed-up `~/.ts-cli/` directory; the key is unlocked with your login session and never written under `~/.ts-cli` | macOS may ask for permission once; Linux needs a running Secret Service (GNOME Keyring, KWallet) |

Switching copies the key, verifies it can be read back, and only then removes it from the old location, so existing credentials stay readable. The active store is recorded in `~/.ts-cli/secret_store`.

**Fallback.** If the store is `keychain` but the keychain cannot be reached (headless or SSH session, locked login keychain, no D-Bus), ts-cli uses the key file if one still exists and prints a warning. It never silently creates a new key in that situation, because that would make the existing credentials unreadable. Run `ts-cli secrets use file` from a session where the keychain works to go back permanently.

**Manage:** Press `o` → "Machines with saved passwords" to view or delete stored credentials.

## Commands

### `login`

```bash
ts-cli login --tailnet=<tailnet-name> [--api-key=<key>]
```

Flags: `--api-key`, `--tailnet`

### `list`

```bash
ts-cli list [--format=<table|json>] [--tailnet=<name>] [--api-key=<key>]
```

### `ssh`

```bash
ts-cli ssh <device-name-or-hostname> [--user=<username>] [--tailnet=<name>] [--api-key=<key>]
```

### `interactive` / `i` / `tui`

```bash
ts-cli [interactive|i|tui]
```

Launches the interactive TUI (default when no subcommand is given).

### `secrets`

```bash
ts-cli secrets status
ts-cli secrets use <keychain|file>
```

Shows or changes where the master encryption key is stored.

## Project Structure

```
ts-cli/
├── main.go              # Application entry point
├── go.mod               # Go module definition
├── LICENSE              # MIT + Commons Clause
├── client/
│   └── tailscale.go     # Tailscale REST API client
├── commands/
│   ├── root.go          # Root command, CLI setup
│   ├── login.go         # login subcommand
│   ├── list.go          # list subcommand
│   ├── ssh.go           # ssh subcommand
│   ├── interactive.go   # interactive subcommand
│   ├── account.go       # account list/set-active/remove
│   ├── config.go        # Account config persistence
│   ├── install.go       # install subcommand
│   ├── uninstall.go     # uninstall subcommand
│   ├── secrets.go       # secrets status / use keychain|file
│   ├── up.go            # tailscale up helpers
│   └── tailscale_check.go
├── tui/
│   ├── model.go         # Bubbletea model, Update/View entry points
│   ├── state.go         # Sub-state type definitions
│   ├── handlers.go      # Key dispatch maps (Command pattern)
│   ├── commands.go      # action type, shared constants
│   ├── view.go          # Rendering / layout
│   ├── layout.go        # Panel sizing helpers
│   ├── styles.go        # Lipgloss styles
│   ├── messages.go      # tea.Msg types
│   ├── clipboard.go     # Clipboard read/write (cross-platform)
│   ├── ssh.go           # SSH / remote command execution
│   ├── tailscale.go     # Local tailscale daemon interaction
│   ├── help.go/.txt     # F1 help screen
│   ├── config.go        # TUI config persistence helpers
│   ├── runpanel.go      # Run panel: scripts / saved commands / recent, placeholders
│   ├── runjob.go        # Streaming execution of a remote job
│   ├── device_utils.go  # Device filtering, sorting, status
│   └── utils.go         # Misc helpers
├── scripts/
│   └── scripts.go       # Discovery and metadata of ~/.ts-cli/scripts, remote run snippet
│   ├── sshflow.go       # SSH credential flow (key-first, prompt on failure, save on success)
├── config/
│   └── config.go        # Single owner of ~/.ts-cli/config.json (used by commands and tui)
└── util/
    ├── crypto.go        # AES-256-GCM encryption of passwords and API keys
    ├── keystore.go      # Master key: file or OS keychain, switching, fallback
    ├── history.go       # Per-device command history
    ├── paths.go         # Config directory resolution (TS_CLI_CONFIG_DIR override)
    └── validation.go    # Input sanitization and validation
```

## Architecture

The TUI is built on [Bubbletea](https://github.com/charmbracelet/bubbletea) with [Lipgloss](https://github.com/charmbracelet/lipgloss) for styling.

Key-handling uses a **Command pattern**: each input mode (normal, search, username prompt, command input) has its own `map[string]keyHandler` dispatch table where `keyHandler` is `func(model) (tea.Model, tea.Cmd)`. Shared commands (quit, cursor movement, tab cycle) are pre-allocated `var` values reused across maps.

## Security

See [SECURITY.md](SECURITY.md) for the threat model, where secrets live and how to report a vulnerability.

- Config file stored with `0600` permissions; directory with `0700`
- SSH passwords and API keys are encrypted at rest; the master key can live in the OS keychain (`ts-cli secrets use keychain`)
- API keys can be supplied via `TAILSCALE_API_KEY` environment variable to avoid writing them to disk
- All user input in the TUI is sanitized and validated before use in SSH usernames or remote commands

## Development

### Running tests

```bash
go test ./...
go test -v ./...
go test -cover ./...
```

Test files:

- `config/config_test.go` — account operations, atomic save, corrupt-file refusal, legacy migration
- `commands/list_test.go` — JSON output validity
- `commands/install_test.go` — `tsc` alias safety
- `client/tailscale_test.go` — API client (httptest), path escaping, multi-account errors
- `tui/model_test.go` — device filtering, sorting, status detection
- `tui/ssh_test.go` — SSH command construction, exit-code classification, status JSON parsing
- `tui/sshflow_test.go` — credential flow: key-first attempt, password prompt on failure, save on success, key migration
- `util/*_test.go` — encryption round-trip/tamper detection, key store switching (mock keychain), fallback, validation, history

CI (`.github/workflows/ci.yml`) runs `go vet`, `go test -race`, `staticcheck`, `gosec` and `govulncheck` on every push and pull request.

### Shell Completion

```bash
./ts-cli completion bash   > /etc/bash_completion.d/ts-cli
./ts-cli completion zsh    > "${fpath[1]}/_ts-cli"
./ts-cli completion fish   > ~/.config/fish/completions/ts-cli.fish
./ts-cli completion powershell > ts-cli.ps1
```

## API Reference

Uses the [Tailscale API v2](https://tailscale.com/api).

## License

MIT License + Commons Clause — free to use and modify, but cannot be sold commercially. See [LICENSE](LICENSE) for details.

## Author

Ihor Yatsenko — yatsenko.ihor@gmail.com

## Support

For issues and questions:

- Tailscale API documentation: https://tailscale.com/kb/1101/api
- Tailscale devices management: https://tailscale.com/kb/1372/manage-devices
