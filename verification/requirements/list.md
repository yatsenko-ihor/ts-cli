# Requirements: `ts-cli list`, `commands/list.go` (REQ-LST)

`list` prints the devices of the tailnet(s) as a table, JSON or names, with filters (combined by AND) and sorting. Scripts parse its output, so the formats and the exit status are contractual. Tests stub the fetch (`listDevicesFn`) for the command and use a fake API for the fetch itself.

## Options

- **REQ-LST-001** `list` shall reject `--online` together with `--offline` ("mutually exclusive") before fetching anything.
- **REQ-LST-002** `list` shall accept `--sort` values `name`, `lastseen`, `os`, `account` in any letter case and reject every other value, including the empty one, before fetching anything.
- **REQ-LST-003** `list` shall accept `--format` values `table`, `json`, `names` in any letter case and reject every other value (empty, trailing blank) before fetching anything.
- **REQ-LST-004** `list` shall pass `--api-key` and `--tailnet` to the fetch and request a quiet fetch (no Tailscale warning) exactly when the format is not `table` or `--quiet` is given.
- **REQ-LST-005** A fetch error shall be returned unchanged with nothing printed; positional arguments shall be rejected.

## Output

- **REQ-LST-006** When nothing is to be shown, `list` shall exit successfully and print: `[]` for the json format; "No devices match the filters." on standard error (standard output empty) when a filter is set; otherwise "No devices found in any of your configured accounts." on standard output. Sorting alone is not a filter.
- **REQ-LST-007** `list` shall print the filtered, sorted devices in the requested format and shall not reorder the fetched slice.
- **REQ-LST-008** `deviceFilter.active` shall be true when any of online, offline, os, tags, accounts or name is set and false otherwise (sort does not count).
- **REQ-LST-009** A device shall be online when it is connected to control or was last seen at most five minutes before now (exactly five minutes included, a future time included); a device never seen and not connected is offline.
- **REQ-LST-010** A tag shall match a device tag after trimming, lower-casing and removing one `tag:` prefix on both sides, and only when the whole tag is equal.
- **REQ-LST-011** `deviceFilter.matches` shall require every set filter at once: online, offline, OS (case-insensitive equality), all listed tags, any listed account (case-insensitive equality with the account name) and a case-insensitive substring of the name or the hostname.
- **REQ-LST-012** `filterDevices` shall sort by name (default and fallback for an unknown key; the hostname when the name is empty), by last seen (connected first as "now", most recent first, never seen last), by OS or by account (case-insensitive, ties by name), shall be stable, shall not modify its input and shall never return nil.
- **REQ-LST-013** The table shall have one row per device with account (`-` when empty), name (the hostname when empty), hostname, first address (`N/A` when none), OS, tags (comma-separated, `-` when none) when the TAGS column is shown, last seen ("online now" for a connected device, "never" for a device never seen, otherwise a relative time) and authorized (`Yes`/`No`).
- **REQ-LST-014** The table shall have a header with an underline and a footer "Total devices: N" unless `--quiet` is given, and shall have the TAGS column only when at least one displayed device has a tag.
- **REQ-LST-015** The names format shall print one line per device: the hostname, or the name when the hostname is empty.
- **REQ-LST-016** The json format shall be a valid JSON array of objects with `id, name, hostname, os, addresses, authorized, lastSeen, connectedToControl, account, tags, online`: `addresses` and `tags` are `[]` when empty, `lastSeen` is RFC 3339 and omitted when unknown, `account` omitted when empty; a write error shall be returned.
- **REQ-LST-017** `formatDuration` shall print "just now" below one minute (and for negative values), "1 minute ago" / "N minutes ago" below an hour, "1 hour ago" / "N hours ago" below a day and "1 day ago" / "N days ago" from a day on, truncating.

## Fetching

- **REQ-LST-018** `fetchDevicesForList` shall return "failed to load configuration" when the configuration is unreadable and "no accounts configured" with the login hint when no account is stored, also when explicit credentials are given, without any request.
- **REQ-LST-019** With both an API key and a tailnet it shall list only that tailnet with that key (devices tagged with the tailnet as account name and tailnet; an API error is returned as "failed to list devices"); otherwise (also when only one is given) it shall list all stored accounts, tagging each device with the account name and tailnet, and an account that fails shall only produce a warning.
- **REQ-LST-020** Unless quiet, `fetchDevicesForList` shall warn when the local Tailscale is not running; when quiet it shall print nothing.
- **REQ-LST-021** The `list` command shall provide the flags `--api-key`, `--tailnet`, `--format` (table), `--quiet/-q`, `--online`, `--offline`, `--os`, `--tag`, `--account`, `--name`, `--sort` (name) and shall work end to end with the real fetch.

## MC/DC

1. `f.Online && f.Offline` (mutual exclusion): `TestDO178CListRejectsInvalidOptionsBeforeFetching` first case (both: error) vs the accepted `--online` and `--offline` alone.
2. `format != "table" || quiet`: `TestDO178CListQuietnessOfTheFetch`: default table (false,false: not quiet) vs `--format json` (true,false) vs `-q` (false,true: quiet).
3. `deviceFilter.active` (six-way OR): `TestDO178CDeviceFilterActive`: each single filter (true) against the empty filter (false).
4. `matches`: `f.Online && !deviceOnline(d)`: `TestDO178CFilterMatchesEachCondition` "online filter, online device" (keep) vs "online filter, offline device" (drop) vs "no online filter, offline device" (keep). `f.Offline && deviceOnline(d)`: the three mirrored cases. `f.OS != "" && !EqualFold`: "os filter different" (drop) vs "os filter equal" (keep) vs "os filter empty, any os" (keep). Name: `!Contains(name) && !Contains(hostname)`: "name matches hostname only" and "name matches name only" (keep) vs "name matches neither" (drop).
5. `deviceOnline`: `d.ConnectedToControl` and `d.LastSeen.IsZero()`: `TestDO178CDeviceOnlineBoundaries` ("connected, long ago" vs "not connected, never seen" vs "4m59s").
6. `lastSeen` display switch (`ConnectedToControl`, `LastSeen.IsZero()`): `TestDO178CTableContent` rows `bare` (connected, zero time: "online now"), `never` (zero time, not connected) and `full` (neither).
7. `len(shown) == 0` then `format == "json"` / `f.active()`: `TestDO178CListEmptyResults` covers json/table/names with and without filters.
8. `apiKey != "" && tailnet != ""` in `fetchDevicesForList`: `TestDO178CFetchDevicesSources` "explicit credentials replace..." (both) vs "only one of key and tailnet" (false for each).
9. Single-condition decisions, each exercised both ways: `len(cfg.Accounts) == 0`, `!quiet`, `err != nil` of the API, `showTags`, `account == ""`, `name == ""`, `len(Addresses) > 0`, `!device.Authorized`, `quiet` in the table, `d.Name != ""` in `sortKeyName`, the sort comparators and the `d < ...` ladder of `formatDuration` (`TestDO178CFormatDuration`).

## Derived requirements / unreachable code

Derived requirements:

- An unknown sort key never reaches `filterDevices` from the command (REQ-LST-002) but the function falls back to name order (REQ-LST-012).
- A device whose lastSeen is in the future counts as online (clock skew), REQ-LST-009.
- Names output prints an empty line for a device with neither hostname nor name (REQ-LST-015).
- The relative "last seen" in the table depends on the current time; tests use values far from a boundary.

Test seams: `listDevicesFn` (already present). No non-test code of `list.go` was changed.

Unreachable code: none. Every statement of `list.go` is executed by the tests (100 % statement coverage).
