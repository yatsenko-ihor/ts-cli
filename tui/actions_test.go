package tui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
)

// Requirements-based tests for the Machine tab (verification/requirements/actions.md).

type apiCall struct{ method, path, body string }

type fakeAPI struct {
	mu    sync.Mutex
	calls []apiCall
	code  int
	reply string
}

func (f *fakeAPI) last() (apiCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return apiCall{}, false
	}
	return f.calls[len(f.calls)-1], true
}

func startFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{code: 200, reply: `{}`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, apiCall{r.Method, r.URL.Path, string(b)})
		code, reply := f.code, f.reply
		f.mu.Unlock()
		w.WriteHeader(code)
		_, _ = w.Write([]byte(reply))
	}))
	restore := client.SetBaseURL(srv.URL)
	t.Cleanup(func() { restore(); srv.Close() })
	return f
}

func actionsFixtureModel(t *testing.T) model {
	t.Helper()
	dev := client.Device{
		ID: "n42", Name: "box.tail1234.ts.net", Hostname: "box", Addresses: []string{"100.64.0.5"},
		AccountName: "work", AccountTailnet: "work.com", Tags: []string{"tag:old"}, LastSeen: time.Now(),
	}
	m := model{
		set:  loadUISettings(nil),
		acct: accounts{list: []client.AccountInfo{{Name: "work", Tailnet: "work.com", APIKey: "tskey-api-kAAA-0123456789abcdef"}}},
		list: deviceList{devices: []client.Device{dev}},
	}
	m.filterDevices()
	m.run.visible = true
	m.run.tab = tabMachine
	return m
}

// pick sets the cursor to the action with this title and runs it.
func pick(t *testing.T, m model, title string) (model, tea.Cmd) {
	t.Helper()
	d, _ := m.targetDevice()
	for i, a := range machineActions(d) {
		if a.title == title {
			m.run.cursors[tabMachine] = i
			updated, cmd := m.runMachineAction()
			return updated.(model), cmd
		}
	}
	t.Fatalf("no action %q", title)
	return m, nil
}

// deliver runs cmd and feeds its message back through Update.
func deliver(t *testing.T, m model, cmd tea.Cmd) (model, tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	updated, next := m.Update(cmd())
	return updated.(model), next
}

// Verifies: REQ-ACT-001
func TestMachineActionsListDependsOnKeyExpiryState(t *testing.T) {
	titles := func(d client.Device) []string {
		var out []string
		for _, a := range machineActions(d) {
			out = append(out, a.title)
		}
		return out
	}
	on := strings.Join(titles(client.Device{}), "|")
	off := strings.Join(titles(client.Device{KeyExpiryDisabled: true}), "|")
	if !strings.Contains(on, "Disable key expiry") || strings.Contains(on, "Enable key expiry") {
		t.Fatalf("expiring key offers disabling: %s", on)
	}
	if !strings.Contains(off, "Enable key expiry") || strings.Contains(off, "Disable key expiry") {
		t.Fatalf("non-expiring key offers enabling: %s", off)
	}
	for _, want := range []string{"Ping with details", "Expire key now", "Rename…", "Edit tags…", "Delete from tailnet…"} {
		if !strings.Contains(on, want) {
			t.Errorf("missing action %q", want)
		}
	}
	d := client.Device{}
	for _, a := range machineActions(d) {
		if (a.title == "Expire key now" || strings.HasPrefix(a.title, "Delete")) != a.danger {
			t.Errorf("%q: danger flag = %v", a.title, a.danger)
		}
	}
}

// Verifies: REQ-ACT-002
func TestKeyExpiryActionsCallTheAPIAndReload(t *testing.T) {
	api := startFakeAPI(t)
	m := actionsFixtureModel(t)

	m, cmd := pick(t, m, "Disable key expiry")
	if !m.act.busy {
		t.Fatal("an API call in flight is shown as busy")
	}
	m, next := deliver(t, m, cmd)
	c, _ := api.last()
	if c.method != "POST" || c.path != "/device/n42/key" || c.body != `{"keyExpiryDisabled":true}` {
		t.Fatalf("call = %+v", c)
	}
	if m.act.busy || !strings.HasPrefix(m.act.result, "✓ Key expiry disabled") || !m.reloading || next == nil {
		t.Fatalf("success must be shown and the list reloaded: %+v reloading=%v", m.act, m.reloading)
	}

	// the other direction
	m.reloading = false
	m.list.devices[0].KeyExpiryDisabled = true
	m.filterDevices()
	m, cmd = pick(t, m, "Enable key expiry")
	m, _ = deliver(t, m, cmd)
	if c, _ := api.last(); c.body != `{"keyExpiryDisabled":false}` {
		t.Fatalf("call = %+v", c)
	}
}

// Verifies: REQ-ACT-003
func TestDestructiveActionsNeedTheMachineNameTyped(t *testing.T) {
	for _, tc := range []struct{ title, method, path string }{
		{"Expire key now", "POST", "/device/n42/expire"},
		{"Delete from tailnet…", "DELETE", "/device/n42"},
	} {
		t.Run(tc.title, func(t *testing.T) {
			api := startFakeAPI(t)
			m := actionsFixtureModel(t)
			m, _ = pick(t, m, tc.title)
			if m.input.mode != inputText || !strings.Contains(m.input.hint, "box") {
				t.Fatalf("a confirmation naming the machine is expected: %+v", m.input)
			}

			// wrong text: nothing is sent
			updated, cmd := m.input.onSubmit(m, "nope")
			m2 := updated.(model)
			if cmd != nil || m2.act.err == nil || m2.input.mode != inputNone {
				t.Fatalf("wrong confirmation: cmd=%v act=%+v", cmd != nil, m2.act)
			}
			if _, called := api.last(); called {
				t.Fatal("the API must not be called without confirmation")
			}

			// right text
			updated, cmd = m.input.onSubmit(m, " box ")
			m3, _ := deliver(t, updated.(model), cmd)
			if c, _ := api.last(); c.method != tc.method || c.path != tc.path {
				t.Fatalf("call = %+v", c)
			}
			if !strings.HasPrefix(m3.act.result, "✓") {
				t.Fatalf("result = %+v", m3.act)
			}
		})
	}
}

// Verifies: REQ-ACT-004
func TestRenameAndTagPrompts(t *testing.T) {
	api := startFakeAPI(t)
	m := actionsFixtureModel(t)

	m, _ = pick(t, m, "Rename…")
	if m.input.value != "box" || m.input.cursor != 3 {
		t.Fatalf("prompt must start with the current short name: %+v", m.input)
	}
	// unchanged or empty: no call
	for _, v := range []string{"box", "  "} {
		updated, cmd := m.input.onSubmit(m, v)
		if cmd != nil || updated.(model).input.mode != inputNone {
			t.Fatalf("%q must close the prompt without a call", v)
		}
	}
	updated, cmd := m.input.onSubmit(m, "laptop")
	deliver(t, updated.(model), cmd)
	if c, _ := api.last(); c.path != "/device/n42/name" || c.body != `{"name":"laptop"}` {
		t.Fatalf("call = %+v", c)
	}

	m, _ = pick(t, m, "Edit tags…")
	if m.input.value != "tag:old" {
		t.Fatalf("prompt must start with the current tags: %q", m.input.value)
	}
	updated, cmd = m.input.onSubmit(m, "prod, tag:db")
	deliver(t, updated.(model), cmd)
	if c, _ := api.last(); c.path != "/device/n42/tags" || c.body != `{"tags":["tag:prod","tag:db"]}` {
		t.Fatalf("call = %+v", c)
	}
	updated, cmd = m.input.onSubmit(m, " , ")
	if cmd != nil || updated.(model).act.err == nil {
		t.Fatal("no tags is an error, not a call")
	}
}

// Verifies: REQ-ACT-005
func TestAPIFailuresAndMissingAccountAreShown(t *testing.T) {
	api := startFakeAPI(t)
	api.code, api.reply = 403, `{"message":"requires devices:core scope"}`
	m := actionsFixtureModel(t)
	m, cmd := pick(t, m, "Disable key expiry")
	m, next := deliver(t, m, cmd)
	if m.act.err == nil || !strings.Contains(m.act.err.Error(), "devices:core") || m.act.busy || next != nil || m.reloading {
		t.Fatalf("a failure is shown as is and nothing reloads: %+v", m.act)
	}

	m = actionsFixtureModel(t)
	m.acct.list = nil
	m, cmd = pick(t, m, "Disable key expiry")
	if cmd != nil || m.act.err == nil || !strings.Contains(m.act.err.Error(), "no account") {
		t.Fatalf("a device of an unknown account cannot be changed: %+v", m.act)
	}

	// while a reload is already running, a success does not start another
	m = actionsFixtureModel(t)
	m.reloading = true
	updated, cmd := m.handleDeviceAction(deviceActionMsg{done: "x"})
	if cmd != nil || !updated.(model).reloading {
		t.Fatal("no second reload")
	}
}

// Verifies: REQ-ACT-006
func TestPingWithDetailsStoresPathAndReachability(t *testing.T) {
	m := actionsFixtureModel(t)
	stubPingN(t, func(string, int) ([]byte, error) {
		return []byte("pong from box (100.64.0.5) via 192.168.1.20:41641 in 3ms\n"), nil
	})
	m, cmd := pick(t, m, "Ping with details")
	if !m.act.busy {
		t.Fatal("busy while pinging")
	}
	m, _ = deliver(t, m, cmd)
	if got := m.pingText(m.list.devices[0]); !strings.HasPrefix(got, "direct 192.168.1.20:41641") {
		t.Fatalf("details must show the path: %q", got)
	}
	if m.ping["n42"] != pingOK || m.act.busy || !strings.Contains(m.act.result, "direct") {
		t.Fatalf("state = %v, act = %+v", m.ping["n42"], m.act)
	}

	// no reply: unreachable; CLI missing: says nothing about the machine
	stubPingN(t, func(string, int) ([]byte, error) { return []byte("no reply"), errTest("exit 1") })
	m, cmd = pick(t, m, "Ping with details")
	m, _ = deliver(t, m, cmd)
	if m.ping["n42"] != pingFail {
		t.Fatal("no reply marks the machine unreachable")
	}
	stubPingN(t, func(string, int) ([]byte, error) { return nil, &execNotFound })
	m.ping = map[string]pingState{}
	m, cmd = pick(t, m, "Ping with details")
	m, _ = deliver(t, m, cmd)
	if _, set := m.ping["n42"]; set {
		t.Fatal("a missing tailscale CLI must not mark the machine unreachable")
	}
}

// Verifies: REQ-ACT-007
func TestMachineTabIsInertWithoutTargetOrWhileBusy(t *testing.T) {
	m := actionsFixtureModel(t)
	m.act.busy = true
	if _, cmd := m.runMachineAction(); cmd != nil {
		t.Fatal("no second action while one is running")
	}
	m.act.busy = false
	m.list = deviceList{}
	if _, cmd := m.runMachineAction(); cmd != nil {
		t.Fatal("no machine, no action")
	}
	if m.tabLength() != 0 {
		t.Fatal("no machine, no rows")
	}
	m = actionsFixtureModel(t)
	m.run.cursors[tabMachine] = 99
	if _, _, ok := m.selectedAction(); ok {
		t.Fatal("cursor out of range")
	}
}

// Verifies: REQ-ACT-008
func TestMachineTabRendersAndEnterRunsTheAction(t *testing.T) {
	m := actionsFixtureModel(t)
	m.width, m.height = 140, 40
	m.activeFocus = focusRun
	out := m.renderRunPanel()
	for _, want := range []string{"Machine", "Ping with details", "Delete from tailnet", "only"} {
		if !strings.Contains(out, want) {
			t.Errorf("panel lacks %q", want)
		}
	}
	m.act = actionState{result: "✓ done"}
	if !strings.Contains(m.renderRunPanel(), "✓ done") {
		t.Error("the result line must be shown")
	}
	m.act = actionState{err: errTest("boom")}
	if !strings.Contains(m.renderRunPanel(), "boom") {
		t.Error("the error must be shown")
	}

	// Enter on the tab runs the action rather than a script
	stubPingN(t, func(string, int) ([]byte, error) { return []byte("no reply"), errTest("x") })
	m.act = actionState{}
	m.run.cursors[tabMachine] = 0
	updated, cmd := m.handleRunPanel(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !updated.(model).act.busy {
		t.Fatal("enter on the Machine tab must start the highlighted action")
	}
	// tab navigation reaches it
	m.run.tab = tabRecent
	updated, _ = m.handleRunPanel(key("]"))
	if updated.(model).run.tab != tabMachine {
		t.Fatal("] from Recent must reach Machine")
	}
}

// Verifies: REQ-ACT-009
func TestPeersAreLoadedOnStartAndReload(t *testing.T) {
	old := tailscaleAvailable
	tailscaleAvailable = func() bool { return true }
	t.Cleanup(func() { tailscaleAvailable = old })
	oldStatus := tailscaleStatusJSON
	tailscaleStatusJSON = func() ([]byte, error) {
		return []byte(`{"Peer":{"k":{"TailscaleIPs":["100.64.0.5"],"Online":true,"Relay":"fra","SSHHostKeys":["x"]}}}`), nil
	}
	t.Cleanup(func() { tailscaleStatusJSON = oldStatus })

	m := actionsFixtureModel(t)
	if m.Init() == nil {
		t.Fatal("start must load the peer status")
	}
	updated, _ := m.Update(peersMsg{peers: map[string]peerInfo{"100.64.0.5": {relay: "fra", ssh: true, online: true}}})
	m = updated.(model)
	m.run.visible = false
	m.width, m.height = 140, 40
	panel := m.renderDetailsPanel(60, 30)
	for _, want := range []string{"Path", "relay fra", "Tailscale SSH"} {
		if !strings.Contains(panel, want) {
			t.Errorf("details lack %q:\n%s", want, panel)
		}
	}
	// an error keeps the previous data
	updated, _ = m.Update(peersMsg{err: errTest("down")})
	if len(updated.(model).peers) != 1 {
		t.Fatal("a failed refresh must not wipe the known peers")
	}
	// reload brings a refresh
	updated, cmd := m.Update(reloadMsg{devices: m.list.devices})
	if cmd == nil || updated.(model).pingInfo != nil {
		t.Fatal("reload refreshes peers and forgets old ping details")
	}
}

var execNotFound = *newNotFound()

func newNotFound() *exec.Error { return &exec.Error{Name: "tailscale", Err: exec.ErrNotFound} }
