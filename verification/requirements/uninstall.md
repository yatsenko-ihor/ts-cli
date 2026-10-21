# Requirements: `ts-cli uninstall`, `commands/uninstall.go` (REQ-UNI)

`uninstall` deletes files: the binary, the alias and, on request, the configuration directory that holds API keys and the master key. It must remove only what ts-cli installed and only after an explicit confirmation. Tests redirect the install directory (`installDirFn`), the configuration directory (`TS_CLI_CONFIG_DIR`, `HOME`), the operating system (`goos`) and the removal functions.

## Detection and report

- **REQ-UNI-001** When neither `ts-cli` nor the alias is installed in the install directory, `uninstall` shall print "ts-cli is not installed in system PATH.", return nil, ask nothing and remove nothing (also with `--config`).
- **REQ-UNI-002** `uninstall` shall list the components it found (the binary, the alias marked "(alias)") and, only with `--config` and only when the directory exists, the configuration directory marked "(configuration)", and then ask for confirmation.
- **REQ-UNI-003** `uninstall` shall remove something only after the first input line, trimmed and case-folded, is exactly `y` or `yes` (REQ-INS-012); otherwise it shall print "Uninstallation cancelled.", return nil and leave everything in place.
- **REQ-UNI-004** `uninstall` shall treat `tsc` as ours only when it is a symbolic link whose target is exactly `ts-cli`; a regular file, a link to another program or a lookalike target shall be neither listed nor removed.

## Removal

- **REQ-UNI-005** After confirmation `uninstall` shall remove the binary and the alias (reporting "Removed <path>" for each) and finish with "ts-cli has been uninstalled."; the configuration directory shall be removed only when `--config` is given (default false).
- **REQ-UNI-006** `uninstall` shall also work when only the binary or only the alias is present, touching only what exists.
- **REQ-UNI-007** When the binary cannot be removed `uninstall` shall return an error naming the path and suggesting sudo and shall not remove the alias or the configuration; when the alias or the configuration directory cannot be removed it shall print a warning ("Failed to remove ...") and continue.
- **REQ-UNI-008** `--config` shall remove the configuration directory in use: `$TS_CLI_CONFIG_DIR` when set, otherwise `~/.ts-cli`; when that directory cannot be determined (no home directory) or does not exist, nothing shall be removed, no error shall result, and a directory relative to the current working directory shall never be touched.
- **REQ-UNI-009** On windows `uninstall` shall look for `ts-cli.exe` and `tsc.exe` (any existing `tsc.exe` in the ts-cli directory), and shall ignore the extension-less names.
- **REQ-UNI-010** `fileExists` shall be true for an existing file or directory and false for a missing path, an empty path and a dangling symbolic link.

## MC/DC

1. `!tsCliExists && !tscExists` (nothing installed): `TestDO178CUninstallNothingInstalled` (true) vs `TestDO178CUninstallPartialInstallations/alias only` (tsCliExists false, tscExists true: false) and `.../binary only` (tsCliExists true: false).
2. `err == nil && target == "ts-cli"` (alias detection, non-windows).
   - err == nil: `TestDO178CUninstallLeavesForeignTscAlone/regular file` (Readlink fails: false) vs `TestDO178CUninstallRemovesBinaryAliasAndConfig` (link to `ts-cli`: true).
   - target == "ts-cli": `TestDO178CUninstallLeavesForeignTscAlone/symlink to typescript` (err == nil, target differs: false) vs `TestDO178CUninstallRemovesBinaryAliasAndConfig`.
3. `configDir != "" && fileExists(configDir)` (listing, and again before removal).
   - configDir != "": `TestDO178CUninstallConfigDirIsTheOneInUse/home unknown ...` (false) vs `.../default is ~/.ts-cli` (true).
   - fileExists: `.../directory missing is not an error` (false) vs `.../default is ~/.ts-cli` (true).
4. Single-condition decisions: `removeConfig`; `tsCliExists`; `tscExists`; `goos == "windows"` / `!= "windows"` (`TestDO178CUninstallWindowsNames`); `!confirmed(...)` (REQ-UNI-003 tests); each `err != nil` of a removal (REQ-UNI-007).

## Derived requirements / unreachable code

Derived requirements:

- The alias detection is deliberately stricter than the one of `install` (`isOurTscAlias` accepts an absolute target ending in `ts-cli`): uninstall removes less than install may replace (REQ-UNI-004).
- Code changes made while writing the tests: the confirmation uses `confirmed` and the command's input (`fmt.Scanln` accepted `y please`); the configuration directory is now `util.ConfigDir()` (honours `TS_CLI_CONFIG_DIR`) instead of `~/.ts-cli` computed from `os.UserHomeDir()`, which deleted the wrong directory when `TS_CLI_CONFIG_DIR` was set and, when the home directory was unknown, the path `.ts-cli` relative to the working directory (REQ-UNI-008).
- Test seams (default behaviour identical): `installDirFn`, `goos`, `removeFn`, `removeAllFn`.
- Residual risk: `--config` removes the whole `$TS_CLI_CONFIG_DIR` directory after the confirmation, which lists it by path.

Unreachable code: none. Every statement of `uninstall.go` is executed by the tests (100 % statement coverage).
