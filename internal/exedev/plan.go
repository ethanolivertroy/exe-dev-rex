package exedev

import (
	"context"
	"encoding/json"
	"fmt"
)

// Plan contains reported limits, not a hardcoded catalog of subscription tiers.
// A zero capacity is unknown. Included disk is an allowance, not a hard limit.
type Plan struct {
	Name           string `json:"plan"`
	Tier           string `json:"tier"`
	MaxCPUs        int    `json:"max_cpus"`
	MaxMemoryGB    int64  `json:"max_memory_gb"`
	IncludedDiskGB int64  `json:"pooled_disk_gb"`
	MaxDiskGB      int64  `json:"max_disk_gb"`
	DefaultDiskGB  int64  `json:"default_disk_gb"`
}

func BillingPlan(ctx context.Context) (Plan, error) {
	out, err := Cmd(ctx, "billing", "plan", "--json")
	if err != nil {
		return Plan{}, err
	}
	return parsePlan(out)
}

func parsePlan(out string) (Plan, error) {
	var p Plan
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		return p, fmt.Errorf("reading billing plan: %w", err)
	}
	if p.Name == "" || p.MaxCPUs < 0 || p.MaxMemoryGB < 0 || p.MaxDiskGB < 0 || p.IncludedDiskGB < 0 || p.DefaultDiskGB < 0 {
		return Plan{}, fmt.Errorf("billing plan did not report a recognized plan")
	}
	return p, nil
}
