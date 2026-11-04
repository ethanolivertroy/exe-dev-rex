package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/ethanolivertroy/exe-dev-rex/internal/exedev"
	"github.com/ethanolivertroy/exe-dev-rex/internal/resources"
)

type capacityState struct {
	plan   exedev.Plan
	loaded bool
	err    error
}
type capacityMsg capacityState

func (m *model) fetchCapacity() tea.Cmd {
	m.capacity = capacityState{}
	ctx := m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		p, err := exedev.BillingPlan(ctx)
		return capacityMsg{plan: p, loaded: true, err: err}
	}
}

func (m *model) capacityDescription() string {
	c := m.capacity
	if !c.loaded {
		return "Checking your exe.dev plan…"
	}
	if c.err != nil {
		return "Plan limits unavailable. exe.dev will validate capacity when you create.\nCheck your plan: ssh exe.dev billing plan --json"
	}
	p := c.plan
	label := strings.TrimSpace(p.Name + " / " + p.Tier)
	var totals []string
	if p.MaxCPUs > 0 {
		totals = append(totals, fmt.Sprintf("%d vCPU", p.MaxCPUs))
	}
	if p.MaxMemoryGB > 0 {
		totals = append(totals, fmt.Sprintf("%d GB memory", p.MaxMemoryGB))
	}
	if p.IncludedDiskGB > 0 {
		totals = append(totals, fmt.Sprintf("%d GB included disk", p.IncludedDiskGB))
	}
	return strings.TrimSuffix(label, " /") + "\n" + strings.Join(totals, " · ") + "\nCapacity is shared across your VMs. These totals are not remaining capacity."
}

func (m *model) validateResource(kind, s string) error {
	var n, limit int64
	p := m.capacity.plan
	if kind == "cpu" {
		if err := resources.ValidateCPU(s); err != nil {
			return err
		}
		v, _ := strconv.Atoi(strings.TrimSpace(s))
		n, limit = int64(v), int64(p.MaxCPUs)
	} else {
		var err error
		n, err = resources.ParseSize(s)
		if err != nil {
			return err
		}
		if kind == "memory" {
			limit = p.MaxMemoryGB
		} else {
			limit = p.MaxDiskGB
		}
		// Compare in GB without overflowing reported limits.
		if limit > 0 && n > 0 && (n-1)/1024 >= limit {
			return fmt.Errorf("%s exceeds your plan's %d GB limit; choose a smaller value or change capacity at exe.dev/billing", kind, limit)
		}
		return nil
	}
	if limit > 0 && n > limit {
		return fmt.Errorf("choose 1–%d vCPU for your plan, or change capacity at exe.dev/billing", limit)
	}
	return nil
}

// resourceGroups is shared by VM creation and saved defaults. One resource per
// page keeps its instructions visible in an ordinary terminal window.
func (m *model) resourceGroups(cpu, memory, disk *string, title string) []*huh.Group {
	return []*huh.Group{
		huh.NewGroup(huh.NewInput().Title("CPU (vCPU)").Placeholder("blank = exe.dev default (2)").Value(cpu).
			DescriptionFunc(func() string {
				s := "Positive whole number, e.g. 1 or 2. Blank uses exe.dev's default (2)."
				if n := m.capacity.plan.MaxCPUs; n > 0 {
					s += fmt.Sprintf("\nYour plan: 1–%d vCPU per VM, sharing account capacity.", n)
				}
				return s
			}, &m.capacity).Validate(func(s string) error { return m.validateResource("cpu", s) })).Title(title + " · CPU"),
		huh.NewGroup(huh.NewInput().Title("Memory (GB or MB)").Placeholder("blank = exe.dev default").Value(memory).
			DescriptionFunc(func() string {
				s := "Examples: 4, 4GB, 8G or 512MB. Bare numbers mean GB.\nPositive whole sizes only. Blank uses exe.dev's default."
				if n := m.capacity.plan.MaxMemoryGB; n > 0 {
					s += fmt.Sprintf("\nYour plan's memory limit: %d GB; shared across VMs.", n)
				}
				return s
			}, &m.capacity).Validate(func(s string) error { return m.validateResource("memory", s) })).Title(title + " · Memory"),
		huh.NewGroup(huh.NewInput().Title("Disk (GB or MB)").Placeholder("blank = exe.dev default").Value(disk).
			DescriptionFunc(func() string {
				s := "Examples: 25, 25GB or 50G. Bare numbers mean GB.\nPositive whole sizes only. Blank uses exe.dev's default."
				p := m.capacity.plan
				if p.DefaultDiskGB > 0 {
					s += fmt.Sprintf("\nDefault disk: %d GB.", p.DefaultDiskGB)
				}
				if p.MaxDiskGB > 0 {
					s += fmt.Sprintf(" Maximum: %d GB.", p.MaxDiskGB)
				}
				if p.IncludedDiskGB > 0 {
					s += fmt.Sprintf("\n%d GB included across VMs; extra disk may incur charges.", p.IncludedDiskGB)
				}
				return s
			}, &m.capacity).Validate(func(s string) error { return m.validateResource("disk", s) })).Title(title + " · Disk"),
	}
}

func (m *model) reviewNew() tea.Cmd { m.mode = modeReviewNew; return nil }

func (m *model) newReviewView() string {
	value := func(s string) string {
		if strings.TrimSpace(s) == "" {
			return "exe.dev default"
		}
		s = resources.SizeArgument(s)
		if s[len(s)-1] >= '0' && s[len(s)-1] <= '9' {
			s += "GB"
		}
		return s
	}
	a := m.newVM
	return fmt.Sprintf("  Create %s?\n\n  CPU:    %s\n  Memory: %s\n  Disk:   %s\n\n%s\n\n  Rex restarts once to add this VM to the sidebar.\n\n  y create · esc back to edit", strings.TrimSpace(a.name), cpuReview(a.cpu), value(a.memory), value(a.disk), m.capacityDescription())
}

func cpuReview(s string) string {
	if s = strings.TrimSpace(s); s == "" {
		return "exe.dev default (2 vCPU)"
	}
	return s + " vCPU"
}
