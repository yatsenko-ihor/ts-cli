# Requirements: login, `commands/login.go` (REQ-LGN)

`login` is `account add` that also makes the account active. The key handling, validation and storage are the shared code verified in `account.md`; this file states what `login` itself guarantees. Tests use an isolated config directory and a fake Tailscale API.

- **REQ-LGN-001** `ts-cli login --tailnet=<t>` shall store the account for an unknown tailnet and make it the only active account, whatever account was active before.
- **REQ-LGN-002** `login` shall print the validation steps and the result and shall never print the API key, never store it in clear in the configuration file, and never include it in an error.
- **REQ-LGN-003** The key shall be read from `--api-key-stdin`, else `--api-key`, else `TAILSCALE_API_KEY`; a stdin that cannot be read shall make `login` fail before any request and store nothing.
- **REQ-LGN-004** `login` shall store nothing, and keep the previously active account, when the API rejects the key or cannot be reached (error "failed to validate API key").
- **REQ-LGN-005** `login` shall reject a missing `--tailnet`, a missing key, an invalid or blank tailnet, a malformed key and an invalid name, before any request is sent and without creating the configuration.
- **REQ-LGN-006** When the configuration cannot be read or written (corrupt file) `login` shall fail with "failed to store config" and shall leave the file unchanged.
- **REQ-LGN-007** `login` shall document that the key is better passed through stdin or the environment, and shall offer the flags `--tailnet`, `--name`, `--api-key` (usage text discouraging it) and `--api-key-stdin`.
- **REQ-LGN-008** `login` for an already configured tailnet shall update that account's key (and its name when `--name` is given and differs from the tailnet) in place, without a duplicate, and make it the only active account.

## MC/DC

`login.go` contains no compound boolean decision; its only conditions are the single `err != nil` after `keys.resolve` (true: `TestLGNLoginUnreadableStdinIsAnError`, false: `TestLGNLoginStoresAndActivatesAccount`) and the discarded error of `MarkFlagRequired`. The decisions made on its behalf in `addAccount` and `keyInput.resolve` are in `account.md`.

## Derived requirements / unreachable code

- `login` declares no `Args` constraint, so extra positional arguments are accepted and ignored (not required by a user need; not asserted).
- Unreachable statements: none.
