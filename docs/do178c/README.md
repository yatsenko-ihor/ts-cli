# Verification approach (DO-178C style)

ts-cli handles SSH passwords, API keys and executes commands on remote machines.
This directory applies the verification discipline of **DO-178C** (software
considerations in airborne systems) to the parts of the code where a defect
costs the most. It is a *verification practice*, not a certification claim:
ts-cli is not airborne software, there is no certification authority, and the
independence and tool-qualification objectives of DO-178C are not met.

## What was applied

| DO-178C objective | How it is met here |
|---|---|
| Requirements-based testing (6.4) | Low-level requirements (LLR) for each critical file live in `verification/requirements/*.md`, each with an ID (`REQ-<AREA>-<NNN>`). Every test states which requirement it verifies with a `// Verifies: REQ-…` comment. |
| Normal-range and robustness test cases (6.4.2) | Each requirement is exercised with normal inputs, boundary values (just inside / just outside a limit) and invalid or hostile inputs. |
| Bidirectional traceability (6.5, 7.2.4) | `verification/trace_test.go` fails when a requirement has no test, when a test cites an unknown requirement, or when an ID is defined twice. The generated matrix is `docs/do178c/traceability.md`. |
| Structural coverage (6.4.4.2) | Statement coverage is measured and **gated** per file (`verification/coverage-targets.txt`, enforced by `verification/cmd/covergate` and in CI). Decision/condition coverage is measured with `gobco` and reported in `coverage.md`. |
| MC/DC for compound decisions (Level A) | Go has no MC/DC tool. For each compound boolean decision in the critical files the tests include an *independence pair* per condition (a pair of cases that differ in that one condition only and flip the outcome); the decisions and their pairs are listed in the requirement files under "MC/DC". |
| Reviews and analyses (6.3) | Requirement files record the *derived requirements* (behaviour found in the code that no user need asked for) and the *dead or defensive code* that cannot be reached by a test, with the justification. |

## Assurance level chosen

The files below are treated as the equivalent of **DAL B** (statement +
decision coverage required, MC/DC not required) with **MC/DC added for the
compound decisions that guard secrets or command construction** (the DAL A
objective, applied selectively):

1. `util/validation.go`: input barrier against option and command injection
2. `util/crypto.go`: encryption of stored secrets
3. `util/keystore.go`, `util/paths.go`: master-key custody and location
4. `tui/ssh.go`: construction of the ssh process, how the password is passed, failure classification
5. `tui/sshflow.go`: when credentials are saved or forgotten
6. `config/config.go`: persistence of secrets, atomic writes
7. `client/tailscale.go`: API access and API-key health
8. `tui/runjob.go`, `scripts/scripts.go`: process execution, script argument quoting

Selection rule: a file is on the list if a defect in it can (a) leak or lose a
secret, (b) run an unintended command, or (c) corrupt the stored
configuration. UI rendering, layout and key handling are deliberately *not*
on the list; their failures are visible and recoverable.

## How to run

```bash
go test -race ./...                         # all tests, including the traceability check
go run ./verification/cmd/covergate         # per-file statement coverage gate
UPDATE_TRACE=1 go test ./verification       # regenerate docs/do178c/traceability.md
```

## Conventions for new tests of these files

- Add or change the requirement first (`verification/requirements/<area>.md`), then the test.
- Put `// Verifies: REQ-AREA-001, REQ-AREA-002` in the comment block directly above the `func Test…`.
- A table-driven test must contain the boundary and the hostile case, not only the happy path.
- Do not lower a threshold in `coverage-targets.txt` to make a build pass; add the test or document the unreachable code in the requirement file.
