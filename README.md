# Tailscale CLI (ts-cli)

A command-line interface tool for managing Tailscale devices and resources via the Tailscale REST API.

## Features

- **Authentication**: Securely validate and store your Tailscale API key
- **Device Management**: List and view all devices in your Tailscale tailnet
- **SSH Integration**: Connect to devices via SSH with a single command
- **Interactive TUI**: Browse devices with an intuitive terminal interface
- **Shell Completion**: Built-in support for bash, zsh, fish, and powershell
- Clean and intuitive CLI interface powered by `cobra`

## Prerequisites

- Go 1.23 or later
- A Tailscale account with API access
- A valid Tailscale API key (get one from https://login.tailscale.com/admin/settings/keys)

## Installation

### Build from source

```bash
# Clone the repository
cd /Users/ihor/Development/ts-cli

# Download dependencies
go mod download

# Build the application
go build -o ts-cli .
```

This will create a `ts-cli` binary in the current directory.

### Install globally (optional)

```bash
go install
```

This will install `ts-cli` to your `$GOPATH/bin` directory.

## Usage

### Authentication

Before using the CLI, you need to authenticate with your Tailscale API key:

```bash
# Set the API key as an environment variable
export TAILSCALE_API_KEY=tskey-api-xxxxx

# Login and validate the key
./ts-cli login --tailnet=example.com
```

Or provide the API key directly:

```bash
./ts-cli login --api-key=tskey-api-xxxxx --tailnet=example.com
```

**Note**: Replace `example.com` with your actual tailnet name (e.g., `mycompany.com` or `user@example.com`).

The login command will:

1. Validate your API key against the Tailscale API
2. Store the configuration locally in `~/.ts-cli/config`

### List Devices

After authentication, list all devices in your tailnet:

```bash
./ts-cli list
```

#### Output format

By default, devices are displayed in a formatted table:

```
NAME                HOSTNAME            ADDRESS         OS       LAST SEEN      AUTHORIZED
----                --------            -------         --       ---------      ----------
laptop.example.com  laptop              100.64.0.1      linux    2 hours ago    Yes
phone.example.com   phone               100.64.0.2      iOS      just now       Yes

Total devices: 2
```

#### JSON output

For programmatic use, you can output in JSON format:

```bash
./ts-cli list --format=json
```

#### Override tailnet

You can override the stored tailnet configuration:

```bash
./ts-cli list --tailnet=different-tailnet.com
```

## Configuration

The CLI stores configuration in `~/.ts-cli/config`. This file contains:

- Your Tailscale API key
- Your tailnet name

You can always override these values using command-line flags.

## Commands

### `login`

Validate and store your Tailscale API credentials.

```bash
ts-cli login --tailnet=<tailnet-name> [--api-key=<key>]
```

**Flags:**

- `--api-key`: Tailscale API key (optional if `TAILSCALE_API_KEY` env var is set)
- `--tailnet`: Your tailnet name (required)

### `list`

List all devices in your Tailscale tailnet.

```bash
ts-cli list [--format=<table|json>] [--tailnet=<name>]
```

**Flags:**

- `--format`: Output format - `table` (default) or `json`
- `--tailnet`: Override the configured tailnet name
- `--api-key`: Override the configured API key

### `ssh`

Open an SSH connection to a Tailscale device.

```bash
ts-cli ssh <device-name-or-hostname> [--user=<username>]
```

**Arguments:**

- `device-name-or-hostname`: The name or hostname of the device to connect to

**Flags:**

- `--user`: SSH user (default: current user)
- `--tailnet`: Override the configured tailnet name
- `--api-key`: Override the configured API key

**Examples:**

```bash
# SSH to a device by name
ts-cli ssh laptop.example.com

# SSH with custom user
ts-cli ssh laptop.example.com --user=admin

# SSH using device hostname
ts-cli ssh laptop
```

### `interactive` (or `i`, `tui`)

Launch an interactive terminal UI to browse and manage devices with split-screen support.

```bash
ts-cli interactive
ts-cli i      # Short alias
ts-cli tui    # Alternative alias
```

**Interactive Controls:**

- `↑/k`: Move cursor up
- `↓/j`: Move cursor down
- `Enter`: Select device to view details
- `/`: Enter search mode (vim-style)
- `s`: SSH to selected device (prompts for username if not configured)
- `c`: Copy SSH command to clipboard
- `Tab`: Toggle split-screen SSH panel
- `q`: Quit

**Features:**

- **Split-Screen Layout**: View device list on the left and SSH details on the right
- **Vim-Style Search**: Press `/` to search devices by name, hostname, OS, or IP address
- **Device Status Icons**: 🟢 Online (active within 5 minutes) / 🔴 Offline
- **Scrollable List**: Navigate through many devices with automatic viewport scrolling
- **SSH Username Memory**: Configure SSH username once, use automatically for all connections
- **Clipboard Integration**: Copy SSH commands with `c` key (works on macOS, Linux, Windows)

**Split-Screen Mode:**

When terminal width > 80 columns, the split-screen mode shows:
- **Left Panel**: Device list with search and selection
- **Right Panel**: SSH connection details including:
  - Selected device information
  - Formatted SSH command
  - Connection instructions
  - Username configuration status

Press `Tab` to toggle the SSH panel visibility.

**Note**: When you run `ts-cli` without any subcommand, it defaults to interactive mode.

## Project Structure

```
ts-cli/
├── main.go              # Application entry point
├── go.mod               # Go module definition
├── go.sum               # Go module checksums
├── client/
│   └── tailscale.go     # Tailscale API client implementation
├── commands/
│   ├── root.go          # Root command and CLI setup
│   ├── login.go         # Login command implementation
│   ├── list.go          # List devices command implementation
│   ├── ssh.go           # SSH connection command
│   └── interactive.go   # Interactive TUI command
├── tui/
│   └── model.go         # Bubbletea TUI model and view logic
└── README.md            # This file
```

## Architecture

- **main.go**: Initializes the CLI application using Cobra framework
- **commands/root.go**: Defines the root command and registers subcommands
- **client/tailscale.go**: Handles all interactions with the Tailscale REST API
    - HTTP client setup
    - Authentication
    - API request/response handling
    - Error handling and JSON parsing
- **commands/**: Individual command implementations
    - Each command is a `*cobra.Command`
    - Uses Cobra's flag system for argument parsing
    - Implements RunE for execution with error handling

## Security

- API keys are stored with restricted permissions (0600) in `~/.ts-cli/config`
- The config directory is created with restricted permissions (0700)
- API keys can be provided via environment variables to avoid storing them on disk

## Development

### Shell Completion

Cobra provides built-in shell completion. Generate completion scripts for your shell:

```bash
# Bash
./ts-cli completion bash > /etc/bash_completion.d/ts-cli

# Zsh
./ts-cli completion zsh > "${fpath[1]}/_ts-cli"

# Fish
./ts-cli completion fish > ~/.config/fish/completions/ts-cli.fish

# PowerShell
./ts-cli completion powershell > ts-cli.ps1
```

### Running tests

```bash
go test ./...
```

### Adding new commands

1. Create a new file in the `commands/` directory
2. Create a function that returns `*cobra.Command`
3. Register the command in `commands/root.go` by adding it to the root command

Example:

```go
func NewMyCommand() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "mycommand",
        Short: "Short description",
        RunE: func(cmd *cobra.Command, args []string) error {
            // Implementation
            return nil
        },
    }
    return cmd
}
```

## API Documentation

This CLI uses the Tailscale API v2. For more information about available endpoints and capabilities, refer to:

- https://tailscale.com/api

## License

BSD 3-Clause License (same as Tailscale)

## Support

For issues and questions:

- Tailscale API documentation: https://tailscale.com/kb/1101/api
- Tailscale devices management: https://tailscale.com/kb/1372/manage-devices
