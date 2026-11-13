package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
)

func withLatestRelease(t *testing.T, fn func() (client.Release, error)) {
	t.Helper()
	old := latestRelease
	latestRelease = fn
	t.Cleanup(func() { latestRelease = old })
}

func TestUpdateCheckAsksOnceADayAndCaches(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TS_CLI_CONFIG_DIR", dir)
	calls := 0
	withLatestRelease(t, func() (client.Release, error) { calls++; return client.Release{Tag: "v9.0.0"}, nil })

	if msg := updateCheckCmd()().(updateMsg); msg.tag != "v9.0.0" || calls != 1 {
		t.Fatalf("first: %+v calls=%d", msg, calls)
	}
	st, err := os.Stat(filepath.Join(dir, "update_check.json"))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("cache: %v %v", st, err)
	}
	if msg := updateCheckCmd()().(updateMsg); msg.tag != "v9.0.0" || calls != 1 {
		t.Fatalf("a fresh cache is used: %+v calls=%d", msg, calls)
	}
	// a stale cache asks again
	stale, _ := json.Marshal(updateCache{CheckedAt: time.Now().Add(-2 * updateCheckEvery), Tag: "v1.0.0"})
	os.WriteFile(filepath.Join(dir, "update_check.json"), stale, 0o600)
	if msg := updateCheckCmd()().(updateMsg); msg.tag != "v9.0.0" || calls != 2 {
		t.Fatalf("stale: %+v calls=%d", msg, calls)
	}
	// garbage in the cache is ignored
	os.WriteFile(filepath.Join(dir, "update_check.json"), []byte("{broken"), 0o600)
	if msg := updateCheckCmd()().(updateMsg); msg.tag != "v9.0.0" || calls != 3 {
		t.Fatalf("garbage: %+v calls=%d", msg, calls)
	}
}

func TestUpdateCheckFailuresAreSilent(t *testing.T) {
	t.Setenv("TS_CLI_CONFIG_DIR", t.TempDir())
	withLatestRelease(t, func() (client.Release, error) { return client.Release{}, errors.New("offline") })
	if msg := updateCheckCmd()().(updateMsg); msg.tag != "" {
		t.Fatalf("%+v", msg)
	}
	// a config directory that cannot be made: the answer still comes, uncached
	file := filepath.Join(t.TempDir(), "afile")
	os.WriteFile(file, []byte("x"), 0o600)
	t.Setenv("TS_CLI_CONFIG_DIR", filepath.Join(file, "sub"))
	withLatestRelease(t, func() (client.Release, error) { return client.Release{Tag: "v2.0.0"}, nil })
	if msg := updateCheckCmd()().(updateMsg); msg.tag != "v2.0.0" {
		t.Fatalf("%+v", msg)
	}
	// no home and no config dir: nothing happens
	t.Setenv("TS_CLI_CONFIG_DIR", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if msg := updateCheckCmd()().(updateMsg); msg.tag != "" {
		t.Fatalf("no config dir: %+v", msg)
	}
}

func TestUpdateNoticeOnlyForNewerVersions(t *testing.T) {
	m := model{version: "1.3.0"}
	u, _ := m.handleUpdate(updateMsg{tag: "v1.4.0"})
	if got := u.(model).notify.info; !strings.Contains(got, "v1.4.0 is available") || !strings.Contains(got, "ts-cli upgrade") {
		t.Fatalf("info = %q", got)
	}
	for _, tag := range []string{"v1.3.0", "v1.2.0", ""} {
		u, _ := m.handleUpdate(updateMsg{tag: tag})
		if u.(model).notify.info != "" {
			t.Fatalf("%q must not show a notice", tag)
		}
	}
}

func TestUpdateCheckSetting(t *testing.T) {
	if !loadUISettings(nil).updateCheck || loadUISettings(map[string]string{uiUpdateCheck: "off"}).updateCheck {
		t.Fatal("on by default, off when stored off")
	}
	s := loadUISettings(nil).toggled(uiUpdateCheck)
	if s.updateCheck || s.value(uiUpdateCheck) != "off" {
		t.Fatal("toggle")
	}
	if (model{set: uiSettings{updateCheck: true}}).Init() == nil {
		t.Fatal("Init starts the check")
	}
}
