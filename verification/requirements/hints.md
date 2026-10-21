# Requirements: key hints and help (REQ-HNT) - tui/hints.go

- **REQ-HNT-001** The hint bar shall show as many hints as fit the width, most useful first, dropping the least useful, and shall always end with the tail ("? help • q quit"), even when nothing else fits.
- **REQ-HNT-002** The list's hint bar shall mention the current keys (ssh, search, ping, run, run panel and machine actions, offline toggle, accounts, settings, sort, reload, copy, profile, options, about, clear-user when a default user is set); it shall fit the terminal; and while the Run panel has focus, "enter" shall be followed by what it does on the active tab (run, run again, do it).
- **REQ-HNT-003** `?` shall open the full help screen from the list and close it again.
