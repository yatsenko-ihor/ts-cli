# Requirements: master-key custody (REQ-KEY) - util/keystore.go

The master key is 32 random bytes that live in exactly one place: the key file
(`<config>/key`, 0600) or the OS keychain. A key shall never be created
silently when the keychain is the configured store.

- **REQ-KEY-001** CurrentSecretStore shall report `file` when the `secret_store` file does not exist.
- **REQ-KEY-002** CurrentSecretStore shall report `keychain` when the `secret_store` file contains `keychain`, ignoring surrounding white space and a trailing newline.
- **REQ-KEY-003** CurrentSecretStore shall report `file` for any other content (empty, `file`, different case, unknown word, `keychain` embedded in other text) or when the file cannot be read (e.g. it is a directory).
- **REQ-KEY-004** CurrentSecretStore shall report `file` when the configuration directory cannot be determined.
- **REQ-KEY-005** setSecretStore shall create the configuration directory if needed and write `<store>\n` to `secret_store` with mode 0600.
- **REQ-KEY-006** setSecretStore shall return an error when the configuration directory cannot be created, when its path cannot be resolved, or when the file cannot be written.
- **REQ-KEY-007** KeyStoreNotice shall return the empty string when no notice has been recorded, and the recorded notice otherwise.
- **REQ-KEY-008** resetKeyCache shall forget both the cached key and the notice.
- **REQ-KEY-009** KeychainAvailable shall return nil when the keychain probe succeeds or answers "not found", and shall return the keychain error otherwise.
- **REQ-KEY-010** keychainGetKey shall return the 32-byte key decoded from the base64 keychain item, ignoring surrounding white space.
- **REQ-KEY-011** keychainGetKey shall return an error for an item that is not valid base64, for a decoded length other than 32 (0, 31 and 33 rejected, 32 accepted) and when the keychain read itself fails.
- **REQ-KEY-012** keychainSetKey shall store the key as standard base64 under service `ts-cli` and account `master-key`, and shall return the keychain error on failure.
- **REQ-KEY-013** keychainDeleteKey shall return nil when the item was deleted or does not exist, and shall return any other keychain error.
- **REQ-KEY-014** readFileKey shall return the content of the key file only when it is exactly 32 bytes (0, 31 and 33 rejected, 32 accepted), and shall return an error when the file is missing, unreadable or its directory cannot be resolved.
- **REQ-KEY-015** newRandomKey shall return 32 bytes from the cryptographic random source, different on each call, and an error (no key) when the random source fails.
- **REQ-KEY-016** writeFileKey shall write the key to the key file with mode 0600 via a temporary file and rename, replacing an existing key file atomically, and shall leave no temporary file behind on success.
- **REQ-KEY-017** writeFileKey shall return an error and remove its temporary file when the directory cannot be ensured or resolved, or when creating, chmod-ing, writing, closing or renaming the temporary file fails.
- **REQ-KEY-018** loadOrCreateFileKey shall return an existing valid key file unchanged without rewriting it.
- **REQ-KEY-019** loadOrCreateFileKey shall, when the key file does not exist, create the configuration directory (0700) and a 32-byte key file with mode 0600 and return that key.
- **REQ-KEY-020** loadOrCreateFileKey shall return an error and preserve the file when the key file exists but is unreadable or has an invalid length (a corrupt key is never silently replaced, because that would orphan all encrypted data).
- **REQ-KEY-021** loadOrCreateFileKey shall, when another process creates the key file between the existence check and the exclusive create, return the key read from that file (or the read error) rather than overwrite it.
- **REQ-KEY-022** loadOrCreateFileKey shall return an error when the random source, the directory, the path resolution, the exclusive create, the write or the close fails, and shall remove a partially written key file.
- **REQ-KEY-023** removeFileKey shall remove the key file, shall succeed when it does not exist, and shall return any other removal error or a path-resolution error.
- **REQ-KEY-024** masterKey shall cache the key per process: after the first success no store (file, keychain) is consulted again until the cache is reset.
- **REQ-KEY-025** masterKey with the file store shall return the file key, creating it on first use, and shall return the error from loadOrCreateFileKey.
- **REQ-KEY-026** masterKey with the keychain store shall return the key held in the keychain.
- **REQ-KEY-027** masterKey with the keychain store shall, when the keychain read fails and a valid key file exists, return the file key and record a notice naming the keychain error.
- **REQ-KEY-028** masterKey with the keychain store shall, when the keychain read fails and no valid key file exists, return an error that points to `ts-cli secrets use file`, create no key file and cache nothing.
- **REQ-KEY-029** UseKeychain shall return an error and change nothing when the keychain is unavailable.
- **REQ-KEY-030** UseKeychain shall be a no-op when the keychain store is active and the keychain item is a valid key.
- **REQ-KEY-031** UseKeychain shall move the existing key to the keychain unchanged, set the store to `keychain`, remove the key file and reset the key cache, so that previously encrypted data remains readable.
- **REQ-KEY-032** UseKeychain shall, when the keychain store is configured but the item is missing or invalid, re-upload the key from the key file.
- **REQ-KEY-033** UseKeychain shall verify the keychain round trip before deleting anything, and when the read-back fails or differs shall delete the keychain item, keep the key file and keep the store unchanged.
- **REQ-KEY-034** UseKeychain shall return an error and keep the key file and the store setting when the key file cannot be loaded, or when the keychain write fails.
- **REQ-KEY-035** UseKeychain shall delete the keychain item again and return an error when the store setting cannot be written, keeping the key file.
- **REQ-KEY-036** UseKeychain shall return an error that says the key was stored when the key file cannot be removed afterwards.
- **REQ-KEY-037** UseFile shall be a no-op when the file store is active.
- **REQ-KEY-038** UseFile shall move the key from the keychain back to a 0600 key file unchanged, set the store to `file`, delete the keychain item and reset the key cache.
- **REQ-KEY-039** UseFile shall, when the keychain cannot be read but a valid key file exists, switch the store back using the file key.
- **REQ-KEY-040** UseFile shall return an error and change nothing when the keychain cannot be read and no valid key file exists.
- **REQ-KEY-041** UseFile shall return an error and keep the store and the keychain item when the key file cannot be written, and shall keep the keychain item when the store setting cannot be written.
- **REQ-KEY-042** UseFile shall return an error that says the key file was written when the keychain item cannot be deleted afterwards.
- **REQ-KEY-043** Concurrent callers of masterKey shall obtain the same key and exactly one key file shall result.

## MC/DC

### D1 `KeychainAvailable`: `err == nil || errors.Is(err, ErrNotFound)` (true = available)

Conditions: A `err == nil`, B `errors.Is(err, ErrNotFound)`.

| Condition | Pair | Outcome |
|---|---|---|
| A | probe returns nil (A=T) vs probe returns other error `boom` (A=F,B=F) | available / unavailable |
| B | probe returns ErrNotFound (A=F,B=T) vs `boom` (A=F,B=F) | available / unavailable |

Test: `TestKeychainAvailableMCDC` (REQ-KEY-009).

### D2 `keychainDeleteKey`: `err != nil && !errors.Is(err, ErrNotFound)` (true = return error)

Conditions: A `err != nil`, B `!errors.Is(err, ErrNotFound)`.

| Condition | Pair | Outcome |
|---|---|---|
| A | delete returns nil (A=F) vs `boom` (A=T,B=T) | nil / error |
| B | delete returns ErrNotFound (A=T,B=F) vs `boom` (A=T,B=T) | nil / error |

Test: `TestKeychainDeleteKeyMCDC` (REQ-KEY-013).

### D3 `removeFileKey`: `err != nil && !errors.Is(err, os.ErrNotExist)` (true = return error)

| Condition | Pair | Outcome |
|---|---|---|
| A `err != nil` | file exists, removal succeeds (A=F) vs removal returns `boom` (A=T,B=T) | nil / error |
| B `!Is(ErrNotExist)` | key file absent, os.ErrNotExist (A=T,B=F) vs `boom` (A=T,B=T) | nil / error |

Test: `TestRemoveFileKeyMCDC` (REQ-KEY-023).

### D4 `UseKeychain` round trip: `err != nil || string(back) != string(key)` (true = abort)

| Condition | Pair | Outcome |
|---|---|---|
| A `err != nil` | read-back returns error (A=T) vs read-back returns the same key (A=F,B=F) | abort / continue |
| B `back != key` | read-back returns a different 32-byte key (A=F,B=T) vs the same key (A=F,B=F) | abort / continue |

Test: `TestUseKeychainRoundTripMCDC` (REQ-KEY-033).

### D5 `masterKey`/`UseFile` nested decisions

`if CurrentSecretStore() == SecretStoreKeychain { if _, err := keychainGetKey(); err == nil {...} }`
in UseKeychain is a nested pair of single-condition decisions, not a compound
one; it is covered by REQ-KEY-030 (both true), REQ-KEY-032 (outer true, inner
false) and REQ-KEY-031 (outer false). The error-chain conditions of masterKey
(`err != nil`, `ferr != nil`) and UseFile (`err != nil`, `ferr == nil`) are
single conditions, each covered true and false by REQ-KEY-026..028 and
REQ-KEY-038..040. The `switch CurrentSecretStore()` is covered for both its
`SecretStoreKeychain` case and its default case.

## Derived requirements / unreachable code

- Seams (package-level function variables, defaults are the real functions, behaviour unchanged): `keyringGet`, `keyringSet`, `keyringDelete`, `writeFile`, `removeFile`, `createTemp`, `openExclusive`, plus `userHomeDir` (paths.go). They exist because failures of the keychain after a successful probe, of chmod/write/close on a file, and of write permission as root cannot be produced portably with real files. Every statement is reachable with these seams.
- Fix made during verification: writeFileKey left its temporary file, containing the key, in the config directory when the final rename failed. It now removes it (REQ-KEY-017).
- Decision (was a finding): `UseKeychain` when the keychain store is already configured but the keychain item is lost and no key file exists creates a new random key and stores it (it does not fail). This is now announced: `KeyWasReset()` becomes true (REQ-KEY-044) and the `secrets use keychain` command removes the secrets encrypted with the lost key (`config.DropUnreadableSecrets`, REQ-CFG-038) and prints what was removed. Pinned by `TestUseKeychainLostItemCreatesNewKey`.
- A non-ENOENT stat error on the key file (for example ENOTDIR when the config dir is a regular file) is treated as an error, not as "missing", so that a corrupt setup never creates a key elsewhere (REQ-KEY-020).
- On failure after a successful keychain write in UseFile (REQ-KEY-041) the key file is left in place together with a keychain store setting; the next masterKey then uses the keychain and the file is a harmless duplicate.
- The package-level key cache is shared by the whole process; tests reset it via resetKeyCache under keyMu.

Unreachable code: none with the seams in place.
- **REQ-KEY-044** `KeyWasReset` shall be true after a `UseKeychain` call that found the keychain item lost and no key file and therefore created a new key, and false after every other `UseKeychain` call (first move into the keychain, key file still present, no-op).
