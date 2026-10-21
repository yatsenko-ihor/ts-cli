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
- **Ping and status icons**: coloured ● online, ● offline, ○ no reply to the last ping, `pw` a password is saved, `exp` the device key expired (switchable to the 🟢/🔴/⭕️/🔐 emoji style in Settings). See [Machine status and ping](#machine-status-and-ping)
- **Resume where you left off**: on start the cursor is placed on the machine you connected to last time (if it is online)
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

Each account is a tailnet plus an API key (stored encrypted, see [SSH Credential Management](#ssh-credential-management)). Manage accounts from the TUI (`m`, the **Accounts** window) or with `ts-cli account`:

```bash
# Add (validates the key first). Prefer stdin or the environment: --api-key shows up in shell history and `ps`.
pass show tailscale/work | ts-cli account add --tailnet=work.example.com --name=work --api-key-stdin

ts-cli account list [--check] [--format=json]   # --check verifies each key and shows its expiry
ts-cli account edit work --name=job             # rename; add --api-key-stdin to replace the key
ts-cli account use work                         # make active and `tailscale switch` this computer
ts-cli account hide work | show work            # hide/show the account's machines in the list
ts-cli account remove work
```

`ts-cli login --tailnet=... ` is the same as `account add` and also makes the account active.

**Active vs. visible.** *Visible* accounts decide whose machines appear in the list; hide one and its machines disappear without anything being deleted. The *active* account is the one this computer's `tailscale` is logged in to (`◉` in the Accounts window): `Enter` switches to it.

**Accounts window (`m`)**: `↑/↓` move, `Enter` use (switch this computer), `Space` show/hide, `a` add, `e` edit (name, replace key), `d` remove (asks to confirm), `c` re-check keys, `Esc` close. Each row shows online/total machines and the API key state: valid, expiry countdown, or rejected. The main screen shows a `⚠ API key` line when a key expires within 14 days or is rejected. Key expiry is read best-effort from the Tailscale API; a key that cannot read its own record is shown as plain "key ok".

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

Filter and script it (see [`list`](#list) for all flags):

```bash
ts-cli list --online --os linux
ts-cli list --tag tag:server --format names
ts-cli list --account work --format json
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
| `P`         | Switch profile (account filter)                   |
| `m`         | Accounts window (add, edit, remove, show/hide, switch) |
| `u`         | Tailscale up                                      |
| `o`         | Options menu (manage credentials)                 |
| `a`         | About (version, author info)                      |
| `d`         | Clear default SSH username                        |
| `x`         | Dismiss install tip                               |
| `:`         | Run a one-off command on the selected machine(s)  |
| `Space`     | Mark/unmark the machine for a multi-machine run   |
| `*`         | Mark all visible machines (again to clear)        |
| `p`         | Ping the selected machine(s)                      |
| `f` / `F`   | Upload to the selected machine(s) / download from one (scp) |
| `H`         | Show/hide offline machines                        |
| `F12`       | Settings                                          |
| `?` / `F1`  | Help                                              |
| `Esc`       | Clear marks / close the right panels              |
| `Tab`       | Cycle panel focus: machines → run → output        |
| `Shift+Tab` | Reverse cycle panel focus                         |
| `1`         | Jump focus to device list panel                   |
| `2`         | Jump focus to the Run panel                       |
| `3`         | Jump focus to the Output panel                    |
| `Esc`       | Exit current mode / return to list focus          |
| `q`         | Quit                                              |

#### Text Editing (search, command line, username/password and other prompts)

All single-line inputs have a real cursor and the usual shell-style editing keys:

| Key                                         | Action                              |
| ------------------------------------------- | ----------------------------------- |
| `←` / `→`                                   | Move one character                  |
| `Option+←` / `Option+→` (macOS)             | Move one word back / forward        |
| `Alt+←` / `Alt+→`, `Ctrl+←` / `Ctrl+→`     | Same, on Linux/Windows terminals    |
| `Alt+b` / `Alt+f`                           | Same (what many terminals actually send) |
| `Home` / `Ctrl+A`, `End` / `Ctrl+E`         | Start / end of line                 |
| `Backspace`                                 | Delete the previous character       |
| `Delete` / `Ctrl+D`                         | Delete the next character           |
| `Option+Backspace` / `Ctrl+W`               | Delete the previous word            |
| `Alt+D`                                     | Delete the next word                |
| `Ctrl+U` / `Ctrl+K`                         | Delete to the start / end of line   |
| `ctrl+v` / `cmd+v`                          | Paste from clipboard at the cursor  |
| `Enter`                                     | Confirm input                       |
| `Esc` / `ctrl+c`                            | Cancel and clear input              |

Words are runs of letters, digits and `_`, as in bash, so `/var/log` is two words.

**Option+arrows in your terminal.** ts-cli can only react to the keys the terminal sends. Terminals report `Option+←/→` in two ways and both work:

- as `Esc b` / `Esc f` — iTerm2 with the *Natural Text Editing* preset or *Left Option key = Esc+* (Settings → Profiles → Keys), and Terminal.app with *Use Option as Meta key*;
- as modified arrow sequences (`Esc[1;3D` / `Esc[1;3C`) — kitty, WezTerm, Alacritty, and most Linux terminals with `Alt`.

If Option+arrows do nothing, the terminal is not sending either form: enable one of the settings above (iTerm2: Profiles → Keys → Key Mappings → Presets → *Natural Text Editing*). Typing of non-ASCII text (for example Ukrainian) works in all inputs.

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

#### List appearance and settings (F12)

Press `F12` to open the settings window (Up/Down, Space/Enter to toggle, Esc to close). Choices are saved in `~/.ts-cli/config.json` under `ui`:

- **Short names**: the tailnet suffix is stripped (`box-043.tail813b1c.ts.net` → `box-043`; a name that would become ambiguous keeps a ` ·tailnet` hint).
- **Hide offline machines**: offline machines collapse into one `▸ N offline hidden` row. `H` toggles it; searching always includes offline machines.
- **Details panel**: with the Run panel closed and a terminal at least 100 columns wide, the highlighted machine's OS, address, last seen, key expiry, SSH setup and ping result are shown on the right.
- **Group by account**: section headers per account (only with more than one account).
- **SSH in a tmux split**: inside tmux, `s` opens the session in a new pane to the right of the list instead of taking over ts-cli's terminal (see [Running In tmux](#running-in-tmux)). On by default; has no effect outside tmux.
- **Use Tailscale SSH when offered** (off by default): for machines that run the Tailscale SSH server, `s` runs `tailscale ssh [user@]machine` instead of plain ssh. No password, key or username prompt and nothing is saved; the tailnet's ACL decides who may connect. Machines that offer it show `Tailscale SSH` in the details panel.
- **Theme**: `light` (default, keeps your terminal background), `dark` or `hacker` (green on black). `dark` and `hacker` paint the whole screen, so they look the same on any terminal background.
- **Icon style**: coloured symbols (default) or emoji.

#### Machine status and ping

The colour of a machine comes from two sources, combined:

1. **Tailscale API baseline**, shown instantly: a machine is 🟢 when the API reports `connectedToControl` (it recently kept a TCP connection to Tailscale's control server; the API omits `lastSeen` while this is true) or was last seen less than 5 minutes ago. Otherwise 🔴.
2. **`tailscale ping`** refines it for the machines you are about to use, and overrides the baseline in both directions: a reply makes a machine 🟢 (and usable) even if the API says offline, no reply makes it ⭕️ (unreachable) even if the API says online.

A ping is started:

- manually with `p` for the highlighted or all marked machines;
- ~~automatically in the background for the machine under the cursor~~ — **currently disabled** (the code is kept; set `autoPingEnabled = true` in `tui/ping.go` to re-enable: at start and whenever the cursor rests on a machine for half a second, results reused for 60 seconds);
- as a **pre-flight check** (at most 2 seconds) before `s` or before running a command/script on a single machine, so an unreachable machine fails right away with a clear message instead of waiting for ssh to time out. Press `Esc` to cancel the check.

Machines that belong to another Tailscale account than the active one are never pinged automatically (and are skipped by the pre-flight check), because this computer cannot reach them until the account is switched; their colour stays the API baseline. Nothing is pinged if the `tailscale` CLI is not installed, and a missing CLI never blocks a connection.

#### Machine actions and ping details

The **Machine** tab of the Run panel (`Tab`, then `]` to the last tab) acts on the *highlighted* machine, never on the marked set:

| Action | What it does |
|---|---|
| Ping with details | `tailscale ping`: whether traffic goes direct (with the endpoint) or through a relay (with the region), and the latency |
| Disable / enable key expiry | The device key never expires (or expires again). The API cannot extend a key; to renew one, log in again on the device |
| Expire key now | Logs the device out; asks you to type the machine name |
| Rename… | Sets the machine name |
| Edit tags… | Replaces the tags (`tag:server, tag:prod`); the ACL must allow them |
| Delete from tailnet… | Removes the machine; asks you to type the machine name |

These use the Tailscale API with the account's key, which therefore needs permission to write devices; a read-only key shows the API's error. The details panel also shows what the local daemon knows about the machine (`tailscale status`): the current path, traffic and whether it offers Tailscale SSH.

**Key reminders.** A device whose key expires drops off the tailnet. Expired keys and keys expiring within 14 days are flagged in the list (`exp`, red when expired, yellow when soon) and summarised in a line on the main screen.

#### Copying files

`f` uploads a file or directory to the selected machines (the highlighted one, or all marked), `F` downloads from the highlighted one. Both ask for the paths (`~` works for local paths; an empty remote path means the home directory; downloads go to an existing local directory, `.` by default) and run `scp -r` as an ordinary job: the usual credential flow for one machine, saved credentials only for several, per-machine output and status, stop with `x`. A password is passed like for ssh, never in the argument list. Local paths are made absolute so they cannot be read as an option or as `host:path`.

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

Mark machines in the list with `Space` (`*` marks every visible one, `Esc` clears). Unreachable machines (🔴 or ⭕️) cannot be marked and are never run on; press `p` to ping a 🔴 machine, a reply makes it 🟢 and usable even when the Tailscale API still reports it offline. While marks exist, the Run panel and `:` target the whole set:

- Jobs run in parallel, up to 8 at a time, in list order.
- No prompts: each machine uses its saved username and password, or the default username. Machines that are offline, have no username, belong to another Tailscale account, or do not match the script's `os=` are listed as **skipped** with the reason. A machine whose key or saved password is rejected is listed as **failed** with a hint to connect once via `s` so the credentials get saved.
- The Output panel shows one section per machine, headed by its status (`● running`, `✓ exit 0 · 1.2s`, `✗ exit 1`, `⊘ skipped`, `✗ failed`). `{` / `}` jump between machines, `c` copies the selected line as an absolute path on that machine, `x` stops everything still running.
- The panel title summarises the run: `3/4 done · 1 failed · 1 skipped · 4.8s`.

Every machine's result is recorded in its own Recent history.

Machines that belong to another Tailscale account than the one currently active on this computer are skipped in a multi-machine run. `tailscale switch` changes the account for the whole computer, so it cannot be done safely in the middle of a parallel run. To cover machines from several accounts, filter with `P` (profile) and do one run per account. A single-machine run still switches the account for you automatically.

**Notes on multi-machine runs**

- Run once per machine interactively (`s`) to save its login; after that it takes part in batch runs without any prompt.
- A script that declares `os=` runs only on matching machines; the others are listed as skipped, not as failed.
- Batch runs use the same non-interactive login as single runs: a saved password via `sshpass`, otherwise key-based auth.
- Stopping a run (`x`) kills the running ssh processes and marks machines that were still waiting as skipped.

### Running In tmux

**Sessions in a split.** If ts-cli itself runs inside tmux, `s` runs `tmux split-window -h` and opens the ssh session there: the list stays on the left and a real terminal is on the right, so vim, htop, the mouse, scrolling and copying work as usual (no terminal emulation in ts-cli). Details:

- The credentials are proven first with a quick non-interactive login (key, typed password or saved password), exactly as for a typed password in a normal session; only then is the pane opened. A wrong saved password is therefore still forgotten and asked for again, and a good one is saved before the pane opens.
- A password never appears in the arguments of tmux, sshpass or ssh: it is written to a private named pipe (0600, in a 0700 temp directory) that the pane's shell opens as file descriptor 3 for `sshpass -d 3`; the pipe holds no data on disk and its directory is removed right away.
- If the session ends with an error, the pane prints the status and waits for Enter so the message can be read.
- Turn it off in `F12` → *SSH in a tmux split*. Runs of commands and scripts (`:`, Run panel) are not affected; they stream into the Output panel as before.

Other tmux layouts for ts-cli itself:

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
4. **Connection succeeds** → the username and, if you entered one, the encrypted password are saved for this machine. Nothing is written to disk before success. For an interactive session a password you just typed is first verified with a quick non-interactive login (`ssh ... true`) and saved right away, then the session opens, so how the session later ends (dropped connection, `~.`, exit code 255) can never cost you the password
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

**Lost key.** If the store is `keychain`, the keychain item is gone and no key file exists, the old key cannot be recovered. `ts-cli secrets use keychain` then creates a new key, removes the accounts and saved SSH passwords that were encrypted with the old one, and prints exactly what was removed (add the accounts again with `ts-cli account add`; passwords are asked again on the next connection).

**Manage:** Press `o` → "Machines with saved passwords" to view or delete stored credentials.

## Commands

### `login`

```bash
ts-cli login --tailnet=<tailnet-name> [--name=<name>] [--api-key-stdin | --api-key=<key>]
```

Same as `account add`, and marks the account active. The key may also come from `TAILSCALE_API_KEY`.

### `account`

`add`, `list`, `edit`, `use`, `hide`, `show`, `remove`. See [Multi-Account Setup](#multi-account-setup).

### `list`

```bash
ts-cli list [filters] [--sort=<key>] [--format=<table|json|names>] [-q] [--tailnet=<name>] [--api-key=<key>]
```

Filters are optional and combine with AND.

| Flag | Description |
|------|-------------|
| `--online` / `--offline` | Only online (connected, or seen within 5 minutes) or offline devices; mutually exclusive |
| `--os <name>` | OS equals `<name>`, case-insensitive (`linux`, `macOS`, `windows`, ...) |
| `--tag <tag>` | Device has the tag; `tag:` prefix optional; repeatable, all must match |
| `--account <name>` | Device belongs to the account (case-insensitive); repeatable, any of |
| `--name <text>` | Case-insensitive substring of the name or hostname |
| `--sort <key>` | `name` (default), `lastseen` (most recent first), `os`, `account` |
| `--format <fmt>` | `table` (default), `json`, or `names` (one hostname per line, for scripts) |
| `-q`, `--quiet` | Table without header and footer |

A `tags` column appears in the table only when some device has tags. JSON output includes `tags` (array) and `online` (bool). When nothing matches, `No devices match the filters.` is printed to stderr, the exit status is 0, and `--format json` prints `[]`.

```bash
ts-cli list --online --os linux
ts-cli list --tag tag:server --format names
ts-cli list --account work --format json
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
│   ├── account.go       # account add/list/edit/use/hide/show/remove
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
