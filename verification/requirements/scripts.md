# Requirements: scripts discovery and remote snippet (REQ-SCR) - scripts/scripts.go

Scope: finding local scripts, parsing their metadata header, deciding which machines they fit, and building the POSIX
shell snippet that runs a script on a remote host. Security intent: arguments typed by the user must reach the script
as literal data (never be interpreted by the remote shell), and the script content travels on stdin, never in the
command line.

## Discovery

- **REQ-SCR-001** Dir shall return `<config dir>/scripts` without creating it, and an error when the config directory cannot be determined (Load then returns that error and no scripts).
- **REQ-SCR-002** Load shall return an empty list and no error when the scripts directory does not exist.
- **REQ-SCR-003** Load shall return an error "failed to read scripts directory" and no scripts when the directory cannot be read (for example the path is a file).
- **REQ-SCR-004** Load shall ignore sub-directories, entries whose name starts with a dot and entries that cannot be read (dangling link), and load every other entry with its content.
- **REQ-SCR-005** Load shall set each script's absolute Path and sort the scripts by Name, ignoring case.

## Metadata

- **REQ-SCR-006** Parse shall return, for content without metadata (including empty and binary-looking content), Name and File equal to the file name, the untouched content, and empty Description, OS, Args and Path.
- **REQ-SCR-007** Parse shall read `ts-cli:` metadata from comment lines starting with `#` or `//` (optional surrounding whitespace, optional space after the marker, CRLF line ends), accept the keys name, desc, description, os and args case-insensitively, accept several keys on one line separated by `;`, let a later line override an earlier one, keep `=` inside values, and ignore ordinary comments, other prefixes (`ts-clix:`), items without `=`, unknown keys and non-comment lines.
- **REQ-SCR-008** An empty or blank `name=` shall keep the file name as the name; an empty `desc=` shall clear the description.
- **REQ-SCR-009** `os=` and `args=` shall be split on commas and spaces, dropping empty and whitespace-only items; OS names shall be lower-cased, argument names keep their case; other punctuation is not a separator.
- **REQ-SCR-010** Parse shall examine only the first 30 lines (line 30 is read, line 31 is not) and shall not fail on a line longer than the 1 MiB scanner limit; metadata after that is ignored.

## Matching and shebang

- **REQ-SCR-011** SupportsOS shall be true for a script without OS filter (nil or empty); otherwise true when the device OS equals a filter entry ignoring case, or when one is `darwin` and the other `macos`, and false in every other case (including `linux` against `macos`/`darwin`, and an empty device OS).
- **REQ-SCR-012** HasShebang shall be true exactly when the content starts with the two bytes `#!`.

## Remote snippet and quoting

- **REQ-SCR-013** RemoteCommand shall return one POSIX shell snippet that creates a private temp file with mktemp under `${TMPDIR:-/tmp}`, copies stdin into it, makes it mode 700, runs it with the quoted arguments (directly when the script has a shebang, otherwise through `sh`), removes it and exits with the script's status; with no arguments no trailing blank is added, and the script content shall not appear in the snippet.
- **REQ-SCR-014** ShellQuote shall wrap its input in single quotes and replace each embedded single quote by `'"'"'` (empty input gives `''`).
- **REQ-SCR-015** For every input, including quotes, `$()`, backticks, `;`, `&&`, `|`, `>`, newlines, tabs, backslashes, variables, globs, `~`, option-like text (`-n`, `--`, `-oProxyCommand=x`), empty string, non-ASCII text and `!`, a real `sh` shall see exactly the original text as one argument and shall execute nothing from it.
- **REQ-SCR-016** Several quoted arguments joined by spaces shall keep their boundaries and order when passed through a real shell.
- **REQ-SCR-017** Executed through a real `sh -c` with the script on stdin, RemoteCommand shall deliver every hostile argument to the script as a separate literal argument (script with and without shebang), propagate the script's exit status (0, 1, 7, 255), leave no temp file behind, create the file with mode 700, run a script body with quotes and dollar signs unchanged, and exit with status 97 without running the script when the temp file cannot be created.

## MC/DC

Compound decisions (test names are `TestFunc/subtest`):

1. Load: `e.IsDir() || strings.HasPrefix(e.Name(), ".")` (outcome true = skip), `TestDO178CLoad/skips directories, dot files and unreadable entries; sorts case-insensitively` with these entries: `subdir` (IsDir true, no dot), `.hidden.sh` (IsDir false, dot), `b.sh` (both false).
   - IsDir: `subdir` vs `b.sh`.
   - dot prefix: `.hidden.sh` vs `b.sh`.
2. Parse: `i < 30 && sc.Scan()` (`TestDO178CParseScansOnlyFirst30Lines`, `TestDO178CParseMetadata`).
   - `i < 30`: `header on line 30 (boundary, last scanned)` vs `header on line 31 (boundary, first ignored)`.
   - `sc.Scan()`: `TestDO178CParseMetadata/hash comment name` (a line exists, parsed) vs `TestDO178CParseDefaults/nil content` (no line, nothing parsed).
3. SupportsOS: `o == d || (o == "darwin" && d == "macos") || (o == "macos" && d == "darwin")` (`TestDO178CSupportsOS`).
   - `o == d`: `exact match` vs `mismatch`.
   - `o == "darwin"`: `filter darwin, device macOS` vs `filter linux, device macos is not an alias`.
   - `d == "macos"` (first alias): `filter darwin, device macOS` vs `filter darwin, device windows`.
   - `o == "macos"`: `filter macos, device darwin` vs `filter linux, device darwin is not an alias`.
   - `d == "darwin"` (second alias): `filter macos, device darwin` vs `filter macos, device windows`.
4. splitList: `r == ',' || r == ' '` (`TestDO178CParseMetadata`).
   - comma: `list separator comma` (splits) vs `other punctuation is not a list separator`.
   - space: `list separator space` (splits) vs `other punctuation is not a list separator`.
5. splitList: `p = TrimSpace(p); p != ""`: `whitespace-only list item (tab) is dropped` vs `list separator comma`.
6. applyMeta: `case "name": value != ""`: `hash comment name` vs `empty name keeps file name`; `kv` without `=`: `item without equals ignored` vs `keys combined with semicolons`.
7. RemoteCommand: `!s.HasShebang()` (`TestDO178CRemoteCommandShape/shebang script is executed directly` vs `script without shebang runs under sh`) and `len(quoted) > 0` (`no arguments leaves no trailing blank` vs `arguments are quoted and space separated`).

## Derived requirements / unreachable code

Derived behaviour (no user need asked for it; each covered by a test above):

- Only the first 30 lines and lines up to 1 MiB are scanned for metadata (REQ-SCR-010).
- An empty `name=` is ignored while an empty `desc=` clears the description (REQ-SCR-008).
- A file that cannot be read is skipped silently (REQ-SCR-004).
- The snippet exits with 97 when no temp file can be created (REQ-SCR-017).
- The temp file is created in `$TMPDIR` (or `/tmp`) on the remote host and removed even when the script fails.

Unreachable code: none. Every statement of scripts/scripts.go is executed (100 % statement coverage, every condition true and false under gobco). The unprivileged-read failure of Load is provoked with a dangling symlink, not file permissions, so the test also works when run as root.
