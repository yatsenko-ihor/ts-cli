# TODO

## Done

- [x] Security audit and fixes (SSH credential handling, encrypted API keys, atomic config)
- [x] OS keychain as an option for the master key, with file fallback (`ts-cli secrets`)
- [x] Shared `config` package; one owner of `~/.ts-cli/config.json`
- [x] Run panel: local scripts, saved commands, recent runs, streaming output
- [x] Run on several machines in parallel (mark with `Space`/`*`)
- [x] Key-first SSH, ask for the password only on failure, save it on success
- [x] Start on the last connected machine (if online)
- [x] Option/Alt word navigation in all inputs
- [x] No marking/running on unreachable machines; ping (`p`); hybrid status (API + ping)
- [x] List redesign: short names, hide offline, details panel, group by account, symbols, F12 settings
- [x] Accounts: window in the TUI (add/edit/remove/show-hide/switch), key health and expiry warning
- [x] Accounts: `ts-cli account add|list|edit|use|hide|show|remove`, `--api-key-stdin`, `list --format=json`, `list --check`
- [x] Active (this computer) vs. visible (shown in list) accounts
- [x] Themes: light, dark, hacker (F12 → Theme)
- [x] DO-178C-style verification of the critical files: requirements, traceability check, 100% statement coverage gate in CI (`docs/do178c/`)
- [x] `ts-cli install`: install as a new file (and ad-hoc re-sign on macOS) so `tsc`/`ts-cli` are not killed after reinstall
- [x] Themes: dark-pastel, calmer hacker, custom themes from `~/.ts-cli/themes/*.json`
- [x] Version 1.2.0

## Not done

- [ ] `ts-cli account import`: create accounts from `TAILSCALE_API_KEY` / `tailscale switch --list`
- [ ] Per-account default SSH username (today there is one global default)
- [x] SSH in a tmux split (inside tmux, `s` opens a pane beside the list)
- [x] Machine tab: ping with details, key expiry, expire, rename, tags, delete
- [x] `ts-cli list` filters (--online, --os, --tag, --account, --name, --sort, names format)
- [x] File copy with scp (`f` upload, `F` download); local path completion (Tab) and browser (Ctrl+O)
- [x] Device key reminders (line on the main screen, list flag)
- [x] Contextual key hints, `?` for help
- [x] Tailscale SSH when the machine offers it (option, off by default)
- [ ] Embedded terminal inside ts-cli itself (PTY emulator) for use outside tmux
- [ ] Re-enable background auto-ping (`autoPingEnabled` in `tui/ping.go`) once it is cheap enough
- [x] `UseKeychain` with a lost key: creates a new key, removes the unreadable secrets and says what was removed
- [x] `runJob.kill` kills the whole process group (sshpass's ssh child too)
- [x] Requirement/coverage gate extended to `commands/` and the TUI account/list logic (all gated files at 100 %)
- [x] `account add` identifies the account by tailnet only and rejects a duplicate display name

## Ideas (not planned yet)

- Browse remote paths in the upload/download prompts (an `ssh host ls -1Ap <path>` listing through the same credential flow: needs async loading, quoting of paths, parsing names with spaces; browse the highlighted machine only)
- Tailscale API: create auth keys (`ts-cli key create --tag … --ephemeral`) to onboard machines
- Tailscale API: subnet routes (view, approve), device posture attributes
- Tailscale API: ACL policy (`acl show`, `acl validate`), DNS settings, users and invites, webhooks, audit and flow logs
- Export machines as `~/.ssh/config` blocks (`ts-cli ssh-config`); importing hosts from `~/.ssh/config` is a bigger job
- Saved machine groups / tag-based selection for runs
- Ready-made group actions (reboot, update, disk check) on the marked machines
- Tailscale SSH: fall back to plain ssh when the ACL refuses
- Ping/latency history per machine
- Mouse click on the Browse button (needs terminal mouse capture, which disables normal text selection; could be enabled only while a path prompt is open)
