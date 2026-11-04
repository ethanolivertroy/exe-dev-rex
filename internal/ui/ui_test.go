package ui

import (
	"testing"
	"time"
	"unicode/utf8"
)

// The bar must never wrap: a wrapped line breaks the \r redraw and the
// terminal fills with copies of it.
func TestLineFitsWidth(t *testing.T) {
	msg := "Installing Rex on the VM with a deliberately long message that goes on"
	for cols := 1; cols <= 200; cols++ {
		for _, pct := range []int{0, 37, 100} {
			l := Line(cols, pct, msg, "⠋", 83*time.Second)
			if n := utf8.RuneCountInString(l); n > cols-1 && cols > 1 {
				t.Fatalf("cols=%d pct=%d: %d runes: %q", cols, pct, n, l)
			}
		}
	}
}
