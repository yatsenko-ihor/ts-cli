package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDevicesPathEscapesTailnet(t *testing.T) {
	cases := map[string]string{
		"example.com":          "/tailnet/example.com/devices",
		"user@example.com":     "/tailnet/user@example.com/devices",
		"evil.com/../../admin": "/tailnet/evil.com%2F..%2F..%2Fadmin/devices",
		"x.com?foo=bar":        "/tailnet/x.com%3Ffoo=bar/devices",
	}
	for in, want := range cases {
		if got := devicesPath(in); got != want {
			t.Errorf("devicesPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func withTestServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	old := baseURL
	baseURL = srv.URL
	t.Cleanup(func() {
		baseURL = old
		srv.Close()
	})
	return srv
}

func TestListDevicesSendsBearerAndParses(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tskey-api-test" {
			t.Errorf("Authorization = %q", got)
		}
		if r.URL.Path != "/tailnet/example.com/devices" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"devices":[{"id":"1","name":"a.example.com","hostname":"a","addresses":["100.64.0.1"]}]}`))
	})

	devices, err := NewClient("tskey-api-test").ListDevices("example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].Hostname != "a" || devices[0].Addresses[0] != "100.64.0.1" {
		t.Fatalf("unexpected devices: %+v", devices)
	}
}

func TestListDevicesSurfacesAPIError(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"tailnet not found"}`))
	})

	_, err := NewClient("k").ListDevices("nope.com")
	if err == nil || !strings.Contains(err.Error(), "tailnet not found") {
		t.Fatalf("expected API error message, got %v", err)
	}
}

func TestValidateAPIKeyUnauthorized(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	if err := NewClient("bad").ValidateAPIKey("example.com"); err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestListDevicesFromAccountsWithErrors(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tailnet/bad.com/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"devices":[{"id":"1","hostname":"ok"}]}`))
	})

	devices, errs := ListDevicesFromAccountsWithErrors([]AccountInfo{
		{Name: "good", APIKey: "k", Tailnet: "good.com"},
		{Name: "bad", APIKey: "k", Tailnet: "bad.com"},
	})
	if len(devices) != 1 || devices[0].AccountName != "good" {
		t.Fatalf("devices = %+v", devices)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "bad") {
		t.Fatalf("errs = %v", errs)
	}
}

func TestDeviceParsesConnectedToControlAndOmittedLastSeen(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// While connectedToControl is true the API omits lastSeen.
		_, _ = w.Write([]byte(`{"devices":[{"id":"1","hostname":"live","connectedToControl":true},{"id":"2","hostname":"gone","lastSeen":"2026-01-02T03:04:05Z","connectedToControl":false}]}`))
	})
	devices, err := NewClient("k").ListDevices("example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !devices[0].ConnectedToControl || !devices[0].LastSeen.IsZero() {
		t.Fatalf("live device: %+v", devices[0])
	}
	if devices[1].ConnectedToControl || devices[1].LastSeen.IsZero() {
		t.Fatalf("gone device: %+v", devices[1])
	}
}

func TestAPIKeyID(t *testing.T) {
	for in, want := range map[string]string{
		"tskey-api-kABC123CNTRL-secretpart": "kABC123CNTRL",
		"tskey-auth-kABC-secret":            "",
		"tskey-api-nodash":                  "",
		"":                                  "",
	} {
		if got := APIKeyID(in); got != want {
			t.Errorf("APIKeyID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckAccount(t *testing.T) {
	expires := time.Now().Add(10 * 24 * time.Hour).UTC().Truncate(time.Second)
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/devices"):
			_, _ = w.Write([]byte(`{"devices":[]}`))
		case strings.HasSuffix(r.URL.Path, "/keys/kID"):
			_, _ = w.Write([]byte(`{"expires":"` + expires.Format(time.RFC3339) + `"}`))
		default:
			http.NotFound(w, r)
		}
	})
	h := CheckAccount(AccountInfo{APIKey: "tskey-api-kID-secret", Tailnet: "example.com"})
	if !h.Valid || !h.HasExpiry || !h.Expires.Equal(expires) {
		t.Fatalf("health = %+v", h)
	}

	// A token that cannot read its own record is valid with unknown expiry.
	h = CheckAccount(AccountInfo{APIKey: "tskey-api-other-secret", Tailnet: "example.com"})
	if !h.Valid || h.HasExpiry || h.ExpiryNote == "" {
		t.Fatalf("unreadable expiry must not be an error: %+v", h)
	}
}

func TestCheckAccountRejectedAndRevoked(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"nope"}`, http.StatusUnauthorized)
	})
	if h := CheckAccount(AccountInfo{APIKey: "tskey-api-kID-secret", Tailnet: "x.com"}); h.Valid || !h.Unauth {
		t.Fatalf("401 must be rejected: %+v", h)
	}

	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/devices") {
			_, _ = w.Write([]byte(`{"devices":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"expires":"2030-01-01T00:00:00Z","revoked":"2025-01-01T00:00:00Z"}`))
	})
	if h := CheckAccount(AccountInfo{APIKey: "tskey-api-kID-secret", Tailnet: "x.com"}); h.Valid || !h.Unauth {
		t.Fatalf("revoked key must be rejected: %+v", h)
	}
}
