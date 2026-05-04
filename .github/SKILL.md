---
name: .github
description: Building production-ready CLI applications in Go using cobra framework and bubbletea TUI. Use this skill for Go CLI projects involving command-line interfaces, terminal UIs, API client integration, and SSH connectivity.
---

# Go CLI Development Skill

## Overview

This skill provides expertise in building modern, production-ready command-line interface (CLI) applications in Go, featuring:

- **Cobra framework** for robust command structure and flag parsing
- **Bubbletea** for interactive terminal user interfaces (TUI)
- **API client integration** with proper error handling and configuration management
- **SSH connectivity** with terminal suspension/resumption
- **Shell completion** support for bash, zsh, fish, and PowerShell

## Architecture Patterns

### 1. Command Structure

Use cobra's command pattern with clear separation of concerns:

```go
// commands/root.go - Root command registration
func NewRootCommand() *cobra.Command {
    rootCmd := &cobra.Command{
        Use:   "app-name",
        Short: "Brief description",
        Long:  "Detailed description",
        Run: func(cmd *cobra.Command, args []string) {
            // Default behavior when no subcommand
        },
    }

    // Register subcommands
    rootCmd.AddCommand(NewSubCommand1())
    rootCmd.AddCommand(NewSubCommand2())

    return rootCmd
}
```

### 2. API Client Layer

Separate API logic into its own package:

```go
// client/api.go
type Client struct {
    apiKey     string
    baseURL    string
    httpClient *http.Client
}

func NewClient(apiKey string) *Client {
    return &Client{
        apiKey:     apiKey,
        baseURL:    "https://api.example.com",
        httpClient: &http.Client{Timeout: 30 * time.Second},
    }
}

func (c *Client) doRequest(method, path string, body interface{}) ([]byte, error) {
    // Centralized request handling with error management
}
```

### 3. Configuration Management

Store configuration securely with appropriate permissions:

```go
func storeConfig(apiKey, value string) error {
    homeDir, _ := os.UserHomeDir()
    configDir := filepath.Join(homeDir, ".app-name")

    // Create directory with restricted permissions
    if err := os.MkdirAll(configDir, 0700); err != nil {
        return err
    }

    configPath := filepath.Join(configDir, "config")
    config := fmt.Sprintf("api_key=%s\nvalue=%s\n", apiKey, value)

    // Write file with restricted permissions
    return os.WriteFile(configPath, []byte(config), 0600)
}
```

### 4. Interactive TUI with Bubbletea

Implement the Elm architecture pattern:

```go
// tui/model.go
type model struct {
    items    []Item
    cursor   int
    selected int
}

func (m model) Init() tea.Cmd {
    return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "up", "k":
            if m.cursor > 0 {
                m.cursor--
            }
        case "down", "j":
            if m.cursor < len(m.items)-1 {
                m.cursor++
            }
        case "enter":
            m.selected = m.cursor
        }
    }
    return m, nil
}

func (m model) View() string {
    // Render the UI using lipgloss for styling
}
```

### 5. SSH Integration with TUI

Use `tea.ExecProcess` to suspend the TUI and run SSH:

```go
func (m model) sshToDevice(address string) tea.Cmd {
    sshCmd := exec.Command("ssh", address)
    return tea.ExecProcess(sshCmd, func(err error) tea.Msg {
        if err != nil {
            return errorMsg{err}
        }
        return successMsg{}
    })
}
```

## Key Dependencies

```go
// go.mod
require (
    github.com/spf13/cobra v1.8.1         // CLI framework
    github.com/charmbracelet/bubbletea v1.3.10  // TUI framework
    github.com/charmbracelet/lipgloss v1.1.0    // Terminal styling
)
```

## Command Implementation Pattern

Each command should follow this structure:

```go
func NewCommandName() *cobra.Command {
    var flagVar string

    cmd := &cobra.Command{
        Use:   "command [args]",
        Short: "Brief description",
        Long:  "Detailed description with usage context",
        Example: `  # Example 1
  app command arg1

  # Example 2
  app command arg2 --flag=value`,
        Args: cobra.ExactArgs(1), // or other validator
        RunE: func(cmd *cobra.Command, args []string) error {
            // Command logic here
            // Return errors instead of calling os.Exit()
            return nil
        },
    }

    cmd.Flags().StringVar(&flagVar, "flag", "", "Flag description")

    return cmd
}
```

## Error Handling Best Practices

1. **Use RunE instead of Run** to return errors properly
2. **Wrap errors with context**: `fmt.Errorf("operation failed: %w", err)`
3. **Validate inputs early** before making API calls
4. **Provide helpful error messages** with actionable suggestions

## Testing Approach

- Build frequently: `go build -o app .`
- Test commands individually: `./app command --help`
- Verify shell completion: `./app completion bash`
- Test TUI interactively in a real terminal

## Security Considerations

- Store credentials with 0600 permissions
- Create config directories with 0700 permissions
- Support environment variables for sensitive data
- Never log or display API keys in output

## Project Structure

```
app/
├── main.go              # Entry point
├── go.mod               # Dependencies
├── commands/
│   ├── root.go         # Root command
│   ├── cmd1.go         # Subcommand 1
│   └── cmd2.go         # Subcommand 2
├── client/
│   └── api.go          # API client
├── internal/
│   ├── constants/      # All magic values (timeouts, URLs, messages)
│   ├── errors/         # Typed error types with AppError struct
│   ├── formatters/     # Display formatting utilities
│   └── services/       # Business logic layer
├── tui/
│   ├── model.go        # Model struct, Init, Update, View (thin orchestration)
│   ├── view.go         # All rendering functions
│   ├── handlers.go     # Key event handlers grouped by mode
│   ├── layout.go       # Size/dimension calculations
│   ├── device_utils.go # Device filtering, sorting, status helpers
│   ├── messages.go     # Message types, panelFocus enum
│   ├── styles.go       # Lipgloss styles, layout constants, frame titles
│   └── utils.go        # Shared rendering utilities (applyFrameTitle, etc.)
└── README.md
```

**Key Rule**: When a TUI file grows past ~300 lines, split by responsibility. All files share the same package, so no import changes needed.

## TUI Patterns Specific to This Project

### Frame Title with Bold on Focus

```go
// utils.go
func applyFrameTitle(frame, title string, borderColor lipgloss.Color, bold bool) string {
    titleStyle := lipgloss.NewStyle().Foreground(borderColor)
    if bold {
        titleStyle = titleStyle.Bold(true)
    }
    // splice title into first border line
}

// view.go - caller passes active focus check
return applyFrameTitle(listPanel, listFrameTitle, borderColor, m.activeFocus == focusList)
```

### Handler Map Pattern for Key Events

```go
// handlers.go
var normalModeHandlers = map[string]func(m model) model{
    "up": func(m model) model { /* ... */ },
    "k":  func(m model) model { /* ... */ },
}

func (m model) handleNormalMode(key string) model {
    if handler, ok := normalModeHandlers[key]; ok {
        return handler(m)
    }
    return m
}
```

## Common Patterns

### Aliases for Commands

```go
cmd := &cobra.Command{
    Use:     "interactive",
    Aliases: []string{"i", "tui"},
    // ...
}
```

### Loading Configuration

```go
func loadConfig() (string, string, error) {
    homeDir, _ := os.UserHomeDir()
    configPath := filepath.Join(homeDir, ".app-name", "config")

    data, err := os.ReadFile(configPath)
    if err != nil {
        return "", "", err
    }

    // Parse configuration
    return parseConfig(string(data))
}
```

### Environment Variable Fallback

```go
if apiKey == "" {
    apiKey = os.Getenv("API_KEY")
}
if apiKey == "" {
    return fmt.Errorf("API key required")
}
```

## When to Use This Skill

- Building CLI tools in Go
- Creating interactive terminal applications
- Integrating with REST APIs from CLI
- Adding SSH or other external command execution
- Implementing command-line workflows with multiple subcommands
- Creating developer tools with rich terminal UIs
