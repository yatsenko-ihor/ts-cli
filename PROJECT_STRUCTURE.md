# ts-cli Visual Project Structure

## 📊 Complete Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                                                                         │
│                          ts-cli Application                             │
│                                                                         │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  main.go  ──────────────────┐                                          │
│                              │                                          │
│                              ▼                                          │
│                       commands/root.go                                  │
│                              │                                          │
│           ┌──────────────────┼──────────────────┐                      │
│           │                  │                  │                      │
│           ▼                  ▼                  ▼                      │
│      commands/           commands/          commands/                  │
│       login.go            list.go            ssh.go                    │
│           │                  │                  │                      │
│           │                  │                  │                      │
│           └──────────────────┼──────────────────┘                      │
│                              │                                          │
│                              ▼                                          │
│         ┌────────────────────────────────────────┐                     │
│         │     internal/services/                 │                     │
│         │                                        │                     │
│         │  ┌──────────────────────────────┐     │                     │
│         │  │  device_service.go           │     │                     │
│         │  │  • ListDevices               │     │                     │
│         │  │  • FindDevice                │     │                     │
│         │  │  • ValidateAPIKey            │     │                     │
│         │  └──────────────────────────────┘     │                     │
│         │                                        │                     │
│         │  ┌──────────────────────────────┐     │                     │
│         │  │  config_service.go           │     │                     │
│         │  │  • Load                      │     │                     │
│         │  │  • Save                      │     │                     │
│         │  │  • AddAccount                │     │                     │
│         │  └──────────────────────────────┘     │                     │
│         └────────────────────────────────────────┘                     │
│                              │                                          │
│           ┌──────────────────┼──────────────────┐                      │
│           │                  │                  │                      │
│           ▼                  ▼                  ▼                      │
│      client/            internal/         internal/                    │
│    tailscale.go        formatters/        constants/                   │
│           │         device_formatter.go  constants.go                  │
│           │                  │                                          │
│           │                  │                                          │
│           ▼                  ▼                                          │
│     Tailscale API        User Output                                   │
│  api.tailscale.com                                                     │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│                       Support Modules                                   │
│                                                                         │
│  internal/errors/     internal/constants/     util/                    │
│    errors.go            constants.go          validation.go            │
│                                                history.go               │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 🗂️ Directory Tree with Descriptions

```
ts-cli/
│
├── 📄 main.go                                    Entry point
│
├── 📁 commands/                                  PRESENTATION LAYER
│   ├── 📄 root.go                               Root command, registers subcommands
│   ├── 📄 login.go                              Login & authentication
│   ├── 📄 list.go                               List devices (table/JSON)
│   ├── 📄 interactive.go                        Interactive TUI mode
│   ├── 📄 ssh.go                                SSH into devices
│   ├── 📄 up.go                                 Bring up connection
│   ├── 📄 account.go                            Multi-account management
│   ├── 📄 install.go                            Installation helpers
│   ├── 📄 config.go                             [DEPRECATED] Old config
│   └── 📄 tailscale_check.go                    Check daemon status
│
├── 📁 internal/                                  PRIVATE PACKAGES
│   │
│   ├── 📁 services/                             SERVICE LAYER (Business Logic)
│   │   ├── 📄 device_service.go                Device operations & queries
│   │   │   • NewDeviceService()
│   │   │   • ListDevices()
│   │   │   • ListDevicesFromMultipleAccounts()
│   │   │   • FindDeviceByIdentifier()
│   │   │   • ValidateAPIKey()
│   │   │   • GetDevicePrimaryAddress()
│   │   │
│   │   └── 📄 config_service.go                Configuration management
│   │       • NewConfigService()
│   │       • Load()
│   │       • Save()
│   │       • AddOrUpdateAccount()
│   │       • GetActiveAccount()
│   │
│   ├── 📁 constants/                            CONSTANTS
│   │   └── 📄 constants.go                     All hardcoded values
│   │       • Application metadata
│   │       • API configuration
│   │       • File paths & permissions
│   │       • Error messages
│   │       • Log messages
│   │
│   ├── 📁 errors/                               ERROR HANDLING
│   │   └── 📄 errors.go                        Typed error system
│   │       • ErrorType enum
│   │       • AppError struct
│   │       • NewXxxError() constructors
│   │       • IsXxxError() type checkers
│   │
│   └── 📁 formatters/                           OUTPUT FORMATTING
│       └── 📄 device_formatter.go              Device output formatters
│           • FormatAsTable()
│           • FormatAsJSON()
│           • FormatDeviceSummary()
│           • FormatSuccess/Error/Warning()
│
├── 📁 client/                                   DATA ACCESS LAYER
│   └── 📄 tailscale.go                         Tailscale API client
│       • NewClient()
│       • ValidateAPIKey()
│       • ListDevices()
│       • doRequest() [private]
│
├── 📁 tui/                                      TERMINAL UI
│   ├── 📄 model.go                             Bubbletea TUI model
│   └── 📄 model_test.go                        TUI tests
│
├── 📁 util/                                     UTILITIES
│   ├── 📄 history.go                           Command history tracking
│   └── 📄 validation.go                        Input validation
│
├── 📁 .copilot/                                 AI AGENT CONFIGURATION
│   ├── 📄 AGENTS.md                            Custom agent definitions
│   │   • ts-cli-refactorer
│   │   • ts-cli-tester
│   │   • ts-cli-documenter
│   │   • ts-cli-api-developer
│   │   • ts-cli-command-builder
│   │   • ts-cli-optimizer
│   │   • ts-cli-security-auditor
│   │
│   ├── 📁 roles/
│   │   └── 📄 go-architect.md                  Agent role definition
│   │       • Guiding principles
│   │       • Architecture layers
│   │       • Naming conventions
│   │       • Testing strategy
│   │
│   ├── 📁 skills/
│   │   └── 📄 go-patterns.md                   Common patterns & skills
│   │       • Refactoring patterns
│   │       • Error handling patterns
│   │       • Testing patterns
│   │       • Concurrent patterns
│   │
│   └── 📁 learnings/
│       └── 📄 project-decisions.md             Architecture decisions
│           • Decision log
│           • Code improvements
│           • Patterns used
│           • Common pitfalls
│
└── 📁 Documentation/                            PROJECT DOCUMENTATION
    ├── 📄 ARCHITECTURE.md                      Complete architecture docs
    ├── 📄 REFACTORING_GUIDE.md                 Refactoring guide
    ├── 📄 CHANGELOG.md                         Detailed changelog
    ├── 📄 REFACTORING_SUMMARY.md               Executive summary
    ├── 📄 QUICK_REFERENCE.md                   Quick reference guide
    └── 📄 README.md                            [Original] Project readme
```

---

## 🔄 Data Flow Diagram

```
┌─────────┐
│  User   │
└────┬────┘
     │
     │ ts-cli list
     ▼
┌──────────────────────────────────────────────┐
│         PRESENTATION LAYER                    │
│         commands/list.go                      │
│                                               │
│  1. Parse flags (--format, --tailnet)        │
│  2. Load configuration                        │
│  3. Call services                             │
│  4. Format output                             │
│  5. Display to user                           │
└──┬───────────────────────────────┬───────────┘
   │                               │
   │ LoadConfig()                  │ FormatAsTable()
   ▼                               ▼
┌─────────────────────┐      ┌──────────────────┐
│   ConfigService     │      │  DeviceFormatter │
│                     │      │                  │
│ Load config from    │      │ Format devices   │
│ ~/.ts-cli/config    │      │ as table/JSON    │
└─────────────────────┘      └──────────────────┘
   │
   │ Returns Config
   │ with accounts
   ▼
┌──────────────────────────────────────────────┐
│         SERVICE LAYER                         │
│         internal/services/device_service.go   │
│                                               │
│  ListDevicesFromMultipleAccounts()            │
│                                               │
│  ┌────────────────────────────────┐           │
│  │  For each account:             │           │
│  │  ┌──────────────────────────┐  │           │
│  │  │  goroutine {             │  │           │
│  │  │    ListDevices()         │  │           │
│  │  │    mutex.Lock()          │  │           │
│  │  │    append results        │  │           │
│  │  │    mutex.Unlock()        │  │           │
│  │  │  }                       │  │           │
│  │  └──────────────────────────┘  │           │
│  │  WaitGroup.Wait()              │           │
│  └────────────────────────────────┘           │
└──┬───────────────────────────────────────────┘
   │
   │ ListDevices(tailnet)
   ▼
┌──────────────────────────────────────────────┐
│         DATA ACCESS LAYER                     │
│         client/tailscale.go                   │
│                                               │
│  1. Create HTTP request                       │
│  2. Add Bearer token auth                     │
│  3. Make API call                             │
│  4. Parse JSON response                       │
│  5. Return []Device                           │
└──┬───────────────────────────────────────────┘
   │
   │ GET /api/v2/tailnet/{name}/devices
   ▼
┌──────────────────────────────────────────────┐
│         Tailscale API                         │
│         api.tailscale.com                     │
│                                               │
│  Returns JSON with device list                │
└───────────────────────────────────────────────┘
```

---

## 🎨 Layer Responsibilities

```
┌─────────────────────────────────────────────────────────┐
│              PRESENTATION LAYER                         │
│              commands/                                  │
│                                                         │
│  ✅ Handle user input (CLI flags, TUI events)          │
│  ✅ Display formatted output                           │
│  ✅ Coordinate service calls                           │
│  ✅ Error display to user                              │
│                                                         │
│  ❌ NO business logic                                  │
│  ❌ NO direct API calls                                │
│  ❌ NO data processing                                 │
└─────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│              SERVICE LAYER                              │
│              internal/services/                         │
│                                                         │
│  ✅ Implement business logic                           │
│  ✅ Data validation & processing                       │
│  ✅ Orchestrate data operations                        │
│  ✅ Multi-source data aggregation                      │
│                                                         │
│  ❌ NO user interaction                                │
│  ❌ NO output formatting                               │
│  ❌ NO HTTP details                                    │
└─────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│              DATA ACCESS LAYER                          │
│              client/                                    │
│                                                         │
│  ✅ HTTP communication                                 │
│  ✅ API authentication                                 │
│  ✅ Response parsing                                   │
│  ✅ Network error handling                             │
│                                                         │
│  ❌ NO business logic                                  │
│  ❌ NO data validation                                 │
│  ❌ NO output formatting                               │
└─────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│              SUPPORT MODULES                            │
│              internal/{constants,errors,formatters}     │
│                                                         │
│  ✅ Shared utilities                                   │
│  ✅ Constants & configuration                          │
│  ✅ Error types & handling                             │
│  ✅ Output formatting                                  │
└─────────────────────────────────────────────────────────┘
```

---

## 🔗 Dependency Graph

```
                    main.go
                       │
                       ▼
                 commands/root
                       │
        ┌──────────────┼──────────────┐
        │              │              │
        ▼              ▼              ▼
   commands/      commands/      commands/
    login          list           ssh
        │              │              │
        └──────────────┼──────────────┘
                       │
        ┌──────────────┼──────────────┐
        │              │              │
        ▼              ▼              ▼
   services/      services/     formatters/
    device         config        device
        │              │              │
        │              └──────┬───────┘
        ▼                     │
   client/                    │
  tailscale ◄─────────────────┘
        │
        ▼
    Tailscale
       API

Legend:
  ──► Direct dependency
  ◄── Uses/calls
```

---

## 📦 Package Dependencies Matrix

```
Package          │ Depends On
─────────────────┼────────────────────────────────────
main             │ commands/root
commands/*       │ services, formatters, constants
services/device  │ client, errors, constants
services/config  │ errors, constants
client           │ errors, constants
formatters       │ client (types only)
errors           │ (standard library only)
constants        │ (standard library only)
util             │ (standard library only)
```

---

## 🎯 SOLID Principles Mapping

```
┌─────────────────────────────────────────────────────────┐
│  Single Responsibility Principle (SRP)                  │
├─────────────────────────────────────────────────────────┤
│  commands/       → Only CLI interface                   │
│  services/       → Only business logic                  │
│  client/         → Only HTTP communication              │
│  formatters/     → Only output formatting               │
│  Each has ONE reason to change                          │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│  Open/Closed Principle (OCP)                            │
├─────────────────────────────────────────────────────────┤
│  ✅ Add new error types without modifying errors.go    │
│  ✅ Add new formatters without changing existing ones   │
│  ✅ Add new services without changing commands          │
│  Open for extension, closed for modification            │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│  Liskov Substitution Principle (LSP)                    │
├─────────────────────────────────────────────────────────┤
│  ✅ Mock services in tests                             │
│  ✅ Replace client with test client                    │
│  ✅ All implementations honor contracts                │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│  Interface Segregation Principle (ISP)                  │
├─────────────────────────────────────────────────────────┤
│  ✅ Services have focused interfaces                   │
│  ✅ No fat interfaces                                  │
│  ✅ Clients only depend on methods they use            │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│  Dependency Inversion Principle (DIP)                   │
├─────────────────────────────────────────────────────────┤
│  ✅ Commands depend on services (abstraction)          │
│  ✅ Services depend on client interface                │
│  ✅ High-level doesn't depend on low-level             │
└─────────────────────────────────────────────────────────┘
```

---

## 🚀 Quick Navigation

```
Want to...                          Go to...
──────────────────────────────────────────────────────────
Add a constant                      internal/constants/constants.go
Add business logic                  internal/services/*
Add error type                      internal/errors/errors.go
Add output format                   internal/formatters/*
Add CLI command                     commands/newcommand.go
Modify API client                   client/tailscale.go
Understand architecture             ARCHITECTURE.md
See what changed                    CHANGELOG.md
Learn refactoring                   REFACTORING_GUIDE.md
Get started quickly                 QUICK_REFERENCE.md
Use AI agents                       .copilot/AGENTS.md
```

---

## 📈 Project Metrics

```
Component        Files  Lines  Complexity  Coverage  Status
──────────────────────────────────────────────────────────────
commands/          10   1,200      Low        10%    ⚠️ Needs refactor
services/           2     330      Low         0%    ✅ New, needs tests
client/             1     200      Low         0%    ⚠️ Needs tests
formatters/         1     140      Low         0%    ✅ New, needs tests
constants/          1     120      None       N/A    ✅ Complete
errors/             1     130      Low         0%    ✅ New, needs tests
util/               2     150      Low        20%    ⚠️ Needs tests
tui/                2   2,000      High       30%    ⚠️ Complex
──────────────────────────────────────────────────────────────
Total              20   4,270      Med        12%    🎯 Improving
──────────────────────────────────────────────────────────────

Legend:
✅ Good   ⚠️ Needs work   ❌ Critical
```

---

## 🎓 Learning Path Map

```
              Start Here
                  │
                  ▼
        [REFACTORING_SUMMARY.md]
              (10 min)
                  │
       ┌──────────┼──────────┐
       ▼          ▼          ▼
 [ARCHITECTURE]  [CHANGELOG]  [QUICK_REFERENCE]
   (30 min)      (20 min)      (15 min)
       │          │          │
       └──────────┼──────────┘
                  │
                  ▼
       [REFACTORING_GUIDE.md]
              (30 min)
                  │
                  ▼
         [.copilot/AGENTS.md]
              (15 min)
                  │
                  ▼
       Study actual code in:
       • internal/services/
       • commands/list.go
              (30 min)
                  │
                  ▼
       Ready to contribute! 🎉
```

**Total Learning Time: ~2.5 hours**

---

This visual guide provides a quick overview of the entire refactored project structure, making it easy to understand the architecture at a glance and find what you need quickly.
