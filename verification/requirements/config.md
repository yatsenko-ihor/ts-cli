# Requirements: configuration store, `config/config.go` (REQ-CFG)

`config.go` owns `$TS_CLI_CONFIG_DIR` (default `~/.ts-cli`) `/config.json`. It holds API keys and SSH password ciphertext, so a defect can leak a secret, lose it, or corrupt the whole configuration.

## Location and directory

- **REQ-CFG-001** `Path` shall return `<config dir>/config.json`, honouring `TS_CLI_CONFIG_DIR`, shall create a missing config directory (including parents) with mode 0700, shall succeed when the directory already exists, and shall not create the file itself.
- **REQ-CFG-002** When the config directory cannot be determined or created (no home directory, a regular file where the directory should be), `Path`, `Load`, `Save` and `Update` shall return an error, and `Update` shall not call its callback.

## Load

- **REQ-CFG-003** `Load` on a missing `config.json` (and no legacy file) shall return an empty configuration (non-nil empty `Accounts`, `ConfigVersion` = current version) and shall not create the file.
- **REQ-CFG-004** `Load` shall replace a null or absent `accounts` by an empty list and an empty `config_version` by the current version, and shall keep an explicit version unchanged.
- **REQ-CFG-005** `Load` shall return an error naming the file when its content is not valid configuration JSON (garbage, empty, truncated, wrong type, trailing junk), and neither `Load` nor `Update` shall modify or replace that file.
- **REQ-CFG-006** `Load` shall return an error ("failed to read config file") when the file exists but cannot be read, and shall leave it alone.
- **REQ-CFG-007** `Load` shall decrypt every account's `api_key_enc` into the in-memory `APIKey`, and shall not rewrite a file in which every key is already encrypted.
- **REQ-CFG-008** When an `api_key_enc` value cannot be decrypted (bad base64, too short, wrong key or corrupted), `Load` shall return an error naming the account, shall return no partial configuration, and shall not modify the file. An empty `api_key_enc` is "no encrypted key" and is not an error.
- **REQ-CFG-009** A legacy cleartext `api_key` shall be usable after `Load` and shall be re-encrypted on disk by that same `Load`: afterwards no cleartext key and no `api_key` field remain in the file, the file mode is 0600 and no temporary file is left.
- **REQ-CFG-010** A failure while re-encrypting legacy keys shall not prevent `Load` from returning the configuration, and shall leave the file unchanged. When an account has both `api_key_enc` and `api_key`, the encrypted value shall win and no rewrite shall be triggered.

## Save

- **REQ-CFG-011** `Save` shall write API keys only as `api_key_enc` (AES-GCM via `util.Encrypt`): no API key substring and no `api_key` field shall appear anywhere in the file bytes, and the caller's in-memory configuration shall keep its cleartext keys and gain no ciphertext.
- **REQ-CFG-012** The file written by `Save` shall have mode 0600 even when it replaces a file with looser permissions, and a directory created by `Save` shall have mode 0700.
- **REQ-CFG-013** `Save` shall be atomic (temporary file in the same directory, then rename): after success no temporary file remains, and when creating the temp file, setting its mode, writing, closing or renaming fails, `Save` shall return an error, shall leave the previous file byte-for-byte intact, and shall leave no temporary file.
- **REQ-CFG-014** When an API key cannot be encrypted or the configuration cannot be marshalled, `Save` shall return an error (naming the account but never the key), and shall leave the previous file untouched.
- **REQ-CFG-015** `Save` shall set an empty `ConfigVersion` to the current version, and shall keep a non-empty one.
- **REQ-CFG-016** A `Save`/`Load` round trip shall preserve every field: accounts (name, tailnet, active, hidden, key), SSH username, machine credentials (password ciphertext stored verbatim), saved commands, UI settings and last machine. An account carrying only `api_key_enc` shall keep it, and an account without any key shall be written without key fields.

## Update

- **REQ-CFG-017** `Update` shall load the current on-disk configuration, pass it to the callback, and save the result, so changes made by an earlier writer and fields the callback does not touch are preserved.
- **REQ-CFG-018** `Update` shall write nothing when `Load` fails (and then not call the callback) or when the callback returns an error (and then return that error), and shall return the error of a failing save.
- **REQ-CFG-019** Concurrent `Update` calls in one process shall be serialised so that none of their changes is lost.

## Accounts

- **REQ-CFG-020** `FindAccount` shall return a pointer to the stored account whose `Name` or `Tailnet` equals the query exactly (case-sensitive), and nil when none matches (including the empty query).
- **REQ-CFG-021** `AddAccount` for an unknown tailnet shall append a new account; with `setActive` it shall become the only active account.
- **REQ-CFG-022** `AddAccount` for an existing tailnet shall update that account's name and key in place (no duplicate, hidden flag kept), shall make it the only active account only when `setActive` is true, and shall otherwise leave all active flags unchanged. Matching is by tailnet only.
- **REQ-CFG-023** `AddAccount` shall preserve the invariant "when any account exists exactly one is active": a new account becomes active when no account is active, even if `setActive` is false.
- **REQ-CFG-024** `RemoveAccount` shall remove every account whose `Name` or `Tailnet` equals the argument and return true, and shall change nothing and return false when none matches (including the empty argument).
- **REQ-CFG-025** When `RemoveAccount` removes the active account and others remain, the first remaining account shall become active; removing an inactive account shall keep the active one; removing the last account shall leave an empty list without error.
- **REQ-CFG-026** `GetActiveAccount` shall return the account flagged active or nil; `GetAccountByTailnet` shall match the tailnet only (not the name) and nil for no match; `GetAllAccounts` shall return all accounts in order.
- **REQ-CFG-027** `SetActiveAccount` shall make the matching account (by name or tailnet) the only active account and return true; for no match it shall change nothing and return false. It shall also repair a configuration with several active flags.
- **REQ-CFG-028** `SetAccountHidden` shall set the hidden flag of the matching account only and return true, or return false for no match; it shall not touch the active flag.
- **REQ-CFG-029** `UpdateAccount` shall rename and/or re-key the matching account, leaving a field unchanged when its argument is empty (or the new name equals the current one); it shall return an error and change nothing when the account is unknown, or when the new name equals the `Name` or `Tailnet` of a different account.

## Machine credentials and saved commands

- **REQ-CFG-030** `SetMachineCredential` shall create the map if needed and store or overwrite the credential under the key; `DeleteMachineCredential` shall remove it and shall be a no-op for an unknown key, an empty key or a nil map.
- **REQ-CFG-031** `UpsertCommand` shall replace the command named like the new one; when `oldName` is non-empty it shall replace the command stored under `oldName` instead (rename), and when no such command exists it shall append.
- **REQ-CFG-032** `DeleteCommand` shall remove the command with exactly the given name preserving the order of the others and return true, or return false and change nothing when none matches.

## Legacy migration

- **REQ-CFG-033** When `config.json` is missing and a pre-1.0 `config` file holds `TAILSCALE_API_KEY` and `TAILNET`, `Load` shall build one active account named after the tailnet (plus `SSH_USERNAME`, if any) from the KEY=VALUE lines (surrounding whitespace trimmed, blank, comment, no-`=` and unknown lines ignored, value split at the first `=`), save it encrypted with mode 0600, rename the old file to `config.backup` unchanged, and return the new configuration.
- **REQ-CFG-034** The migration shall not happen, and the old file shall be left untouched and no `config.json` created, when the key or the tailnet is missing or empty.
- **REQ-CFG-035** When the migrated configuration cannot be saved, `Load` shall return the empty configuration and leave the old file in place with no backup and no temporary file.
- **REQ-CFG-036** When the old file cannot be renamed to `.backup`, `Load` shall still return the migrated configuration (already saved) and print a warning on stderr.
- **REQ-CFG-037** The migration shall be attempted only when `config.json` does not exist; an unreadable old file or an undeterminable config directory shall mean "nothing to migrate".

## MC/DC

Short-circuit evaluation applies; independence pairs are given as test cases (subtests) that differ in that condition only and flip the outcome of the decision.

| # | Decision | Condition | Pair (outcome false / outcome true) |
|---|---|---|---|
| D1 | `FindAccount`: `Name == q \|\| Tailnet == q` | Name==q | `TestFindAccount/query=nothing` (F,F) / `query=work` (T,-) |
| | | Tailnet==q | `TestFindAccount/query=nothing` (F,F) / `query=test.com` (F,T) |
| D2 | `RemoveAccount`: `Name == q \|\| Tailnet == q` | Name==q | `TestRemoveAccount/unknown…` (F,F) / `by name` (T,-) |
| | | Tailnet==q | `TestRemoveAccount/unknown…` (F,F) / `by tailnet` (F,T) |
| D3 | `RemoveAccount`: `GetActiveAccount()==nil && len(Accounts)>0` | no active | `TestRemoveAccount/removing inactive keeps the active account` (F) / `removing active promotes first remaining` (T,T) |
| | | len>0 | `TestRemoveAccount/removing the last account…` (T,F) / `removing active promotes…` (T,T) |
| D4 | `AddAccount`: `setActive \|\| GetActiveAccount()==nil` | setActive | `TestAddAccountNew/appends inactive when another is active` (F,F) / `setActive steals the active flag` (T,-) |
| | | no active | `TestAddAccountNew/appends inactive when another is active` (F,F) / `no active among existing: new one becomes active` (F,T) |
| D5 | `UpdateAccount`: `newName != "" && newName != acc.Name` | newName != "" | `TestUpdateAccount/empty args change nothing` (F) / `no conflict: new name unused` (T,T) |
| | | newName != acc.Name | `…/rename to own current name is a no-op` (T,F) / `…/no conflict: new name unused` (T,T); observable through the conflict check: `…/no conflict: new name equals own old name while another account uses it as tailnet` (no error) / `…/conflict: new name differs from own and another account uses it as tailnet` (error) |
| D6 | `UpdateAccount`: `&Accounts[i] != acc && (Name == n \|\| Tailnet == n)` | other account (A) | `…/no conflict: rename to own tailnet` (own record matches Tailnet, A=F: no error) / `…/conflict: other account has that Tailnet` (A=T, same match: error) |
| | | Name==n (B) | `…/no conflict: new name unused` (A=T,B=F,C=F: no error) / `…/conflict: other account has that Name` (A=T,B=T: error) |
| | | Tailnet==n (C) | `…/no conflict: new name unused` (A=T,B=F,C=F) / `…/conflict: other account has that Tailnet` (A=T,B=F,C=T: error) |
| D7 | `migrateOldConfig`: `apiKey == "" \|\| tailnet == ""` | apiKey=="" | `TestMigrationRequiresKeyAndTailnet/missing key` and `empty key value` (T,F: no migration) / `TestMigrationParsesOldFormatAndEncrypts` (F,F: migrated) |
| | | tailnet=="" | `…/missing tailnet` and `empty tailnet val` (F,T: no migration) / `TestMigrationParsesOldFormatAndEncrypts` (F,F) |

Single-condition decisions (`if err != nil`, `errors.Is(err, ErrNotExist)`, `if cfg.ConfigVersion == ""`, `if cfg.Accounts == nil`, `acc.APIKey != ""` in `forDisk`, `if setActive`, `if !removed`, `target == nil`, `acc == nil`, `newAPIKey != ""`, `oldName == ""`, `ok` from `strings.Cut`, loop conditions) are covered with both outcomes by the tests named under their requirements. The `switch` in `load` (`api_key_enc` set / else cleartext `api_key` set / neither) is covered for all three arms and the "both set" case (REQ-CFG-009, 010, 008).

## Derived requirements / unreachable code

Derived requirements (behaviour no user need asked for, found in the code and now specified):

- D-1 `FindAccount`, `RemoveAccount`, `SetActiveAccount` treat name and tailnet interchangeably, so an account whose name equals another's tailnet is ambiguous (first match wins; `RemoveAccount` removes both). Account names are not required to be unique by `AddAccount`; uniqueness is enforced only by `UpdateAccount` (REQ-CFG-029). Left as is.
- D-2 `UpdateAccount` allows renaming an account to its own tailnet (own record is excluded from the conflict check).
- D-3 `Load` normalises a null/absent account list and version, and does not repair a file with zero or several active accounts (only `SetActiveAccount` repairs it).
- D-4 The migration trims whitespace around keys and values and splits at the first `=`, so values may contain `=`. Windows line endings are tolerated because lines are trimmed.
- D-5 Blank and `#` comment lines in the legacy file need no explicit test in the parser: they cannot yield a recognised key. The original redundant `line == "" || HasPrefix(line, "#")` decision was removed (behaviour identical) because its conditions had no observable effect and so could not have independence pairs.

Non-test changes made for verifiability (all behaviour-preserving unless stated):

- Test seams `encryptSecret`, `decryptSecret`, `marshalIndent` and `createTemp` (plus the `tmpFile` interface) so I/O and crypto failures that cannot be provoked portably (chmod, write and close failure of the temp file, encryption failure, JSON marshal failure) can be injected. Production values are `util.Encrypt`, `util.Decrypt`, `json.MarshalIndent`, `os.CreateTemp`.
- Bug fix (REQ-CFG-023): `AddAccount` appended the first account as inactive when `setActive` was false, leaving a configuration with accounts but none active. A new account now becomes active when no account is active.

Unreachable statements: none. All statements of `config.go` are executed by the tests (100.0 %). The error branch of `json.MarshalIndent` is reachable only through the `marshalIndent` seam, because the `Config` type cannot fail to marshal.

## Lost master key (added with `DropUnreadableSecrets`)

- **REQ-CFG-038** `DropUnreadableSecrets` shall remove every account whose encrypted API key cannot be decrypted, clear every saved SSH password that cannot be decrypted (keeping the machine's username), report both (passwords by machine name, falling back to the machine key, sorted), keep everything readable and all unrelated settings, keep the file mode 0600 and leave no temporary file; when the removed account was the active one the first remaining account shall become active.
- **REQ-CFG-039** `DropUnreadableSecrets` shall not write the file when every secret is readable.
- **REQ-CFG-040** `DropUnreadableSecrets` shall leave a surviving active account active, shall not activate anything when no account remains, and shall not touch accounts that have only a legacy cleartext key.
- **REQ-CFG-041** `DropUnreadableSecrets` shall do nothing when the config file does not exist and shall return an error, leaving the file unchanged, when it is corrupt, unreadable, its directory cannot be created, or the rewrite fails.
