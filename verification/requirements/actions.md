# Requirements: Machine tab (REQ-ACT) - tui/actions.go

The Machine tab of the Run panel offers actions on the highlighted machine through the Tailscale API, and a ping with details. They address the highlighted machine only, never the marked set.

- **REQ-ACT-001** The tab shall list: ping with details, disable key expiry (or enable it when expiry is already disabled), expire key now, rename, edit tags, delete from tailnet; expire and delete shall be flagged as dangerous.
- **REQ-ACT-002** Disabling or enabling key expiry shall call the API of the machine's account, show the call as busy while it runs, show success and reload the machine list.
- **REQ-ACT-003** Expiring the key and deleting the machine shall first ask the user to type the machine's short name; any other text shall cancel the action without calling the API and say so.
- **REQ-ACT-004** Rename shall start with the current short name and do nothing for an unchanged or empty name; edit tags shall start with the current tags, normalise the input and refuse an empty list.
- **REQ-ACT-005** An API failure shall be shown with the API's message and shall not reload the list; a machine whose account is not configured shall be refused with a message; a success shall not start a second reload while one runs.
- **REQ-ACT-006** Ping with details shall store the path and latency (shown in the details panel), mark the machine reachable on a reply and unreachable on no reply, and leave the state unchanged when the tailscale CLI is missing.
- **REQ-ACT-007** No action shall start while another one is running, without a highlighted machine, or with the cursor out of range.
- **REQ-ACT-008** The tab shall render its rows, the one-machine-only hint and the last result or error; Enter on the tab shall start the highlighted action instead of running a script; `]` from the last other tab shall reach it.
- **REQ-ACT-009** The daemon's peer status shall be loaded at start and after every reload; the details panel shall show the path, and Tailscale SSH when the peer offers it; a failed refresh shall keep the previous data, and a reload shall forget old ping details.
