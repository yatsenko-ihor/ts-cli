# Tailscale CLI Project Learnings

## Project Context

This document captures key learnings from building a production-ready Tailscale CLI tool in Go, featuring command-line interface, interactive terminal UI, and SSH integration.

## Technical Stack Evolution

### Framework Migration: mitchellh/cli → cobra

**Initial Choice**: Started with `mitchellh/cli` for simplicity
**Migration**: Switched to `spf13/cobra` for better features

**Reasons for Migration**:

- Better shell completion support (bash, zsh, fish, PowerShell)
- More active maintenance and community
- Richer flag handling with pflag integration
- Built-in help generation
- Command aliasing support

**Migration Pattern**:

```go
// Before (mitchellh/cli)
type LoginCommand struct{}
func (c *LoginCommand) Run(args []string) int { /* ... */ }

// After (cobra)
func NewLoginCommand() *cobra.Command {
    return &cobra.Command{
        Use: "login",
        RunE: func(cmd *cobra.Command, args []string) error { /* ... */ }
    }
}
```

**Key Lesson**: Choose cobra for new CLI projects in Go. It's the de facto standard.

## Bubbletea TUI Integration

### Terminal Suspension for External Commands

**Challenge**: Running SSH from within the TUI requires suspending the terminal UI

**Wrong Approach**:

```go
// ❌ This exits the TUI completely
cmd := exec.Command("ssh", address)
cmd.Stdin = os.Stdin
cmd.Stdout = os.Stdout
cmd.Stderr = os.Stderr
cmd.Run()
return m, tea.Quit()
```

**Correct Approach**:

```go
// ✅ Properly suspends and resumes TUI
func (m model) sshToDevice(address string) tea.Cmd {
    sshCmd := exec.Command("ssh", address)
    return tea.ExecProcess(sshCmd, func(err error) tea.Msg {
        if err != nil {
            return sshMsg{err: err}
        }
        return sshMsg{}
    })
}
```

**Key Lesson**: Use `tea.ExecProcess` for external commands that need terminal control. It properly suspends the alternate screen, runs the command, then resumes the TUI.

### Navigation Patterns

Implemented dual navigation modes:

- Arrow keys (`↑`/`↓`) for general users
- Vim bindings (`k`/`j`) for power users

```go
case "up", "k":
    if m.cursor > 0 { m.cursor-- }
case "down", "j":
    if m.cursor < len(m.devices)-1 { m.cursor++ }
```

**Key Lesson**: Support both arrow keys and vim bindings for broader user appeal.

## Cobra Command Delegation

### Run vs RunE Gotcha

**Problem**: When delegating from one command to another, calling `cmd.Run()` directly causes a nil pointer panic if the command uses `RunE` instead of `Run`.

**Wrong Approach**:
```go
// ❌ Panic! RunE commands don't have Run set
Run: func(cmd *cobra.Command, args []string) {
    interactiveCmd := NewInteractiveCommand()
    interactiveCmd.Run(cmd, args)  // nil pointer dereference!
},
```

**Error Message**:
```
panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation]
goroutine 1 [running]:
github.com/ihor/ts-cli/commands.NewRootCommand.func1(...)
```

**Correct Approach**:
```go
// ✅ Call RunE and handle the error
Run: func(cmd *cobra.Command, args []string) {
    interactiveCmd := NewInteractiveCommand()
    if err := interactiveCmd.RunE(cmd, args); err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
},
```

**Key Lesson**: When delegating between Cobra commands:
- Commands using `RunE` have `Run` set to `nil`
- Always call `RunE` when delegating to a command that returns errors
- Handle errors explicitly in the delegating command
- Consider using `cmd.Execute()` for proper subcommand execution

## API Client Design

### Separation of Concerns

**Pattern**: Keep API logic completely separate from commands

```
client/
  └── tailscale.go    # Pure API client, no CLI logic
commands/
  └── list.go         # CLI logic, uses client package
```

**Benefits**:

- Easier to test API client independently
- Can reuse client in different contexts
- Clear separation between business logic and UI

### Error Handling Strategy

**Pattern**: Wrap errors with context at each layer

```go
// In client
if err != nil {
    return nil, fmt.Errorf("API request failed: %w", err)
}

// In command
if err != nil {
    return fmt.Errorf("failed to list devices: %w", err)
}
```

**Key Lesson**: Use `%w` for error wrapping to preserve error chains.

## Configuration Management

### Security Best Practices

Store sensitive data with restricted permissions:

```go
// Directory: 0700 (rwx------)
os.MkdirAll(configDir, 0700)

// File: 0600 (rw-------)
os.WriteFile(configPath, data, 0600)
```

### Configuration Hierarchy

Implement a priority system for configuration:

1. Command-line flags (highest priority)
2. Environment variables
3. Stored configuration file
4. Default values (lowest priority)

```go
if apiKey == "" {
    storedKey, _, _ := loadConfig()
    apiKey = storedKey
}
if apiKey == "" {
    apiKey = os.Getenv("TAILSCALE_API_KEY")
}
if apiKey == "" {
    return fmt.Errorf("API key required")
}
```

**Key Lesson**: Give users flexibility through multiple configuration methods.

## Git Workflow

### Incremental Commits

Strategy: Commit after each major feature completion

```
Commit 1: Initial MVP with mitchellh/cli
Commit 2: Refactor to cobra framework
Commit 3: Implement interactive TUI (TODO Step 1)
Commit 4: Add SSH capability (TODO Step 2)
Commit 5: (planned) Split-screen layout
```

**Benefits**:

- Clear history of feature development
- Easy to revert specific features
- Reviewable commits

**Key Lesson**: Commit frequently at logical checkpoints, especially when implementing TODO lists step-by-step.

## Build and Test Workflow

### Iterative Development Pattern

1. **Implement**: Write the feature code
2. **Build**: `go build -o ts-cli .`
3. **Test**: `./ts-cli command --help`
4. **Fix**: Address any compilation errors
5. **Verify**: Run the actual command
6. **Commit**: Git commit with descriptive message

### Common Build Issues

**Issue 1**: Unused imports
```
tui/model.go:5:2: "os" imported and not used
```
**Solution**: Remove unused imports immediately

**Issue 2**: Duplicate package declarations
```
package tui
package tui  // Duplicate!
```
**Solution**: Copy-paste carefully; check for duplicates

**Issue 3**: Nil pointer dereference when delegating commands
```
panic: runtime error: invalid memory address or nil pointer dereference
goroutine 1 [running]:
github.com/ihor/ts-cli/commands.NewRootCommand.func1(...)
```
**Solution**: Call `RunE` instead of `Run` when delegating to commands that use `RunE`

**Key Lesson**: Build frequently to catch errors early. Don't accumulate changes.

## Style and UX

### Lipgloss Styling

Create reusable styles for consistent UI:

```go
var (
    titleStyle = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("#7D56F4"))

    selectedStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#FF06B7")).
        Bold(true)
)
```

### Help Text Best Practices

Provide comprehensive examples in help:

```go
Example: `  # Example with description
  ts-cli ssh laptop.example.com

  # Another example
  ts-cli ssh laptop --user=admin`,
```

**Key Lesson**: Good examples are worth more than lengthy explanations.

## Command Design Patterns

### Default Behavior

Make the most common action the default:

```go
rootCmd := &cobra.Command{
    Run: func(cmd *cobra.Command, args []string) {
        // Run interactive mode by default
        NewInteractiveCommand().Run(cmd, args)
    },
}
```

**Key Lesson**: Reduce friction for the primary use case.

### Command Aliases

Provide short aliases for frequently used commands:

```go
cmd := &cobra.Command{
    Use:     "interactive",
    Aliases: []string{"i", "tui"},
}
```

**Key Lesson**: Support both full names and short aliases.

## Development Practices

### Documentation Updates

Update README.md as features are added:

- Add new commands to usage section
- Update feature list
- Add examples for new functionality
- Keep project structure diagram current

### TODO Management

Use TODO.md for tracking multi-step features:

- Break complex features into checkboxes
- Mark items as completed: `- [x]`
- Keep it visible in project root

## Performance Considerations

### API Client Timeout

Always set timeouts for HTTP clients:

```go
httpClient: &http.Client{
    Timeout: 30 * time.Second,
}
```

**Key Lesson**: Prevent indefinite hangs with reasonable timeouts.

## Future Improvements

Based on TODO.md, next step is split-screen layout:

- Left panel: device list with search (top) + details (bottom)
- Right panel: SSH session view
- Vim-style search navigation

**Implementation Notes**:

- Will need to refactor model to support multiple panels
- Consider using bubbletea's `tea.WindowSizeMsg` for responsive layout
- May need to track panel focus state

## Key Takeaways

1. **Cobra**: Use it for Go CLIs - it's the standard
2. **Bubbletea**: Great for interactive TUIs; use `tea.ExecProcess` for external commands
3. **Security**: Always set proper file permissions (0600 for files, 0700 for dirs)
4. **Error Handling**: Use `%w` for error wrapping; return errors, don't exit
5. **Testing**: Build and test frequently, not just at the end
6. **UX**: Support both arrow keys and vim bindings; provide good examples
7. **Git**: Commit at logical checkpoints with descriptive messages
8. **Documentation**: Keep README updated as features are added

## Tools and Versions

- Go: 1.24.0
- Cobra: v1.8.1
- Bubbletea: v1.3.10
- Lipgloss: v1.1.0
- Tailscale API: v2
