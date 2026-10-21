# Requirements: Accounts window (REQ-AWN) - tui/accounts.go

Scope: the Accounts window (key m) in which Tailscale accounts are added, edited, removed, shown/hidden and switched,
with a key-health line per account. Intent: every change is written to config.json through config.Update and the
in-memory list is then reloaded from disk; a new or changed API key is verified against the Tailscale API before it is
stored; the API key is only ever typed into a masked prompt and is never displayed or put into a status line; a
destructive action needs a confirmation; nothing happens when the list is empty or a change is running.

## Account helpers and key health

- **REQ-AWN-001** sameTailnet shall be true when, ignoring case, one of the two names starts with the other with a trailing "@" removed (so a truncated "user@" login matches "user@example.com" in either direction, and equal names match) and false for unrelated names; the empty string matches every name (callers guard against it).
- **REQ-AWN-002** isThisComputer shall be false while the login of the Tailscale daemon is unknown (empty) and otherwise true exactly when the account tailnet is the same tailnet as that login.
- **REQ-AWN-003** accountHidden and hasAccount shall look an account up by name: accountHidden returns the Hidden flag of the account and false for an unknown name; hasAccount returns whether the name exists (false for an empty list).
- **REQ-AWN-004** checkAllHealth shall return no command for an empty account list and otherwise one background check per account; each check shall deliver the account name together with the result of the health checker for that account.
- **REQ-AWN-005** daysLeft shall return the whole number of days until a time, rounded down (0 within the next day).
- **REQ-AWN-006** healthText shall describe an account whose key was not checked yet (no entry, or an entry not marked checked) as "checking…" level 0; a key the API rejected as "key rejected" level 2; an otherwise invalid key as "unreachable" level 1; a valid key with a known expiry as "key expired" (level 2) when the expiry has passed, "key expires in Nd" level 2 for fewer than 3 days, level 1 for fewer than 14 days (keyWarnDays), and "key ok, Nd left" level 0 from 14 days on; a valid key without a known expiry (with or without an expiry note) as "key ok" level 0. A rejected key wins over an invalid state, an invalid state over an expiry.
- **REQ-AWN-007** keyWarning shall return "" when no account has a key problem, and otherwise the line "⚠ API key: <account>: <text>; ... (m)" listing every account whose health level is above 0 (expired, rejected, expiring within 14 days), in list order; accounts that are merely unreachable, still being checked or healthy shall not appear.
- **REQ-AWN-008** accountsFromConfig shall copy name, API key, tailnet, active and hidden flags of every configured account, in order, and return an empty list for a configuration without accounts.

## Persisting changes

- **REQ-AWN-009** addAccountCmd shall verify the API key against the tailnet before touching the configuration: when the check fails it shall return the error "API key check failed: ..." and store nothing; when it succeeds it shall store the account (the first account becomes the active one, later ones do not), report "Added account <name>" and request a reload of the machines; when the configuration cannot be written it shall return that error and not request a reload.
- **REQ-AWN-010** editAccountCmd shall verify a new API key before storing it (failure: "API key check failed: ...", nothing stored); with no new key it shall not contact the API; it shall store the new name and/or key of the named account and report "Account updated"; it shall request a reload only when the account was renamed and the write succeeded; an unknown account or a name already taken shall be returned as an error without a reload.
- **REQ-AWN-011** removeAccountCmd shall remove the named account from the configuration, report "Removed account <name>" and request a reload; an unknown account shall give the error "account '<name>' not found" and change nothing.
- **REQ-AWN-012** hideAccountCmd shall set or clear the hidden flag of the named account, report "<name> is now hidden" or "<name> is now shown", and not request a reload; an unknown account shall give the error "account '<name>' not found".
- **REQ-AWN-013** useAccountCmd shall make the named account the active one in the configuration and then run `tailscale switch <tailnet>`: on success it shall report "This computer now uses <tailnet>"; when the switch fails it shall keep the configuration change and report that the account was set but the computer was not switched (a notice, not an error); an unknown account shall give an error and shall not run tailscale.

## Reacting to a change

- **REQ-AWN-014** handleAccountsChanged shall always clear the busy flag; for a message with an error it shall show the error text as an error status and change nothing else.
- **REQ-AWN-015** handleAccountsChanged shall, for a successful message, show the notice as a success status, reload the account list, the active account (empty when there is none) and the daemon login from disk and from tailscale, move a manage cursor beyond the end of the shorter list onto the last account (0 for an empty list), and reset the profile filter when that account no longer exists (but not when it still exists, nor when none is selected).
- **REQ-AWN-016** handleAccountsChanged shall show the error text as an error status, and change nothing else, when the configuration cannot be read back.
- **REQ-AWN-017** handleAccountsChanged shall keep the key health of accounts that are unchanged in name, tailnet and key, drop the health of removed accounts, and start a new check for every account that is new or whose tailnet or key changed (sameKey is true only for an identical name, tailnet and key).
- **REQ-AWN-018** handleAccountsChanged shall, when a reload is requested, empty the machine list when no account is left, otherwise start a reload of the machines unless one is already running (a second one is never started); without a reload request it shall start no reload; it shall then re-apply the list filters keeping the cursor on the same machine.

## Prompts

- **REQ-AWN-019** backToAccounts shall return to the Accounts window.
- **REQ-AWN-020** promptAddAccount shall ask in order for the tailnet, the API key (masked) and the display name; each answer shall be sanitised (control characters removed, trimmed) and validated; an invalid answer shall close the prompt and show "Invalid tailnet/API key/name: ..." as an error status without starting any command; an empty name shall default to the tailnet; the last answer shall mark the window busy with "Checking API key…" and return the add command with the typed tailnet, name and key; cancelling at any step shall return to the Accounts window.
- **REQ-AWN-021** The API key typed into a prompt shall be masked in the displayed prompt, and shall not appear in the Accounts window, in any status line or in any error message produced by a rejected answer.
- **REQ-AWN-022** promptEditAccount shall ask for the new name (prefilled with the current name, cursor at its end, counted in characters) and then for the new API key (masked); an answer equal to the current name or empty keeps the name; an invalid new name or key shall close the prompt and show an error status without starting a command; an empty key keeps the key; when neither changed no command shall be started and the window shall not become busy; otherwise the window shall become busy with "Saving…" and the edit command for the account (by its current name and tailnet) shall be returned; cancelling at either step shall return to the Accounts window.

## Keys

- **REQ-AWN-023** While a removal awaits confirmation any key shall consume the confirmation; only "y" or "Y" shall start the removal (marking the window busy), every other key shall cancel it without a command.
- **REQ-AWN-024** The keys esc, ctrl+c and q shall close the window; "a" shall close it and open the add prompt; "c" shall discard the health of every account and start a new check of all of them.
- **REQ-AWN-025** The keys up/k and down/j shall move the cursor by one row and stop at the first and the last account (no movement on an empty list).
- **REQ-AWN-026** The account actions enter, space, e and d (delete) shall do nothing while a change is running or when the cursor is not on an account (empty list or out-of-range cursor); any other key shall do nothing.
- **REQ-AWN-027** On an account, enter shall mark the window busy with "Switching…" and start the use command for the account, space shall start the hide command with the opposite of the current hidden flag, e shall close the window and open the edit prompt for the account, and d or delete shall ask for a confirmation naming the account without starting a command.

## View

- **REQ-AWN-028** renderAccountManagement shall show the title with the program version, a hint for an empty list, and per account a row with the name, the tailnet only when at least one tailnet differs from its name, "online/total" machine counts (reachable by the API state, machines of that account, hidden or not), the key health text, a "hidden" marker for a hidden account, the computer marker for the account this computer is logged in to, and the cursor marker on the cursor row.
- **REQ-AWN-029** renderAccountManagement shall show at most one status line, in this priority: the removal confirmation question, the error status, the success status (prefixed with a check mark); no line when there is none; it shall never contain an API key.
- **REQ-AWN-030** renderAccountManagement shall size the panel to the terminal (width minus 6) between 50 and 100 cells, and 100 when the width is unknown, and shall render without failing for zero or tiny terminal sizes, unicode names and any number of accounts.

## MC/DC

Decision D1: `HasPrefix(a, TrimSuffix(b,"@")) || HasPrefix(b, TrimSuffix(a,"@"))` (sameTailnet, REQ-AWN-001). Cases: "user@example.com" vs "user@" -> true (first); "user@" vs "user@example.com" -> true (second); "home.com" vs "work.com" -> false. Pairs: condition 1 (case 1 vs case 3), condition 2 (case 2 vs case 3).

Decision D2: `m.acct.tailscaleActiveAccount != "" && sameTailnet(...)` (isThisComputer, REQ-AWN-002). Cases: login known and same -> true; login empty (tailnet would match everything) -> false; login known and different -> false. Pairs: login known (1,2); sameTailnet (1,3).

Decision D3: `!ok || !hs.checked` (healthText, REQ-AWN-006). Cases: no entry -> checking; entry unchecked -> checking; entry checked -> health text. Pairs: !ok (no entry vs checked entry), !checked (unchecked entry vs checked entry).

Decision D4: `level > 0 && text != "unreachable"` (keyWarning, REQ-AWN-007). Cases: "key expired" (level 2) -> listed; "key ok" (level 0) -> not listed; "unreachable" (level 1) -> not listed; "key expires in 10d" (level 1) -> listed. Pairs: level (key expired vs key ok; text differs only in a condition that is true in both), text (key expires in 10d vs unreachable, both level 1).

Decision D5: `err == nil && newName != ""` (editAccountCmd reload, REQ-AWN-010). Cases: rename ok -> reload; key only ok -> no reload; rename to a taken name -> no reload. Pairs: newName (rename vs key only), err (rename ok vs rename taken).

Decision D6: `err == nil` in addAccountCmd/removeAccountCmd reload (REQ-AWN-009, REQ-AWN-011): single condition, both outcomes tested (write ok / config unwritable; removal ok / unknown account).

Decision D7: `hs, ok := health[name]; ok && sameKey(old, acc)` (handleAccountsChanged, REQ-AWN-017). Cases: unchanged account with health -> kept; account with health but re-keyed -> re-checked; new account without health -> checked. Pairs: ok (unchanged vs new account... differs in sameKey too: new account has no health and no old entry; use an account that is unchanged in the old list but has no health entry vs the unchanged account with health), sameKey (unchanged vs re-keyed, both with health).

Decision D8: `m.list.selectedProfile != "" && !m.acct.hasAccount(selectedProfile)` (REQ-AWN-015). Cases: profile that vanished -> reset; profile that still exists -> kept; no profile -> nothing. Pairs: selectedProfile!="" (vanished vs none), !hasAccount (vanished vs existing).

Decision D9: `len(m.acct.list) == 0` / `!m.reloading` (REQ-AWN-018): reload requested with no accounts -> machines cleared; with accounts and not reloading -> reload started; with accounts while reloading -> not started again. Each condition has its pair (with accounts vs none; reloading vs not).

Decision D10: `key == "y" || key == "Y"` (REQ-AWN-023). Cases: "y" -> remove; "Y" -> remove; "n" -> cancel. Pairs: first (y vs n), second (Y vs n).

Decision D11: `m.acct.busy || m.acct.manageCursor >= len(m.acct.list)` (REQ-AWN-026). Cases: busy on an account -> ignored; not busy, cursor beyond the list -> ignored; not busy and on an account -> acts. Pairs: busy (case 1 vs case 3), cursor (case 2 vs case 3).

Decision D12: `newName == "" && key == ""` (promptEditAccount, REQ-AWN-022). Cases: neither changed -> nothing; name only -> command; key only -> command. Pairs: newName (neither vs name only), key (neither vs key only).

Decision D13: `m.width > 0 && m.width-6 < width` (renderAccountManagement, REQ-AWN-030). Cases: width 0 -> 100; width 80 -> 74; width 200 -> 100. Pairs: width>0 (0 vs 80, 0 vs ... width 0 gives 100, a width of 80 gives 74), width-6<100 (80 vs 200).

Decision D14: `newName == acc.Name` / `newName != ""` / `key != ""` chains in promptEditAccount: single conditions, each both ways (REQ-AWN-022).

Decision D15: `acc.Tailnet != acc.Name` showTail (REQ-AWN-028): single condition inside a loop, both outcomes tested (all equal -> no tailnet column; one different -> column).

## Derived requirements / unreachable code

- Derived: sameTailnet("", x) is true for every x; isThisComputer and needsAccountSwitch guard against the empty login, but an account with an empty tailnet would be shown as "this computer". Accounts cannot be created with an empty tailnet (ValidateTailnet), so this is not reachable through the UI.
- Derived: the daemon login "<not connected>" is compared like any login and matches no valid tailnet.
- Derived: in healthText the case `ExpiryNote != ""` and the final return produce the same result ("key ok", level 0); the note is only documentation of why the expiry is unknown.
- Derived: the key `"space"` in handleAccountManagement is an alias that bubbletea never produces (the space key reports " "); the statement belongs to the case clause that " " reaches.
- Derived: removing an account that makes another one active (config invariant) and the machine reload are triggered together by one message; the Accounts window does not tell the user which account became active.
- No unreachable statements.
