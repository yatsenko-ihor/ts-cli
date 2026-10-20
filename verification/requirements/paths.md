# Requirements: configuration location (REQ-PTH) - util/paths.go

- **REQ-PTH-001** ConfigDir shall return the value of `TS_CLI_CONFIG_DIR` unchanged when that variable is set and non-empty, without consulting the home directory.
- **REQ-PTH-002** ConfigDir shall return `<home>/.ts-cli` when `TS_CLI_CONFIG_DIR` is unset.
- **REQ-PTH-003** ConfigDir shall treat an empty `TS_CLI_CONFIG_DIR` as unset.
- **REQ-PTH-004** ConfigDir shall return a non-nil error that wraps the cause, and an empty path, when `TS_CLI_CONFIG_DIR` is unset and the home directory cannot be determined.
- **REQ-PTH-005** ConfigDir shall not create, modify or stat any file system object.
- **REQ-PTH-006** EnsureConfigDir shall create the configuration directory (including missing parents) with permission bits 0700 (before umask) and return its path.
- **REQ-PTH-007** EnsureConfigDir shall succeed and leave contents untouched when the directory already exists.
- **REQ-PTH-008** EnsureConfigDir shall propagate a ConfigDir error and return an empty path.
- **REQ-PTH-009** EnsureConfigDir shall return an error and an empty path when the directory cannot be created (for example a regular file occupies the path or a parent).

## MC/DC

No compound boolean decisions exist in util/paths.go (the only decisions are the
single conditions `dir != ""` and `err != nil`). MC/DC is therefore trivially
satisfied by the true and false cases of REQ-PTH-001/003 and REQ-PTH-004/008/009.

## Derived requirements / unreachable code

- `userHomeDir` is a package-level function variable (default `os.UserHomeDir`), a test seam added so the home-directory failure and call-order failures in dependants (keystore) can be provoked deterministically on every platform and as root. Behaviour is unchanged.
- A relative `TS_CLI_CONFIG_DIR` is accepted verbatim (it will resolve against the working directory); no normalisation is done.
- EnsureConfigDir does not tighten the permissions of a pre-existing directory (MkdirAll semantics); a directory created earlier with wider permissions keeps them. The key file itself is always 0600 (REQ-KEY-016, REQ-KEY-019).

Unreachable code: none.
