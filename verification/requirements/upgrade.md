# Requirements: `ts-cli upgrade`, `commands/upgrade.go` (REQ-UPG)

`upgrade` compares this version with the latest GitHub release and, with `--install`, replaces the running binary. The binary is code that will run on the user's machine, so nothing unverified is ever installed.

- **REQ-UPG-001** `upgrade` shall print that ts-cli is up to date when this version is equal to or newer than the latest release, and otherwise the old and new version with the release page; without `--install` it shall change nothing. A failed release lookup shall be returned unchanged.
- **REQ-UPG-002** `--install` shall download only from https URLs (or loopback http for tests), refuse a body larger than its limit and any non-200 answer, and use the archive for the running OS and architecture; a release without such an archive or without `checksums.txt` shall be refused.
- **REQ-UPG-003** The archive's SHA-256 shall equal the entry for its name in `checksums.txt`; a missing entry or a mismatch shall stop the installation with the installed binary untouched.
- **REQ-UPG-004** Only a regular file named `ts-cli` shall be taken from the `.tar.gz` archive; an archive without it, a damaged archive, and a binary above the size limit shall be refused.
- **REQ-UPG-005** The binary shall be installed with the atomic new-inode copy of `install` (and the ad-hoc signature on macOS) over the real path of the running executable; a failure shall be reported with a hint about administrator rights and the old binary shall stay. On Windows no replacement shall be attempted.
