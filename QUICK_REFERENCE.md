# ts-cli Quick Reference Guide

## 🚀 Quick Start for Developers

This guide helps you quickly find what you need in the refactored codebase.

---

## 📖 Documentation Map

**Want to understand the architecture?**
→ Read [ARCHITECTURE.md](./ARCHITECTURE.md)

**Want to know what changed?**
→ Read [CHANGELOG.md](./CHANGELOG.md)

**Want to refactor existing code?**
→ Read [REFACTORING_GUIDE.md](./REFACTORING_GUIDE.md)

**Want a high-level overview?**
→ Read [REFACTORING_SUMMARY.md](./REFACTORING_SUMMARY.md)

**Want to use AI agents?**
→ Read [.copilot/AGENTS.md](./.copilot/AGENTS.md)

---

## 🗂️ File Locations

### Where to Find...

**Constants (URLs, timeouts, error messages):**

```
internal/constants/constants.go
```

**Business Logic (device operations, validation):**

```
internal/services/device_service.go     # Device operations
internal/services/config_service.go     # Configuration
```

**Error Types (typed errors):**

```
internal/errors/errors.go
```

**Output Formatting (table, JSON):**

```
internal/formatters/device_formatter.go
```

**CLI Commands:**

```
commands/
├── root.go         # Root command
├── login.go        # Authentication
├── list.go         # List devices
├── interactive.go  # TUI mode
└── ssh.go          # SSH connection
```

**API Client:**

```
client/tailscale.go
```

**AI Agent Configuration:**

```
.copilot/
├── AGENTS.md                        # Custom agents
├── roles/go-architect.md            # Agent role
├── skills/go-patterns.md            # Patterns
└── learnings/project-decisions.md   # Decisions
```

---

## 🔨 Common Tasks

### Adding a New Constant

**File:** `internal/constants/constants.go`

```go
const (
    // Add your constant here
    NEW_TIMEOUT = 60 * time.Second
    NEW_ERROR_MESSAGE = "something went wrong"
)
```

**Usage:**

```go
import "github.com/ihor/ts-cli/internal/constants"

timeout := constants.NEW_TIMEOUT
```

---

### Creating a New Service Method

**File:** `internal/services/device_service.go` or create new service

```go
// Add to existing service
func (s *DeviceService) NewMethod(param string) (Result, error) {
    // Business logic here
    return result, nil
}
```

**Usage in command:**

```go
service := services.NewDeviceService(apiKey)
result, err := service.NewMethod("test")
```

---

### Adding a New Error Type

**File:** `internal/errors/errors.go`

```go
// Add error type
const (
    ErrorTypeNewType ErrorType = "NEW_TYPE_ERROR"
)

// Add constructor
func NewMyError(message string, err error) *AppError {
    return &AppError{
        Type:    ErrorTypeNewType,
        Message: message,
        Err:     err,
    }
}

// Add checker
func IsMyError(err error) bool {
    var appErr *AppError
    return errors.As(err, &appErr) && appErr.Type == ErrorTypeNewType
}
```

**Usage:**

```go
import "github.com/ihor/ts-cli/internal/errors"

if err != nil {
    return errors.NewMyError("operation failed", err)
}

if errors.IsMyError(err) {
    // Handle specific error
}
```

---

### Adding a New Output Format

**File:** `internal/formatters/device_formatter.go`

```go
// Add method to DeviceFormatter
func (f *DeviceFormatter) FormatAsYAML(devices []client.Device) (string, error) {
    // YAML formatting logic
    return yamlString, nil
}
```

**Usage:**

```go
formatter := formatters.NewDeviceFormatter()
output, err := formatter.FormatAsYAML(devices)
```

---

### Creating a New Command

**File:** `commands/newcommand.go`

```go
package commands

import (
    "github.com/spf13/cobra"
    "github.com/ihor/ts-cli/internal/services"
    "github.com/ihor/ts-cli/internal/formatters"
    "github.com/ihor/ts-cli/internal/constants"
)

func NewMyCommand() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "mycommand",
        Short: "Description",
        RunE: func(cmd *cobra.Command, args []string) error {
            // 1. Load config
            configService, _ := services.NewConfigService()
            config, err := configService.Load()

            // 2. Use service
            service := services.NewDeviceService(apiKey)
            result, err := service.DoSomething()

            // 3. Format output
            formatter := formatters.NewDeviceFormatter()
            output := formatter.FormatAsTable(result)

            // 4. Display
            fmt.Print(output)
            return nil
        },
    }
    return cmd
}
```

**Register in root.go:**

```go
rootCmd.AddCommand(NewMyCommand())
```

---

### Writing Tests

**File:** `internal/services/device_service_test.go`

```go
package services

import (
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestDeviceService_ListDevices(t *testing.T) {
    tests := []struct {
        name    string
        tailnet string
        want    int
        wantErr bool
    }{
        {"valid", "example.com", 5, false},
        {"empty", "", 0, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Mock client
            // Call service
            // Assert results
        })
    }
}
```

**Run tests:**

```bash
go test ./internal/services/...
go test -cover ./...
```

---

## 🤖 Using AI Agents

### Invoke an Agent

```bash
# In your commit message or PR description
@ts-cli-refactorer Extract SSH logic into a service

@ts-cli-tester Write tests for ConfigService

@ts-cli-documenter Update README with new command
```

### Available Agents

1. **@ts-cli-refactorer** - Refactoring specialist
2. **@ts-cli-tester** - Testing specialist
3. **@ts-cli-documenter** - Documentation specialist
4. **@ts-cli-api-developer** - API client specialist
5. **@ts-cli-command-builder** - Command creation specialist
6. **@ts-cli-optimizer** - Performance specialist
7. **@ts-cli-security-auditor** - Security specialist

See [.copilot/AGENTS.md](./.copilot/AGENTS.md) for details.

---

## 📐 Code Patterns

### Service Pattern

```go
// Create service
service := services.NewDeviceService(apiKey)

// Use service
devices, err := service.ListDevices(tailnet)
if err != nil {
    return fmt.Errorf("failed: %w", err)
}
```

### Error Handling Pattern

```go
// Create typed error
return errors.NewAPIError("operation failed", err)

// Check error type
if errors.IsNotFoundError(err) {
    // Handle not found
}

// Wrap with context
return fmt.Errorf("failed to process: %w", err)
```

### Formatter Pattern

```go
// Create formatter
formatter := formatters.NewDeviceFormatter()

// Format output
table := formatter.FormatAsTable(devices)
json, _ := formatter.FormatAsJSON(devices)

// Status messages
fmt.Println(formatters.FormatSuccess("Done!"))
fmt.Println(formatters.FormatError(err))
```

### Concurrent Pattern

```go
var (
    results []Data
    mu      sync.Mutex
    wg      sync.WaitGroup
)

for _, item := range items {
    wg.Add(1)
    go func(i Item) {
        defer wg.Done()

        result := process(i)

        mu.Lock()
        results = append(results, result)
        mu.Unlock()
    }(item)
}

wg.Wait()
```

---

## 🎯 Design Principles

### SOLID Principles

**Single Responsibility:**

- Each package/type has one reason to change
- Commands → CLI interface only
- Services → Business logic only
- Client → API communication only

**Open/Closed:**

- Extend functionality without modifying existing code
- Add new error types, formatters, services

**Liskov Substitution:**

- Mock services in tests
- Swap implementations without breaking contracts

**Interface Segregation:**

- Small, focused interfaces
- No fat interfaces

**Dependency Inversion:**

- Depend on abstractions, not concretions
- Commands depend on services, not client

### DRY Principle

- No duplicated code
- Use constants for repeated values
- Extract common logic to utilities

### Clean Code

- Functions < 30 lines
- Clear naming
- Comments for complex logic
- Error handling everywhere

---

## 📊 Import Paths

```go
import (
    // Standard library
    "fmt"
    "time"

    // External
    "github.com/spf13/cobra"

    // Internal
    "github.com/ihor/ts-cli/client"
    "github.com/ihor/ts-cli/internal/constants"
    "github.com/ihor/ts-cli/internal/services"
    "github.com/ihor/ts-cli/internal/errors"
    "github.com/ihor/ts-cli/internal/formatters"
)
```

---

## 🔍 Finding Examples

**How to use DeviceService?**
→ See `commands/list.go`

**How to use ConfigService?**
→ See `commands/login.go`

**How to format output?**
→ See `commands/list.go` displayTable/displayJSON

**How to handle errors?**
→ See `internal/services/device_service.go`

**How to write tests?**
→ See `tui/model_test.go` for inspiration

---

## 🚦 Before You Commit

### Checklist

- [ ] No hardcoded values (use constants)
- [ ] Services used for business logic (not in commands)
- [ ] Typed errors used (from internal/errors)
- [ ] Formatters used for output
- [ ] Code follows Go conventions
- [ ] Functions < 30 lines
- [ ] Error handling present
- [ ] Tests written (if applicable)
- [ ] Documentation updated

### Run Before Commit

```bash
# Format code
go fmt ./...

# Run tests
go test ./...

# Run linter
golangci-lint run

# Check coverage
go test -cover ./...
```

---

## 📞 Getting Help

**Architecture questions?**
→ See [ARCHITECTURE.md](./ARCHITECTURE.md)

**Refactoring help?**
→ See [REFACTORING_GUIDE.md](./REFACTORING_GUIDE.md)
→ Use `@ts-cli-refactorer` agent

**Need tests?**
→ Use `@ts-cli-tester` agent

**Documentation?**
→ Use `@ts-cli-documenter` agent

**Performance issues?**
→ Use `@ts-cli-optimizer` agent

**Security concerns?**
→ Use `@ts-cli-security-auditor` agent

---

## 🎓 Learning Path

1. **Start here:**
    - Read [REFACTORING_SUMMARY.md](./REFACTORING_SUMMARY.md) (10 min)

2. **Understand architecture:**
    - Read [ARCHITECTURE.md](./ARCHITECTURE.md) (30 min)

3. **See what changed:**
    - Read [CHANGELOG.md](./CHANGELOG.md) (20 min)

4. **Learn to refactor:**
    - Read [REFACTORING_GUIDE.md](./REFACTORING_GUIDE.md) (30 min)

5. **Use AI agents:**
    - Read [.copilot/AGENTS.md](./.copilot/AGENTS.md) (15 min)

6. **Study the code:**
    - Review `internal/services/` (20 min)
    - Review `commands/list.go` as example (10 min)

**Total time: ~2 hours to full proficiency**

---

## 💡 Pro Tips

1. **Use constants for everything repeated** - No magic numbers!

2. **Keep commands thin** - Move logic to services

3. **Use typed errors** - Better error handling

4. **Leverage AI agents** - They know the patterns

5. **Write table-driven tests** - Cover multiple scenarios easily

6. **Check existing code** - Follow established patterns

7. **Read the docs** - Everything is documented!

---

## ⚡ Quick Commands

```bash
# Build
go build -o ts-cli

# Test
go test ./...

# Test with coverage
go test -cover ./...

# Format
go fmt ./...

# Lint
golangci-lint run

# Run specific test
go test -run TestDeviceService ./internal/services/

# Benchmark
go test -bench=. ./...
```

---

**Happy Coding! 🚀**
