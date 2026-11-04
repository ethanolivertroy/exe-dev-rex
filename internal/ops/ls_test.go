package ops

import (
	"testing"
	"time"
)

func TestAgeString(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second: "30s", 12 * time.Minute: "12m", 90 * time.Minute: "1h", 49 * time.Hour: "2d",
	} {
		if got := AgeString(d); got != want {
			t.Errorf("AgeString(%v) = %s, want %s", d, got, want)
		}
	}
}
