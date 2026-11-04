package ops

import "testing"

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
