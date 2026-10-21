# Requirements: input validation (REQ-VAL) - util/validation.go

Low-level requirements for the input barrier. Lengths are measured in bytes
(Go `len`), which is what ssh and the Tailscale API see.

- **REQ-VAL-001** ValidateSSHUsername shall reject the empty string.
- **REQ-VAL-002** ValidateSSHUsername shall accept a name of 32 bytes and reject a name of 33 bytes or more (limit-1 = 31 and limit = 32 accepted, limit+1 = 33 rejected).
- **REQ-VAL-003** ValidateSSHUsername shall reject any name containing a character outside `[A-Za-z0-9._-]` (space, shell metacharacters, `$(...)`, `@`, newline, NUL, non-ASCII), so that no name can inject shell syntax or extra arguments.
- **REQ-VAL-004** ValidateSSHUsername shall reject any name starting with '-', because ssh would read it as an option (e.g. `-oProxyCommand=...`), even though '-' is otherwise an allowed character.
- **REQ-VAL-005** ValidateSSHUsername shall accept ordinary names built from letters, digits, '.', '_' and '-' where '-' is not the first character (e.g. `root`, `john.doe`, `svc_account`, `a-b`).
- **REQ-VAL-006** ValidateTailnet shall reject the empty string.
- **REQ-VAL-007** ValidateTailnet shall accept a value of 253 bytes and reject 254 bytes or more (252 and 253 accepted, 254 rejected).
- **REQ-VAL-008** ValidateTailnet shall reject a value containing neither '@' nor '.', and shall accept a value containing at least one of them.
- **REQ-VAL-009** ValidateAccountName shall reject the empty string.
- **REQ-VAL-010** ValidateAccountName shall accept a name of 64 bytes and reject 65 bytes or more (63 and 64 accepted, 65 rejected).
- **REQ-VAL-011** ValidateAccountName shall accept only characters from `[A-Za-z0-9 ._-]` and reject any other character (control characters, '/', ';', non-ASCII).
- **REQ-VAL-012** ValidateAPIKey shall reject the empty string.
- **REQ-VAL-013** ValidateAPIKey shall reject a key shorter than 20 bytes and accept one of 20 bytes (19 rejected, 20 and 21 accepted).
- **REQ-VAL-014** ValidateAPIKey shall reject a key longer than 200 bytes and accept one of 200 bytes (199 and 200 accepted, 201 rejected).
- **REQ-VAL-015** ValidateAPIKey shall require the prefix `tskey-` (which includes `tskey-api-` and `tskey-auth-`) and reject any other prefix, including a prefix differing in case or only partially present.
- **REQ-VAL-016** SanitizeInput shall remove every NUL byte from its input.
- **REQ-VAL-017** SanitizeInput shall remove every control character below U+0020 except tab, newline and carriage return, which are preserved when interior to the text.
- **REQ-VAL-018** SanitizeInput shall preserve every character of U+0020 and above unchanged (printable ASCII, non-ASCII text, emoji).
- **REQ-VAL-019** SanitizeInput shall trim leading and trailing white space from its result, and shall return the empty string for input that is empty or consists only of removable or white-space characters.

## MC/DC

Decisions with more than one condition. Short-circuit evaluation applies
(masking MC/DC): a condition not evaluated in a pair is masked.

### D1 `SanitizeInput`: `r >= 32 || r == '\t' || r == '\n' || r == '\r'` (true = keep)

Conditions: A `r >= 32`, B `r == '\t'`, C `r == '\n'`, D `r == '\r'`.

| Condition | Pair (test input A / test input B) | Outcome |
|---|---|---|
| A | `'a'` (A=T) vs `U+0001` (A=F,B=F,C=F,D=F) | keep / drop |
| B | `'\t'` (A=F,B=T) vs `U+0001` | keep / drop |
| C | `'\n'` (A=F,B=F,C=T) vs `U+0001` | keep / drop |
| D | `'\r'` (A=F,B=F,C=F,D=T) vs `U+0001` | keep / drop |

Test: `TestSanitizeInputMCDC` (REQ-VAL-017, REQ-VAL-018). Boundary: U+001F dropped, U+0020 kept.

### D2 `ValidateTailnet`: `!Contains(t,"@") && !Contains(t,".")` (true = reject)

Conditions: A `!Contains("@")`, B `!Contains(".")`.

| Condition | Pair | Outcome |
|---|---|---|
| A | `nodots` (A=T,B=T) vs `user@nodots` (A=F,B=T) | reject / accept |
| B | `nodots` (A=T,B=T) vs `example.com` (A=T,B=F) | reject / accept |

Test: `TestValidateTailnetMCDC` (REQ-VAL-008).

Single-condition decisions (empty, length limits, regexp mismatch, prefix
loop) are covered by the boundary cases of their requirements.

## Derived requirements / unreachable code

Behaviour present in the code that no user need asked for:

- The regexp in ValidateSSHUsername admits '.' and '..' and names consisting only of digits; these are accepted (not a security issue for ssh, but not a user need).
- The prefix list in ValidateAPIKey used to list `tskey-api-` and `tskey-auth-` besides `tskey-`; the longer entries were redundant and are gone, the rule is REQ-VAL-015.
- SanitizeInput keeps U+007F (DEL) and C1 controls (U+0080..U+009F) because it only filters below U+0020; it is defence in depth, not a complete control-character filter.
- ValidateTailnet accepts any text containing '.' or '@' (e.g. `@`, `.`, spaces); it is a sanity check, not a domain validator.
- The regexps are compiled on every call (performance only).

Unreachable code: none. All statements are reachable by the tests.
