# ts-cli — Code Review and Security Audit

| | |
|---|---|
| **Scope** | Entire application on branch `claude/task-51vzek` (identical to `main` at commit `d5668b4`). ~7,700 lines of Go across `client/`, `commands/`, `tui/`, `util/`, `internal/`. |
| **Method** | Full manual read of every Go source file; `go build`, `go vet`, `go test`; `staticcheck`; `gosec`; secret scan of full git history; verification of third-party behaviour (bubbletea `ExecProcess`) against vendored source. |
| **Not performed** | `govulncheck` could not reach `vuln.go.dev` from the review environment (HTTP 403 via proxy). Dependency CVE status is therefore **unverified** and should be run locally (see §6). No dynamic testing against a live tailnet. |
| **Baseline** | Build: clean. `go vet`: clean. Tests: 2 packages pass (`commands`, `tui`). |

## 1. Executive summary

ts-cli is a well-structured Cobra + Bubbletea application. The cryptographic primitives are used correctly (AES-256-GCM, random nonce, `crypto/rand`), file permissions are tightened (`0600`/`0700`), user-supplied SSH usernames are validated, and no secrets were found in git history. There is no remote attack surface: the tool only talks to the Tailscale API as a client and to local binaries.

The material risks are **local**: the saved SSH password is handed to `sshpass -p` on the command line, where any other user on the host can read it from the process table, and the "machine-bound" encryption key is derived from public values, so the stored passwords are obfuscated rather than protected. On the correctness side, three bugs will bite real users: a normal SSH session that ends with a non-zero exit code is treated as an authentication failure and **deletes the saved credentials**; `ts-cli install` **deletes an existing `/usr/local/bin/tsc`** even when it has just detected it is the TypeScript compiler; and `list --format=json` does not emit valid JSON.

| Severity | Count |
|---|---|
| Critical | 0 |
| High | 3 |
| Medium | 8 |
| Low | 6 |
| Informational | 9 |

Recommended order of work: H‑1, H‑2, H‑3, then M‑1 through M‑4. Everything in the High tier is a contained, single-file fix.

## 2. Security findings

### H‑1 — Saved SSH password exposed on the command line via `sshpass -p`

**Severity:** High  **CWE‑214** (Information exposure through process environment/arguments)

**Where:** `tui/ssh.go:61`, `tui/ssh.go:164`, `tui/ssh.go:217`

Every path that uses a saved password builds `sshpass -p <plaintext>`. On Linux and macOS the argument vector of a running process is readable by every local user (`ps -ef`, `/proc/<pid>/cmdline`). `sshpass` tries to overwrite its own argv after start‑up, but the window before that and the shell wrapper in `buildSSHCommand` (which is spawned as `sh -c "... sshpass -p '<pw>' ssh ..."`, so the password also sits in **`sh`'s** argv for the life of the SSH session) make the exposure real and persistent on shared or multi-user machines.

**Remediation:** Never pass the password as an argument. Use one of:
- `sshpass -e` with the password in `SSHPASS` on `cmd.Env` (not visible in argv; visible in `/proc/<pid>/environ` only to the same user/root), or
- `sshpass -d <fd>` with an `os.Pipe()` passed through `cmd.ExtraFiles` (strongest; nothing on disk or in argv/env).

For `buildSSHCommand`, drop the `sh -c` wrapper entirely: print the "Connection to …" banner from Go before `tea.ExecProcess`, and exec `sshpass`/`ssh` directly with an argv slice. That also closes L‑1.

### H‑2 — Any non‑zero SSH exit deletes saved credentials and shows "connection failed"

**Severity:** High (correctness with security side‑effect: encourages users to re‑type and re‑store passwords)

**Where:** `tui/model.go:146‑163`, `tui/ssh.go:38‑43`

`tea.ExecProcess` calls `cmd.Run()` and passes **any** error to the callback, including `*exec.ExitError` for a non‑zero exit status (verified in bubbletea v1.3.10 `exec.go:116‑120`). `ssh` propagates the remote shell's exit status, so a perfectly successful session that ends with `exit 1`, a failing last command, a killed remote process, or a network drop is reported as `sshMsg{err}`. The handler then unconditionally:

1. deletes the per‑machine credentials from memory and disk, and
2. opens the "SSH Connection Failed — change username/password" menu.

Users will see their saved password disappear after an ordinary session and be asked to re‑enter it.

**Remediation:** Distinguish authentication failure from other outcomes. Options in increasing robustness:
- Treat only `sshpass` exit code **5** ("invalid password") and `ssh` exit **255** (connection/auth failure) as credential problems; for all other `*exec.ExitError` codes, surface the exit code informationally and keep the credentials.
- Better: never delete credentials automatically. Show the retry menu with an explicit "Forget saved credentials for this machine" item.

### H‑3 — `ts-cli install` removes a pre‑existing `/usr/local/bin/tsc` regardless of what it is

**Severity:** High (destructive to the user's system)

**Where:** `commands/install.go:203‑206`

```go
// Remove existing tsc if it's ours
if _, err := os.Lstat(tscPath); err == nil {
    os.Remove(tscPath)
}
```

The comment says "if it's ours" but no check is made. `runInstall` already detects the TypeScript compiler (lines 57‑69) and prints a warning, then proceeds to delete it anyway and replace it with a symlink to `ts-cli`. On a developer machine this silently breaks TypeScript tooling (and anything shelling out to `tsc`).

**Remediation:** Before removal, `os.Readlink(tscPath)` and only remove when the target is `ts-cli` (the same check `uninstall.go:58` already performs). If `tsc` exists and is not ours, skip the alias and say so. Consider making the alias opt‑in (`--alias tsc`) since `tsc` is a globally well‑known name.

### M‑1 — Password "encryption" key is derived from public, predictable values

**Severity:** Medium  **CWE‑321 / CWE‑329**

**Where:** `util/crypto.go:20‑34`; README §"SSH Credential Management"

Key = `SHA‑256("ts-cli:<username>:<uid>:<hostname>")`. None of these are secret: username and hostname are visible to anyone on the machine, and uid is usually 501/1000. Anyone who obtains `~/.ts-cli/config.json` (backup, sync folder, stolen laptop image, a leaked dotfiles repo) can reconstruct the key offline in seconds. The README's claim that passwords "cannot be decrypted on another machine or by another user" is therefore **not accurate**; the realistic protection is the `0600` file mode and the fact that the password is not stored in cleartext.

Secondary effect: macOS commonly changes `os.Hostname()` with network location (DHCP‑assigned names), and users rename machines. When that happens `DecryptPassword` fails, `getMachinePassword` silently returns `""`, and the TUI falls back to plain `ssh`, prompting for a password it thinks it has. There is no diagnostic.

**Remediation:**
- Preferred: store the SSH password (and the Tailscale API keys) in the OS credential store (macOS Keychain, Linux Secret Service, Windows Credential Manager) via a library such as `github.com/zalando/go-keyring`.
- If a file‑based scheme must remain: generate a random 32‑byte key once, store it `0600` in `~/.ts-cli/key`, and use it for AES‑GCM. This at least separates key from ciphertext and removes hostname fragility. Document honestly what the scheme does and does not protect against.
- Either way, when decryption fails, surface a one‑line warning ("saved password for X could not be decrypted on this host; re‑enter to re‑save").

### M‑2 — Tailscale API key passed on the command line in the add‑account flow

**Severity:** Medium  **CWE‑214**

**Where:** `tui/tailscale.go:150`

The generated bash script runs `ts-cli login --tailnet="$TAILNET" --api-key="$API_KEY"`. The key is visible in the process table for the duration of the (network‑bound) `login` call. The script is otherwise careful (`read -s`, temp file `0700`, deleted afterwards).

**Remediation:** `login` already reads `TAILSCALE_API_KEY` from the environment. Change the script to `TAILSCALE_API_KEY="$API_KEY" ts-cli login --tailnet="$TAILNET"`. Consider removing the `--api-key` flag from docs/examples in favour of the env var or an interactive prompt, since flags also land in shell history.

### M‑3 — Tailscale API keys and SSH passwords are stored in the same JSON file

**Severity:** Medium (design)

**Where:** `commands/config.go:19‑30`, `tui/config.go:107‑118`

API keys are stored in cleartext (standard for CLIs, but worth stating) alongside encrypted SSH passwords, SSH usernames, and the machine list. A single file compromise exposes full control of every configured tailnet (the API key can authorize/deauthorize devices, change ACLs, etc. depending on scope). See M‑1 for the keychain recommendation; at minimum, document in the README that the config holds live admin‑capable credentials and should not be synced or backed up unencrypted.

### M‑4 — Data race between the Bubbletea update loop and background command goroutines

**Severity:** Medium (potential panic / corrupted history file)

**Where:** `tui/ssh.go:159‑192` (`executeRemoteCommand`), `tui/handlers.go:317‑337` (`cmdHistoryDelete`), `tui/model.go:152` and `tui/handlers.go:883` (map mutation)

The closure returned by `executeRemoteCommand` runs on a separate goroutine and (a) reads `m.ssh.machineCreds` via `getMachinePassword`, and (b) mutates the shared `*util.HistoryStore` (`AddCommand`, `Save`). Meanwhile `Update` can `delete()` from the same map (concurrent read/write on a Go map is a fatal runtime error, not a recoverable race) and `cmdHistoryDelete` can mutate and save the same `HistoryStore`. The `model` is a value type but the map and pointer inside it are shared.

**Remediation:** Resolve the password and build the full `*exec.Cmd` synchronously inside `executeRemoteCommand` before returning the closure (the closure should only run the command). Return the history entry in `commandExecutedMsg` and let `Update` append/save it on the main loop, or add a mutex inside `HistoryStore`.

### M‑5 — Shell wrapper interpolates unescaped SSH target; username loaded from disk is never re‑validated

**Severity:** Medium (defence in depth; exploitable only by someone who can already write the user's config or control API responses)

**Where:** `tui/ssh.go:66‑73`

`buildSSHCommand` runs `sh -c` with `sshTarget` (`user@ip`) interpolated raw. `ValidateSSHUsername` is applied only to usernames typed in the prompt; usernames read from `config.json` (`ssh_username`, `machine_credentials[*].username`) are trusted as‑is, and `Addresses[0]` comes from the API response. A crafted value such as `x;curl …|sh` would execute. Trust boundaries make this low‑likelihood, but the mitigation (no shell) is free and also fixes H‑1.

**Remediation:** As in H‑1, drop `sh -c` and pass an argv slice to `exec.Command`. Additionally validate usernames when loading config and validate that each address parses with `net.ParseIP`.

### M‑6 — `login` silently discards an unreadable config and overwrites it

**Severity:** Medium (data loss of other accounts' keys)

**Where:** `commands/login.go:68‑75`

If `LoadConfig()` fails (e.g. one bad byte in the JSON, or a transient permission error), `login` creates a fresh empty `Config` and `SaveConfig` writes it over the existing file, destroying every other stored account. The comment ("If config doesn't exist, create a new one") is wrong: a missing file already returns an empty config without error; the only way to reach this branch is a corrupt or unreadable file.

**Remediation:** Return the error instead. If a recovery path is wanted, back the file up (`config.json.broken-<timestamp>`) before overwriting and tell the user.

### M‑7 — Tailnet is interpolated into the API URL path without escaping

**Severity:** Medium (low exploitability; self‑inflicted only)

**Where:** `client/tailscale.go:87`, `client/tailscale.go:111`

`fmt.Sprintf("/tailnet/%s/devices", tailnet)` with no `url.PathEscape`. `ValidateTailnet` only checks for `@`/`.` and length, so values containing `/`, `?`, `#` or `%` reach the request line and change the endpoint. Only the user controls the value, so this is not an attack vector, but it produces confusing API errors and would become dangerous if tailnet names were ever sourced from elsewhere.

**Remediation:** `url.PathEscape(tailnet)`; add `context.Context` with a deadline to `doRequest` and cap the body read with `io.LimitReader`.

### M‑8 — Password persisted to disk before the connection is attempted

**Severity:** Medium (design trade‑off; flag for awareness)

**Where:** `tui/handlers.go:1004‑1016`

A typed password is encrypted and written to `config.json` **before** `ssh` runs, with cleanup relying on H‑2's (buggy) failure path. A mistyped password is therefore stored, and if the TUI is killed mid‑session it stays stored. The code comment explains the intent (survive terminal close). Combined with H‑2 this creates confusing states.

**Remediation:** Once H‑2 differentiates auth failures, this is acceptable. Alternatively hold the password in memory and persist on the first successful `sshMsg{err: nil}`; a closed terminal only costs the user one re‑entry.

### L‑1 — `StrictHostKeyChecking=accept-new` applied only on the sshpass path

**Severity:** Low

**Where:** `tui/ssh.go:61`, `165`, `218` vs `66`, `169`, `222`

Behaviour differs depending on whether a password is saved, which is surprising. `accept-new` is a reasonable default for Tailscale IPs (stable, authenticated network); apply it consistently or not at all, and consider `-o UserKnownHostsFile` isolation only if users ask.

### L‑2 — Add‑account script: `set -e` makes the failure branch unreachable; Windows uses `cmd /c` on a bash script

**Severity:** Low

**Where:** `tui/tailscale.go:96`, `152‑163`, `191‑192`

With `set -e`, a non‑zero exit from `ts-cli login` aborts the script before `if [ $? -eq 0 ]`, so the "Failed to add account. Press Enter…" UX never shows. On Windows the generated bash script is passed to `cmd /c`, which cannot run it. Remove `set -e` (or wrap the login in `if ! …; then`), and either skip the feature on Windows or generate a PowerShell equivalent.

### L‑3 — Hand‑rolled JSON parsing of `tailscale status --json` may pick the wrong user

**Severity:** Low (correctness of account‑switch heuristic)

**Where:** `tui/tailscale.go:238‑256`

The code returns the first `"LoginName"` string in the document. In `tailscale status --json`, `LoginName` lives in the `User` map (keyed by user ID), which lists every user with nodes visible to you, including shared‑node owners. The current login is `User[Self.UserID].LoginName`. Parse with `encoding/json` into a struct with `Self.UserID` and `User map[string]struct{ LoginName string }`.

### L‑4 — Shelling out for `sleep` and `which`

**Severity:** Low

**Where:** `commands/ssh.go:109`, `commands/tailscale_check.go:106`, `tui/tailscale.go:321`

Use `time.Sleep` and `exec.LookPath`; both are portable and avoid spawning processes (`which` is absent on some minimal Linux images and Windows).

### L‑5 — stderr output during TUI reload corrupts the alt‑screen

**Severity:** Low

**Where:** `client/tailscale.go:173` called from `tui/model.go:130`

`ListDevicesFromAccounts` prints warnings to stderr. When invoked from the TUI's `r` reload this writes raw text into the Bubbletea alt‑screen. Return per‑account errors to the caller and let the TUI render them in `notify.sshError`.

### L‑6 — Password mask length uses bytes, not runes

**Severity:** Low (cosmetic, leaks nothing meaningful)

**Where:** `tui/view.go:522`

`strings.Repeat("•", len(value))` shows more dots than characters for non‑ASCII passwords. Use `utf8.RuneCountInString`.

## 3. Correctness and behaviour findings (non‑security)

### C‑1 — `list --format=json` emits invalid JSON

**Where:** `commands/list.go:148‑167`

Output is built with `fmt.Printf`: strings are not escaped (a `"` in a device name breaks the document) and `addresses` is printed with `%v`, producing `[100.64.0.1 fd7a::1]` — not JSON. `internal/formatters.FormatAsJSON` already does this correctly with `encoding/json` but is unused. Replace the body with `json.NewEncoder(os.Stdout).SetIndent(...); Encode(devices)`.

### C‑2 — Key‑based‑auth users are forced to enter a password every time

**Where:** `tui/handlers.go:770‑787`; README "Connection flow" step 2

README says: "No per‑machine creds, global username exists → try connecting (key‑based auth)". The code instead always opens the password prompt when a username exists but no per‑machine password does. Empty Enter is ignored, so the only way to attempt key auth is Esc (which cancels). Either implement the documented behaviour (attempt plain `ssh`, prompt for a password only on failure) or update the README and add an explicit "connect without password" choice.

### C‑3 — The TUI config writer will drop unknown top‑level fields

**Where:** `tui/config.go:107‑112`

`configJSON` mirrors four fields of `commands.Config` and uses `json.RawMessage` for accounts. Any future field added to `commands.Config` (or written by another tool/version) is silently discarded whenever the TUI saves a username or credential. Today the structs are in sync, so this is latent. Fix by moving config I/O to a shared package (e.g. `internal/config`) imported by both `commands` and `tui`, which also removes the duplicated load/save code and the `configMu`‑only‑in‑one‑package inconsistency.

### C‑4 — Divergent account‑matching semantics

**Where:** `commands/config.go:169` (match by `Tailnet`), `commands/login.go:81` (by `Name` **or** `Tailnet`), `commands/account.go:103,155` (by `Name` or `Tailnet`), `internal/services/config_service.go:93` (by `Name`)

Different code paths decide "same account" differently. Pick one key (tailnet is the natural one) and route all mutations through `Config.AddAccount`/`SetActiveAccount`.

### C‑5 — `d` in the saved‑credentials view also fires on Backspace

**Where:** `tui/handlers.go:879`

Backspace is a surprising destructive key in a list view and is easy to hit by reflex after Esc from a text prompt. Keep `d`/`delete` only, or require confirmation.

### C‑6 — Inconsistent machine identity keys

**Where:** `tui/handlers.go:739` (creds keyed by `Name`, fallback `Hostname`) vs `tui/ssh.go:154` and `tui/handlers.go:288` (history keyed by `ID`, fallback `Hostname`)

Credentials and history for the same device use different keys. Device `Name` (FQDN) can change on rename; `ID` is stable. Standardise on `ID` with a one‑time migration of existing credential keys.

### C‑7 — Install/uninstall ignore `GOBIN`/`~/.local/bin` and need root

**Where:** `commands/install.go:89`

Hard‑coded `/usr/local/bin` requires `sudo` on most systems; the resulting binary is then root‑owned while the config stays in the user's home. Prefer `~/.local/bin` (XDG) when `/usr/local/bin` is not writable, and tell the user which was chosen.

## 4. Code quality and maintainability

| Ref | Observation | Location | Recommendation |
|---|---|---|---|
| Q‑1 | **Module path does not match repository.** `go install github.com/ihor/ts-cli@latest` (README) will fail; the repo is `github.com/yatsenko-ihor/ts-cli`. | `go.mod:1`, `README.md:38` | Rename module and update imports (`gofmt -r` / sed), or fix README. |
| Q‑2 | **Entire `internal/` tree is dead code.** `services`, `errors`, `constants`, `formatters` are imported by nothing outside themselves and duplicate (with drift) logic in `commands/`. | `internal/**` | Delete, or adopt them and delete the duplicates in `commands/`. Having two config loaders with different semantics is a maintenance hazard. |
| Q‑3 | `staticcheck` reports 16 issues: unused `keyDispatcher` type and methods, unused fields in `machineCredsSavedMsg`, unused styles, unused `runTailscaleDown`, `applyFrameTitle`; three capitalised error strings. | `tui/commands.go`, `tui/messages.go:89‑91`, `tui/styles.go`, `tui/tailscale.go:47`, `tui/utils.go:37`, `tui/handlers.go:350,724,730` | Remove dead code; lowercase error strings. |
| Q‑4 | Version string duplicated (`commands.Version` and `constants.APP_VERSION`). | `commands/root.go:11`, `internal/constants/constants.go:10` | Single source; inject via `-ldflags "-X"` at build time. |
| Q‑5 | README inconsistencies: config path stated as `~/.config/ts-cli/config.json` (§Authentication, §Configuration) but code uses `~/.ts-cli/config.json`; "Go 1.23 or later" vs `go 1.24.0` in `go.mod`; project tree omits `account.go`, `uninstall.go`, `help.go`, `internal/`. | `README.md` | Correct. |
| Q‑6 | `.gitignore` lists `.github/` and `.copilot/` as "local only", yet both are tracked. | `.gitignore:56‑60` | Either `git rm --cached` them or remove the ignore rules. Also note `.github/` is the conventional CI location; ignoring it will hide future workflow files. |
| Q‑7 | No CI. No workflow runs `go vet`, `staticcheck`, `gosec`, `govulncheck`, or tests. | `.github/` | Add a workflow (see §6). |
| Q‑8 | Test coverage is thin: no tests for `util/crypto.go` (round‑trip, tamper detection, short ciphertext), `util/validation.go`, `util/history.go`, `client/` (use `httptest`), credential state machine in `tui/handlers.go`. | | Prioritise crypto, validation, and the SSH credential flow (the area with the most churn in recent commits). |
| Q‑9 | `commands/ssh.go` ignores saved per‑machine credentials and `sshpass`, so CLI and TUI behave differently for the same device. | `commands/ssh.go:117‑131` | Share one `BuildSSHCommand(device, creds)` helper. |
| Q‑10 | `getMaxVisibleItems`/`renderDeviceList` use magic numbers (7, 6, 25, 12) for layout. | `tui/layout.go`, `tui/view.go:120` | Name them in `styles.go` next to `panelBorderWidth` etc. |

## 5. Static‑analysis summary

**gosec** (42 raw findings, triaged):

| Rule | Count | Assessment |
|---|---|---|
| G104 unhandled error | 15 | Low. Worth fixing `fmt.Scanln` (install/uninstall: EOF on stdin yields empty response, which already means "no"), `os.Rename` in migration, `MarkFlagRequired`. Others are `Flush()`/`sleep`. |
| G703 path traversal (install/uninstall) | 9 | False positive: paths are compile‑time constants plus `%ProgramFiles%`. |
| G204 subprocess with variable | 11 | Expected for this tool. The one that matters is the `sh -c` wrapper (H‑1/M‑5). |
| G304 file inclusion via variable | 4 | False positive: paths derive from `os.UserHomeDir()`. |
| G301/G302/G306 permissions | 3 | False positive: an installed binary must be `0755`. |

**staticcheck:** 16 findings, all U1000 (unused) or ST1005 (error string case). See Q‑3.

**Secret scan:** full `git log -p` searched for `tskey-(api|auth)-…`; only the documentation placeholder `tskey-auth-xxxx…` was found. No live keys in history.

## 6. Dependency and toolchain notes

- Direct deps: `bubbletea v1.3.10`, `lipgloss v1.1.0`, `cobra v1.8.1`. All current major lines; no known advisories at the time of review, **but** `govulncheck` could not be executed here (vulnerability DB blocked by the environment's proxy). Run locally:

  ```bash
  go run golang.org/x/vuln/cmd/govulncheck@latest ./...
  ```

- `go.mod` declares `go 1.24.0`. Go 1.24.x has had several stdlib security point releases; build with the latest 1.24 patch (or add a `toolchain` directive) and keep it current.
- Runtime dependency on `sshpass` is undocumented beyond a brew hint; the TUI silently falls back to plain `ssh` when it is missing (`tui/ssh.go:60`). Surface this in the UI once per session.

**Suggested CI (GitHub Actions):**

```yaml
name: ci
on: [push, pull_request]
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - run: go vet ./...
      - run: go test -race ./...
      - run: go run honnef.co/go/tools/cmd/staticcheck@latest ./...
      - run: go run github.com/securego/gosec/v2/cmd/gosec@latest -exclude=G104,G304,G703 ./...
      - run: go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

## 7. What is done well

- AES‑256‑GCM used correctly: fresh random nonce per encryption, nonce prepended, authenticated decryption, length check before slicing (`util/crypto.go`).
- Config directory `0700`, config/history files `0600`; temp add‑account script `0700` and removed after use.
- SSH username validation (`^[a-zA-Z0-9._-]+$`, no leading `-`, ≤32 chars) blocks option injection into `ssh`.
- All `exec.Command` calls except the one `sh -c` wrapper pass argv slices, not shell strings.
- `StrictHostKeyChecking=accept-new` rather than `no`.
- API key accepted from the environment, and `login` validates the key against the API before storing.
- Clipboard paste sanitisation strips control characters and newlines before inserting into single‑line prompts.
- Clear separation of TUI state into sub‑structs and a dispatch‑table key‑handler design that is easy to read and test.

## 8. Prioritised remediation plan

| Priority | Items | Effort |
|---|---|---|
| **P0 — this week** | H‑1 (sshpass via fd/env, drop `sh -c`), H‑2 (don't delete creds on generic exit), H‑3 (check `tsc` symlink target) | ~half a day |
| **P1 — next release** | M‑1 (keychain or random key file + honest docs), M‑2 (env var in script), M‑4 (race), M‑6 (login data loss), C‑1 (JSON), C‑2 (key‑auth flow) | 1–2 days |
| **P2 — hygiene** | Q‑1 module path, Q‑2 delete `internal/`, Q‑3 staticcheck, Q‑5/Q‑6 docs and gitignore, Q‑7 CI, Q‑8 tests for crypto/validation/history | 1–2 days |
| **P3 — polish** | L‑1…L‑6, C‑3…C‑7, Q‑4, Q‑9, Q‑10 | opportunistic |
