// Package tui is exe-rex's terminal UI: a setup wizard and a table of your
// exe.dev VMs. Every action runs the same code as the matching CLI command
// (ops.CreateVM, ops.RemoveVM, ops.RunInstall, ops.RunDoctor), with output
// sent here through a ui.NewSink instead of to the terminal.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/ethanolivertroy/exe-dev-rex/internal/config"
	"github.com/ethanolivertroy/exe-dev-rex/internal/exedev"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ops"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
)

type mode int

const (
	modeTable mode = iota
	modeDetails
	modeConfirmDelete
	modeForm   // a huh form: new VM, wizard, token
	modeOp     // watching an operation's output
	modeReview // the wizard's plan, before applying
	modeQuitAsk
)

const (
	fastEvery = 10 * time.Second
	deepEvery = 30 * time.Second
)

// op is one command run from the TUI.
type op struct {
	kind    string // new, rm, plan, apply, doctor
	name    string // VM name, for new/rm
	title   string
	lines   []ui.Event
	running bool
	err     error
	started time.Time
	took    time.Duration
}

// Messages.
type (
	overviewMsg struct {
		ov   ops.Overview
		err  error
		deep bool
	}
	setupMsg  struct{ needed bool }
	tickMsg   struct{}
	secMsg    struct{}
	eventMsg  ui.Event
	opDoneMsg struct {
		err       error
		needToken bool
	}
	sshDoneMsg struct{ err error }
)

type model struct {
	ctx    context.Context
	cancel context.CancelFunc // stops the running op
	mode   mode
	back   mode // where esc goes from details/op

	ov       ops.Overview
	ovErr    error
	loaded   bool
	lastDeep time.Time
	deepBusy bool
	cursor   int

	cfg   config.Config
	flash string

	op     *op
	events chan tea.Msg

	form     *huh.Form
	formKind string // new, wizard, token
	newVM    newAnswers
	wiz      wizard
	token    string // in memory only

	width, height int

	// start runs an op in the background; tests replace it.
	start func(m *model, o *op, run func(ctx context.Context, u *ui.UI) (bool, error)) tea.Cmd
}

// Run opens the TUI and returns when the user quits.
func Run(ctx context.Context) error {
	ui.DisablePrompts()
	m := newModel(ctx)
	_, err := tea.NewProgram(m).Run()
	if m.cancel != nil {
		m.cancel()
	}
	return err
}

func newModel(ctx context.Context) *model {
	cfg, _ := config.Load()
	return &model{ctx: ctx, cfg: cfg, events: make(chan tea.Msg, 256), start: startOp}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.fetch(false), checkSetup(m.ctx, m.cfg), waitEvent(m.events), tick())
}

func tick() tea.Cmd { return tea.Tick(fastEvery, func(time.Time) tea.Msg { return tickMsg{} }) }
func sec() tea.Cmd  { return tea.Tick(time.Second, func(time.Time) tea.Msg { return secMsg{} }) }

func waitEvent(ch chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-ch } }

func (m *model) fetch(deep bool) tea.Cmd {
	if deep {
		m.deepBusy = true
	}
	ctx := m.ctx
	return func() tea.Msg {
		ov, err := ops.ListRows(ctx, deep)
		return overviewMsg{ov, err, deep}
	}
}

// checkSetup decides whether to open on the wizard: no working Tailscale
// identity, or exe-rex isn't installed where the config says.
func checkSetup(ctx context.Context, cfg config.Config) tea.Cmd {
	return func() tea.Msg {
		_, err := os.Stat(filepath.Join(cfg.BinDir(), "exe-rex"))
		s, ierr := exedev.TailscaleIntegration(ctx)
		return setupMsg{needed: err != nil || (ierr == nil && (!s.Exists || s.Pending))}
	}
}

// startOp runs fn in a goroutine with a sink UI; its lines and its end arrive
// as messages.
func startOp(m *model, o *op, fn func(ctx context.Context, u *ui.UI) (bool, error)) tea.Cmd {
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	ch := m.events
	go func() {
		u := ui.NewSink(func(e ui.Event) { ch <- eventMsg(e) })
		need, err := fn(ctx, u)
		cancel()
		ch <- opDoneMsg{err: err, needToken: need}
	}()
	return sec()
}

func (m *model) busy() bool { return m.op != nil && m.op.running }

func (m *model) begin(kind, name, title string, fn func(ctx context.Context, u *ui.UI) (bool, error)) tea.Cmd {
	m.op = &op{kind: kind, name: name, title: title, running: true, started: time.Now()}
	m.mode, m.back = modeOp, modeTable
	return m.start(m, m.op, fn)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case overviewMsg:
		if msg.deep {
			m.deepBusy, m.lastDeep = false, time.Now()
		}
		m.ovErr = msg.err
		if msg.err == nil {
			msg.ov.Rows = mergeRows(m.ov.Rows, msg.ov.Rows)
			m.ov, m.loaded = msg.ov, true
			if m.cursor >= len(m.ov.Rows) {
				m.cursor = max(len(m.ov.Rows)-1, 0)
			}
		}
		if !msg.deep && !m.deepBusy && time.Since(m.lastDeep) > deepEvery {
			return m, m.fetch(true)
		}
	case setupMsg:
		if msg.needed && m.mode == modeTable && !m.busy() {
			m.flash = "Let's set things up first."
			return m, m.openWizard()
		}
	case tickMsg:
		return m, tea.Batch(m.fetch(false), tick())
	case secMsg:
		if m.busy() {
			return m, sec()
		}
	case eventMsg:
		if m.op != nil {
			m.op.lines = append(m.op.lines, ui.Event(msg))
		}
		return m, waitEvent(m.events)
	case opDoneMsg:
		return m, tea.Batch(m.finish(msg), waitEvent(m.events))
	case sshDoneMsg:
		if msg.err != nil {
			m.flash = "ssh: " + msg.err.Error()
		}
		return m, m.fetch(false)
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	if m.mode == modeForm && m.form != nil {
		return m.updateForm(msg)
	}
	return m, nil
}

// finish handles the end of an operation.
func (m *model) finish(d opDoneMsg) tea.Cmd {
	o := m.op
	if o == nil {
		return nil
	}
	o.running, o.err, o.took = false, d.err, time.Since(o.started)
	m.cancel = nil
	switch o.kind {
	case "plan":
		if d.err == nil && d.needToken && m.token == "" {
			return m.askToken()
		}
		if d.err == nil {
			m.mode = modeReview
		}
	case "apply":
		m.token = "" // used; forget it
		if d.err == nil {
			m.flash = "Settings applied."
			m.cfg, _ = config.Load()
		}
	case "new", "rm":
		if d.err == nil {
			verb := map[string]string{"new": "is ready in Rex", "rm": "is gone"}[o.kind]
			m.flash = fmt.Sprintf("%s %s (%s)", o.name, verb, o.took.Round(time.Second))
		} else {
			m.flash = o.name + ": " + d.err.Error()
		}
		return tea.Batch(m.fetch(false), m.fetch(true))
	}
	return nil
}

func (m *model) selectedRow() (ops.VMRow, bool) {
	if m.cursor < 0 || m.cursor >= len(m.ov.Rows) {
		return ops.VMRow{}, false
	}
	return m.ov.Rows[m.cursor], true
}

func (m *model) key(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := k.String()
	if s == "ctrl+c" && m.mode != modeQuitAsk {
		return m.quit()
	}
	switch m.mode {
	case modeForm:
		if s == "esc" {
			m.form, m.mode = nil, modeTable
			return m, nil
		}
		return m.updateForm(k)
	case modeQuitAsk:
		switch s {
		case "q", "y", "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "w", "n", "esc":
			m.mode = m.back
		}
		return m, nil
	case modeConfirmDelete:
		r, ok := m.selectedRow()
		m.mode = modeTable
		if ok && (s == "y" || s == "Y") {
			name := r.Name
			return m, m.begin("rm", name, "Removing "+name, func(ctx context.Context, u *ui.UI) (bool, error) {
				_, err := ops.RemoveVM(ctx, u, name)
				return false, err
			})
		}
		m.flash = "Not deleted."
		return m, nil
	case modeDetails:
		if s == "esc" || s == "enter" || s == "q" {
			m.mode = modeTable
		}
		return m, nil
	case modeOp:
		switch s {
		case "esc", "q":
			if m.op != nil && m.op.kind == "plan" && !m.op.running {
				m.mode = modeTable
				return m, nil
			}
			m.mode = modeTable
		}
		return m, nil
	case modeReview:
		switch s {
		case "enter", "a":
			return m, m.apply()
		case "esc":
			return m, m.openWizard()
		case "q":
			m.token, m.mode = "", modeTable
		}
		return m, nil
	}

	// modeTable
	m.flash = ""
	switch s {
	case "q":
		return m.quit()
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.ov.Rows)-1 {
			m.cursor++
		}
	case "r":
		return m, tea.Batch(m.fetch(false), m.fetch(true))
	case "enter":
		if _, ok := m.selectedRow(); ok {
			m.mode = modeDetails
		}
	case "o":
		if m.op != nil {
			m.mode, m.back = modeOp, modeTable
		}
	case "n", "d", "c", "h":
		if m.busy() {
			m.flash = fmt.Sprintf("Wait for %q to finish (o shows it).", m.op.title)
			return m, nil
		}
		switch s {
		case "n":
			return m, m.openNew()
		case "d":
			if _, ok := m.selectedRow(); ok {
				m.mode = modeConfirmDelete
			}
		case "c":
			return m, m.openWizard()
		case "h":
			bin := m.cfg.BinDir()
			return m, m.begin("doctor", "", "Health check", func(ctx context.Context, u *ui.UI) (bool, error) {
				return false, ops.RunDoctor(ctx, u, bin)
			})
		}
	case "s":
		if r, ok := m.selectedRow(); ok {
			return m, tea.ExecProcess(exec.Command("ssh", r.SSH), func(err error) tea.Msg { return sshDoneMsg{err} })
		}
	}
	return m, nil
}

func (m *model) quit() (tea.Model, tea.Cmd) {
	if m.busy() && (m.op.kind == "new" || m.op.kind == "rm" || m.op.kind == "apply") {
		m.back = m.mode
		if m.back == modeQuitAsk {
			m.back = modeTable
		}
		m.mode = modeQuitAsk
		return m, nil
	}
	if m.cancel != nil {
		m.cancel()
	}
	return m, tea.Quit
}

// mergeRows keeps the slow columns from the last deep refresh when a fast
// refresh doesn't have them.
func mergeRows(old, cur []ops.VMRow) []ops.VMRow {
	prev := map[string]ops.VMRow{}
	for _, r := range old {
		prev[r.Name] = r
	}
	for i, r := range cur {
		p, ok := prev[r.Name]
		if !ok {
			continue
		}
		if r.Rex == "" {
			cur[i].Rex = p.Rex
		}
		if r.Link == "" && r.Rex != "paused" {
			cur[i].Link = p.Link
		}
		if r.Node == "" {
			cur[i].Node = p.Node
		}
	}
	return cur
}

func trimLines[T any](lines []T, n int) []T {
	if n > 0 && len(lines) > n {
		return lines[len(lines)-n:]
	}
	return lines
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}
