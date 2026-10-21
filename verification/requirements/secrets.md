# Requirements: master-key commands, `commands/secrets.go` (REQ-SEC)

`ts-cli secrets status|use keychain|use file` shows and moves the master key that encrypts stored API keys and SSH passwords, and, when the key was lost, removes the secrets that can no longer be read and says what was removed. Tests use an isolated config directory and go-keyring's in-memory provider (`keyring.MockInit`, `MockInitWithError`); the real keychain is never touched. All output goes through the command's writer so tests can capture it.

## status

- **REQ-SEC-001** `secrets status` shall print the configured secret store (`file` when none is recorded, `keychain` after a switch) and whether the OS keychain is available, giving the reason when it is not.
- **REQ-SEC-002** `secrets status` shall load the configuration and print the number of accounts and of saved machine credentials.
- **REQ-SEC-003** No command in this group shall print an API key, an SSH password, the master key (raw or base64) or the key file path contents, on any path.
- **REQ-SEC-004** When the configuration cannot be decrypted (or read), `secrets status` shall print "cannot be decrypted" with the reason instead of the counts and shall still succeed.
- **REQ-SEC-005** When the key store recorded a notice (keychain unreachable, key file used instead), `secrets status` shall print it after the counts; without a notice it shall print none.

## use

- **REQ-SEC-006** `secrets use keychain` shall move the master key into the keychain (same key value, key file removed, store recorded), print the confirmation, verify that the configuration is still readable and print that; repeating it shall be a harmless no-op that prints no warning.
- **REQ-SEC-007** `secrets use file` shall move the key back into `~/.ts-cli/key` (mode 0600, same key value), remove the keychain item, record the store, print the confirmation and the verification; when the file store is already active it shall be a harmless no-op.
- **REQ-SEC-008** An argument other than exactly `keychain` or `file` (other case, spaces, shell syntax, NUL, empty) shall fail with "unknown store", print no success text and change nothing; zero or more than one argument shall be rejected.
- **REQ-SEC-009** When the keychain cannot be used (`use keychain`) or the key cannot be read from anywhere (`use file`) the command shall fail with the reason, print no success text and leave the key file, the keychain and the recorded store as they were.
- **REQ-SEC-010** After a switch the command shall try to read the configuration, and when it cannot be decrypted it shall fail with "switched, but the config can no longer be decrypted" and shall not print the verification line; it shall not modify the configuration file.

## Lost key

- **REQ-SEC-011** When `use keychain` finds the keychain as the configured store but its item gone and no key file (key lost), it shall create a new key, store it, and print a warning that the old secrets cannot be recovered; when the key is not lost no warning shall be printed.
- **REQ-SEC-012** After that warning the command shall remove the accounts whose key is unreadable and clear the saved SSH passwords that are unreadable (usernames stay), and shall print what was removed: the account names (with how to add them again) and/or the machine names (the id when a machine has no name, sorted), or "No stored secret depended on the old key." when nothing was removed; a category with nothing removed shall not be mentioned.
- **REQ-SEC-013** When the unreadable secrets cannot be removed (for example the configuration is corrupt) the command shall print "Could not remove the unreadable secrets" with the reason and shall fail through the verification step, leaving the file unchanged.
- **REQ-SEC-014** A switch where nothing was lost (file to keychain, repeated keychain, keychain healthy) shall never touch accounts or passwords.

## Wiring

- **REQ-SEC-015** The `secrets` command shall offer exactly the subcommands `status` and `use`; `use` shall advertise `keychain` and `file` as valid arguments.

## MC/DC

1. `reportLostKey`: `len(dropped.Accounts) == 0 && len(dropped.Passwords) == 0`
   - A (accounts empty): "nothing depended" (`TestSECLostKeyReportsOnlyWhatWasLost`, A true, B true: "No stored secret depended") against "only passwords" in the same test (A true, B false: continues to the lists), B true in both pairs' counterpart.
   - B (passwords empty): "only accounts" (A false) cannot isolate B; the pair for B is "nothing depended" (A true, B true: message) against "only passwords" (A true, B false: lists), A true in both.
   - A alone is shown by "nothing depended" (A true, B true) against "only accounts" (A false, B true).
2. `reportLostKey`: `len(dropped.Accounts) > 0` and `len(dropped.Passwords) > 0` are separate decisions, each true and false in `TestSECLostKeyReportsOnlyWhatWasLost` ("only accounts", "only passwords") and both true in `TestSECLostKeyRemovesUnreadableSecretsAndSaysSo`.

All other decisions in the file (`err != nil`, `util.KeyWasReset()`, the `switch` on the store, `util.KeychainAvailable() != nil`, `util.KeyStoreNotice() != ""`) are single conditions covered both ways by the tests named above.

## Derived requirements / unreachable code

- Output is written to `cmd.OutOrStdout()` (was `os.Stdout`) so it can be captured; the text is unchanged.
- `secrets status` accepts and ignores extra arguments (no `Args` constraint); not asserted.
- Unreachable statements: none; every statement of `secrets.go` is executed.
