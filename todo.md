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
- [x] Version 1.0.0

## Not done

- [ ] `ts-cli account import`: create accounts from `TAILSCALE_API_KEY` / `tailscale switch --list`
- [ ] Per-account default SSH username (today there is one global default)
- [ ] Embedded terminal: tmux split first (open the SSH session beside the list), PTY emulator later
- [ ] Re-enable background auto-ping (`autoPingEnabled` in `tui/ping.go`) once it is cheap enough
- [ ] Decide: `UseKeychain` silently creates a new key when the keychain item is lost and no key file exists (old data becomes unreadable); make it an error?
- [ ] `runJob.kill` signals only the direct child; use a process group so ssh children of sshpass die too (see `verification/requirements/runjob.md`)
- [ ] Extend the requirement/coverage gate to `commands/` and the TUI account/list logic
