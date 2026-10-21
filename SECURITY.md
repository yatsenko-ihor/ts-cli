# Security

## Reporting a vulnerability

Please do not open a public issue for security problems. Email
**yatsenko.ihor@gmail.com** with a description and, if possible, steps to
reproduce. You will get a reply within a few days.

## What ts-cli stores and how

Everything lives in `~/.ts-cli/` (override with `TS_CLI_CONFIG_DIR`):

| File | Contents | Mode |
|---|---|---|
| `config.json` | accounts (tailnet + **encrypted** API key), default SSH username, per-machine SSH usernames and **encrypted** passwords | 0600 |
| `history.json` | remote commands you ran and up to 1 KB of their output | 0600 |
| `key` | random 32-byte master key (only when the store is `file`) | 0600 |
| `secret_store` | `file` or `keychain` | 0600 |

Encryption is AES-256-GCM with a fresh random nonce per secret. The master
key never derives from anything predictable.

## Where the master key lives

- **`file`** (default): `~/.ts-cli/key`. Protects a leaked `config.json`
  on its own. Does **not** protect against anyone who can read your home
  directory as your user, since they have both files.
- **`keychain`**: macOS Keychain, Linux Secret Service (D-Bus) or Windows
  Credential Manager. The key is unlocked with your login session and is
  never written under `~/.ts-cli`, so a copied or backed-up config
  directory alone cannot be decrypted.

Switch with `ts-cli secrets use keychain|file`; inspect with
`ts-cli secrets status`. If the keychain is selected but unreachable
(headless or SSH session), ts-cli falls back to the key file when one still
exists and warns. It never silently generates a new key in that state.

## Process-level hygiene

- Saved SSH passwords are handed to `sshpass` over a private file descriptor
  (`-d`), never on the command line, so they do not appear in `ps`.
- `ssh` and `tailscale` are always executed with argument vectors, never
  through a shell.
- SSH usernames are validated (`^[A-Za-z0-9._-]+$`, no leading `-`) and
  device addresses must parse as IPs before they are used.
- `StrictHostKeyChecking=accept-new` is used; an already-known host whose key
  changed is still rejected.
- Pass the Tailscale API key via `TAILSCALE_API_KEY` rather than `--api-key`:
  flags end up in shell history and are visible to other local users while
  the command runs.

## Threat model summary

| Protects against | Does not protect against |
|---|---|
| a leaked `config.json` (either store) | malware running as your user (either store) |
| a copied `~/.ts-cli/` directory or unencrypted backup (`keychain` store) | a compromised remote host you SSH into |
| other local users reading the process list | physical access to an unlocked session |

Treat `~/.ts-cli/` like `~/.ssh/`: do not sync it to shared folders or commit
it anywhere. If you ever suspect an API key leaked, revoke it in the Tailscale
admin console first; cleaning git history is not a substitute.

## Supply chain

CI runs `gofmt`, `go vet`, `go test -race`, `staticcheck`, `gosec` and
`govulncheck` on every push. Dependencies are pinned in `go.sum`.

## API endpoint override

`TS_CLI_API_URL` redirects all Tailscale API requests, including the Authorization header with your API key, to the given `http(s)` URL. It exists for proxies and test servers. Treat it like any other credential-bearing setting: do not set it from untrusted sources.
