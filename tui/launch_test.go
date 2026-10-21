package tui

import (
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
)

// A failed ping blocks a launch only while it is fresh; a stale failure is
// left to the pre-flight ping, so a cold path does not fail every attempt.
func TestLaunchBlockedStaleFailure(t *testing.T) {
	orig := tailscaleAvailable
	tailscaleAvailable = func() bool { return true }
	defer func() { tailscaleAvailable = orig }()

	d := client.Device{Name: "box", Hostname: "box", Addresses: []string{"100.64.0.1"}, ConnectedToControl: true}
	m := model{ping: map[string]pingState{}, pingAt: map[string]time.Time{}}
	key := machineKey(d)

	if m.launchBlocked(d) {
		t.Fatal("online machine without ping result must not be blocked")
	}
	m.ping[key], m.pingAt[key] = pingFail, time.Now()
	if !m.launchBlocked(d) {
		t.Fatal("fresh failed ping must block")
	}
	m.pingAt[key] = time.Now().Add(-2 * pingFreshFor)
	if m.launchBlocked(d) {
		t.Fatal("stale failed ping must not block; pre-flight re-checks")
	}
	tailscaleAvailable = func() bool { return false }
	if !m.launchBlocked(d) {
		t.Fatal("without the tailscale CLI a failed ping stays authoritative")
	}
}
