package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ethanolivertroy/exe-dev-rex/internal/ops"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
)

// testModel has two VMs and records ops instead of running them.
func testModel(t *testing.T) (*model, *[]string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newModel(context.Background())
	m.loaded = true
	m.ov = ops.Overview{Tag: "rex-host", Ephemeral: true, Tailnet: "t.ts.net",
		Rows: []ops.VMRow{{Name: "alpha", Rex: "ok"}, {Name: "beta", Rex: "ok"}}}
	var started []string
	m.start = func(m *model, o *op, _ func(context.Context, *ui.UI) (bool, error)) tea.Cmd {
		started = append(started, o.kind+" "+o.name)
		return nil
	}
	return m, &started
}

func press(m *model, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		default:
			msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		m.Update(msg)
	}
}

func TestDeleteConfirmY(t *testing.T) {
	m, started := testModel(t)
	press(m, "down", "d")
	if m.mode != modeConfirmDelete || !strings.Contains(m.render(), "Delete beta") {
		t.Fatalf("d should ask about beta; mode=%v", m.mode)
	}
	press(m, "n")
	if len(*started) != 0 || m.mode != modeTable {
		t.Fatalf("n must not delete: %v", *started)
	}
	press(m, "d", "y")
	if len(*started) != 1 || (*started)[0] != "rm beta" {
		t.Fatalf("d y should remove beta: %v", *started)
	}
}

func TestNoSecondOp(t *testing.T) {
	m, started := testModel(t)
	press(m, "d", "y", "esc") // removing alpha, back to the table
	press(m, "d")
	if m.mode != modeTable || !strings.Contains(m.flash, "Wait for") {
		t.Fatalf("a second op must wait; mode=%v flash=%q", m.mode, m.flash)
	}
	press(m, "n")
	if m.mode == modeForm || len(*started) != 1 {
		t.Fatalf("n while busy must not open the form; started=%v", *started)
	}
}

func TestQuitWhileBusyAsks(t *testing.T) {
	m, _ := testModel(t)
	press(m, "d", "y", "esc")
	_, cmd := m.key(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd != nil || m.mode != modeQuitAsk {
		t.Fatalf("q while removing must ask first; mode=%v", m.mode)
	}
	press(m, "w")
	if m.mode != modeTable {
		t.Fatalf("w should go back; mode=%v", m.mode)
	}
	m.key(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if _, cmd = m.key(tea.KeyPressMsg{Code: 'q', Text: "q"}); cmd == nil {
		t.Fatal("q q should quit")
	}
}

func TestQuitWhenIdle(t *testing.T) {
	m, _ := testModel(t)
	if _, cmd := m.key(tea.KeyPressMsg{Code: 'q', Text: "q"}); cmd == nil || m.mode == modeQuitAsk {
		t.Fatal("q with nothing running quits at once")
	}
}

func TestOpLinesAndFinish(t *testing.T) {
	m, _ := testModel(t)
	press(m, "d", "y")
	m.Update(eventMsg(ui.Event{Kind: ui.KindOK, Text: "Removed from the Rex sidebar"}))
	m.Update(opDoneMsg{})
	if m.busy() || !strings.Contains(m.flash, "alpha is gone") {
		t.Fatalf("flash=%q busy=%v", m.flash, m.busy())
	}
	if !strings.Contains(m.render(), "Removed from the Rex sidebar") {
		t.Fatal("the op's lines should show")
	}
}

func TestPlanNeedsToken(t *testing.T) {
	m, _ := testModel(t)
	m.op = &op{kind: "plan", running: true, started: time.Now()}
	m.Update(opDoneMsg{needToken: true})
	if m.mode != modeForm || m.formKind != "token" {
		t.Fatalf("a plan that needs a token asks for one; mode=%v kind=%s", m.mode, m.formKind)
	}
	if strings.Contains(m.render(), "tskey-api-secret") {
		t.Fatal("never render the token")
	}
}

func TestMergeRowsKeepsDeepColumns(t *testing.T) {
	old := []ops.VMRow{{Name: "a", Rex: "ok", Link: "direct", Node: "ephemeral"}}
	got := mergeRows(old, []ops.VMRow{{Name: "a"}, {Name: "b"}})
	if got[0].Rex != "ok" || got[0].Link != "direct" || got[0].Node != "ephemeral" || got[1].Rex != "" {
		t.Fatalf("got %+v", got)
	}
	got = mergeRows(old, []ops.VMRow{{Name: "a", Rex: "paused"}})
	if got[0].Rex != "paused" || got[0].Link != "" {
		t.Fatalf("a paused VM has no link: %+v", got)
	}
}

func TestRenderStageDone(t *testing.T) {
	got := renderEvent(ui.Event{Kind: ui.KindOK, Text: "Created the VM", Took: 1400 * time.Millisecond})
	if !strings.Contains(got, "✓") || !strings.Contains(got, "Created the VM") || !strings.Contains(got, "1.4s") {
		t.Fatalf("got %q", got)
	}
}

func TestDoctorIsNotShiftD(t *testing.T) {
	m, started := testModel(t)
	press(m, "h")
	if len(*started) != 1 || (*started)[0] != "doctor " {
		t.Fatalf("h runs the health check: %v", *started)
	}
}
