package tailnet

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tailscale/hujson"
)

// The policy exe-rex needs: VMs are tagged tag:exe (owned by admins), and admins
// can reach Rex on them. Rex on the VMs runs with --tailnet-allow-identified,
// so this grant decides who gets a shell.
const (
	ExeTag   = "tag:exe"
	RexPort  = "tcp:7297"
	TagOwner = "autogroup:admin"
)

// PolicyChange is one edit EnsureExePolicy made.
type PolicyChange string

// EnsureExePolicy returns policy with tag:exe and the Rex grant added if they're
// missing. It edits the HuJSON in place, so comments and formatting elsewhere
// in the file are kept. changes is empty when nothing was needed.
func EnsureExePolicy(policy []byte) (out []byte, changes []PolicyChange, err error) {
	v, err := hujson.Parse(policy)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing the policy file: %w", err)
	}
	std := v.Clone()
	std.Standardize()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(std.Pack(), &top); err != nil {
		return nil, nil, fmt.Errorf("the policy file isn't a JSON object: %w", err)
	}

	type op struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value"`
	}
	var ops []op

	var owners map[string][]string
	if raw, ok := top["tagOwners"]; ok {
		if err := json.Unmarshal(raw, &owners); err != nil {
			return nil, nil, fmt.Errorf("tagOwners: %w", err)
		}
		if _, ok := owners[ExeTag]; !ok {
			ops = append(ops, op{"add", "/tagOwners/" + ExeTag, []string{TagOwner}})
			changes = append(changes, "add tag:exe to tagOwners (owner autogroup:admin)")
		}
	} else {
		ops = append(ops, op{"add", "/tagOwners", map[string][]string{ExeTag: {TagOwner}}})
		changes = append(changes, "add a tagOwners section with tag:exe (owner autogroup:admin)")
	}

	if !hasRexAccess(top) {
		grant := map[string][]string{"src": {TagOwner}, "dst": {ExeTag}, "ip": {RexPort}}
		if _, ok := top["grants"]; ok {
			ops = append(ops, op{"add", "/grants/-", grant})
		} else {
			ops = append(ops, op{"add", "/grants", []any{grant}})
		}
		changes = append(changes, "add a grant: autogroup:admin → tag:exe on tcp:7297")
	}

	if len(ops) == 0 {
		return policy, nil, nil
	}
	patch, _ := json.Marshal(ops)
	if err := v.Patch(patch); err != nil {
		return nil, nil, fmt.Errorf("editing the policy file: %w", err)
	}
	return v.Pack(), changes, nil
}

// hasRexAccess reports whether a grant or ACL already lets admins reach
// tag:exe on port 7297.
func hasRexAccess(top map[string]json.RawMessage) bool {
	srcOK := func(src []string) bool { return contains(src, TagOwner) || contains(src, "*") }
	if raw, ok := top["grants"]; ok {
		var grants []struct{ Src, Dst, IP []string }
		if json.Unmarshal(raw, &grants) == nil {
			for _, g := range grants {
				if srcOK(g.Src) && (contains(g.Dst, ExeTag) || contains(g.Dst, "*")) &&
					(contains(g.IP, RexPort) || contains(g.IP, "*") || contains(g.IP, "tcp:*") || contains(g.IP, "7297")) {
					return true
				}
			}
		}
	}
	if raw, ok := top["acls"]; ok {
		var acls []struct {
			Action string
			Src    []string
			Dst    []string
		}
		if json.Unmarshal(raw, &acls) == nil {
			for _, a := range acls {
				if a.Action != "accept" || !srcOK(a.Src) {
					continue
				}
				for _, d := range a.Dst {
					if d == "*:*" || d == ExeTag+":*" || d == ExeTag+":7297" {
						return true
					}
				}
			}
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Diff returns the lines that differ between a and b, marked - and +, with
// line numbers from b. It's for showing a policy edit before applying it.
func Diff(a, b []byte) string {
	al, bl := strings.Split(string(a), "\n"), strings.Split(string(b), "\n")
	// Longest common subsequence; policy files are small.
	n, m := len(al), len(bl)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if al[i] == bl[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var out strings.Builder
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && al[i] == bl[j]:
			i, j = i+1, j+1
		case j < m && (i == n || lcs[i][j+1] >= lcs[i+1][j]):
			fmt.Fprintf(&out, "%4d + %s\n", j+1, bl[j])
			j++
		default:
			fmt.Fprintf(&out, "     - %s\n", al[i])
			i++
		}
	}
	return out.String()
}
