# Requirements: account management, `commands/account.go` (REQ-ACC)

`account.go` implements `ts-cli account add|list|edit|use|hide|show|remove` and the shared API-key input (`keyInput`) that `login` also uses. It is the user's first contact with Tailscale API keys: a defect can print a key, store an unvalidated key, or corrupt the account list. Tests call the real commands against an isolated `TS_CLI_CONFIG_DIR`, a fake Tailscale API (`client.SetBaseURL`) and a fake `tailscale` executable on a private `PATH`.

## API key input

- **REQ-ACC-001** The API key shall be taken from standard input when `--api-key-stdin` is given, otherwise from `--api-key`, otherwise from the `TAILSCALE_API_KEY` environment variable, and shall be empty when none supplies one; a higher-priority source that is selected but empty shall not fall back to a lower one.
- **REQ-ACC-002** With `--api-key-stdin` only the first line of standard input shall be read (at most 4096 bytes), surrounding whitespace and CR/LF shall be trimmed, end of input without a newline shall be accepted, an empty input shall yield an empty key, and a read failure shall be returned as an error saying the key could not be read from stdin, before any network request.
- **REQ-ACC-003** Whatever the source, the key shall have NUL and control characters (other than tab, CR, LF, which are then trimmed at the ends) removed before use.

## add

- **REQ-ACC-004** `account add` shall fail with an actionable message when no API key was supplied (checked first) or when the tailnet is empty after sanitising, and shall not contact the API or create the configuration.
- **REQ-ACC-005** The display name, when given, shall be 1 to 64 characters of letters, digits, space, dot, underscore or hyphen (65 characters, slashes, shell metacharacters are rejected); when absent it shall default to the tailnet.
- **REQ-ACC-006** The key format (20 to 200 characters, `tskey-` prefix) and the tailnet (an e-mail or a domain, at most 253 characters) shall be validated before any request is sent; invalid input shall produce an error naming what was invalid, no request and no stored account.
- **REQ-ACC-007** `account add` shall check the key against the tailnet's devices endpoint with that key as bearer token before storing it; when the API rejects it (401, 403), answers with an error status or is unreachable, the command shall fail with "failed to validate API key", print no success marker and leave the configuration untouched.
- **REQ-ACC-008** For an unknown tailnet `account add` shall store a new account (name, tailnet, key encrypted on disk, never in clear), shall not make it the active account unless it is the first account, and shall print the validation steps and "Added new account".
- **REQ-ACC-009** For a tailnet that is already configured `account add` shall replace that account's key in place without creating a duplicate, shall change its name only when an explicit name different from the tailnet is given, and shall print "Updated account".
- **REQ-ACC-010** An API key shall never appear in the standard output, standard error, error text or configuration file bytes of any `account` subcommand, on success or on any failure path.
- **REQ-ACC-011** When the configuration cannot be read or written (for instance it is corrupt), `account add` shall fail with "failed to store config", shall not claim success and shall leave the existing file byte-for-byte unchanged.
- **REQ-ACC-012** `account add` shall require `--tailnet`, shall reject positional arguments, and shall accept the key from each of the three sources.

## list

- **REQ-ACC-013** `account list --format=json` shall print a JSON array (empty array for no accounts) of objects with name, tailnet, active and hidden, and no key material; `key` and `expires` shall appear only with `--check`.
- **REQ-ACC-014** `account list` shall fail with "failed to load configuration" when the configuration is unreadable (without modifying it), and with "unknown format" for any format other than exactly `table` or `json`.
- **REQ-ACC-015** `account list` (table) shall print a header, one row per account marking the active account and hidden accounts, the total, and the default SSH username when one is set; with no accounts it shall print a hint on how to add one.
- **REQ-ACC-016** `account list --check` shall check every account's key against the API and add an API KEY column (table) or `key` and RFC 3339 `expires` fields (JSON): ok with the expiry in days, ok with a note when the expiry is unknown or unreadable, invalid when rejected, unreachable when the API cannot be asked; keys shall not be printed.
- **REQ-ACC-017** The key status shall be "invalid / key rejected" when the key was rejected (even if otherwise valid), "unreachable / cannot check" when the check did not succeed, "invalid / expired" when the expiry is not after now (the boundary instant included), "ok / expires in N days (date)" with N the whole days remaining, "ok / <note>" when only a note exists, and "ok / valid" otherwise.

## edit

- **REQ-ACC-018** `account edit <account>` shall rename the account selected by name or tailnet to `--name` (sanitised) without any API request when no key is given, and print "Account updated".
- **REQ-ACC-019** When a new key is given (stdin or flag) `account edit` shall validate it against the API for that account's tailnet before storing it, shall store it encrypted, and shall leave the configuration unchanged when the API rejects it.
- **REQ-ACC-020** `account edit` shall fail, without changing the configuration or contacting the API, when nothing is to change (no name, no key, or a name that is blank after sanitising), when the key is empty or malformed, when the name is invalid, when the account is unknown, when the new name equals another account's name or tailnet, when the configuration is corrupt, or when the number of arguments is not one.

## use, hide, show, remove

- **REQ-ACC-021** `account use` (alias `set-active`) shall fail with "no accounts configured" or "account '<x>' not found" (including for the empty name) and leave the configuration unchanged when the account cannot be selected.
- **REQ-ACC-022** `account use <account>` shall make the selected account the only active one and print it; when a `tailscale` executable is on `PATH` it shall run `tailscale switch <tailnet>` and print a confirmation, a failing switch shall only produce a warning on standard error containing the tool's trimmed output (the command still succeeds and the active account stays changed), and without the executable nothing more shall be printed.
- **REQ-ACC-023** `account hide` and `account show` shall set or clear only the hidden flag of the selected account (by name or tailnet), print the new state, and fail with "account '<x>' not found" for an unknown or empty name.
- **REQ-ACC-024** `account remove` (aliases `rm`, `delete`) shall remove the selected account, print its name, and when the removed account was active and others remain print and apply the promotion of the next account; removing an inactive or the last account shall print no promotion; an unknown, empty or wrong-case name, or an empty list, shall fail ("not found", "no accounts configured") and change nothing.

## Wiring

- **REQ-ACC-025** The `account` command shall offer exactly the subcommands add, list (ls), edit, use (set-active), hide, show and remove (rm, delete); the key flags shall exist on add and edit with a usage text that discourages `--api-key`; an unknown subcommand shall be an error.

## MC/DC

Compound decisions in `account.go` and the independence pair for each condition (A, B = left, right condition).

1. `resolve`: `err != nil && err != io.EOF` (stdin read result)
   - A (`err != nil`): "first line" in `TestACCKeyResolveStdinBounds` (err nil, B true: no failure) against "read failure" in the same test (err is a real error: failure), B true in both.
   - B (`err != io.EOF`): "no newline" (EOF, A true: accepted) against "read failure" (A true: error).
2. `list`: `format != "table" && format != "json"`
   - A: `--format=table` in `TestACCListTable` (A false, B true: accepted) against `--format=xml` in `TestACCListErrors` (A true, B true: error).
   - B: `--format=json` in `TestACCListJSONShapeAndNoKeys` (A true, B false: accepted) against `--format=xml` in `TestACCListErrors`.
3. `edit`: `keys.stdin || keys.flag != ""`
   - stdin: `edit work --api-key-stdin` (`TestACCEditReplacesKeyAfterValidation`: key read) against `edit ... --name=` only (`TestACCEditRenames`: no key read), flag empty in both.
   - flag: `edit job --api-key=<key>` (second call of `TestACCEditReplacesKeyAfterValidation`, stdin false) against `TestACCEditRenames` (neither).
4. `edit`: `newName == "" && key == ""`
   - A (`newName == ""`): "nothing to change" in `TestACCEditErrors` (A true, B true: error) against the rename in `TestACCEditRenames` (A false, B true: proceeds).
   - B (`key == ""`): "nothing to change" (error) against `edit job --api-key=<key>` in `TestACCEditReplacesKeyAfterValidation` (A true, B false: proceeds).

All other branches in the file are single-condition decisions (`if`, `switch` cases, `keyStatus` cases), each covered true and false by the tests named above (`TestACCKeyStatusClassification` for the status switch).

## Derived requirements / unreachable code

Derived (behaviour in the code that no user need asked for, now pinned by tests):

- Standard input is limited to 4096 bytes so an endless pipe cannot exhaust memory (REQ-ACC-002).
- Sanitising control characters (REQ-ACC-003) protects terminals and the Authorization header.
- `account list` loads the configuration before checking the format, so a corrupt configuration is reported first (REQ-ACC-014).
- Failure of the `tailscale switch` step is deliberately non-fatal (REQ-ACC-022).

Observations recorded, behaviour deliberately not changed:

- `account add` finds an existing account with `FindAccount`, which matches the account's name as well as its tailnet; adding a tailnet that equals another account's display name therefore updates that account (and overwrites its tailnet).
- `account add --name X` on an existing tailnet does not check that `X` is already another account's name (`account edit` does), so two accounts can end up with the same display name.

Unreachable statements: none; every statement of `account.go` is executed by the tests.
