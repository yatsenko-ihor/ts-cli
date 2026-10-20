package client

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Requirements-based tests for tailscale.go (see verification/requirements/client.md).

// deadServerURL returns a URL on which nothing listens (connection refused).
func deadServerURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	return u
}

func setBaseURL(t *testing.T, u string) {
	t.Helper()
	old := baseURL
	baseURL = u
	t.Cleanup(func() { baseURL = old })
}

// truncatedBody serves a Content-Length larger than the bytes actually sent,
// so the client's body read fails with an unexpected EOF.
func truncatedBody(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Length", "1000")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"devices":`))
}

// ─── doRequest ───────────────────────────────────────────────────────────────

// Verifies: REQ-API-001
func TestRequestsCarryBearerTokenAndAcceptHeader(t *testing.T) {
	for _, key := range []string{"tskey-api-abc-def", "", "key with space", "kéy"} {
		t.Run("key="+key, func(t *testing.T) {
			var gotAuth, gotAccept, gotMethod string
			var gotBody int64
			withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotAuth, gotAccept, gotMethod = r.Header.Get("Authorization"), r.Header.Get("Accept"), r.Method
				gotBody = r.ContentLength
				_, _ = w.Write([]byte(`{"devices":[]}`))
			})
			if _, err := NewClient(key).ListDevices("x.com"); err != nil {
				t.Fatal(err)
			}
			wantAuth := "Bearer " + key
			if key == "" {
				wantAuth = "Bearer" // net/http trims the trailing space of a header value
			}
			if gotAuth != wantAuth || gotAccept != "application/json" || gotMethod != "GET" || gotBody != 0 {
				t.Fatalf("auth=%q accept=%q method=%q body=%d", gotAuth, gotAccept, gotMethod, gotBody)
			}
		})
	}
}

// Verifies: REQ-API-002
func TestRequestErrorsAreWrapped(t *testing.T) {
	t.Run("invalid request URL", func(t *testing.T) {
		setBaseURL(t, "http://bad host\x7f")
		_, err := NewClient("k").ListDevices("x.com")
		if err == nil || !strings.Contains(err.Error(), "failed to create request") {
			t.Fatalf("err = %v", err)
		}
		if err := NewClient("k").ValidateAPIKey("x.com"); err == nil {
			t.Fatal("ValidateAPIKey must fail too")
		}
		if _, err := NewClient("k").KeyInfo("x.com", "id"); err == nil {
			t.Fatal("KeyInfo must fail too")
		}
	})
	t.Run("connection refused", func(t *testing.T) {
		setBaseURL(t, deadServerURL(t))
		_, err := NewClient("k").ListDevices("x.com")
		if err == nil || !strings.Contains(err.Error(), "failed to execute request") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("the key is never part of the error text", func(t *testing.T) {
		setBaseURL(t, deadServerURL(t))
		_, err := NewClient("tskey-api-SUPERSECRET").ListDevices("x.com")
		if err == nil || strings.Contains(err.Error(), "SUPERSECRET") {
			t.Fatalf("err = %v", err)
		}
	})
}

// Verifies: REQ-API-003
func TestNewClientTimeoutAndKey(t *testing.T) {
	c := NewClient("tskey-api-x")
	if c.apiKey != "tskey-api-x" || c.httpClient == nil || c.httpClient.Timeout != 30*time.Second {
		t.Fatalf("client = %+v", c)
	}
	if NewClient("").apiKey != "" {
		t.Fatal("empty key kept as is")
	}
}

// Verifies: REQ-API-004
func TestTailnetIsPathEscapedOnTheWire(t *testing.T) {
	hostile := map[string]string{ // tailnet -> expected RequestURI
		"example.com":      "/tailnet/example.com/devices",
		"user@example.com": "/tailnet/user@example.com/devices",
		"a/b":              "/tailnet/a%2Fb/devices",
		"../../admin":      "/tailnet/..%2F..%2Fadmin/devices",
		"x?y=1#frag":       "/tailnet/x%3Fy=1%23frag/devices",
		"sp ace":           "/tailnet/sp%20ace/devices",
		"":                 "/tailnet//devices", // boundary: empty
		"100%":             "/tailnet/100%25/devices",
		"a\nb":             "/tailnet/a%0Ab/devices",
	}
	for tailnet, want := range hostile {
		t.Run(fmt.Sprintf("%q", tailnet), func(t *testing.T) {
			var gotURI string
			withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotURI = r.RequestURI
				_, _ = w.Write([]byte(`{"devices":[]}`))
			})
			c := NewClient("k")
			if _, err := c.ListDevices(tailnet); err != nil {
				t.Fatal(err)
			}
			if gotURI != want {
				t.Fatalf("ListDevices URI = %q, want %q", gotURI, want)
			}
			if err := c.ValidateAPIKey(tailnet); err != nil || gotURI != want {
				t.Fatalf("ValidateAPIKey URI = %q err=%v", gotURI, err)
			}
			if devicesPath(tailnet) != want {
				t.Fatalf("devicesPath = %q", devicesPath(tailnet))
			}
		})
	}
}

// ─── readBody / apiError ─────────────────────────────────────────────────────

// Verifies: REQ-API-005
func TestReadBodyIsCapped(t *testing.T) {
	for name, tc := range map[string]struct{ in, want int }{
		"empty":         {0, 0},
		"small":         {10, 10},
		"cap minus one": {maxResponseBytes - 1, maxResponseBytes - 1},
		"exactly cap":   {maxResponseBytes, maxResponseBytes},
		"cap plus one":  {maxResponseBytes + 1, maxResponseBytes},
		"far above cap": {maxResponseBytes + 4096, maxResponseBytes},
	} {
		t.Run(name, func(t *testing.T) {
			resp := &http.Response{Body: io.NopCloser(bytes.NewReader(make([]byte, tc.in)))}
			b, err := readBody(resp)
			if err != nil || len(b) != tc.want {
				t.Fatalf("len=%d err=%v, want %d", len(b), err, tc.want)
			}
		})
	}
	t.Run("oversized API response is not buffered whole", func(t *testing.T) {
		withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			chunk := bytes.Repeat([]byte("x"), 1<<20)
			for i := 0; i < 20; i++ { // 20 MiB
				if _, err := w.Write(chunk); err != nil {
					return
				}
			}
		})
		_, err := NewClient("k").ListDevices("x.com")
		if err == nil || len(err.Error()) > maxResponseBytes+200 {
			t.Fatalf("error text length %d", len(fmt.Sprint(err)))
		}
	})
	t.Run("truncated JSON at the cap is a parse error, not a panic", func(t *testing.T) {
		withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"devices":[`))
			_, _ = w.Write(bytes.Repeat([]byte(" "), maxResponseBytes))
			_, _ = w.Write([]byte(`]}`))
		})
		_, err := NewClient("k").ListDevices("x.com")
		if err == nil || !strings.Contains(err.Error(), "failed to parse response") {
			t.Fatalf("err = %v", err)
		}
	})
}

// Verifies: REQ-API-006
func TestAPIErrorMapping(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   string
	}{
		"json message":                {404, `{"message":"tailnet not found"}`, "API error: tailnet not found"},
		"json empty message":          {500, `{"message":""}`, `API returned status 500: {"message":""}`},
		"json without message":        {502, `{"error":"x"}`, `API returned status 502: {"error":"x"}`},
		"not json":                    {503, `<html>down</html>`, "API returned status 503: <html>down</html>"},
		"truncated json":              {500, `{"message":"x"`, `API returned status 500: {"message":"x"`},
		"empty body":                  {500, ``, "API returned status 500: "},
		"json null":                   {500, `null`, "API returned status 500: null"},
		"message with format verbs":   {400, `{"message":"100%s %d"}`, "API error: 100%s %d"},
		"status 399 body is reported": {399, `x`, "API returned status 399: x"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := NewClient("k").ListDevices("x.com")
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
	t.Run("unreadable error body still yields a status error", func(t *testing.T) {
		withTestServer(t, func(w http.ResponseWriter, r *http.Request) { truncatedBody(w, 500) })
		_, err := NewClient("k").ListDevices("x.com")
		if err == nil || !strings.Contains(err.Error(), "API returned status 500") {
			t.Fatalf("err = %v", err)
		}
	})
}

// ─── ValidateAPIKey ──────────────────────────────────────────────────────────

// Verifies: REQ-API-007
func TestValidateAPIKeyStatusMapping(t *testing.T) {
	cases := map[string]struct {
		status  int
		wantErr string // "" = nil, else substring
		invalid bool   // must be the invalid-key sentinel
	}{
		"200":                     {200, "", false},
		"204":                     {204, "", false},
		"399 (just below 400)":    {399, "", false},
		"400 (boundary)":          {400, "API returned status 400", false},
		"401":                     {401, "invalid API key or insufficient permissions", true},
		"403":                     {403, "invalid API key or insufficient permissions", true},
		"404 with message":        {404, "API error: tailnet not found", false},
		"500":                     {500, "API returned status 500", false},
		"402 neither 401 nor 403": {402, "API returned status 402", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				if tc.status == 404 {
					_, _ = w.Write([]byte(`{"message":"tailnet not found"}`))
				}
			})
			err := NewClient("k").ValidateAPIKey("x.com")
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("err = %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			if got := errors.Is(err, errInvalidKey); got != tc.invalid {
				t.Fatalf("errors.Is(errInvalidKey) = %v, want %v", got, tc.invalid)
			}
		})
	}
	t.Run("network error", func(t *testing.T) {
		setBaseURL(t, deadServerURL(t))
		err := NewClient("k").ValidateAPIKey("x.com")
		if err == nil || !strings.Contains(err.Error(), "failed to execute request") || errors.Is(err, errInvalidKey) {
			t.Fatalf("err = %v", err)
		}
	})
}

// ─── ListDevices ─────────────────────────────────────────────────────────────

// Verifies: REQ-API-008
func TestListDevicesResults(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		withTestServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"devices":[]}`)) })
		d, err := NewClient("k").ListDevices("x.com")
		if err != nil || len(d) != 0 {
			t.Fatalf("%v %v", d, err)
		}
	})
	t.Run("no devices field", func(t *testing.T) {
		withTestServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
		d, err := NewClient("k").ListDevices("x.com")
		if err != nil || len(d) != 0 {
			t.Fatalf("%v %v", d, err)
		}
	})
	for _, status := range []int{201, 204, 299, 301, 400, 401, 403, 404, 500} {
		t.Run(fmt.Sprint("status ", status, " is an error"), func(t *testing.T) {
			withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"devices":[{"id":"1"}]}`))
			})
			d, err := NewClient("k").ListDevices("x.com")
			if err == nil || d != nil {
				t.Fatalf("status %d: devices=%v err=%v", status, d, err)
			}
		})
	}
	for name, body := range map[string]string{
		"not json":     "nope",
		"empty":        "",
		"wrong type":   `{"devices":"x"}`,
		"array at top": `[]`,
		"truncated":    `{"devices":[{"id":`,
	} {
		t.Run("parse error: "+name, func(t *testing.T) {
			withTestServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
			d, err := NewClient("k").ListDevices("x.com")
			if err == nil || !strings.Contains(err.Error(), "failed to parse response") || d != nil {
				t.Fatalf("%v %v", d, err)
			}
		})
	}
	t.Run("body read failure", func(t *testing.T) {
		withTestServer(t, func(w http.ResponseWriter, r *http.Request) { truncatedBody(w, 200) })
		_, err := NewClient("k").ListDevices("x.com")
		if err == nil || !strings.Contains(err.Error(), "failed to read response body") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("network error", func(t *testing.T) {
		setBaseURL(t, deadServerURL(t))
		if d, err := NewClient("k").ListDevices("x.com"); err == nil || d != nil {
			t.Fatalf("%v %v", d, err)
		}
	})
}

// Verifies: REQ-API-009
func TestDeviceParsing(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"devices":[
{"id":"1","name":"a.t.ts.net","hostname":"a","clientVersion":"1.2","os":"linux","created":"2025-01-02T03:04:05Z",
 "connectedToControl":true,"authorized":true,"keyExpiryDisabled":true,"addresses":["100.64.0.1","fd7a::1"],"tags":["tag:srv"],
 "expires":"2030-01-01T00:00:00Z","isExternal":true,"updateAvailable":true,"blocksIncomingConnections":true,
 "AccountName":"spoof","AccountTailnet":"spoof"},
{"id":"2","hostname":"b","lastSeen":"2026-01-02T03:04:05Z","connectedToControl":false},
{"id":"3"}]}`))
	})
	d, err := NewClient("k").ListDevices("x.com")
	if err != nil || len(d) != 3 {
		t.Fatalf("%v %v", d, err)
	}
	a := d[0]
	if a.ID != "1" || a.Hostname != "a" || a.OS != "linux" || a.ClientVersion != "1.2" || !a.ConnectedToControl || !a.LastSeen.IsZero() ||
		!a.Authorized || !a.KeyExpiryDisabled || !a.IsExternal || !a.UpdateAvailable || !a.BlocksIncomingConnections ||
		len(a.Addresses) != 2 || a.Addresses[1] != "fd7a::1" || len(a.Tags) != 1 || a.Created.Year() != 2025 || a.Expires.Year() != 2030 {
		t.Fatalf("device a: %+v", a)
	}
	if a.AccountName != "" || a.AccountTailnet != "" {
		t.Fatalf("local-only fields must not be read from the API: %+v", a)
	}
	if b := d[1]; b.ConnectedToControl || b.LastSeen.IsZero() {
		t.Fatalf("device b: %+v", b)
	}
	if c := d[2]; c.ConnectedToControl || c.Hostname != "" || len(c.Addresses) != 0 {
		t.Fatalf("device c: %+v", c)
	}
}

// ─── Multi-account listing ───────────────────────────────────────────────────

func multiAccountServer(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/tailnet/bad.com/"):
			w.WriteHeader(http.StatusForbidden)
		case strings.HasPrefix(r.URL.Path, "/tailnet/broken.com/"):
			_, _ = w.Write([]byte("garbage"))
		default:
			host := strings.Split(strings.TrimPrefix(r.URL.Path, "/tailnet/"), "/")[0]
			_, _ = w.Write([]byte(`{"devices":[{"id":"` + host + `-1","hostname":"h1"},{"id":"` + host + `-2","hostname":"h2"}]}`))
		}
	})
}

// Verifies: REQ-API-010
func TestMultiAccountListingMergesAndIsolatesErrors(t *testing.T) {
	t.Run("no accounts", func(t *testing.T) {
		devices, errs := ListDevicesFromAccountsWithErrors(nil)
		if len(devices) != 0 || len(errs) != 0 {
			t.Fatalf("%v %v", devices, errs)
		}
	})
	t.Run("all succeed: devices tagged with their account", func(t *testing.T) {
		multiAccountServer(t)
		devices, errs := ListDevicesFromAccountsWithErrors([]AccountInfo{
			{Name: "one", APIKey: "k", Tailnet: "one.com"},
			{Name: "two", APIKey: "k", Tailnet: "two.com"},
		})
		if len(errs) != 0 || len(devices) != 4 {
			t.Fatalf("%v %v", devices, errs)
		}
		for _, d := range devices {
			wantName := map[bool]string{true: "one", false: "two"}[strings.HasPrefix(d.ID, "one.com")]
			if d.AccountName != wantName || d.AccountTailnet != wantName+".com" {
				t.Fatalf("device %s tagged %q/%q", d.ID, d.AccountName, d.AccountTailnet)
			}
		}
	})
	t.Run("partial failure: one error per failing account, the rest still listed", func(t *testing.T) {
		multiAccountServer(t)
		devices, errs := ListDevicesFromAccountsWithErrors([]AccountInfo{
			{Name: "good", APIKey: "k", Tailnet: "good.com"},
			{Name: "bad", APIKey: "k", Tailnet: "bad.com"},
			{Name: "broken", APIKey: "k", Tailnet: "broken.com"},
		})
		if len(devices) != 2 || len(errs) != 2 {
			t.Fatalf("devices=%d errs=%v", len(devices), errs)
		}
		joined := errs[0].Error() + "|" + errs[1].Error()
		if !strings.Contains(joined, "account bad:") || !strings.Contains(joined, "account broken:") || strings.Contains(joined, "account good") {
			t.Fatalf("errs = %v", errs)
		}
		for _, e := range errs {
			if !strings.HasPrefix(e.Error(), "failed to fetch devices from account ") {
				t.Fatalf("err = %v", e)
			}
		}
		if !strings.Contains(joined, "invalid") && !strings.Contains(joined, "API returned status 403") {
			t.Fatalf("cause lost: %v", errs)
		}
	})
	t.Run("all fail", func(t *testing.T) {
		setBaseURL(t, deadServerURL(t))
		devices, errs := ListDevicesFromAccountsWithErrors([]AccountInfo{{Name: "a", Tailnet: "a.com"}, {Name: "b", Tailnet: "b.com"}})
		if len(devices) != 0 || len(errs) != 2 {
			t.Fatalf("%v %v", devices, errs)
		}
		if !errors.Is(errs[0], errs[0]) || errors.Unwrap(errs[0]) == nil {
			t.Fatal("cause must be wrapped with %w")
		}
	})
	t.Run("each account uses its own API key", func(t *testing.T) {
		var mu sync.Mutex
		seen := map[string]string{}
		withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			seen[r.URL.Path] = r.Header.Get("Authorization")
			mu.Unlock()
			_, _ = w.Write([]byte(`{"devices":[]}`))
		})
		ListDevicesFromAccountsWithErrors([]AccountInfo{{Name: "a", APIKey: "KA", Tailnet: "a.com"}, {Name: "b", APIKey: "KB", Tailnet: "b.com"}})
		if seen["/tailnet/a.com/devices"] != "Bearer KA" || seen["/tailnet/b.com/devices"] != "Bearer KB" {
			t.Fatalf("seen = %v", seen)
		}
	})
}

// Verifies: REQ-API-011
func TestListDevicesFromAccountsReportsWarningsOnStderr(t *testing.T) {
	multiAccountServer(t)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	devices := ListDevicesFromAccounts([]AccountInfo{
		{Name: "good", APIKey: "k", Tailnet: "good.com"},
		{Name: "bad", APIKey: "k", Tailnet: "bad.com"},
	})
	os.Stderr = saved
	w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if len(devices) != 2 {
		t.Fatalf("devices = %v", devices)
	}
	if !strings.HasPrefix(buf.String(), "Warning: failed to fetch devices from account bad") || strings.Count(buf.String(), "Warning:") != 1 {
		t.Fatalf("stderr = %q", buf.String())
	}
	// No failures: nothing printed.
	r2, w2, _ := os.Pipe()
	os.Stderr = w2
	ListDevicesFromAccounts([]AccountInfo{{Name: "good", APIKey: "k", Tailnet: "good.com"}})
	os.Stderr = saved
	w2.Close()
	buf.Reset()
	_, _ = buf.ReadFrom(r2)
	if buf.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", buf.String())
	}
}

// ─── APIKeyID ────────────────────────────────────────────────────────────────

// Verifies: REQ-API-012
func TestAPIKeyIDParsing(t *testing.T) {
	for in, want := range map[string]string{
		"tskey-api-kABC123CNTRL-secretpart": "kABC123CNTRL",
		"tskey-api-id-":                     "id", // boundary: empty secret
		"tskey-api--secret":                 "",   // boundary: empty id
		"tskey-api-":                        "",   // prefix only
		"tskey-api-nodash":                  "",   // no secret separator
		"tskey-api":                         "",   // prefix incomplete
		"tskey-auth-kABC-secret":            "",   // other token family
		"TSKEY-API-kABC-secret":             "",   // case-sensitive
		" tskey-api-kABC-secret":            "",   // leading space is not trimmed
		"":                                  "",
		"tskey-api-a-b-c-d":                 "a",       // only the first segment
		"tskey-api-../../x-secret":          "../../x", // hostile: caller must escape
		"xtskey-api-kABC-secret":            "",
	} {
		if got := APIKeyID(in); got != want {
			t.Errorf("APIKeyID(%q) = %q, want %q", in, got, want)
		}
	}
}

// ─── KeyInfo ─────────────────────────────────────────────────────────────────

// Verifies: REQ-API-013
func TestKeyInfoParsing(t *testing.T) {
	cases := map[string]struct {
		body string
		want KeyInfo
	}{
		"expiry only":   {`{"expires":"2030-01-02T03:04:05Z"}`, KeyInfo{Expires: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}},
		"revoked":       {`{"expires":"2030-01-02T03:04:05Z","revoked":"2025-01-01T00:00:00Z"}`, KeyInfo{Expires: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC), Revoked: true}},
		"invalid":       {`{"invalid":true}`, KeyInfo{Invalid: true}},
		"empty object":  {`{}`, KeyInfo{}},
		"zero revoked":  {`{"revoked":"0001-01-01T00:00:00Z"}`, KeyInfo{}},
		"invalid false": {`{"invalid":false,"expires":"2030-01-02T03:04:05Z"}`, KeyInfo{Expires: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var uri, auth string
			withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				uri, auth = r.RequestURI, r.Header.Get("Authorization")
				_, _ = w.Write([]byte(tc.body))
			})
			got, err := NewClient("KK").KeyInfo("t.com", "kID")
			if err != nil {
				t.Fatal(err)
			}
			if !got.Expires.Equal(tc.want.Expires) || got.Revoked != tc.want.Revoked || got.Invalid != tc.want.Invalid {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			if uri != "/tailnet/t.com/keys/kID" || auth != "Bearer KK" {
				t.Fatalf("uri=%q auth=%q", uri, auth)
			}
		})
	}
	t.Run("tailnet and key id are path-escaped", func(t *testing.T) {
		var uri string
		withTestServer(t, func(w http.ResponseWriter, r *http.Request) { uri = r.RequestURI; _, _ = w.Write([]byte(`{}`)) })
		if _, err := NewClient("k").KeyInfo("a/b?c", "../x#y"); err != nil {
			t.Fatal(err)
		}
		if uri != "/tailnet/a%2Fb%3Fc/keys/..%2Fx%23y" {
			t.Fatalf("uri = %q", uri)
		}
	})
}

// Verifies: REQ-API-014
func TestKeyInfoErrors(t *testing.T) {
	t.Run("non-200 maps through apiError", func(t *testing.T) {
		for _, status := range []int{201, 299, 400, 401, 403, 404, 500} {
			withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"message":"no access"}`))
			})
			_, err := NewClient("k").KeyInfo("t.com", "id")
			if err == nil || err.Error() != "API error: no access" {
				t.Fatalf("status %d: err = %v", status, err)
			}
		}
	})
	t.Run("body read failure", func(t *testing.T) {
		withTestServer(t, func(w http.ResponseWriter, r *http.Request) { truncatedBody(w, 200) })
		if _, err := NewClient("k").KeyInfo("t.com", "id"); err == nil {
			t.Fatal("expected error")
		}
	})
	for name, body := range map[string]string{
		"not json":    "nope",
		"bad time":    `{"expires":"tomorrow"}`,
		"bad invalid": `{"invalid":"yes"}`,
		"bad revoked": `{"revoked":"never"}`,
		"empty body":  "",
	} {
		t.Run("parse error: "+name, func(t *testing.T) {
			withTestServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
			got, err := NewClient("k").KeyInfo("t.com", "id")
			if err == nil || !strings.Contains(err.Error(), "failed to parse response") || got != (KeyInfo{}) {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
	t.Run("network error", func(t *testing.T) {
		setBaseURL(t, deadServerURL(t))
		if _, err := NewClient("k").KeyInfo("t.com", "id"); err == nil {
			t.Fatal("expected error")
		}
	})
}

// ─── CheckAccount ────────────────────────────────────────────────────────────

// checkServer answers /devices with devStatus and /keys/... with the given
// handler; keyBody == "" means 404.
func checkServer(t *testing.T, devStatus int, devMsg string, keyStatus int, keyBody string) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/devices") {
			w.WriteHeader(devStatus)
			if devStatus == 200 {
				_, _ = w.Write([]byte(`{"devices":[]}`))
			} else if devMsg != "" {
				_, _ = w.Write([]byte(devMsg))
			}
			return
		}
		w.WriteHeader(keyStatus)
		_, _ = w.Write([]byte(keyBody))
	})
}

// Verifies: REQ-API-015
func TestCheckAccountValidWithExpiry(t *testing.T) {
	exp := time.Date(2031, 5, 6, 7, 8, 9, 0, time.UTC)
	checkServer(t, 200, "", 200, `{"expires":"`+exp.Format(time.RFC3339)+`"}`)
	h := CheckAccount(AccountInfo{APIKey: "tskey-api-kID-secret", Tailnet: "t.com"})
	if !h.Valid || h.Err != nil || h.Unauth || !h.HasExpiry || !h.Expires.Equal(exp) || h.ExpiryNote != "" {
		t.Fatalf("%+v", h)
	}
}

// Verifies: REQ-API-016
func TestCheckAccountValidWithoutExpiry(t *testing.T) {
	t.Run("key never expires", func(t *testing.T) {
		checkServer(t, 200, "", 200, `{}`)
		h := CheckAccount(AccountInfo{APIKey: "tskey-api-kID-secret", Tailnet: "t.com"})
		if !h.Valid || h.HasExpiry || !h.Expires.IsZero() || h.Err != nil || h.Unauth {
			t.Fatalf("%+v", h)
		}
	})
	t.Run("unknown key format", func(t *testing.T) {
		checkServer(t, 200, "", 200, `{"expires":"2030-01-01T00:00:00Z"}`)
		for _, key := range []string{"tskey-auth-x-y", "", "tskey-api-nodash"} {
			h := CheckAccount(AccountInfo{APIKey: key, Tailnet: "t.com"})
			if !h.Valid || h.HasExpiry || h.ExpiryNote != "unknown key format" || h.Err != nil {
				t.Fatalf("key %q: %+v", key, h)
			}
		}
	})
	t.Run("key record not readable", func(t *testing.T) {
		for _, st := range []int{401, 403, 404, 500} {
			checkServer(t, 200, "", st, `{"message":"x"}`)
			h := CheckAccount(AccountInfo{APIKey: "tskey-api-kID-secret", Tailnet: "t.com"})
			if !h.Valid || h.HasExpiry || h.ExpiryNote != "expiry not readable" || h.Err != nil || h.Unauth {
				t.Fatalf("status %d: %+v", st, h)
			}
		}
		checkServer(t, 200, "", 200, `garbage`)
		h := CheckAccount(AccountInfo{APIKey: "tskey-api-kID-secret", Tailnet: "t.com"})
		if !h.Valid || h.ExpiryNote != "expiry not readable" {
			t.Fatalf("garbage record: %+v", h)
		}
	})
}

// Verifies: REQ-API-017
func TestCheckAccountRejectedKey(t *testing.T) {
	for _, st := range []int{401, 403} {
		t.Run(fmt.Sprint(st), func(t *testing.T) {
			keyCalled := false
			withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/devices") {
					keyCalled = true
				}
				w.WriteHeader(st)
			})
			h := CheckAccount(AccountInfo{APIKey: "tskey-api-kID-secret", Tailnet: "t.com"})
			if h.Valid || !h.Unauth || h.Err == nil || h.HasExpiry || h.ExpiryNote != "" {
				t.Fatalf("%+v", h)
			}
			if keyCalled {
				t.Fatal("the key record must not be queried for a rejected key")
			}
		})
	}
}

// Verifies: REQ-API-018
func TestCheckAccountOtherFailuresAreNotAuthRejections(t *testing.T) {
	t.Run("server error", func(t *testing.T) {
		checkServer(t, 500, `{"message":"boom"}`, 200, `{}`)
		h := CheckAccount(AccountInfo{APIKey: "tskey-api-kID-secret", Tailnet: "t.com"})
		if h.Valid || h.Unauth || h.Err == nil || !strings.Contains(h.Err.Error(), "boom") {
			t.Fatalf("%+v", h)
		}
	})
	t.Run("server text that mimics the rejection message is not trusted", func(t *testing.T) {
		checkServer(t, 400, `{"message":"invalid API key or insufficient permissions"}`, 200, `{}`)
		h := CheckAccount(AccountInfo{APIKey: "tskey-api-kID-secret", Tailnet: "t.com"})
		if h.Valid || h.Unauth || h.Err == nil {
			t.Fatalf("%+v", h)
		}
	})
	t.Run("network error", func(t *testing.T) {
		setBaseURL(t, deadServerURL(t))
		h := CheckAccount(AccountInfo{APIKey: "tskey-api-kID-secret", Tailnet: "t.com"})
		if h.Valid || h.Unauth || h.Err == nil || !strings.Contains(h.Err.Error(), "failed to execute request") {
			t.Fatalf("%+v", h)
		}
	})
}

// Verifies: REQ-API-019
func TestCheckAccountRevokedOrInvalidKey(t *testing.T) {
	future := `"expires":"2031-01-01T00:00:00Z"`
	cases := map[string]string{
		"revoked only":        `{` + future + `,"revoked":"2025-01-01T00:00:00Z"}`,
		"invalid only":        `{` + future + `,"invalid":true}`,
		"revoked and invalid": `{` + future + `,"revoked":"2025-01-01T00:00:00Z","invalid":true}`,
		"revoked, no expiry":  `{"revoked":"2025-01-01T00:00:00Z"}`,
		"invalid, no expiry":  `{"invalid":true}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			checkServer(t, 200, "", 200, body)
			h := CheckAccount(AccountInfo{APIKey: "tskey-api-kID-secret", Tailnet: "t.com"})
			if h.Valid || !h.Unauth || h.Err == nil || h.Err.Error() != "API key is revoked or expired" || h.HasExpiry || !h.Expires.IsZero() {
				t.Fatalf("%+v", h)
			}
		})
	}
	t.Run("neither revoked nor invalid stays valid", func(t *testing.T) {
		checkServer(t, 200, "", 200, `{`+future+`,"invalid":false}`)
		h := CheckAccount(AccountInfo{APIKey: "tskey-api-kID-secret", Tailnet: "t.com"})
		if !h.Valid || h.Unauth || !h.HasExpiry {
			t.Fatalf("%+v", h)
		}
	})
}
