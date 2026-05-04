---
# Project Learnings and Best Practices
# Lessons learned and decisions made during ts-cli development

date_created: 2026-03-13
last_updated: 2026-03-13

# === ARCHITECTURE DECISIONS ===

decision_001:
    title: Layered Architecture with Service Layer
    date: 2026-03-13
    context: |
        The original codebase had business logic mixed with presentation logic in commands.
        This made testing difficult and violated Single Responsibility Principle.
    decision: |
        Implemented a three-layer architecture:
        - Presentation Layer (commands/): CLI interface
        - Service Layer (internal/services/): Business logic
        - Data Access Layer (client/): API communication
    rationale: |
        - Enables unit testing of business logic without CLI dependencies
        - Makes it easy to add alternative interfaces (e.g., REST API, gRPC)
        - Improves code reusability across commands
        - Follows SOLID principles (SRP, DIP)
    trade_offs:
        pros:
            - Better testability
            - Improved maintainability
            - Clear separation of concerns
        cons:
            - More files and complexity
            - Slightly more boilerplate
    status: IMPLEMENTED

decision_002:
    title: Centralized Constants Package
    date: 2026-03-13
    context: |
        Magic numbers and strings were scattered throughout the codebase (timeouts,
        URLs, file paths, error messages), making changes difficult.
    decision: |
        Created internal/constants/ package with all hardcoded values using
        SCREAMING_SNAKE_CASE naming convention.
    rationale: |
        - DRY principle: Single source of truth
        - Easy to find and modify configuration values
        - Prevents typos and inconsistencies
        - Improves code readability
    examples:
        - API_TIMEOUT = 30 * time.Second
        - API_BASE_URL = "https://api.tailscale.com/api/v2"
        - ERR_NO_ACCOUNTS_CONFIGURED = "no accounts configured..."
    status: IMPLEMENTED

decision_003:
    title: Structured Error Types
    date: 2026-03-13
    context: |
        Error handling was inconsistent, making it hard to distinguish error types
        and provide appropriate user feedback.
    decision: |
        Created internal/errors/ package with typed errors (AppError) and helper
        functions for error classification.
    rationale: |
        - Enables error type checking with errors.As()
        - Provides structured error information (type, message, wrapped error)
        - Allows for type-specific error handling
        - Improves error messages for users
    usage: |
        if errors.IsNotFoundError(err) {
            // Handle not found case
        }
    status: IMPLEMENTED

decision_004:
    title: Concurrent Account Querying
    date: 2026-03-13
    context: |
        Sequential querying of multiple Tailscale accounts was slow.
    decision: |
        Implemented concurrent device fetching using goroutines with proper
        synchronization (sync.Mutex, sync.WaitGroup).
    rationale: |
        - Significantly improves performance for multi-account setups
        - Non-blocking: Failures in one account don't block others
        - Better user experience with faster response times
    considerations:
        - Must protect shared slice with mutex
        - Use WaitGroup for proper goroutine lifecycle
        - Handle errors from individual goroutines gracefully
    status: IMPLEMENTED

# === CODE QUALITY IMPROVEMENTS ===

improvement_001:
    title: Service Layer Pattern
    description: |
        All business logic extracted from commands into dedicated service classes:
        - DeviceService: Device management and querying
        - ConfigService: Configuration management
    benefits:
        - Testable without CLI framework
        - Reusable across different commands
        - Clear interface definitions
    location: internal/services/

improvement_002:
    title: Output Formatters
    description: |
        Created dedicated formatters for different output types (table, JSON).
        Keeps formatting logic separate from business logic.
    benefits:
        - Single Responsibility Principle
        - Easy to add new output formats
        - Consistent formatting across commands
    location: internal/formatters/

improvement_003:
    title: Config Migration Support
    description: |
        ConfigService automatically migrates from old config format to new format,
        ensuring backward compatibility.
    benefits:
        - Smooth upgrade path for existing users
        - Zero manual migration required
        - Maintains data integrity
    location: internal/services/config_service.go

# === PATTERNS AND PRACTICES ===

pattern_001:
    name: Factory Pattern for Services
    description: |
        Use NewXxxService() functions to create service instances.
    example: |
        func NewDeviceService(apiKey string) *DeviceService {
            return &DeviceService{
                client: client.NewClient(apiKey),
            }
        }
    benefits:
        - Encapsulates initialization logic
        - Makes testing easier (can inject mocks)
        - Clear entry point for service usage

pattern_002:
    name: Error Wrapping with Context
    description: |
        Always wrap errors with additional context using fmt.Errorf with %w.
    example: |
        if err != nil {
            return fmt.Errorf("failed to fetch devices from %s: %w", tailnet, err)
        }
    benefits:
        - Preserves original error for error.Is/As
        - Provides debugging context
        - Creates clear error chains

pattern_003:
    name: Table-Driven Tests
    description: |
        Use table-driven tests for testing multiple scenarios.
    example: |
        tests := []struct {
            name    string
            input   string
            want    string
            wantErr bool
        }{
            {"valid input", "test", "TEST", false},
            {"empty input", "", "", true},
        }

        for _, tt := range tests {
            t.Run(tt.name, func(t *testing.T) {
                // Test logic
            })
        }
    benefits:
        - Easy to add new test cases
        - Clear test structure
        - Comprehensive coverage

# === COMMON PITFALLS ===

pitfall_001:
    issue: Mixing business logic in commands
    solution: Extract to services
    example: |
        // BAD: Business logic in command
        RunE: func(cmd *cobra.Command, args []string) error {
            devices := fetchDevices()
            filtered := filterDevices(devices)
            // ... more logic
        }

        // GOOD: Use service
        RunE: func(cmd *cobra.Command, args []string) error {
            service := services.NewDeviceService(apiKey)
            devices, err := service.ListDevices(tailnet)
            // ... minimal glue code
        }

pitfall_002:
    issue: Hardcoding values
    solution: Use constants
    example: |
        // BAD
        time.Sleep(30 * time.Second)

        // GOOD
        time.Sleep(constants.API_TIMEOUT)

pitfall_003:
    issue: Swallowing errors
    solution: Always check and propagate errors
    example: |
        // BAD
        _ = doSomething()

        // GOOD
        if err := doSomething(); err != nil {
            return fmt.Errorf("operation failed: %w", err)
        }

# === FUTURE IMPROVEMENTS ===

todo_001:
    title: Add Comprehensive Unit Tests
    priority: HIGH
    description: |
        Add unit tests for all services with mocked dependencies.
    files:
        - internal/services/device_service_test.go
        - internal/services/config_service_test.go

todo_002:
    title: Implement Caching Layer
    priority: MEDIUM
    description: |
        Add caching for device lists to reduce API calls.
    approach: |
        - Create CacheService in internal/services/
        - Cache devices with TTL
        - Invalidate on specific actions

todo_003:
    title: Add Telemetry/Metrics
    priority: LOW
    description: |
        Add optional telemetry for usage analytics and error tracking.
    considerations:
        - Must be opt-in
        - Privacy-preserving
        - Useful for debugging common issues

# === MAINTENANCE NOTES ===

maintenance_001:
    area: Dependencies
    frequency: Monthly
    tasks:
        - Check for security updates: go list -m -u all
        - Update dependencies: go get -u ./...
        - Run tests after updates
        - Check for deprecated packages

maintenance_002:
    area: Code Quality
    frequency: Before each release
    tasks:
        - Run gofmt on all files
        - Run golangci-lint
        - Check test coverage: go test -cover ./...
        - Update documentation
        - Review TODO comments

maintenance_003:
    area: Security
    frequency: Continuous
    tasks:
        - Review API key storage (use secure storage)
        - Check for credential leaks in logs
        - Validate all user inputs
        - Keep dependencies updated

# === REFERENCES ===

references:
    style_guides:
        - Effective Go: https://go.dev/doc/effective_go
        - Uber Go Style Guide: https://github.com/uber-go/guide/blob/master/style.md

    patterns:
        - Go Design Patterns: https://github.com/tmrts/go-patterns
        - SOLID Principles in Go: https://dave.cheney.net/2016/08/20/solid-go-design

    tools:
        - golangci-lint: https://golangci-lint.run/
        - go-critic: https://github.com/go-critic/go-critic
---
