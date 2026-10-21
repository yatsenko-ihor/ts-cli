package client

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type seen struct {
	method, path, ctype, body string
}

func recordServer(t *testing.T, status int, reply string) *seen {
	t.Helper()
	got := &seen{}
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.method, got.path, got.ctype, got.body = r.Method, r.URL.EscapedPath(), r.Header.Get("Content-Type"), string(b)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	})
	return got
}

// Verifies: REQ-API-020
func TestDeviceActionsSendTheDocumentedRequests(t *testing.T) {
	c := NewClient("tskey-api-test")
	cases := []struct {
		name       string
		call       func() error
		method     string
		path       string
		body       string
		hasContent bool
	}{
		{"disable expiry", func() error { return c.SetKeyExpiryDisabled("n123", true) }, "POST", "/device/n123/key", `{"keyExpiryDisabled":true}`, true},
		{"enable expiry", func() error { return c.SetKeyExpiryDisabled("n123", false) }, "POST", "/device/n123/key", `{"keyExpiryDisabled":false}`, true},
		{"expire now", func() error { return c.ExpireKey("n123") }, "POST", "/device/n123/expire", "", false},
		{"rename", func() error { return c.RenameDevice("n123", " box ") }, "POST", "/device/n123/name", `{"name":"box"}`, true},
		{"tags", func() error { return c.SetTags("n123", []string{"tag:a", "tag:b"}) }, "POST", "/device/n123/tags", `{"tags":["tag:a","tag:b"]}`, true},
		{"delete", func() error { return c.DeleteDevice("n123") }, "DELETE", "/device/n123", "", false},
		{"id is path-escaped", func() error { return c.DeleteDevice("a/b?c") }, "DELETE", "/device/a%2Fb%3Fc", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := recordServer(t, 200, `{}`)
			if err := tc.call(); err != nil {
				t.Fatal(err)
			}
			if got.method != tc.method || got.path != tc.path || got.body != tc.body {
				t.Fatalf("request = %+v", got)
			}
			if (got.ctype == "application/json") != tc.hasContent {
				t.Fatalf("Content-Type = %q, want JSON only when there is a body", got.ctype)
			}
			if tc.body != "" && !json.Valid([]byte(got.body)) {
				t.Fatal("body must be valid JSON")
			}
		})
	}
}

// Verifies: REQ-API-021
func TestDeviceActionsReportAPIFailures(t *testing.T) {
	c := NewClient("tskey-api-test")
	recordServer(t, 403, `{"message":"requires devices:core scope"}`)
	if err := c.ExpireKey("n1"); err == nil || !strings.Contains(err.Error(), "requires devices:core scope") {
		t.Fatalf("API message must reach the caller: %v", err)
	}
	recordServer(t, 500, `oops`)
	if err := c.DeleteDevice("n1"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("status must be reported: %v", err)
	}
	recordServer(t, 204, ``)
	if err := c.DeleteDevice("n1"); err != nil {
		t.Fatalf("2xx without body is success: %v", err)
	}

	old := baseURL
	baseURL = "http://127.0.0.1:1" // nothing listens
	t.Cleanup(func() { baseURL = old })
	if err := c.DeleteDevice("n1"); err == nil || !strings.Contains(err.Error(), "failed to execute request") {
		t.Fatalf("network error must be wrapped: %v", err)
	}
}

// Verifies: REQ-API-022
func TestDeviceActionsValidateInputBeforeSending(t *testing.T) {
	c := NewClient("tskey-api-test")
	got := recordServer(t, 200, `{}`)
	checks := map[string]error{
		"empty id":     c.DeleteDevice(""),
		"empty name":   c.RenameDevice("n1", "   "),
		"no tags":      c.SetTags("n1", nil),
		"tag w/o:":     c.SetTags("n1", []string{"server"}),
		"bare tag:":    c.SetTags("n1", []string{"tag:"}),
		"one bad of 2": c.SetTags("n1", []string{"tag:ok", "bad"}),
		"expire no id": c.ExpireKey(""),
		"expiry no id": c.SetKeyExpiryDisabled("", true),
		"rename no id": c.RenameDevice("", "x"),
		"tags no id":   c.SetTags("", []string{"tag:a"}),
	}
	for name, err := range checks {
		if err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if got.method != "" {
		t.Fatalf("nothing may be sent for invalid input, got %+v", got)
	}
}

// Verifies: REQ-API-023
func TestNormalizeTags(t *testing.T) {
	cases := map[string][]string{
		"server":        {"tag:server"},
		"tag:db, web":   {"tag:db", "tag:web"},
		" a,,b  c\t":    {"tag:a", "tag:b", "tag:c"},
		"tag:x tag:x x": {"tag:x"},
		"":              nil,
		" , ,":          nil,
	}
	for in, want := range cases {
		got := NormalizeTags(in)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("NormalizeTags(%q) = %q, want %q", in, got, want)
		}
	}
}

// A marshalling failure cannot happen with the payloads above; the branch is
// reachable only through an unencodable payload.
// Verifies: REQ-API-021
func TestDeviceActionRejectsUnencodablePayload(t *testing.T) {
	c := NewClient("tskey-api-test")
	if err := c.deviceAction("POST", "n1", "/x", make(chan int)); err == nil || !strings.Contains(err.Error(), "encode") {
		t.Fatalf("err = %v", err)
	}
}
