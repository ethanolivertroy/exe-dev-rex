package tailnet

import "testing"

func TestParsePing(t *testing.T) {
	relayed := "pong from vm (100.1.2.3) via DERP(nyc) in 80ms\npong from vm (100.1.2.3) via DERP(nyc) in 75ms\n"
	if r, err := parsePing(relayed, "vm"); err != nil || r.Direct || r.Via != "DERP(nyc)" || r.Latency != "75ms" {
		t.Errorf("relayed: %+v %v", r, err)
	}
	direct := "Warning: client version mismatch\npong from vm (100.1.2.3) via DERP(nyc) in 90ms\npong from vm (100.1.2.3) via 45.1.2.3:41641 in 49ms\n"
	if r, err := parsePing(direct, "vm"); err != nil || !r.Direct || r.Via != "45.1.2.3:41641" {
		t.Errorf("direct: %+v %v", r, err)
	}
	if _, err := parsePing("timeout waiting for ping reply\n", "vm"); err == nil {
		t.Error("no pong should be an error")
	}
}
