# Tailscale CLI (ts-cli)

A CLI tool for managing Tailscale devices via the Tailscale REST API. Supports multi-account configurations, an interactive TUI with split-screen layout, remote command execution, per-device command history, and clipboard integration.

## Features

- **Multi-Account Management**: Store and switch between multiple Tailscale accounts/tailnets
- **Interactive TUI**: Browse devices with a keyboard-driven split-screen terminal interface
- **Device Search**: Vim-style incremental search by name, hostname, OS, or IP address
- **SSH Integration**: Connect to devices via SSH directly from the TUI or CLI
- **Remote Command Execution**: Run commands on remote devices and capture output in a dedicated panel
- **Per-Device Command History**: Recall previously executed commands per device with `↑`/`↓`
- **Output Panel**: View remote command output in a scrollable split panel
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
| `Tab`       | Cycle panel focus: list → history → output → list |
| `Shift+Tab` | Reverse cycle panel focus                         |
| `1`         | Jump focus to device list panel                   |
| `2`         | Jump focus to command history panel               |
| `3`         | Jump focus to output panel                        |
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
- **Top-Right — Command History**: Per-device command history; navigate with `↑`/`↓` to recall commands
- **Bottom-Right — Output Panel**: Scrollable output from the last remote command

Focus cycles with `Tab` / `Shift+Tab`, or jump directly with `1`, `2`, `3`.

### Running In tmux

```bash
# Split horizontally
tmux split-window -h -c "#{pane_current_path}" "ts-cli"

# Split vertically
tmux split-window -v -c "#{pane_current_path}" "ts-cli"
```

## Configuration

Configuration is stored in `~/.ts-cli/config.json` and contains all saved accounts (API key + tailnet per account), the active account selection, the default SSH username and per-machine SSH credentials. The file is created with permissions `0600`; the directory with `0700`.

The API keys in this file are stored in cleartext and grant whatever access they were created with in the Tailscale admin console. Treat `~/.ts-cli/` like `~/.ssh/`: do not sync it to shared folders or commit it anywhere.

You can always override stored values using CLI flags (`--api-key`, `--tailnet`).

### SSH Credential Management

ts-cli stores SSH credentials **per-machine** — each device has its own username/password pair.

**Connection flow:**
1. **Saved per-machine creds** → connect directly using that machine's username + password (via `sshpass`)
2. **Default username exists, no per-machine password** → prompt for the password
3. **Nothing saved** → prompt for username, then password
4. **Credentials are saved before the session starts** so they survive a closed terminal
5. **Saved password rejected** (sshpass exit 5) → it is deleted and the retry menu opens
6. **Connection/auth error** (ssh exit 255) → retry menu (change user/pass/both); saved credentials are kept
7. **Remote shell exits non-zero** → shown as information only; nothing is deleted

**Security:**
- Encryption: AES-256-GCM (authenticated encryption) with a random nonce per password
- Key: a random 32-byte key generated on first use and stored in `~/.ts-cli/key` (permissions 0600), separate from `config.json`. Passwords saved by older versions (host-derived key) are still readable and are re-encrypted with the new key the next time you save them.
- Storage: base64-encoded ciphertext in `~/.ts-cli/config.json` (permissions 0600)
- What this protects against: a leaked `config.json` on its own. What it does **not** protect against: anyone who can read your home directory as your user (they have both files). For stronger protection use full-disk encryption and keep `~/.ts-cli/` private.
- The password is handed to `sshpass` over a private file descriptor, never on the command line, so it is not visible in `ps`.
- Requires `sshpass` in PATH; without it ts-cli falls back to plain `ssh` (key-based or interactive password).

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
│   ├── device_utils.go  # Device filtering, sorting, status
│   └── utils.go         # Misc helpers
├── util/
│   ├── crypto.go        # AES-256-GCM password encryption
│   ├── history.go       # Per-device command history
│   └── validation.go    # Input sanitization and validation
└── internal/            # Unused scaffolding (services/errors/constants/formatters); candidate for removal
```

## Architecture

The TUI is built on [Bubbletea](https://github.com/charmbracelet/bubbletea) with [Lipgloss](https://github.com/charmbracelet/lipgloss) for styling.

Key-handling uses a **Command pattern**: each input mode (normal, search, username prompt, command input) has its own `map[string]keyHandler` dispatch table where `keyHandler` is `func(model) (tea.Model, tea.Cmd)`. Shared commands (quit, cursor movement, tab cycle) are pre-allocated `var` values reused across maps.

## Security

- Config file stored with `0600` permissions; directory with `0700`
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

- `commands/config_test.go` — account config operations
- `commands/list_test.go` — JSON output validity
- `commands/install_test.go` — `tsc` alias safety
- `client/tailscale_test.go` — API client (httptest), path escaping, multi-account errors
- `tui/model_test.go` — device filtering, sorting, status detection
- `tui/ssh_test.go` — SSH command construction, exit-code classification, status JSON parsing
- `util/*_test.go` — encryption round-trip/tamper detection, validation, history

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
