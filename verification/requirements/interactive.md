# Requirements: `ts-cli interactive`, `commands/interactive.go` (REQ-INT)

The command loads the configuration, fetches the devices and starts the Bubble Tea program. The tests replace the program by the variable `runProgram` (a recorder), `util.KeyStoreNotice` by `keyStoreNotice`, and use a fake API (`client.SetBaseURL`) and a private PATH.

- **REQ-INT-001** The command shall return "failed to load configuration" when the configuration is unreadable and "no accounts configured" with the `ts-cli login` hint when no account is stored (also when explicit credentials are given), and shall not start the program.
- **REQ-INT-002** When the key store reports a notice the command shall print "Warning: <notice>" on standard error and continue; with no notice it shall print nothing.
- **REQ-INT-003** When both `--api-key` and `--tailnet` are given the command shall fetch only that tailnet with that key (devices tagged with the tailnet), shall not check the local Tailscale or the stored accounts, shall print "No devices found in the specified tailnet." and return nil without starting the program when there are no devices, and shall return "failed to list devices" on an API error.
- **REQ-INT-004** Otherwise (including when only one of the two flags is given) the command shall warn when the local Tailscale is not running, print "Fetching devices from all configured accounts...", fetch the devices of all stored accounts handing the model each account's name, key, tailnet, active and hidden flags, print "Found N device(s) from M account(s)", and when there are no devices print "No devices found in any of your configured accounts." and return nil without starting the program.
- **REQ-INT-005** The command shall start the program with a model built from the devices and the configuration (default user, machine credentials, saved commands, last machine, UI settings) and return nil when it ends, or "TUI error: <cause>" when it fails.
- **REQ-INT-006** The default program runner shall run the Bubble Tea program with the alternate screen and return nil when it ends normally and its error when it fails.
- **REQ-INT-007** The command shall be named `interactive` with the aliases `i` and `tui` and the flags `--api-key` and `--tailnet` (default empty).

## MC/DC

1. `apiKey != "" && tailnet != ""`: `TestDO178CInteractiveExplicitCredentials` (both: explicit path) vs `TestDO178CInteractiveStoredAccounts` loop with only `--api-key` (false) and only `--tailnet` (false).
2. Single-condition decisions, each exercised both ways: `len(cfg.Accounts) == 0`, `notice != ""` (`TestDO178CInteractiveKeyStoreNotice`), `len(devices) == 0` on both paths, `err != nil` for the API (`...ExplicitCredentials`) and for the program (`TestDO178CInteractiveRunsTheProgram`).

## Derived requirements / unreachable code

Derived requirements:

- The device list of the explicit-credentials path is not combined with the stored accounts; the model still receives a one-element account list marked active (REQ-INT-003).
- Hidden accounts are fetched like the others; the model decides what to show (verified through the model's view in `TestDO178CInteractiveStoredAccounts`).
- Test seams added to non-test code (default behaviour identical): `runProgram` (default runs `tea.NewProgram(m, programOptions...)`), `programOptions` (default `tea.WithAltScreen()`), `keyStoreNotice` (default `util.KeyStoreNotice`).

Known limitation: the default `runProgram` is exercised with a model that quits at once, a cancelled context and no terminal input/output; the behaviour on a real terminal is not tested.

Unreachable code: none. Every statement of `interactive.go` is executed by the tests (100 % statement coverage).
