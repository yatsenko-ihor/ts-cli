# Requirements: encryption of stored secrets (REQ-CRY) - util/crypto.go

- **REQ-CRY-001** Decrypt(Encrypt(p)) shall equal p for every plaintext p, including the empty string, non-ASCII text and a 4096-byte value.
- **REQ-CRY-002** Encrypt shall return standard base64 of `nonce(12) || ciphertext || tag(16)` using a fresh random nonce per call, so that two encryptions of the same plaintext differ and the decoded length is `28 + len(plaintext)`.
- **REQ-CRY-003** Encrypt shall encrypt with AES-256-GCM under the master key, so that the output opens with exactly that key and the plaintext does not appear in the output.
- **REQ-CRY-004** Encrypt shall return an empty string and an error when the master key cannot be obtained.
- **REQ-CRY-005** Encrypt shall return an empty string and an error when the master key is not a valid AES key, and when the GCM mode cannot be constructed.
- **REQ-CRY-006** Encrypt shall return an empty string and an error when the nonce cannot be read from the random source (a fixed or empty nonce shall never be used).
- **REQ-CRY-007** Decrypt shall return an error for input that is not valid base64.
- **REQ-CRY-008** Decrypt shall return an error for ciphertext shorter than the 12-byte nonce (limit-1 = 11 bytes) and for ciphertext of exactly the nonce size (no tag), and shall accept the minimal valid ciphertext of 28 bytes (empty plaintext).
- **REQ-CRY-009** Decrypt shall return an error, and no plaintext, for ciphertext in which any byte of the nonce, body or tag was altered (authentication).
- **REQ-CRY-010** Decrypt shall return an error when the master key cannot be obtained.
- **REQ-CRY-011** Decrypt shall fall back to the legacy host-derived key when the master key does not open the ciphertext, so that data saved by earlier versions stays readable.
- **REQ-CRY-012** Decrypt shall return an error that states "wrong key or corrupted data" when neither the master key nor the legacy key opens the ciphertext, and the error shall not contain the plaintext.
- **REQ-CRY-013** Decrypt shall return an error when the legacy key cannot be derived and the master key did not open the ciphertext.
- **REQ-CRY-014** The legacy key shall be SHA-256 of `ts-cli:<username>:<uid>:<hostname>`, and shall use the hostname `unknown-host` when the hostname cannot be read.
- **REQ-CRY-015** The legacy key derivation shall return an error when the current user cannot be determined.
- **REQ-CRY-016** EncryptPassword and DecryptPassword shall behave identically to Encrypt and Decrypt.
- **REQ-CRY-017** The cipher constructor shall accept 16, 24 and 32 byte keys and reject any other length (31 and 33 rejected, 32 accepted), and decryptWith shall return that error rather than panic.
- **REQ-CRY-018** The cipher constructor shall return an error when GCM construction fails.

## MC/DC

util/crypto.go contains no compound boolean decisions: every `if` tests a
single condition (`err != nil`, `len(ciphertext) < nonceSize`). The `err == nil`
guard in `if plaintext, err := decryptWith(key, ciphertext); err == nil` is a
single condition covered true (REQ-CRY-001) and false (REQ-CRY-011). MC/DC is
therefore trivially satisfied.

## Derived requirements / unreachable code

- Legacy fallback (REQ-CRY-011, 013, 014, 015) exists for backward compatibility only; the legacy key is derived from guessable host data and offers no confidentiality, so ciphertext that opens with it must be re-encrypted by callers if they care.
- A hostname lookup failure is tolerated (`unknown-host`) rather than reported (REQ-CRY-014).
- newGCM accepts AES-128 and AES-192 keys (REQ-CRY-017); only the keystore guarantees 32-byte keys (REQ-KEY-014).
- No additional authenticated data is used; ciphertext is not bound to a field name.
- Seams: `osHostname`, `osCurrentUser`, `cipherNewGCM` are package-level function variables (defaults `os.Hostname`, `user.Current`, `cipher.NewGCM`) added only so failures that cannot be provoked portably can be injected. `cipher.NewGCM` cannot fail for an AES block, so without the seam the "failed to create GCM" statement would be unreachable; it is defensive code kept for future cipher changes. Random-source failure is injected by replacing the exported `crypto/rand.Reader`.

Unreachable code: none with the seams in place.
