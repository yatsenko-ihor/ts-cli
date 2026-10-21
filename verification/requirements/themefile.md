# Requirements: custom theme files (REQ-THM) - tui/themefile.go

Every `~/.ts-cli/themes/<name>.json` is a theme called `<name>`. It names a built-in theme as `base` (default light) and overrides colours; the rest comes from the base.

- **REQ-THM-001** A colour shall be `#rgb`, `#rrggbb` (any case, stored lower case) or an ANSI number 0-255; blank shall mean "keep the base's colour"; anything else (256 and above, bad hex, names, negatives) shall be an error.
- **REQ-THM-002** A theme shall be its base with every given colour replaced, each key setting exactly its own field; an unknown base shall be an error listing the built-in names, and a bad colour an error naming its key.
- **REQ-THM-003** Loading a directory shall accept good `.json` files (names of 1-32 characters of a-z, 0-9, `-`, `_`), ignore other files and directories, and report one message per skipped file: bad name, name of a built-in theme, invalid JSON including unknown keys, bad colour, bad base, file over 64 KiB, unreadable file; a missing directory shall be normal and silent.
- **REQ-THM-004** Custom themes shall follow the built-in ones in the Theme cycle, in name order; a stored custom theme name shall load and apply its palette; a stored name whose file is gone shall fall back to light.
- **REQ-THM-005** The settings window shall show how many custom themes are loaded and up to three skip messages (with a count of the rest), and nothing when there is no themes directory.
- **REQ-THM-006** Without a determinable config directory loading themes shall do nothing.

## Derived requirements / unreachable code

- A theme file whose base is another custom theme is refused: only built-in bases exist, which keeps loading free of cycles.
- Themes are read at start; edits need a restart.
