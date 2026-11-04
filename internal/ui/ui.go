// Package ui draws exe-rex's terminal output: the progress bar, step lines,
// Program Status Protocol (OSC 7501) reports and notifications.
package ui

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// UI writes to stdout. When stdout isn't a terminal it prints plain lines and
// sends no escape sequences.
type UI struct {
	TTY bool
	Out io.Writer
	App string // OSC 7501 app= value
	// NoNotify turns off Notify (the TUI shows results itself).
	NoNotify bool

	mu   sync.Mutex
	sink func(Event) // gets every Event; see NewSink
}

// New returns a UI for stdout.
func New() *UI {
	return &UI{TTY: term.IsTerminal(int(os.Stdout.Fd())), Out: os.Stdout, App: "exe-dev"}
}

func (u *UI) color(code, s string) string {
	if !u.TTY {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (u *UI) Bold(s string) string  { return u.color("1", s) }
func (u *UI) Dim(s string) string   { return u.color("2", s) }
func (u *UI) Green(s string) string { return u.color("32", s) }
func (u *UI) Red(s string) string   { return u.color("31", s) }
func (u *UI) Yellow(s string) string {
	return u.color("33", s)
}
func (u *UI) Cyan(s string) string { return u.color("36", s) }

// Header prints a command's title line: "exe.dev → Rex  <what>".
func (u *UI) Header(what string) {
	u.Line("")
	u.Line(fmt.Sprintf(" %s  %s", u.Bold(u.Cyan("exe.dev → Rex")), what))
	u.Line("")
}

// Rule prints a thin divider as wide as the step lines.
func (u *UI) Rule() { u.Line("  " + u.Dim(strings.Repeat("─", 41))) }

// Field prints one aligned "label  value" row of a summary.
func (u *UI) Field(label, value string) {
	u.Line(fmt.Sprintf("    %s %s", u.Dim(fmt.Sprintf("%-7s", label)), value))
}

// Line prints a free-form line (KindInfo), exactly as given.
func (u *UI) Line(s string) { u.Emit(Event{Kind: KindInfo, Text: s}) }

// Detail prints a continuation of the step line before it.
func (u *UI) Detail(format string, a ...any) {
	u.Emit(Event{Kind: KindDetail, Text: fmt.Sprintf(format, a...)})
}

// WouldAsk reports a question a dry run didn't ask.
func (u *UI) WouldAsk(question string) {
	u.Emit(Event{Kind: KindTodo, Text: question, WouldAsk: true})
}

// Step, OK, Todo, Warn and Fail print installer-style lines.
func (u *UI) Step(s string) { u.Emit(Event{Kind: KindSection, Text: s}) }
func (u *UI) OK(format string, a ...any) {
	u.Emit(Event{Kind: KindOK, Text: fmt.Sprintf(format, a...)})
}
func (u *UI) Todo(format string, a ...any) {
	u.Emit(Event{Kind: KindTodo, Text: fmt.Sprintf(format, a...)})
}
func (u *UI) Warn(format string, a ...any) {
	u.Emit(Event{Kind: KindWarn, Text: fmt.Sprintf(format, a...)})
}
func (u *UI) Fail(format string, a ...any) {
	u.Emit(Event{Kind: KindFail, Text: fmt.Sprintf(format, a...)})
}

// Status sends an OSC 7501 report. progress < 0 means indeterminate; kind is
// only used with "blocked" (permission, question, auth).
func (u *UI) Status(state, msg string, progress int, kind string) {
	if !u.TTY {
		return
	}
	var b strings.Builder
	b.WriteString("\x1b]7501;state=" + state)
	if kind != "" {
		b.WriteString(":kind=" + kind)
	}
	if progress >= 0 && (state == "working" || state == "blocked") {
		fmt.Fprintf(&b, ":progress=%d", progress)
	}
	b.WriteString(":app=" + u.App)
	b.WriteString(":msg=" + base64.StdEncoding.EncodeToString([]byte(msg)))
	b.WriteString("\x1b\\")
	fmt.Fprint(u.Out, b.String())
}

// Notify shows a notification: Rex's OSC 777 inside a Rex terminal, otherwise
// a macOS notification.
func (u *UI) Notify(title, body string) {
	if u.NoNotify {
		return
	}
	if os.Getenv("REX_BLOCK") != "" && u.TTY {
		clean := func(s string) string { return strings.ReplaceAll(s, ";", ",") }
		fmt.Fprintf(u.Out, "\x1b]777;notify;%s;%s\x1b\\", clean(title), clean(body))
		return
	}
	q := func(s string) string { return strings.ReplaceAll(s, `"`, "") }
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "osascript", "-e",
		fmt.Sprintf(`display notification "%s" with title "%s"`, q(body), q(title))).Run()
}

// ErrNoPrompt is returned by Prompt, Secret and Confirm after DisablePrompts.
var ErrNoPrompt = errors.New("can't ask a question here")

var noPrompts atomic.Bool

// DisablePrompts makes every prompt fail instead of reading stdin. The TUI
// calls it: while it owns the terminal, a stray read would hang or eat keys.
func DisablePrompts() { noPrompts.Store(true) }

// Prompt reads a line from the terminal. It fails when stdin isn't a terminal.
func Prompt(question string) (string, error) {
	if noPrompts.Load() {
		return "", fmt.Errorf("%w: %s", ErrNoPrompt, strings.TrimSpace(question))
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("can't ask %q: stdin isn't a terminal", strings.TrimSpace(question))
	}
	fmt.Fprint(os.Stdout, question)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// Secret reads a line without echoing it, so a pasted token stays out of the
// terminal's scrollback.
func Secret(question string) (string, error) {
	if noPrompts.Load() {
		return "", fmt.Errorf("%w: %s", ErrNoPrompt, strings.TrimSpace(question))
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("can't ask %q: stdin isn't a terminal", strings.TrimSpace(question))
	}
	fmt.Fprint(os.Stdout, question)
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stdout)
	return strings.TrimSpace(string(b)), err
}

// Confirm asks a yes/no question; an empty answer is yes.
func Confirm(question string) bool {
	a, err := Prompt("  " + question + " [Y/n] ")
	return err == nil && (a == "" || strings.HasPrefix(strings.ToLower(a), "y"))
}

// Bar is the one-line progress bar exe-rex new redraws while it works.
type Bar struct {
	u          *UI
	mu         sync.Mutex
	start      time.Time
	stageStart time.Time
	lo, hi     int
	expect     time.Duration
	msg        string
	lastPct    int
	spin       int
	stop       chan struct{}
	done       chan struct{}
	OnStage    func(msg string) // called on Begin (for tracing)
}

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// NewBar starts a bar that redraws five times a second until Stop.
func (u *UI) NewBar() *Bar {
	b := &Bar{u: u, start: time.Now(), stageStart: time.Now(), lastPct: -1,
		stop: make(chan struct{}), done: make(chan struct{})}
	if u.TTY {
		fmt.Fprint(u.Out, "\x1b[?25l") // hide the cursor while drawing
	}
	go func() {
		defer close(b.done)
		t := time.NewTicker(200 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-b.stop:
				return
			case <-t.C:
				b.mu.Lock()
				b.draw()
				b.mu.Unlock()
			}
		}
	}()
	return b
}

// Elapsed is the time since the bar started.
func (b *Bar) Elapsed() time.Duration { return time.Since(b.start) }

// Begin starts a stage covering lo..hi percent. Inside a stage the bar creeps
// toward hi (t/(t+expected)), so it keeps moving but never claims the stage is done.
func (b *Bar) Begin(lo, hi int, expect time.Duration, msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lo, b.hi, b.expect, b.msg, b.stageStart = lo, hi, expect, msg, time.Now()
	if b.OnStage != nil {
		b.OnStage(msg)
	}
	if !b.u.TTY {
		b.u.Emit(Event{Kind: KindSection, Text: msg, Stage: true})
	}
	b.draw()
}

// Done prints a ✓ line for the stage that just finished.
func (b *Bar) Done(text string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.u.Emit(Event{Kind: KindOK, Text: text, Took: time.Since(b.stageStart)})
}

// Stop stops redrawing and clears the bar line.
func (b *Bar) Stop() {
	select {
	case <-b.stop:
		return
	default:
	}
	close(b.stop)
	<-b.done
	if b.u.TTY {
		fmt.Fprint(b.u.Out, "\r\x1b[2K\x1b[?25h")
	}
}

func (b *Bar) pct() int {
	t := time.Since(b.stageStart).Seconds()
	e := b.expect.Seconds()
	if e <= 0 {
		e = 1
	}
	return b.lo + int(float64(b.hi-b.lo)*t/(t+e))
}

// Line renders the bar for a terminal cols wide. The result is always shorter
// than cols: a wrapped line can't be redrawn with \r and leaves copies behind.
func Line(cols, pct int, msg, spin string, elapsed time.Duration) string {
	s := int(elapsed / time.Second)
	clock := fmt.Sprintf("%d:%02d", s/60, s%60)
	var head string
	switch w := 24; {
	case cols < 40: // too narrow for a bar: spinner, percent, message, clock
		head = fmt.Sprintf("%s %3d%%", spin, pct)
	default:
		if cols < 70 {
			w = 12
		}
		f := min(max(pct*w/100, 0), w)
		head = fmt.Sprintf("%s [%s%s] %3d%%", spin, strings.Repeat("█", f), strings.Repeat("░", w-f), pct)
	}
	room := cols - 1 - utf8.RuneCountInString(head) - 2 - 2 - len(clock)
	if r := []rune(msg); len(r) > room {
		if room < 2 {
			msg = ""
		} else {
			msg = string(r[:room-1]) + "…"
		}
	}
	l := head + "  " + msg + "  " + clock
	if msg == "" {
		l = head + "  " + clock
	}
	// Last resort for absurdly narrow panes: cut, never wrap.
	if r := []rune(l); len(r) > cols-1 {
		l = string(r[:max(cols-1, 0)])
	}
	return l
}

func (b *Bar) draw() {
	if b.msg == "" {
		return
	}
	p := b.pct()
	if p != b.lastPct {
		b.u.Status("working", b.msg, p, "")
		b.lastPct = p
	}
	if !b.u.TTY {
		return
	}
	cols := 80
	if c, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && c > 0 {
		cols = c
	}
	b.spin = (b.spin + 1) % len(spinner)
	line := Line(cols, p, b.msg, spinner[b.spin], time.Since(b.start))
	// Color the spinner and time without changing the visible width.
	line = strings.Replace(line, spinner[b.spin], "\x1b[36m"+spinner[b.spin]+"\x1b[0m", 1)
	fmt.Fprint(b.u.Out, "\r\x1b[2K"+line)
}
