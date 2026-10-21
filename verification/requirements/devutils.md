# Requirements: filter / sort pipeline (REQ-DEV) - tui/device_utils.go

Scope: which machines the list shows and in which order. Intent: machines of hidden accounts never appear; the profile
filter and the search only narrow the list; offline machines are collapsed (and counted) except while searching;
online machines come first in every sort mode; the order of the underlying device list is never disturbed; the cursor
stays on the same machine across a refilter.

## Online state and sort

- **REQ-DEV-001** sortMode.String shall return "Name", "Last seen" and "Added" for the three sort modes and "Unknown" for any other value (sortModeCount, negative, large).
- **REQ-DEV-002** isDeviceOnline shall be true for a device connected to the control plane whatever its LastSeen (including the zero time), and otherwise true exactly when LastSeen is less than 5 minutes ago (just inside true, just outside false, zero time false).
- **REQ-DEV-003** sortDevices shall place every online device before every offline device in every sort mode, keeping the relative order of devices that compare equal (stable), and shall accept an empty or one-element slice.
- **REQ-DEV-004** In name mode sortDevices shall order devices of the same online state alphabetically, case-insensitively, by Name, using the Hostname for a device with an empty Name.
- **REQ-DEV-005** In last-seen mode sortDevices shall order devices of the same online state by LastSeen, most recent first; in created mode by Created, newest first.
- **REQ-DEV-006** An unknown sort mode shall sort like name mode, and sortDevicesByStatus shall sort by name mode.
- **REQ-DEV-007** getStatusIcon shall return the green marker for an online device and the red marker otherwise.
- **REQ-DEV-008** getKeyExpiryIcon shall return "" when key expiry is disabled or the device has no expiry date (also when both hold), the lock for an expiry in the past and the key for an expiry in the future.

## Filtering

- **REQ-DEV-009** filterDevices shall omit every machine whose account is hidden and keep the machines of visible accounts and of accounts not in the account list.
- **REQ-DEV-010** filterDevices shall, when a profile is selected, keep only machines of that account (none when no machine belongs to it) and, when no profile is selected, keep all visible machines.
- **REQ-DEV-011** filterDevices shall, for a non-empty search query, keep exactly the machines whose Name, Hostname, OS or any address contains the query, ignoring case; a query matching nothing shall give an empty list; a machine without name, hostname, OS or addresses never matches a non-empty query.
- **REQ-DEV-012** filterDevices shall, when hide-offline is on and the search query is empty, drop every machine that is not reachable (ping result first, API state otherwise) and record how many were dropped in hiddenOffline; with a search query or with hide-offline off nothing is dropped; hiddenOffline shall be reset on every call.
- **REQ-DEV-013** filterDevices shall order the result with the selected sort mode without changing the order of the underlying device list.
- **REQ-DEV-014** filterDevices shall, when grouping by account is on, order machines by account rank (accounts in configured order, then accounts not configured in alphabetical order), keeping the sort order inside each account; with grouping off the plain sort order is kept.
- **REQ-DEV-015** accountRank shall give configured accounts their index in the account list and accounts found only on devices (including the empty account name) the ranks after the configured ones in alphabetical order.
- **REQ-DEV-016** filterDevices shall put the cursor and the list viewport back on the first row.

## Keeping the cursor

- **REQ-DEV-017** refilterKeepCursor shall leave the cursor on the machine it was on (identified by machineKey) when that machine is still listed after the filters were re-applied, wherever it moved to.
- **REQ-DEV-018** refilterKeepCursor shall put the cursor on the first row when the highlighted machine is no longer listed or when no machine was highlighted (empty list, cursor out of range).
- **REQ-DEV-019** refilterKeepCursor shall scroll the list so that the cursor row is visible.

## MC/DC

Decision D1: `device.KeyExpiryDisabled || device.Expires.IsZero()` (getKeyExpiryIcon). Cases (REQ-DEV-008): A disabled=T zero=F -> ""; B disabled=F zero=T -> ""; C disabled=F zero=F -> icon; D both true -> "". Condition 1 pair (A, C), condition 2 pair (B, C).

Decision D2: `onlineI && !onlineJ` and decision D3: `!onlineI && onlineJ` (sortDevices less function). Cases (REQ-DEV-003, TestDO178CSortOnlineFirst, every pair of devices that the sort compares): (I online, J offline) -> true; (I offline, J online) -> false; (both online) and (both offline) -> mode comparison. D2: condition onlineI pair [(T,F) -> true, (F,F) -> mode comparison]; condition !onlineJ pair [(T,F) -> true, (T,T) -> mode comparison]. D3: condition !onlineI pair [(F,T) -> true, (T,T) -> mode comparison]; condition onlineJ pair [(F,T) -> true, (F,F) -> mode comparison]. The device set of the test contains two online and two offline machines, so every one of the four combinations is compared.

Decision D4: `m.set.hideOffline && m.list.searchQuery == ""` (filterDevices). Cases (REQ-DEV-012): hideOffline=T query="" -> collapse; hideOffline=F query="" -> nothing dropped; hideOffline=T query="x" -> nothing dropped. Pairs: hideOffline (1,2), query (1,3).

Decision D5: `contains(name) || contains(hostname) || contains(os)` followed by the address loop (filterDevices search). Cases (REQ-DEV-011): only name matches; only hostname matches; only OS matches; only an address matches; nothing matches. Each of the three conditions has its pair with the nothing-matches case.

Decision D6: `i < 0 ...` is not in this file. `key != ""` and `machineKey(d) == key` in refilterKeepCursor are simple conditions (REQ-DEV-017/018).

## Derived requirements / unreachable code

- Derived: the account filter treats an account name absent from the account list as visible (accountHidden returns false); this lets machines of an account that was just removed disappear only after the next reload. Covered by REQ-DEV-009.
- Derived: refilterKeepCursor identifies a machine by machineKey (ID, else Hostname); two machines with no ID and the same hostname are indistinguishable and the cursor lands on the first of them.
- No unreachable statements.
