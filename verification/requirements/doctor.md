# Requirements: `ts-cli doctor`, `commands/doctor.go` (REQ-DOC)

`doctor` reports what ts-cli needs and what is missing. Its exit status says whether something is broken, so scripts can use it.

- **REQ-DOC-001** `doctor` shall report ssh and scp as failures when missing, and sshpass, tmux and the tailscale CLI as warnings, each with what stops working; present tools shall show their path.
- **REQ-DOC-002** The local daemon check shall fail with the first line of Tailscale's message when it is not running or logged out, shall pass when it is, and shall be a warning when there is no tailscale CLI.
- **REQ-DOC-003** The configuration check shall fail when `config.json` cannot be read, report the number of accounts and machine logins and the secret store, warn about saved passwords that cannot be decrypted (counting only machines that have one) and about having no accounts.
- **REQ-DOC-004** Each account's API key shall be a failure when rejected or expired, a warning when it could not be checked or expires within 14 days, and a pass otherwise, with the days left or the reason the expiry is unknown; `--offline` shall skip these checks without any request.
- **REQ-DOC-005** The report shall end with the number of problems and warnings; the exit status shall be non-zero exactly when there is at least one failure, and positional arguments shall be rejected.
