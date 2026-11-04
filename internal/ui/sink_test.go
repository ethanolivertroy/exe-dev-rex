package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSinkGetsEvents(t *testing.T) {
	var got []Event
	u := NewSink(func(e Event) { got = append(got, e) })
	u.Step("Tailscale")
	u.OK("fine %d", 1)
	u.Detail("more")
	u.WouldAsk("Apply this?")
	u.Status("working", "x", 5, "") // not a TTY: nothing
	want := []Event{
		{Kind: KindSection, Text: "Tailscale"},
		{Kind: KindOK, Text: "fine 1"},
		{Kind: KindDetail, Text: "more"},
		{Kind: KindTodo, Text: "Apply this?", WouldAsk: true},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestPlainRendering(t *testing.T) {
	var b strings.Builder
	u := &UI{Out: &b}
	u.Step("Tailscale")
	u.OK("fine")
	u.WouldAsk("Apply this?")
	u.Detail("more")
	u.Emit(Event{Kind: KindOK, Text: "Created the VM", Took: 1400 * time.Millisecond})
	want := "\n==> Tailscale\n  ✓ fine\n  → (dry run) would ask: Apply this?\n      more\n    done: Created the VM (1.4s)\n"
	if b.String() != want {
		t.Fatalf("got %q\nwant %q", b.String(), want)
	}
}

func TestDisablePrompts(t *testing.T) {
	DisablePrompts()
	defer noPrompts.Store(false)
	if _, err := Prompt("name? "); !errors.Is(err, ErrNoPrompt) {
		t.Fatalf("Prompt: %v", err)
	}
	if _, err := Secret("token? "); !errors.Is(err, ErrNoPrompt) {
		t.Fatalf("Secret: %v", err)
	}
	if Confirm("ok?") {
		t.Fatal("Confirm must say no")
	}
}
