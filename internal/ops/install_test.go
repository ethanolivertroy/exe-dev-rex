package ops

import (
	"strings"
	"testing"
)

func TestRemoveRequireBlock(t *testing.T) {
	in := "-- mine\nlocal x = 1\n" + requireBlock + "-- after\n"
	got, ok := withoutRequire(in)
	if !ok || got != "-- mine\nlocal x = 1\n-- after\n" {
		t.Fatalf("got %q %v", got, ok)
	}
	hand := "require('exe-dev')\n"
	if got, ok := withoutRequire(hand); ok || got != hand {
		t.Fatal("a hand-written require must stay")
	}
	if !strings.Contains(requireBlock, `require("exe-dev")`) {
		t.Fatal("requireBlock must load exe-dev")
	}
}
