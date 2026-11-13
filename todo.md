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
- [x] Release workflow: tagged builds for Linux/macOS/Windows attached to a GitHub Release
- [x] Version 1.2.0
- [x] Version 1.3.0: auto-reconnect, `d` forgets the saved password, empty timestamps in device JSON
- [x] Version 1.4.0: reboot and wait, port forwarding, health check, monitor, file manager, ssh-config, pins/notes/export, upgrade, doctor

## Not done

- [ ] `ts-cli account import`: create accounts from `TAILSCALE_API_KEY` / `tailscale switch --list`
- [ ] Per-account default SSH username (today there is one global default)
- [x] SSH in a tmux split (inside tmux, `s` opens a pane beside the list)
- [x] Machine tab: ping with details, key expiry, expire, rename, tags, delete
- [x] `ts-cli list` filters (--online, --os, --tag, --account, --name, --sort, names format)
- [x] File copy with scp (`f` upload, `F` download); local path completion (Tab), local browser (Ctrl+O) and remote browser
- [x] Device key reminders (line on the main screen, list flag)
- [x] Contextual key hints, `?` for help
- [x] Tailscale SSH when the machine offers it (option, off by default)
- [x] Background monitor (F12 → Monitor machines): latency history and down/up alerts (replaces the idea of re-enabling auto-ping)
- [x] Machine tab: Reboot and wait, Forward a port, Check health
- [x] `ts-cli ssh-config` (`--write`, `--install`)
- [x] `UseKeychain` with a lost key: creates a new key, removes the unreadable secrets and says what was removed
- [x] `runJob.kill` kills the whole process group (sshpass's ssh child too)
- [x] Requirement/coverage gate extended to `commands/` and the TUI account/list logic (all gated files at 100 %)
- [x] `account add` identifies the account by tailnet only and rejects a duplicate display name

## Future

### Fleet mode ("парк")
- [ ] Saved machine groups (by tag or by hand, e.g. "all Raspberry") and ready-made actions on a group: reboot, update, disk check, view logs, with one result table (a row per machine). Builds on multi-run, the Run panel and saved commands; what is missing is the groups and the action templates.

### Accounts and login
- [ ] `ts-cli account import` (from plain JSON / `TAILSCALE_API_KEY` / `tailscale switch --list`) and a per-account default SSH username (listed above)

### Terminal and scripting
- [ ] Mouse click on the Browse button of the path prompts (needs terminal mouse capture, which disables normal text selection; enable it only while a path prompt is open)
- [ ] Shell completion: `ts-cli completion bash|zsh|fish` (Cobra provides it almost for free; document the install steps)
- [ ] `ts-cli connect <name>` with fuzzy name matching, without the TUI, for scripts and aliases
- [ ] Highlight and search by tag and OS in the list; remember the last filter
- [ ] A background mode for pinging several machines at once (`p` on marked machines exists; the F12 monitor covers the visible list)

### Tailscale API administration
- [ ] **Auth keys**: `ts-cli key create --tag server --ephemeral` to onboard new machines
- [ ] **Subnet routes and exit nodes**: view and approve advertised routes (a machine that offers `192.168.1.0/24` or `0.0.0.0/0`), mark a machine as exit node, use one from this computer (`tailscale set --exit-node=…`) from the TUI; device posture attributes
- [ ] **ACL**: `acl show` and `acl validate` first (read-only); applying a policy only with validation and a confirmation, since a wrong policy can lock you out
- [ ] **Audit and DNS**: the configuration audit log (who changed what, when; depends on the plan) and the DNS settings (nameservers, search paths, MagicDNS)
- [ ] Users and invites, webhooks, flow logs

## Ideas (not planned yet)

- Importing hosts from `~/.ssh/config` is a bigger job
- Tailscale SSH: fall back to plain ssh when the ACL refuses
