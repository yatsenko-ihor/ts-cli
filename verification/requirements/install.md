# Requirements: `ts-cli install`, `commands/install.go` (REQ-INS)

`install` copies the running binary into the PATH directory and creates the `tsc` alias. A defect can leave a binary the operating system refuses to start (macOS code-signature cache), overwrite somebody else's `tsc` (the TypeScript compiler) or write outside the intended directory. The command asks for confirmation on its input stream. Tests redirect the install directory (`installDirFn`), the executable path, the operating system (`goos`), temp-file creation, `remove`, `symlink` and `codesign`; nothing touches `/usr/local/bin` or the real PATH.

## Installing the files

- **REQ-INS-001** `installBinary` shall create the destination directory (with parents) when missing and install the binary as `ts-cli` in it as a NEW file (temporary file in the same directory, mode 0755, then rename) so that a reinstall never rewrites the old inode, a symbolic link at the destination is replaced and never written through, and no temporary file remains.
- **REQ-INS-002** `installBinary` shall create the alias `tsc` as a symbolic link with target `ts-cli`, and shall replace an existing `tsc` only when it is a symbolic link whose target's base name is `ts-cli` (the alias created earlier).
- **REQ-INS-003** `installBinary` shall never modify an existing `tsc` that is not that alias (regular file, symbolic link to another program, directory, dangling link): it shall print a warning containing "skipping tsc alias", still leave `ts-cli` installed and return nil.
- **REQ-INS-004** A failure to remove the old alias or to create the symbolic link shall be reported as a warning only; `installBinary` shall return nil and `ts-cli` shall stay installed.
- **REQ-INS-005** `installBinary` shall return an error ("failed to create directory", "failed to copy binary") when the destination directory cannot be created or the binary cannot be copied, and in the latter case shall leave an existing `ts-cli` unchanged and create no alias.
- **REQ-INS-006** On windows `installBinary` shall name the files `ts-cli.exe` and `tsc.exe`, create the alias as a copy of the binary, never replace an existing `tsc.exe` (it cannot be recognised as ours), and treat a failed alias copy as a warning only.
- **REQ-INS-007** `copyFile` shall write the data to a temporary file next to the destination (mode 0755), then rename it over the destination; when reading the source, creating the temporary file, writing, setting the mode, closing or renaming fails it shall return the error, leave the destination unchanged and leave no temporary file. An empty source is copied as an empty file.
- **REQ-INS-008** On darwin `copyFile` shall give the temporary file (before the rename) an ad-hoc signature (`codesign --force --sign - <file>`); a signing failure shall only print a warning with the manual command and the installation shall succeed. On other systems nothing shall be signed.
- **REQ-INS-009** `isOurTscAlias` shall be true only for a symbolic link whose target has the base name `ts-cli` (relative or absolute), false for a regular file, another target, a name that merely contains `ts-cli`, a missing path, and always false on windows.
- **REQ-INS-010** `isInPath` shall report whether an executable of that name is found on PATH (false for an unknown name, an empty name and a non-executable file).
- **REQ-INS-011** `defaultInstallDir` shall be `/usr/local/bin` on every system except windows, and `%ProgramFiles%\ts-cli` on windows.
- **REQ-INS-012** `confirmed` shall return true only when the first input line, trimmed and lower-cased, is exactly `y` or `yes` (a final line without newline counts); empty input, EOF, `n`, `yy`, `yess`, `y please`, `y;rm -rf /` and everything else shall return false, and only the first line shall be read.

## The command

- **REQ-INS-013** `install` shall proceed only after a confirmation per REQ-INS-012 read from the command's input; otherwise it shall print "Installation cancelled.", return nil and not create or modify anything in the install directory.
- **REQ-INS-014** Before asking, `install` shall print the resolved executable path and the install directory and the current status: whether `ts-cli` is in PATH, whether `tsc` is in PATH and whether that `tsc` is the TypeScript compiler (its `--version` output contains "Version"); it shall say "Both ts-cli and tsc are already installed." and ask "reinstall" only when both are present as ours, say "ts-cli is already installed, but tsc alias is missing." when only `ts-cli` is, and otherwise ask "proceed with installation".
- **REQ-INS-015** After installing, `install` shall verify and report whether `ts-cli` and `tsc` resolve on PATH (reporting a conflict when `tsc` is the TypeScript compiler), and shall print the "Note: You may need to" advice with the install directory, plus a shell-specific line for zsh or bash (not for other shells and not on windows), when at least one of the two does not resolve.
- **REQ-INS-016** `install` shall return an error, before asking anything and without touching the install directory, when the path of the executable cannot be determined ("failed to get executable path") or its symbolic links cannot be resolved ("failed to resolve symlinks"); it shall install the resolved target of a symbolic link; and it shall return "installation failed: ..." when `installBinary` fails.
- **REQ-INS-017** `install` shall not replace a foreign `tsc` found in the install directory, and shall report the conflict.

## MC/DC

Compound decisions (a nested `if` pair counts as one decision). Independence pairs name test cases that differ in that condition only and flip the outcome.

1. `runInstall`: `tsCliAlreadyInstalled && tscAlreadyInstalled` (status text and prompt text).
   - tsCliAlreadyInstalled: `TestDO178CRunInstallReportsCurrentStatus/both ours` (true) vs `.../our tsc but no ts-cli` (false).
   - tscAlreadyInstalled: `.../both ours` vs `.../ts-cli only` (tsc missing) and `.../TypeScript tsc, ts-cli present`.
2. `runInstall`: `!tsCliInPath || !tscInPath` (print the note).
   - !tsCliInPath: `TestDO178CRunInstallVerifiesAndAdvisesAboutPATH/only tsc resolves afterwards: note shown` (true, note) vs `.../everything in PATH afterwards: no note` (false, no note).
   - !tscInPath: `.../only ts-cli resolves afterwards: note shown` vs `.../everything in PATH afterwards: no note`.
3. `confirmed`: `answer == "y" || answer == "yes"`. `TestDO178CConfirmed`: `y` (true,false) and `yes` (false,true) each against `n` (false,false).
4. `installBinary`: existing `tsc` and not ours (nested `Lstat == nil` / `!isOurTscAlias`).
   - exists: `TestDO178CInstallBinaryNeverReplacesForeignTsc/regular file (TypeScript)` (skip) vs `TestDO178CInstallBinaryCreatesAndRefreshesOwnAlias` first install (no tsc, alias created).
   - not ours: `TestDO178CInstallBinaryNeverReplacesForeignTsc/regular file (TypeScript)` (skip) vs `TestDO178CInstallBinaryCreatesAndRefreshesOwnAlias` second install (own alias replaced).
5. `copyFile`: `goos == "darwin"` (single condition): `TestDO178CCopyFileResignsOnDarwinOnly`.
6. Single-condition decisions (no pair needed; every outcome is exercised): `goos == "windows"` in `defaultInstallDir`, `installBinary`, `isOurTscAlias`; `goos != "windows"` in the shell advice; `strings.Contains(shell, "zsh")`, `"bash"` (`TestDO178CRunInstallVerifiesAndAdvisesAboutPATH/not in PATH, shell ...`); `tscIsTypeScript()` (both outcomes in `TestDO178CRunInstallReportsCurrentStatus` and `...VerifiesAndAdvises...`); every `err != nil` after an operation that can fail (REQ-INS-005, -007, -016).

## Derived requirements / unreachable code

Derived requirements (behaviour no user need asked for; each is tested):

- A reinstall must produce a new inode (REQ-INS-001, -007): on macOS the kernel caches the code signature per file and kills a binary that was rewritten in place.
- `tsc` is created as a relative link to `ts-cli`, so the pair stays valid when the directory moves (REQ-INS-002).
- The TypeScript compiler is recognised by "Version" in `tsc --version` (REQ-INS-014); an alias that prints something else counts as ours.
- Code changes made while writing the tests: the confirmation is now read with `confirmed` from the command's input (it used `fmt.Scanln(os.Stdin)`, which treated `y please` as yes and could not be tested); `isOurTscAlias` compares only the base name (the additional `target == "ts-cli"` test was implied by it and made the condition impossible to cover independently); the second `os.Chmod(destPath, 0755)` was removed because `copyFile` already creates the file with mode 0755; the repeated `exec.LookPath("tsc")` after `isInPath("tsc")` was removed (it could only fail in a race with the file system) and the "is it TypeScript" probe moved to `tscIsTypeScript`.
- Test seams (production default behaviour identical): `goos`, `installDirFn`, `executablePath`, `evalSymlinks`, `removeFn`, `symlinkFn`, `createTemp`, `adHocSign` (existing).

Known limitations, deliberately not tested: `install` prints to the process standard output (not to the command's writer); the tests capture `os.Stdout`.

Unreachable code: none. Every statement of `install.go` is executed by the tests (100 % statement coverage).
