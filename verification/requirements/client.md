# Requirements: Tailscale API client, `client/tailscale.go` (REQ-API)

`tailscale.go` sends the API key to the Tailscale API and judges the key's health. A defect can send the key to the wrong place, hang or exhaust memory on a hostile response, or report a dead key as healthy (or the reverse).

## Request construction

- **REQ-API-001** Every request shall be a `GET` without body to `baseURL + path`, carrying `Authorization: Bearer <apiKey>` and `Accept: application/json`.
- **REQ-API-002** A request that cannot be built (invalid URL) shall fail with "failed to create request", and a transport failure (connection refused) with "failed to execute request"; the API key shall never appear in these errors.
- **REQ-API-003** `NewClient` shall keep the given key and use an HTTP client with a 30 second timeout.
- **REQ-API-004** The tailnet (user input) and the key ID shall be path-escaped, so that `/`, `?`, `#`, `%`, space, newline and `..` cannot change the endpoint; an empty tailnet shall yield an empty path segment, not a different endpoint.

## Responses

- **REQ-API-005** No more than `maxResponseBytes` (16 MiB) of any response body shall be buffered: a body of exactly the cap is read completely, anything beyond it is cut off, and a body cut off in the middle of a JSON document shall produce a parse error, never a panic.
- **REQ-API-006** An API error shall be reported as `API error: <message>` when the body is JSON with a non-empty `message`, and otherwise as `API returned status <code>: <body>` (non-JSON, empty message, no message, truncated JSON, empty or unreadable body), without interpreting the text as a format string.
- **REQ-API-007** `ValidateAPIKey` shall return nil for statuses below 400 (including 399), the "invalid API key or insufficient permissions" error (recognisable with `errors.Is(err, errInvalidKey)`) for 401 and 403, the mapped API error for any other status >= 400 (400 included), and the transport error unchanged otherwise.
- **REQ-API-008** `ListDevices` shall return the devices for status 200 (empty list when empty or absent), an API error and no devices for any other status (201, 204, 3xx, 4xx, 5xx), "failed to parse response" for a body that is not a device list, "failed to read response body" when reading fails, and the transport error otherwise.
- **REQ-API-009** Device JSON shall be parsed into `Device` including `connectedToControl` (true while `lastSeen` is omitted, leaving `LastSeen` zero), addresses, tags, expiry flags and timestamps; the locally added `AccountName` and `AccountTailnet` shall never be taken from the API payload.

## Multiple accounts

- **REQ-API-010** `ListDevicesFromAccountsWithErrors` shall query every account with that account's own key, return the merged devices each tagged with its account's name and tailnet, return exactly one error per failing account ("failed to fetch devices from account <name>: " wrapping the cause) while still returning the devices of the others, and return nothing for no accounts.
- **REQ-API-011** `ListDevicesFromAccounts` shall return the same devices and print one `Warning: <error>` line on stderr per failing account, and print nothing when no account fails.

## API key health

- **REQ-API-012** `APIKeyID` shall return the ID segment of a `tskey-api-<id>-<secret>` token (up to the first `-` after the prefix, case-sensitive, no trimming) and "" for any other token, a token without a secret separator, or the prefix alone; an empty ID yields "".
- **REQ-API-013** `KeyInfo` shall `GET /tailnet/<tailnet>/keys/<keyID>` (both escaped) and return `Expires`, `Revoked` (true iff the `revoked` timestamp is non-zero) and `Invalid` from the response.
- **REQ-API-014** `KeyInfo` shall return a zero `KeyInfo` and the mapped API error for any status other than 200, an error for an unreadable body, "failed to parse response" for malformed JSON or wrongly typed fields, and the transport error otherwise.
- **REQ-API-015** `CheckAccount` shall report a key that can list devices and whose record has an expiry as `Valid`, `HasExpiry` with that `Expires`, no error and no note.
- **REQ-API-016** `CheckAccount` shall report a key that can list devices but whose expiry is unknown as `Valid` without `HasExpiry` and with no error: with note "unknown key format" when the token has no ID, with note "expiry not readable" when the key record cannot be read or parsed, and with an empty note when the record shows no expiry.
- **REQ-API-017** `CheckAccount` shall report a key rejected with 401 or 403 as not `Valid`, `Unauth`, with the error, and shall not query the key record.
- **REQ-API-018** `CheckAccount` shall report any other failure (server error, network error) as not `Valid` with the error and without `Unauth`; the text of a server-supplied message shall not cause `Unauth`.
- **REQ-API-019** `CheckAccount` shall report a key whose record is revoked or invalid as not `Valid`, `Unauth`, error "API key is revoked or expired" and without expiry, whichever of the two (or both) applies, and with or without an expiry in the record.

## MC/DC

| # | Decision | Condition | Pair (outcome false / outcome true) |
|---|---|---|---|
| D1 | `ValidateAPIKey`: `status == 401 \|\| status == 403` | status==401 | `TestValidateAPIKeyStatusMapping/402 neither 401 nor 403` (F,F) / `401` (T,-) |
| | | status==403 | `…/402 neither 401 nor 403` (F,F) / `403` (F,T) |
| D2 | `CheckAccount`: `info.Revoked \|\| info.Invalid` | Revoked | `TestCheckAccountRevokedOrInvalidKey/neither revoked nor invalid stays valid` (F,F) / `revoked only` (T,-) |
| | | Invalid | `…/neither revoked nor invalid stays valid` (F,F) / `invalid only` (F,T) |
| D3 | `apiError`: `json.Unmarshal(...) == nil && errResp.Message != ""` | Unmarshal ok | `TestAPIErrorMapping/truncated json` (F; the message is then empty) / `json message` (T,T) |
| | | Message != "" | `TestAPIErrorMapping/json empty message` (T,F) / `json message` (T,T) |

For D3 the pair for the first condition is a masking pair: when the unmarshal fails, `encoding/json` validates the document before filling the struct, so the message is empty in that case too, and the decision is false whatever the second condition would have been.

Single-condition decisions are covered with both outcomes: `err != nil` after each request, body read and parse (REQ-API-002, 005, 008, 013, 014); `resp.StatusCode >= 400` (399/400 boundary, REQ-API-007); `StatusCode != 200` in `ListDevices` and `KeyInfo` (REQ-API-008, 014); `result.Error != nil` (REQ-API-010); `len(errs)` loop over `errs` (REQ-API-011); `ok` of `CutPrefix` and `Cut` (REQ-API-012); the `switch` in `CheckAccount` (error / revoked-or-invalid / expiry non-zero / none, REQ-API-015 to 019); `id == ""` (REQ-API-016).

## Derived requirements / unreachable code

Derived requirements:

- D-1 `ValidateAPIKey` treats every status below 400 as success, including 3xx that the HTTP client did not follow and 1xx/2xx other than 200; `ListDevices` on the other hand accepts only 200 (REQ-API-007, 008). The asymmetry is kept.
- D-2 An empty API key is sent as `Authorization: Bearer` (net/http trims the trailing space of a header value); the server rejects it. No client-side check exists.
- D-3 `ListDevicesFromAccountsWithErrors` returns errors in completion order, not account order, because accounts are queried in parallel; callers must not depend on the order.
- D-4 The key ID returned by `APIKeyID` is not validated; it is safe because `KeyInfo` escapes it (REQ-API-004).

Non-test change made (message text and behaviour unchanged for all non-hostile responses):

- Bug fix (REQ-API-018): `CheckAccount` set `Unauth` when `strings.Contains(err.Error(), "invalid API key")`, so a server-supplied message such as `{"message":"invalid API key format"}` on a 400/500 was classified as an auth rejection. The 401/403 error is now the sentinel `errInvalidKey` (same text) and `CheckAccount` uses `errors.Is`.

Unreachable statements: none. All statements of `tailscale.go` are executed by the tests (100.0 %). The `Content-Length` mismatch trick in the tests provokes the body-read failures without any test seam.

## Device actions (client/device_actions.go)

- **REQ-API-020** The device action calls shall send exactly the documented requests: `POST /device/{id}/key` with `{"keyExpiryDisabled":bool}`, `POST /device/{id}/expire` (no body), `POST /device/{id}/name` with `{"name":…}` (trimmed), `POST /device/{id}/tags` with `{"tags":[…]}`, `DELETE /device/{id}`; the device ID shall be path-escaped, and the JSON content type shall be set only when there is a body.
- **REQ-API-021** A non-2xx answer shall be returned as an error carrying the API's own message (or the status), a 2xx answer without body shall be success, network errors shall be wrapped, and an unencodable payload shall be an error.
- **REQ-API-022** Invalid input (empty device ID, empty name, no tags, a tag not of the form `tag:name`) shall be rejected before anything is sent.
- **REQ-API-023** `NormalizeTags` shall turn comma/space separated input into `tag:`-prefixed tags without empties or duplicates, preserving order.
