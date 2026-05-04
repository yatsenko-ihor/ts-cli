# ts-cli Architecture Documentation

## Table of Contents

1. [Overview](#overview)
2. [High-Level Architecture](#high-level-architecture)
3. [Layer Descriptions](#layer-descriptions)
4. [Data Flow](#data-flow)
5. [Design Patterns](#design-patterns)
6. [Package Structure](#package-structure)
7. [Dependencies](#dependencies)
8. [Concurrency Model](#concurrency-model)
9. [Error Handling Strategy](#error-handling-strategy)
10. [Configuration Management](#configuration-management)
11. [Testing Strategy](#testing-strategy)

---

## Overview

ts-cli is a command-line interface tool for managing Tailscale devices via the Tailscale REST API. The architecture follows SOLID principles and clean code practices with a clear separation between presentation, business logic, and data access layers.

### Core Technologies

- **Language:** Go 1.21+
- **CLI Framework:** [Cobra](https://github.com/spf13/cobra)
- **TUI Framework:** [Bubbletea](https://github.com/charmbracelet/bubbletea)
- **Architecture:** Layered (3-tier)
- **Concurrency:** Goroutines with sync primitives

---

## High-Level Architecture

```
┌────────────────────────────────────────────────────────────┐
│                     User.                                  │
└───────────────────────┬────────────────────────────────────┘
                        │
                        ▼
┌────────────────────────────────────────────────────────────┐
│                  PRESENTATION LAYER                        │
│                    (commands/)                             │
│                                                            │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│  │  login   │ │   list   │ │   ssh    │ │   tui    │       │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘       │
│                                                            │
│  Responsibilities:                                         │
│  • Handle user input (CLI/TUI)                             │
│  • Parse flags and arguments                               │
│  • Display formatted output                                │
│  • Coordinate service calls                                │
└───────────────────────┬────────────────────────────────────┘
                        │
                        ▼
┌────────────────────────────────────────────────────────────┐
│                   SERVICE LAYER                            │
│               (internal/services/)                         │
│                                                            │
│  ┌──────────────────┐     ┌──────────────────┐             │
│  │  DeviceService   │     │  ConfigService   │             │
│  │                  │     │                  │             │
│  │ • ListDevices    │     │ • Load           │             │
│  │ • FindDevice     │     │ • Save           │             │
│  │ • ValidateAPIKey │     │ • AddAccount     │             │
│  └──────────────────┘     └──────────────────┘             │
│                                                            │
│  Responsibilities:                                         │
│  • Implement business logic                                │
│  • Orchestrate data operations                             │
│  • Validate inputs                                         │
│  • Aggregate data from multiple sources                    │
└───────────────────────┬────────────────────────────────────┘
                        │
                        ▼
┌────────────────────────────────────────────────────────────┐
│                DATA ACCESS LAYER                           │
│                   (client/)                                │
│                                                            │
│  ┌────────────────────────────────────┐                    │
│  │     TailscaleClient                │                    │
│  │                                    │                    │
│  │  • HTTP request handling           │                    │
│  │  • Response parsing                │                    │
│  │  • Authentication management       │                    │
│  └────────────────────────────────────┘                    │
│                                                            │
│  Responsibilities:                                         │
│  • Make HTTP API calls                                     │
│  • Parse API responses                                     │
│  • Handle network errors                                   │
└───────────────────────┬────────────────────────────────────┘
                        │
                        ▼
┌────────────────────────────────────────────────────────────┐
│                   Tailscale API                            │
│                 api.tailscale.com                          │
└────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────┐
│                   SUPPORT MODULES                          │
│                                                            │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│  │constants │ │  errors  │ │formatters│ │   util   │       │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘       │
└────────────────────────────────────────────────────────────┘
```

---

## Layer Descriptions

### 1. Presentation Layer (`commands/`)

**Purpose:** Handles all user interaction through CLI commands and TUI.

**Components:**

- `root.go` - Root command and subcommand registration
- `login.go` - Authentication and account management
- `list.go` - List devices in table/JSON format
- `interactive.go` - Launch interactive TUI
- `ssh.go` - SSH into devices
- `up.go` - Bring up Tailscale connection
- `account.go` - Manage multiple accounts
- `install.go` - Installation helpers

**Characteristics:**

- Thin layer (minimal logic)
- Uses services for all business operations
- Handles only CLI-specific concerns (flags, output)
- No direct API client usage

**Example Pattern:**

```go
func NewListCommand() *cobra.Command {
    cmd := &cobra.Command{
        RunE: func(cmd *cobra.Command, args []string) error {
            // 1. Load config via service
            configService, _ := services.NewConfigService()
            config, err := configService.Load()

            // 2. Create device service
            deviceService := services.NewDeviceService(apiKey)

            // 3. Fetch devices
            devices, err := deviceService.ListDevices(tailnet)

            // 4. Format output
            formatter := formatters.NewDeviceFormatter()
            output := formatter.FormatAsTable(devices)

            // 5. Display
            fmt.Print(output)
            return nil
        },
    }
    return cmd
}
```

### 2. Service Layer (`internal/services/`)

**Purpose:** Contains all business logic and orchestrates data operations.

#### DeviceService (`device_service.go`)

**Responsibilities:**

- Fetch devices from single or multiple tailnets
- Perform concurrent multi-account queries
- Find devices by various identifiers
- Validate API keys
- Extract device information

**Key Methods:**

```go
type DeviceService struct {
    client *client.Client
}

// Core operations
func (s *DeviceService) ListDevices(tailnet string) ([]Device, error)
func (s *DeviceService) ListDevicesFromMultipleAccounts(accounts []AccountInfo) []Device
func (s *DeviceService) FindDeviceByIdentifier(devices []Device, identifier string) *Device
func (s *DeviceService) ValidateAPIKey(tailnet string) error
func (s *DeviceService) GetDevicePrimaryAddress(device *Device) (string, error)
```

**Concurrency:**

- Uses goroutines for parallel account queries
- Thread-safe with mutex protection
- WaitGroup for synchronization

#### ConfigService (`config_service.go`)

**Responsibilities:**

- Load/save configuration files
- Manage accounts (add, update, retrieve)
- Migrate from old config format
- Ensure secure file permissions

**Key Methods:**

```go
type ConfigService struct {
    configPath string
}

func (cs *ConfigService) Load() (*Config, error)
func (cs *ConfigService) Save(config *Config) error
func (cs *ConfigService) AddOrUpdateAccount(config *Config, name, apiKey, tailnet string) (bool, error)
func (cs *ConfigService) GetActiveAccount(config *Config) *Account
```

**Security:**

- Files created with 0600 permissions
- Config directory with 0700 permissions
- No credentials logged

### 3. Data Access Layer (`client/`)

**Purpose:** Handles all HTTP communication with Tailscale API.

**Responsibilities:**

- Make authenticated HTTP requests
- Parse JSON responses
- Handle HTTP errors
- Manage connection timeouts

**Key Methods:**

```go
type Client struct {
    apiKey     string
    httpClient *http.Client
}

func NewClient(apiKey string) *Client
func (c *Client) ValidateAPIKey(tailnet string) error
func (c *Client) ListDevices(tailnet string) ([]Device, error)
```

**Configuration:**

- Timeout: `constants.API_TIMEOUT` (30 seconds)
- Base URL: `constants.API_BASE_URL`
- Authentication: Bearer token

### 4. Support Modules

#### Constants (`internal/constants/`)

- All hardcoded values
- SCREAMING_SNAKE_CASE naming
- Grouped by category

#### Errors (`internal/errors/`)

- Typed error system
- Error constructors and checkers
- Error wrapping with context

#### Formatters (`internal/formatters/`)

- Output formatting (table, JSON)
- Status message formatting
- Time formatting utilities

#### Utilities (`util/`)

- Command history management
- Input validation and sanitization
- Helper functions

---

## Data Flow

### Example: Listing Devices

```
User executes: ts-cli list

1. commands/list.go
   ├─ Parse flags
   └─ Call ConfigService.Load()

2. internal/services/config_service.go
   ├─ Read ~/.ts-cli/config.json
   ├─ Parse JSON
   └─ Return Config with accounts

3. commands/list.go
   └─ Call DeviceService.ListDevicesFromMultipleAccounts()

4. internal/services/device_service.go
   ├─ Create goroutines for each account
   ├─ Each goroutine:
   │  ├─ Create Client
   │  ├─ Call client.ListDevices()
   │  └─ Return devices
   └─ Aggregate results with mutex

5. client/tailscale.go
   ├─ Make HTTP GET to /tailnet/{name}/devices
   ├─ Parse JSON response
   └─ Return []Device

6. commands/list.go
   └─ Call formatter.FormatAsTable(devices)

7. internal/formatters/device_formatter.go
   ├─ Create table with tabwriter
   └─ Return formatted string

8. commands/list.go
   └─ Print to stdout

User sees formatted table
```

---

## Design Patterns

### Factory Pattern

Used for creating service instances:

```go
func NewDeviceService(apiKey string) *DeviceService
func NewConfigService() (*ConfigService, error)
func NewDeviceFormatter() *DeviceFormatter
```

### Repository Pattern

ConfigService acts as a repository for configuration:

```go
Load() (*Config, error)
Save(config *Config) error
```

### Strategy Pattern

Formatters use strategy for different output formats:

```go
FormatAsTable(devices) string
FormatAsJSON(devices) (string, error)
```

### Singleton Pattern

Client instances can be reused:

```go
client := NewClient(apiKey)
// Reuse for multiple operations
```

### Concurrent Pattern

Multi-account device fetching:

```go
var (
    results []Device
    mu      sync.Mutex
    wg      sync.WaitGroup
)

for _, account := range accounts {
    wg.Add(1)
    go func(acc Account) {
        defer wg.Done()
        devices := fetchDevices(acc)
        mu.Lock()
        results = append(results, devices...)
        mu.Unlock()
    }(account)
}

wg.Wait()
```

---

## Package Structure

```
ts-cli/
│
├── main.go                      # Entry point
│
├── client/                      # Data Access Layer
│   └── tailscale.go            # Tailscale API client
│
├── commands/                    # Presentation Layer
│   ├── root.go                 # Root command
│   ├── login.go                # Authentication
│   ├── list.go                 # List devices
│   ├── interactive.go          # TUI mode
│   ├── ssh.go                  # SSH connection
│   ├── up.go                   # Connection management
│   ├── account.go              # Account management
│   ├── install.go              # Installation
│   ├── config.go               # Legacy config (deprecated)
│   └── tailscale_check.go      # Daemon status check
│
├── internal/                    # Private packages
│   ├── constants/              # Application constants
│   │   └── constants.go
│   │
│   ├── services/               # Business Logic Layer
│   │   ├── device_service.go  # Device operations
│   │   └── config_service.go  # Configuration operations
│   │
│   ├── errors/                 # Error handling
│   │   └── errors.go          # Typed errors
│   │
│   └── formatters/             # Output formatting
│       └── device_formatter.go
│
├── tui/                        # Terminal UI
│   ├── model.go               # Bubbletea model
│   └── model_test.go
│
├── util/                       # Utilities
│   ├── history.go             # Command history
│   └── validation.go          # Input validation
│
└── .copilot/                   # AI Agent configuration
    ├── roles/
    │   └── go-architect.md
    ├── skills/
    │   └── go-patterns.md
    ├── learnings/
    │   └── project-decisions.md
    └── AGENTS.md
```

---

## Dependencies

### Direct Dependencies

```
github.com/spf13/cobra         # CLI framework
github.com/charmbracelet/bubbletea  # TUI framework
```

### Dependency Principles

- **Minimize external dependencies** - Use standard library when possible
- **Pin versions** - Lock versions in go.mod for reproducibility
- **Security first** - Regularly audit dependencies
- **No transitive hell** - Avoid dependencies with many dependencies

---

## Concurrency Model

### Multi-Account Device Fetching

```go
func (s *DeviceService) ListDevicesFromMultipleAccounts(accounts []AccountInfo) []Device {
    var (
        allDevices []Device
        mu         sync.Mutex  // Protects shared slice
        wg         sync.WaitGroup  // Waits for all goroutines
    )

    for _, account := range accounts {
        wg.Add(1)
        go func(acc AccountInfo) {
            defer wg.Done()

            // Each goroutine has its own service instance
            deviceService := NewDeviceService(acc.APIKey)
            devices, err := deviceService.ListDevices(acc.Tailnet)

            if err != nil {
                // Log error, don't block other accounts
                fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
                return
            }

            // Safely append to shared slice
            mu.Lock()
            allDevices = append(allDevices, devices...)
            mu.Unlock()
        }(account)
    }

    wg.Wait()  // Block until all goroutines complete
    return allDevices
}
```

### Concurrency Principles

1. **Fail-safe:** Errors in one goroutine don't affect others
2. **Thread-safe:** Use mutex to protect shared data
3. **Clean shutdown:** Use WaitGroup for proper synchronization
4. **No data races:** All shared data access is protected

---

## Error Handling Strategy

### Typed Errors

```go
// Create typed errors
err := errors.NewAPIError("failed to fetch devices", originalErr)
err := errors.NewNotFoundError("device", deviceName)

// Check error types
if errors.IsNotFoundError(err) {
    // Handle not found case
}
```

### Error Wrapping

```go
if err != nil {
    return fmt.Errorf("failed to list devices from %s: %w", tailnet, err)
}
```

### Error Hierarchy

```
Error Types:
├── ValidationError  (user input issues)
├── APIError        (API communication issues)
├── ConfigError     (configuration issues)
├── NetworkError    (network issues)
├── NotFoundError   (resource not found)
└── PermissionError (authorization issues)
```

### Error Handling at Each Layer

**Presentation Layer:**

- Catch all errors from services
- Format user-friendly messages
- Exit with appropriate codes

**Service Layer:**

- Return typed errors
- Add context with wrapping
- Log errors at boundaries

**Data Access Layer:**

- Convert HTTP errors to typed errors
- Include response details
- Don't swallow errors

---

## Configuration Management

### Configuration File Structure

```json
{
    "accounts": [
        {
            "name": "personal",
            "api_key": "tskey-api-...",
            "tailnet": "example.com",
            "active": true
        }
    ],
    "ssh_username": "admin",
    "config_version": "1.0"
}
```

### Configuration Loading Flow

```
1. Check ~/.ts-cli/config.json exists
2. If not, attempt migration from old format
3. If no old format, return empty config
4. Parse JSON
5. Validate structure
6. Return Config object
```

### Security Considerations

- File permissions: `0600` (owner read/write only)
- Directory permissions: `0700` (owner access only)
- No credentials in logs
- Support for environment variables

---

## Testing Strategy

### Unit Tests

**Services:**

```go
func TestDeviceService_ListDevices(t *testing.T) {
    mockClient := &MockClient{}
    service := &DeviceService{client: mockClient}

    devices, err := service.ListDevices("example.com")

    assert.NoError(t, err)
    assert.Len(t, devices, 5)
}
```

**Table-Driven Tests:**

```go
func TestConfigService_AddOrUpdateAccount(t *testing.T) {
    tests := []struct {
        name     string
        existing []Account
        newName  string
        wantNew  bool
    }{
        {"new account", []Account{}, "test", true},
        {"existing account", existingAccounts, "test", false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test logic
        })
    }
}
```

### Integration Tests

```go
func TestListCommand_Integration(t *testing.T) {
    // Set up test config
    // Execute command
    // Verify output
}
```

### Test Coverage Goals

- Services: 80%+
- Commands: 70%+
- Client: 80%+
- Formatters: 90%+

---

## Summary

ts-cli follows a clean, layered architecture that:

✅ Separates concerns (presentation, business logic, data access)
✅ Follows SOLID principles
✅ Uses typed errors for better error handling
✅ Supports concurrent operations safely
✅ Maintains security best practices
✅ Is testable at all layers
✅ Has clear data flow
✅ Uses well-established design patterns

This architecture enables:

- **Maintainability** - Easy to understand and modify
- **Testability** - Each layer can be tested independently
- **Scalability** - Easy to add new features
- **Performance** - Concurrent operations where beneficial
- **Security** - Proper credential handling and validation
