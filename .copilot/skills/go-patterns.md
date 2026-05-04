---
# AI Agent Skills for ts-cli Project
# Specific skills and patterns for this codebase

applyTo:
    - '**/*.go'

skills:
    - name: refactoring-legacy-code
      description: Refactor existing code to follow SOLID principles
      trigger: [refactor, cleanup, improve]
      steps:
          - Identify code smells (long functions, god objects, tight coupling)
          - Extract business logic into services
          - Replace magic numbers/strings with constants
          - Introduce interfaces for testability
          - Separate concerns (presentation vs. logic)
          - Add comprehensive error handling
          - Write tests for refactored code

    - name: implementing-new-command
      description: Add a new CLI command following architecture
      trigger: [new command, add command]
      template: |
          1. Define command structure in commands/ using cobra
          2. Create corresponding service in internal/services/ if needed
          3. Use existing formatters from internal/formatters/
          4. Reference constants from internal/constants/
          5. Implement proper error handling with internal/errors
          6. Add unit tests for service layer
          7. Update README.md with command documentation
      example_files:
          - commands/list.go
          - internal/services/device_service.go

    - name: api-client-enhancement
      description: Add new API endpoints or improve client
      trigger: [api, client, http]
      guidelines:
          - Add new methods to client/tailscale.go
          - Use constants.API_BASE_URL for URL construction
          - Implement proper timeout: constants.API_TIMEOUT
          - Return structured errors using internal/errors
          - Handle all HTTP status codes explicitly
          - Add request/response logging for debugging
          - Write unit tests with mocked HTTP responses

    - name: error-handling-pattern
      description: Standardized error handling across the codebase
      trigger: [error, exception, failure]
      pattern: |
          // In services layer
          if err != nil {
              return errors.NewAPIError("failed to fetch devices", err)
          }

          // In commands layer
          if err := service.DoSomething(); err != nil {
              if errors.IsNotFoundError(err) {
                  return fmt.Errorf("resource not found: %w", err)
              }
              return fmt.Errorf("operation failed: %w", err)
          }

          // At API boundary
          if resp.StatusCode == constants.HTTP_STATUS_UNAUTHORIZED {
              return errors.NewPermissionError(constants.ERR_INVALID_API_KEY, nil)
          }

    - name: adding-constants
      description: Add new constants following naming conventions
      trigger: [magic number, magic string, hardcoded]
      guidelines:
          - Add to internal/constants/constants.go
          - Use SCREAMING_SNAKE_CASE naming
          - Group related constants in const blocks
          - Add documentation comment for each constant
          - Update all usages to reference the constant

    - name: formatting-output
      description: Format data for display to user
      trigger: [output, display, print, format]
      guidelines:
          - Use formatters from internal/formatters/
          - Support both table and JSON formats
          - Use FormatSuccess/FormatError/FormatWarning for messages
          - Keep formatting logic out of business logic
          - Ensure table output is aligned with tabwriter

    - name: configuration-management
      description: Handle configuration loading/saving
      trigger: [config, settings, preferences]
      guidelines:
          - Use ConfigService from internal/services/
          - Never handle file I/O in commands
          - Support migration from old config formats
          - Validate configuration after loading
          - Use constants for file paths and permissions

    - name: concurrent-operations
      description: Implement concurrent operations safely
      trigger: [concurrent, parallel, goroutine]
      pattern: |
          var (
              result []Data
              mu     sync.Mutex
              wg     sync.WaitGroup
          )

          for _, item := range items {
              wg.Add(1)
              go func(i Item) {
                  defer wg.Done()
                  
                  data := processItem(i)
                  
                  mu.Lock()
                  result = append(result, data)
                  mu.Unlock()
              }(item)
          }

          wg.Wait()
          return result

    - name: testing-best-practices
      description: Write comprehensive tests
      trigger: [test, testing, coverage]
      guidelines:
          - Use table-driven tests for multiple scenarios
          - Mock external dependencies (HTTP clients, file system)
          - Test both success and error paths
          - Use t.Run for subtests
          - Keep test names descriptive: TestServiceMethod_Scenario_ExpectedResult
          - Use testify/assert for cleaner assertions
      example: |
          func TestDeviceService_ListDevices_Success(t *testing.T) {
              tests := []struct {
                  name     string
                  tailnet  string
                  want     int
                  wantErr  bool
              }{
                  {"valid tailnet", "example.com", 5, false},
                  {"empty tailnet", "", 0, true},
              }
              
              for _, tt := range tests {
                  t.Run(tt.name, func(t *testing.T) {
                      // Test implementation
                  })
              }
          }
---
