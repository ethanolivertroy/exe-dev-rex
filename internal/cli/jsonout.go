package cli

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"

	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
)

// --json: each command prints one JSON object on stdout and nothing else
// there. Every object has "ok"; a failure is {"ok": false, "error": "..."}
// with exit status 1 (2 for bad arguments). Human-readable progress still
// goes to stderr, without colours, so a caller can log it. --json never
// prompts: give a name, -y or --yes, and TS_API_KEY where a command needs them.

// reportedError has already been printed as JSON; main only sets the exit status.
type reportedError struct{ err error }

func (e *reportedError) Error() string { return e.err.Error() }
func (e *reportedError) Unwrap() error { return e.err }

// IsReported reports whether err was already printed (as JSON).
func IsReported(err error) bool { var r *reportedError; return errors.As(err, &r) }

// takeJSON removes --json from args.
func takeJSON(args []string) ([]string, bool) {
	out := args[:0:0]
	on := false
	for _, a := range args {
		if a == "--json" || a == "-json" {
			on = true
			continue
		}
		out = append(out, a)
	}
	return out, on
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// emit prints v merged with "ok", or the error. It returns err marked as
// reported, so main doesn't print it again.
func emit(v any, err error) error {
	m := map[string]any{}
	if v != nil {
		b, _ := json.Marshal(v)
		_ = json.Unmarshal(b, &m)
	}
	m["ok"] = err == nil
	if err != nil {
		m["error"] = err.Error()
		printJSON(m)
		if IsUsage(err) {
			return err // main exits 2; the message also goes to stderr
		}
		return &reportedError{err}
	}
	printJSON(m)
	return nil
}

// Step is one line of a command's report, for --json.
type Step struct {
	Section string `json:"section,omitempty"`
	Status  string `json:"status"` // ok, todo, warn, fail, info
	Message string `json:"message"`
}

// recorder is a UI for --json: it prints plain progress on stderr and keeps
// the Events as Steps.
type recorder struct {
	mu      sync.Mutex
	section string
	steps   []Step
}

func newRecorder() (*recorder, *ui.UI) {
	r := &recorder{}
	return r, ui.Tee(&ui.UI{Out: os.Stderr, App: "exe-dev"}, r.add)
}

func (r *recorder) add(e ui.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	text := strings.TrimSpace(e.Text)
	switch e.Kind {
	case ui.KindSection:
		r.section = text
		return
	case ui.KindDetail:
		if len(r.steps) > 0 {
			last := &r.steps[len(r.steps)-1]
			last.Message += "\n" + text
			return
		}
	case ui.KindInfo:
		if text == "" {
			return
		}
	}
	status := string(e.Kind)
	switch {
	case e.Kind == ui.KindDetail:
		status = "info"
	case e.Took > 0:
		text += " (" + e.TookString() + ")"
	case e.WouldAsk:
		text = "would ask: " + text
	}
	r.steps = append(r.steps, Step{Section: r.section, Status: status, Message: text})
}

func (r *recorder) all() []Step {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.steps == nil {
		return []Step{}
	}
	return r.steps
}

func (r *recorder) with(status string) []string {
	out := []string{}
	for _, s := range r.all() {
		if s.Status == status {
			out = append(out, s.Message)
		}
	}
	return out
}

// actions is all but the "info" lines: for new and rm, those are the summary
// card, which the result's own fields already carry.
func (r *recorder) actions() []Step {
	out := []Step{}
	for _, s := range r.all() {
		if s.Status != "info" {
			out = append(out, s)
		}
	}
	return out
}
