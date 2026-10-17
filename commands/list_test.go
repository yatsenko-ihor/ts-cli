package commands

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
)

func TestWriteDevicesJSONIsValidJSON(t *testing.T) {
	devices := []client.Device{
		{
			ID:          "1",
			Name:        `weird "name" with \ backslash`,
			Hostname:    "host\twith\ttabs",
			OS:          "linux",
			Addresses:   []string{"100.64.0.1", "fd7a::1"},
			Authorized:  true,
			LastSeen:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			AccountName: "work",
		},
		{ID: "2", Hostname: "no-addresses"},
	}

	var buf bytes.Buffer
	if err := writeDevicesJSON(&buf, devices); err != nil {
		t.Fatal(err)
	}

	var decoded []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if len(decoded) != 2 {
		t.Fatalf("decoded %d devices, want 2", len(decoded))
	}
	if decoded[0]["name"] != `weird "name" with \ backslash` {
		t.Fatalf("name not round-tripped: %v", decoded[0]["name"])
	}
	addrs, ok := decoded[0]["addresses"].([]any)
	if !ok || len(addrs) != 2 {
		t.Fatalf("addresses should be a JSON array, got %v", decoded[0]["addresses"])
	}
	if decoded[0]["lastSeen"] != "2026-01-02T03:04:05Z" {
		t.Fatalf("lastSeen = %v", decoded[0]["lastSeen"])
	}
	if _, ok := decoded[1]["addresses"].([]any); !ok {
		t.Fatalf("nil addresses should encode as [], got %v", decoded[1]["addresses"])
	}
}
