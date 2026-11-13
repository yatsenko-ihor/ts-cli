package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
)

func TestExtraRowsShrinkThePanelsSoTheTitleStaysVisible(t *testing.T) {
	m := model{width: 400, height: 60} // wide enough for the hint bar to be one line
	base := m.extraRows()
	if base != 0 {
		t.Fatalf("a quiet screen has no extra rows, got %d", base)
	}
	list := m.getMaxVisibleItems()
	_, panel := m.getHistoryPanelSize()

	m.list.devices = []client.Device{{ID: "a", Name: "a", Expires: time.Now().Add(-time.Hour)}}
	m.notify.info = "x"
	m.notify.sshError = errors.New("boom")
	m.notify.reloadSuccess = "ok"
	m.notify.copiedText = "c"
	m.inst.showSuggestion = true
	m.run.label, m.run.runs = "job", []*machineRun{{}}
	if got := m.extraRows(); got != 1+1+2+1+1+2+1 {
		t.Fatalf("extraRows = %d", got)
	}
	if m.getMaxVisibleItems() >= list {
		t.Fatal("the list must get shorter when the screen has more lines")
	}
	if _, p := m.getHistoryPanelSize(); p >= panel {
		t.Fatal("the panels must get shorter when the screen has more lines")
	}
	if h := m.listFrameHeight(); h >= 60-25 {
		t.Fatalf("the list frame must shrink too, got %d", h)
	}
}

func TestWrappedHintBarGivesRowsBack(t *testing.T) {
	wide := model{width: 400, height: 60}
	narrow := model{width: 100, height: 60}
	if wide.helpExtraLines() != 0 || narrow.helpExtraLines() < 1 {
		t.Fatalf("wide %d narrow %d", wide.helpExtraLines(), narrow.helpExtraLines())
	}
	if narrow.extraRows() != narrow.helpExtraLines() {
		t.Fatal("the extra bar lines count as extra rows")
	}
	if narrow.getMaxVisibleItems() >= wide.getMaxVisibleItems() {
		t.Fatal("a taller bar leaves the list fewer rows")
	}
}
