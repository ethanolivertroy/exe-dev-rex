package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ethanolivertroy/exe-dev-rex/internal/ops"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
)

var spin = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m *model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "exe-rex"
	return v
}

func (m *model) render() string {
	var b strings.Builder
	b.WriteString(m.header() + "\n\n")
	switch m.mode {
	case modeReviewNew:
		b.WriteString(m.newReviewView())
	case modeForm:
		if m.formKind == "new" || m.formKind == "wizard" {
			b.WriteString(lipgloss.NewStyle().Width(max(20, min(88, m.width-4))).Render(m.capacityDescription()) + "\n\n")
		}
		if m.form != nil {
			b.WriteString(m.form.View())
		}
		b.WriteString("\n" + dim.Render("  enter next · shift+tab back · esc cancel"))
	case modeOp:
		b.WriteString(m.opView())
	case modeReview:
		b.WriteString(m.reviewView())
	case modeDetails:
		b.WriteString(m.detailsView())
	case modeQuitAsk:
		b.WriteString(m.table() + "\n\n")
		b.WriteString(warn.Render(fmt.Sprintf("  Still %s. ", strings.ToLower(m.op.title))) +
			"Stopping now can leave a VM or device behind.\n")
		b.WriteString(bold.Render("  w") + " wait   " + bold.Render("q") + " quit and stop it")
	case modeConfirmDelete:
		b.WriteString(m.table() + "\n\n")
		r, _ := m.selectedRow()
		b.WriteString(warn.Render(fmt.Sprintf("  Delete %s, its Rex host and its Tailscale device? ", r.Name)) + bold.Render("y/n"))
	default:
		b.WriteString(m.table())
		b.WriteString("\n\n" + m.footer())
	}
	return b.String()
}

func (m *model) header() string {
	parts := []string{}
	switch {
	case !m.loaded && m.ovErr == nil:
		parts = append(parts, "loading…")
	case m.ovErr != nil:
		parts = append(parts, bad.Render("can't reach exe.dev: "+m.ovErr.Error()))
	default:
		if m.ov.Tailnet != "" {
			parts = append(parts, "tailnet ok")
		} else {
			parts = append(parts, bad.Render("tailnet down"))
		}
		parts = append(parts, fmt.Sprintf("%d VMs", len(m.ov.Rows)))
		if m.ov.Tag != "" {
			parts = append(parts, "tag "+m.ov.Tag)
			parts = append(parts, map[bool]string{true: "ephemeral", false: "persistent"}[m.ov.Ephemeral])
		}
	}
	h := " " + title.Render("exe-rex") + "  " + dim.Render(strings.Join(parts, " · "))
	if m.busy() && m.mode != modeOp {
		h += "   " + warn.Render(m.spinner()+" "+m.op.title+"…") + dim.Render(" (o)")
	}
	return h
}

func (m *model) spinner() string {
	if m.op == nil {
		return ""
	}
	return spin[int(time.Since(m.op.started)/time.Second)%len(spin)]
}

func pad(s string, w int) string {
	if n := lipgloss.Width(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

func rexCell(r ops.VMRow) string {
	switch r.Rex {
	case "ok":
		return okStyle.Render("●") + " ok"
	case "paused":
		return dim.Render("○ paused")
	case "":
		return dim.Render("…")
	case "off-tailnet":
		return bad.Render("✗") + " not on tailnet"
	default:
		return bad.Render("✗") + " " + r.Rex
	}
}

func (m *model) table() string {
	var b strings.Builder
	for _, w := range m.ov.Warnings {
		b.WriteString(warn.Render("  ! "+w) + "\n")
	}
	if len(m.ov.Warnings) > 0 {
		b.WriteString("\n")
	}
	widths := []int{20, 18, 9, 12, 5}
	row := func(cells ...string) string {
		var s strings.Builder
		for i, c := range cells {
			s.WriteString(pad(c, widths[i]))
		}
		return s.String()
	}
	b.WriteString(dim.Render("  "+row("NAME", "REX", "LINK", "NODE", "AGE")) + "\n")
	if m.loaded && len(m.ov.Rows) == 0 {
		b.WriteString(dim.Render("  No VMs yet. Press n to create one.") + "\n")
	}
	for i, r := range m.ov.Rows {
		age := "—"
		if !r.Created.IsZero() {
			age = ops.AgeString(time.Since(r.Created))
		}
		link := r.Link
		switch link {
		case "relay":
			link = warn.Render("relay")
		case "":
			link = dim.Render("—")
		}
		node := r.Node
		if node == "" {
			node = dim.Render("…")
		}
		line := row(r.Name, rexCell(r), link, node, age)
		if i == m.cursor {
			b.WriteString(selected.Render("▸ "+row(r.Name, plainRex(r), dash(r.Link), dash(r.Node), age)) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	if m.ov.Others > 0 {
		b.WriteString(dim.Render(fmt.Sprintf("\n  %d other exe.dev VM(s) without the %s tag aren't shown", m.ov.Others, m.ov.Tag)) + "\n")
	}
	if m.flash != "" {
		b.WriteString("\n  " + m.flash + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func plainRex(r ops.VMRow) string {
	switch r.Rex {
	case "ok":
		return "● ok"
	case "paused":
		return "○ paused"
	case "":
		return "…"
	case "off-tailnet":
		return "✗ not on tailnet"
	}
	return "✗ " + r.Rex
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func (m *model) footer() string {
	keys := [][2]string{{"n", "new"}, {"d", "delete"}, {"enter", "details"}, {"s", "ssh"}, {"c", "settings"}, {"h", "health check"}, {"r", "refresh"}, {"q", "quit"}}
	var parts []string
	for _, k := range keys {
		parts = append(parts, bold.Render(k[0])+" "+dim.Render(k[1]))
	}
	return "  " + strings.Join(parts, dim.Render(" · "))
}

func (m *model) opView() string {
	o := m.op
	if o == nil {
		return ""
	}
	var b strings.Builder
	status := warn.Render(m.spinner()) + " " + bold.Render(o.title) + dim.Render(fmt.Sprintf("  %ds", int(time.Since(o.started).Seconds())))
	if !o.running {
		if o.err != nil {
			status = bad.Render("✗ ") + bold.Render(o.title) + "  " + bad.Render(o.err.Error())
		} else {
			status = okStyle.Render("✓ ") + bold.Render(o.title) + dim.Render(fmt.Sprintf("  done in %s", o.took.Round(time.Second)))
		}
	}
	b.WriteString("  " + status + "\n\n")
	room := m.height - 8
	lines := o.lines
	if o.kind == "new" {
		// "==> Booting the VM" is followed by its ✓ line; show only the current one.
		lines = nil
		for i, e := range o.lines {
			if e.Kind == ui.KindSection && (i < len(o.lines)-1 || !o.running) {
				continue
			}
			if e.Kind == ui.KindSection {
				e = ui.Event{Kind: ui.KindInfo, Text: "  " + m.spinner() + " " + e.Text + "…"}
			}
			lines = append(lines, e)
		}
	}
	for _, e := range trimLines(lines, room) {
		b.WriteString(renderEvent(e) + "\n")
	}
	if o.running {
		b.WriteString("\n" + dim.Render("  esc back to the table (it keeps running)"))
	} else {
		b.WriteString("\n" + dim.Render("  esc back"))
	}
	return b.String()
}

func (m *model) reviewView() string {
	var b strings.Builder
	b.WriteString("  " + bold.Render("Review") + dim.Render("  nothing has changed yet; → lines are what applying does") + "\n\n")
	room := m.height - 9
	var lines []ui.Event
	changes := 0
	for _, e := range m.op.lines {
		if e.Kind == ui.KindInfo && strings.TrimSpace(e.Text) == ops.DryRunBanner {
			continue
		}
		if e.Kind == ui.KindTodo {
			changes++
		}
		lines = append(lines, e)
	}
	if changes == 0 {
		b.WriteString("  " + okStyle.Render("Already set up this way; applying changes nothing.") + "\n\n")
	} else {
		b.WriteString("  " + warn.Render(fmt.Sprintf("%d change(s) below, marked →.", changes)) + "\n\n")
	}
	for _, e := range trimLines(lines, room-2) {
		b.WriteString(renderEvent(e) + "\n")
	}
	b.WriteString("\n  " + bold.Render("enter") + " apply   " + bold.Render("esc") + " change answers   " + bold.Render("q") + " cancel")
	return b.String()
}

func (m *model) detailsView() string {
	r, ok := m.selectedRow()
	if !ok {
		return ""
	}
	field := func(k, v string) string { return dim.Render(fmt.Sprintf("%-9s", k)) + v + "\n" }
	var b strings.Builder
	b.WriteString(bold.Render(r.Name) + "\n\n")
	b.WriteString(field("Rex", plainRex(r)+"  "+dim.Render(r.Endpoint)))
	b.WriteString(field("Sidebar", r.Name+" - exe.dev"))
	b.WriteString(field("Link", dash(r.Link)))
	b.WriteString(field("Node", dash(r.Node)))
	if r.HTTPSURL != "" {
		b.WriteString(field("Web", r.HTTPSURL))
	}
	b.WriteString(field("SSH", "ssh "+r.SSH))
	if !r.Created.IsZero() {
		b.WriteString(field("Created", r.Created.Local().Format("Jan 2 15:04")+dim.Render(" ("+ops.AgeString(time.Since(r.Created))+" ago)")))
	}
	b.WriteString(field("Remove", "d in the table, or exe-rex rm "+r.Name))
	return indent(box.Render(strings.TrimRight(b.String(), "\n"))) + "\n\n" + dim.Render("  esc back")
}
