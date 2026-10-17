package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestHistory(t *testing.T) (*HistoryStore, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // windows
	h, err := NewHistoryStore()
	if err != nil {
		t.Fatalf("NewHistoryStore: %v", err)
	}
	return h, home
}

func TestHistoryAddSaveLoad(t *testing.T) {
	h, home := newTestHistory(t)

	h.AddCommand("dev1", "laptop", "ls -la", 0, "out")
	h.AddCommand("dev1", "laptop", "uptime", 0, "out")
	h.AddCommand("dev2", "server", "df -h", 1, "out")
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(home, ".ts-cli", "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("history perm = %o, want 0600", perm)
	}

	h2, err := NewHistoryStore()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(h2.GetCommandsForMachine("dev1")); got != 2 {
		t.Fatalf("dev1 commands = %d, want 2", got)
	}
	if got := len(h2.GetAllCommands()); got != 3 {
		t.Fatalf("all commands = %d, want 3", got)
	}
}

func TestHistoryUniqueMostRecentFirst(t *testing.T) {
	h, _ := newTestHistory(t)
	h.AddCommand("dev1", "n", "a", 0, "")
	h.AddCommand("dev1", "n", "b", 0, "")
	h.AddCommand("dev1", "n", "a", 0, "")

	got := h.GetUniqueCommands("dev1")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("GetUniqueCommands = %v, want [a b]", got)
	}
}

func TestHistoryOutputTruncationAndCap(t *testing.T) {
	h, _ := newTestHistory(t)
	h.AddCommand("dev1", "n", "cat big", 0, strings.Repeat("x", 5000))
	if out := h.GetAllCommands()[0].Output; len(out) > 1024+len("... (truncated)") || !strings.HasSuffix(out, "(truncated)") {
		t.Fatalf("output not truncated: len=%d", len(out))
	}

	for i := 0; i < 1100; i++ {
		h.AddCommand("dev1", "n", "cmd", 0, "")
	}
	if got := len(h.GetAllCommands()); got != 1000 {
		t.Fatalf("history cap = %d, want 1000", got)
	}
}

func TestHistoryDeleteCommandForMachine(t *testing.T) {
	h, _ := newTestHistory(t)
	h.AddCommand("dev1", "n", "rm", 0, "")
	h.AddCommand("dev1", "n", "rm", 0, "")
	h.AddCommand("dev2", "n", "rm", 0, "")

	removed, err := h.DeleteCommandForMachine("dev1", "rm")
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	if len(h.GetCommandsForMachine("dev2")) != 1 {
		t.Fatal("dev2 history should be untouched")
	}
	if removed, _ := h.DeleteCommandForMachine("", "rm"); removed != 0 {
		t.Fatal("empty machineID must be a no-op")
	}
}
