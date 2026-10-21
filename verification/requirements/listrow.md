# Requirements: machine list rows (REQ-ROW) - tui/listrow.go

Scope: how the machine list is laid out. Intent: names are shortened without ever making two machines look the same;
the status marker shows reachability (colour in symbol mode, shape in emoji mode); the flag slots have a fixed width so
columns stay aligned; account sections get header rows; collapsed offline machines get a note row; and the window
over the rows always contains the cursor, whatever the list or window size.

## Names

- **REQ-ROW-001** shortHost shall remove one trailing dot and, for a MagicDNS name ending in ".ts.net", return its first label; names not ending in ".ts.net" (plain hostnames, other domains, the empty string) shall be returned unchanged apart from the trailing dot.
- **REQ-ROW-002** tailnetLabel shall return the second label of a MagicDNS name ending in ".ts.net" (trailing dot ignored) and "" for every other name.
- **REQ-ROW-003** shortNames shall map every listed machine (by machineKey) to its full display name (Name, else Hostname) when short names are off, and to an empty map for an empty list.
- **REQ-ROW-004** shortNames shall, with short names on, map each machine to its shortHost; machines whose short names collide shall get the tag " ·<tailnet label>" when they have a MagicDNS label, and keep the plain short name when they have none; machines with a unique short name shall never get a tag.
- **REQ-ROW-005** shortNames shall look for collisions within one account when account headers are shown (the same host in two accounts is not a collision) and across the whole list otherwise.
- **REQ-ROW-006** deviceAddress shall return the first address of a device and "N/A" for a device without addresses (nil or empty).
- **REQ-ROW-007** padRight shall pad a string with spaces to the given number of terminal cells (wide and combining characters counted by cell width) and return it unchanged when it is already that wide or wider, or when the width is zero or negative.

## Status and flags

- **REQ-ROW-008** statusIcon shall return, in emoji mode, the failed-ping marker for a machine whose last ping failed, the green marker for a reachable machine and the red marker otherwise; in symbol mode the hollow marker for a failed ping and the filled marker otherwise.
- **REQ-ROW-009** styledStatus shall return the emoji unchanged in emoji mode; in symbol mode it shall colour the marker with the offline colour by default, the warning colour for a failed ping and the online colour for a reachable machine (a failed ping wins over an API-online state).
- **REQ-ROW-010** statusWidth shall be 2 in emoji mode and 1 otherwise; flagsWidth shall be 6 in emoji mode and 7 otherwise, and styledFlags shall always render exactly that many cells whatever combination of saved password and key state the machine has.
- **REQ-ROW-011** In symbol mode styledFlags shall show "pw" (online colour) for a saved password and "exp" for a key that is expired (offline colour) or about to expire (warning colour), each in its own slot, and blanks in the slots that do not apply.
- **REQ-ROW-012** In emoji mode styledFlags shall show the key icon of the machine (lock when expired, key when valid, hourglass instead of the key when expiring soon, nothing without expiry) and the lock-and-key marker for a saved password, each in its own slot.
- **REQ-ROW-013** statusCounts shall count the reachable and the unreachable machines of the selected profile (all accounts when none is selected), including machines of hidden accounts, taking ping results before the API state.

## Columns and device rows

- **REQ-ROW-014** listColumns shall set the name column to the width of the widest displayed name and the address column to the width of the widest address ("N/A" counts), both measured in terminal cells; both are 0 for an empty list.
- **REQ-ROW-015** listColumns shall cut the name column to the space left in the panel only when the terminal width is known, the space left is at least 12 cells and the widest name is wider than it; in every other case the name column keeps its natural width. A mark column reduces the space by 2 cells.
- **REQ-ROW-016** renderListDevice shall render the selected row with the cursor marker and a plain row with blanks of the same width; a mark column shall show the check mark for a marked machine and blanks otherwise, and shall be absent when the list has no mark column.
- **REQ-ROW-017** renderListDevice shall pad the name and the address to the column widths and cut a name wider than its column with an ellipsis, so every row of one list has the same width in terminal cells (also for unicode names and machines without address or name).

## Display rows

- **REQ-ROW-018** groupCount shall return the number of different accounts among the listed machines, and showGroupHeaders shall be true only when grouping is on and there are at least two such accounts.
- **REQ-ROW-019** accountCounts shall return, per account name, the number of reachable and unreachable machines of the whole device list (also hidden or filtered ones), and an empty map for an empty list.
- **REQ-ROW-020** listRows shall return one device row per listed machine, in list order, referring to it by index; without headers there shall be no header row.
- **REQ-ROW-021** listRows shall, when headers are shown, insert a header row before the first machine of each run of machines of one account, with the text "<account>  ·  N online, M offline" using the counts of REQ-ROW-019 and "(no account)" for an empty account name.
- **REQ-ROW-022** listRows shall append one note row "▸ N offline hidden · H shows them" when N machines are collapsed (N > 0) and no note row otherwise; the note row shall also be returned for an otherwise empty list.
- **REQ-ROW-023** countText shall render "N online, M offline" for any pair of counts.
- **REQ-ROW-024** cursorRow shall return the row index of the device row whose index is the cursor (header and note rows never match, whatever their index) and 0 when there is no such row or no rows.

## Windowing

- **REQ-ROW-025** windowTop shall return 0 for an empty row list or a capacity below 1.
- **REQ-ROW-026** windowTop shall keep the window unchanged when the cursor row is inside it, move it to the cursor row (including the header row directly above it) when the cursor row is above it, move it so that the cursor row is its last row when the cursor row is below it, and never return a negative top.
- **REQ-ROW-027** listCapacity shall be the maximum number of list items of the layout when the Run panel is closed, and otherwise the room of the Run panel minus the "Search in" header (one more row while a search prompt is open or a search query is set) and minus two scroll-indicator rows, but at least 1.
- **REQ-ROW-028** ensureListCursorVisible shall set the top of the window so that the cursor row lies inside the window of listCapacity rows, with the cursor at the first row, the last row, in the middle, for a window smaller than the list, for a list shorter than the window, for an empty list, and for an unknown window size.

## MC/DC

Decision D1: `m.width > 0 && avail >= 12 && c.name > avail` (listColumns, REQ-ROW-015). Test cases (TestDO178CListColumnsTruncation): K1 width=0, widest name 50 cells, avail would be 42 -> no cut; K2 width=60, same devices -> cut to 28; K3 width=60, address 29 cells wide (avail 9), name 40 -> no cut; K4 width=60, name 20 (avail 28) -> no cut; K5 avail exactly 12, name 30 -> cut to 12; K6 avail 11 -> no cut; K7 name exactly equal to avail -> no cut. Independence pairs: width (K1, K2); avail>=12 (K5, K6); c.name>avail (K2, K4; K7 as boundary).

Decision D2: `headers && d.AccountName != last` (listRows, REQ-ROW-021). Cases: headers on, account changes -> header; headers on, same account as the previous machine -> no header; headers off, account changes -> no header. Pairs: headers (case 1, case 3); account change (case 1, case 2).

Decision D3: `m.set.groupByAccount && m.groupCount() > 1` (showGroupHeaders, REQ-ROW-018). Cases: on+2 accounts -> true; off+2 accounts -> false; on+1 account -> false. Pairs: groupByAccount (1,2); groupCount (1,3).

Decision D4: `r.kind == rowDevice && r.idx == cursor` (cursorRow, REQ-ROW-024). Cases: device row with the cursor index -> match; device row with another index -> no match; header row whose idx is 0 and cursor 0 -> no match. Pairs: kind (1,3); idx (1,2).

Decision D5: `len(rows) == 0 || capacity < 1` (windowTop, REQ-ROW-025). Cases: no rows capacity 3 -> 0; rows capacity 0 -> 0; rows capacity 3 -> computed. Pairs: len (1,3); capacity (2,3).

Decision D6: `top > 0 && rows[top-1].kind == rowHeader` (windowTop, REQ-ROW-026), reached when the cursor row is above the window. Cases: W1 cursor row 0 (top becomes 0) -> top stays 0, no header lookup ("cursor at first row, scrolled down"); W2 cursor row 5 with a header at row 4 -> top 4 ("above, header directly above is kept"); W3 cursor row 3 with a device row at row 2 -> top 3 ("above, device row above"). Pairs: top>0 (W1 vs W2, the only difference being whether the top is above the first row), kind (W2 vs W3).

Decision D7: `m.input.mode == inputSearch || m.list.searchQuery != ""` (listCapacity, REQ-ROW-027). Cases: search prompt open, no query -> header 3; no prompt, query set -> header 3; neither -> header 2. Pairs: mode (case 1, case 3); query (case 2, case 3).

Decision D8: `count[...] > 1` and `lab != ""` (shortNames, REQ-ROW-004). Cases: collision with label -> tag; collision without label -> no tag; no collision with label -> no tag. Pairs: count (1,3); label (1,2).

Decision D9: `gap > 0` (padRight), `w > c.name`, `w > c.addr`, `lipgloss.Width(name) > c.name` are single-condition decisions covered both ways by REQ-ROW-007, 014 and 017.

Decision D10: `expired` / `else if soon` (styledFlags symbol mode, REQ-ROW-011): expired -> offline colour; not expired and soon -> warning colour; neither -> blank. A chain of single conditions, each covered both ways.

## Derived requirements / unreachable code

- Derived: shortHost of the bare string ".ts.net" returns "" (first label is empty); it is not a real machine name and no requirement protects it.
- Defensive condition: `len(parts) >= 3` in shortHost and tailnetLabel is always true once the name ends in ".ts.net" (the suffix contains two dots), so its false outcome is unreachable; it has no statement of its own.
- The final clamp of listCapacity was written as `if c < 1 { c = 1 }`; the Run panel height is never below 15, so the clamp can never act. It was rewritten without a branch (`max(..., 1)`) with identical behaviour so no statement is left unexecuted.
- The status icon in symbol mode has the same shape for a reachable and an unreachable machine; the difference is the colour (REQ-ROW-009).
