package exedev

import "testing"

func TestParsePlan(t *testing.T) {
	p, err := parsePlan(`{"plan":"Individual","tier":"Small","max_cpus":2,"max_memory_gb":8,"pooled_disk_gb":100,"max_disk_gb":1024,"default_disk_gb":25,"max_vms":50}`)
	if err != nil || p.MaxCPUs != 2 || p.MaxMemoryGB != 8 || p.IncludedDiskGB != 100 || p.MaxDiskGB != 1024 || p.DefaultDiskGB != 25 {
		t.Fatalf("%+v %v", p, err)
	}
	for _, raw := range []string{`garbage`, `{}`, `null`, `{"error":"no plan"}`} {
		if _, err := parsePlan(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	// Forward-compatible: unknown capacities remain unknown, not guessed.
	p, err = parsePlan(`{"plan":"Work","future":true}`)
	if err != nil || p.MaxCPUs != 0 {
		t.Fatalf("%+v %v", p, err)
	}
}
