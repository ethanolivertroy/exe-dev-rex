package cli

import (
	"slices"
	"testing"
	"time"

	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
)

func TestTakeJSON(t *testing.T) {
	rest, on := takeJSON([]string{"foo", "--json", "--cpu=4"})
	if !on || !slices.Equal(rest, []string{"foo", "--cpu=4"}) {
		t.Fatalf("got %q %v", rest, on)
	}
	if _, on := takeJSON([]string{"foo"}); on {
		t.Fatal("no --json")
	}
}

func TestRecorderSteps(t *testing.T) {
	r, u := newRecorder()
	u.Out = nil // keep the test quiet
	u.Step("Tailscale identity for the VMs")
	u.OK("exe.dev integration is set up")
	u.WouldAsk("Apply this?")
	u.Warn("a warning")
	u.Detail("with a second line")
	u.Line("")
	u.Emit(ui.Event{Kind: ui.KindSection, Text: "Creating the VM", Stage: true})
	u.Emit(ui.Event{Kind: ui.KindOK, Text: "Created the VM", Took: 1400 * time.Millisecond})
	u.Fail("broke")
	got := r.all()
	want := []Step{
		{"Tailscale identity for the VMs", "ok", "exe.dev integration is set up"},
		{"Tailscale identity for the VMs", "todo", "would ask: Apply this?"},
		{"Tailscale identity for the VMs", "warn", "a warning\nwith a second line"},
		{"Creating the VM", "ok", "Created the VM (1.4s)"},
		{"Creating the VM", "fail", "broke"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if !slices.Equal(r.with("todo"), []string{"would ask: Apply this?"}) {
		t.Fatalf("with(todo) = %q", r.with("todo"))
	}
}
