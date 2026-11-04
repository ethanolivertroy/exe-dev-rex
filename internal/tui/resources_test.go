package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"errors"
	"github.com/ethanolivertroy/exe-dev-rex/internal/exedev"
	"strings"
	"testing"
)

func TestPlanAwareInputs(t *testing.T) {
	m, _ := testModel(t)
	m.capacity = capacityState{loaded: true, plan: exedev.Plan{Name: "Individual", Tier: "Small", MaxCPUs: 2, MaxMemoryGB: 8, IncludedDiskGB: 100, MaxDiskGB: 1024, DefaultDiskGB: 25}}
	for _, tc := range []struct {
		kind, input string
		valid       bool
	}{
		{"cpu", "2", true}, {"cpu", "3", false}, {"memory", "8192MB", true}, {"memory", "9GB", false},
		{"disk", "101GB", true}, {"disk", "1025GB", false}, {"disk", "0", false}, {"memory", "", true},
	} {
		if err := m.validateResource(tc.kind, tc.input); (err == nil) != tc.valid {
			t.Errorf("%s %q: %v", tc.kind, tc.input, err)
		}
	}
	got := m.capacityDescription()
	for _, want := range []string{"Individual", "Small", "2 vCPU", "8 GB", "100 GB", "shared"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	m.capacity = capacityState{loaded: true, err: errors.New("offline")}
	if m.validateResource("cpu", "64") != nil || !strings.Contains(m.capacityDescription(), "unavailable") {
		t.Fatal("unknown plan must not invent limits")
	}
}

func TestNewVMReviewAndCancel(t *testing.T) {
	m, started := testModel(t)
	m.newVM = newAnswers{name: "example", cpu: "2", memory: "4GB", disk: "25GB"}
	m.reviewNew()
	if m.mode != modeReviewNew || len(*started) != 0 {
		t.Fatal("review must not create")
	}
	for _, want := range []string{"example", "2", "4GB", "25GB", "Rex"} {
		if !strings.Contains(m.render(), want) {
			t.Errorf("review missing %q", want)
		}
	}
	press(m, "esc")
	if len(*started) != 0 {
		t.Fatal("cancel created a VM")
	}
	m.reviewNew()
	press(m, "y")
	if len(*started) != 1 || (*started)[0] != "new example" {
		t.Fatalf("confirm: %v", *started)
	}
}

// Resolve the form's asynchronous description updates without starting the app
// or any backend calls. Ordinary messages cause huh to refresh bound copy.
func refreshForm(t *testing.T, m *model) {
	t.Helper()
	_, cmd := m.form.Update(struct{}{})
	queue := []tea.Cmd{cmd}
	for n := 0; len(queue) > 0; n++ {
		if n > 100 {
			t.Fatal("form command loop")
		}
		cmd, queue = queue[0], queue[1:]
		if cmd == nil {
			continue
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		_, cmd = m.form.Update(msg)
		queue = append(queue, cmd)
	}
}

func TestResourceFormRendering(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}, {60, 24}} {
		m, _ := testModel(t)
		m.width, m.height = size[0], size[1]
		m.newForm(false)
		m.capacity = capacityState{loaded: true, plan: exedev.Plan{Name: "Individual", Tier: "Small", MaxCPUs: 2, MaxMemoryGB: 8, IncludedDiskGB: 100, MaxDiskGB: 1024, DefaultDiskGB: 25}}
		for _, page := range []struct{ label, hint string }{
			{"CPU (vCPU)", "Positive whole number"},
			{"Memory (GB or MB)", "512MB"},
			{"Disk (GB or MB)", "extra disk may incur"},
		} {
			m.form.NextGroup()
			refreshForm(t, m)
			got := m.render()
			for _, want := range []string{page.label, page.hint} {
				if !strings.Contains(got, want) {
					t.Fatalf("missing %s: %s", want, got)
				}
			}
			if lipgloss.Height(got) > m.height || lipgloss.Width(got) > m.width {
				t.Fatalf("overflow at %v: %dx%d\n%s", size, lipgloss.Width(got), lipgloss.Height(got), got)
			}
		}
	}
}

func TestCapacityRefreshChangesLimits(t *testing.T) {
	m, started := testModel(t)
	m.newVM = newAnswers{name: "example", cpu: "4", memory: "16"}
	m.capacity = capacityState{loaded: true, plan: exedev.Plan{Name: "Individual", Tier: "Medium", MaxCPUs: 4, MaxMemoryGB: 16}}
	if m.validateResource("cpu", "4") != nil || m.validateResource("memory", "16GB") != nil {
		t.Fatal("larger plan rejected")
	}
	m.reviewNew()
	if !strings.Contains(m.newReviewView(), "16GB") {
		t.Fatal("review must explain bare size units")
	}
	m.Update(capacityMsg{loaded: true, plan: exedev.Plan{Name: "Individual", Tier: "Small", MaxCPUs: 2, MaxMemoryGB: 8}})
	press(m, "y")
	if len(*started) > 0 || m.mode != modeForm {
		t.Fatal("a changed plan must be revalidated before creation")
	}
}
