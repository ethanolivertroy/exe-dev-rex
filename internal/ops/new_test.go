package ops

import (
	"context"
	"errors"
	"github.com/ethanolivertroy/exe-dev-rex/internal/config"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
	"slices"
	"testing"
)

func TestStageFor(t *testing.T) {
	for m, want := range map[string]int{
		"": 1, "none": 1, "rex-install-bg": 1, "tailscale": 1, "something-new": 1,
		"rex-install": 2, "rex-start": 2, "setup-done": 3,
	} {
		if got := stageFor(m); got != want {
			t.Errorf("stageFor(%q) = %d, want %d", m, got, want)
		}
	}
	if lastStage != 3 || stageFor("setup-done") != lastStage {
		t.Fatalf("setup-done must map to the last stage (%d)", lastStage)
	}
}

func TestReviewedSizesDoNotReapplySavedDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := (config.Config{New: config.NewDefaults{CPU: 4, Memory: "8GB", Disk: "50GB"}}).Save(); err != nil {
		t.Fatal(err)
	}
	for _, defaults := range []bool{false, true} {
		w := newWorld()
		stop := errors.New("stop before creating")
		w.createErr = stop
		_, err := w.env().createVM(context.Background(), ui.NewSink(func(ui.Event) {}), "example", []string{"--cpu=2"}, defaults)
		if err == nil {
			t.Fatal("expected fake create failure")
		}
		want := []string{"--cpu=2"}
		if defaults {
			want = []string{"--memory=8GB", "--disk=50GB", "--cpu=2"}
		}
		if !slices.Equal(w.createArgs, want) {
			t.Fatalf("defaults=%v got %q want %q", defaults, w.createArgs, want)
		}
	}
}
