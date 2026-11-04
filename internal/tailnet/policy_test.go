package tailnet

import (
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

const basePolicy = `// My tailnet policy. Keep this comment!
{
	// Groups
	"groups": {"group:eng": ["a@example.com"]},
	"tagOwners": {
		"tag:server": ["autogroup:admin"], // servers
	},
	"grants": [
		{"src": ["*"], "dst": ["*"], "ip": ["tcp:22"]},
	],
}
`

func mustStd(t *testing.T, b []byte) string {
	t.Helper()
	s, err := hujson.Standardize(b)
	if err != nil {
		t.Fatalf("result isn't valid HuJSON: %v\n%s", err, b)
	}
	return string(s)
}

func TestEnsureExePolicyAddsAndKeepsComments(t *testing.T) {
	out, changes, err := EnsureExePolicy([]byte(basePolicy))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("want 2 changes, got %v", changes)
	}
	s := string(out)
	for _, want := range []string{"Keep this comment!", "// servers", "// Groups", `"tag:exe"`, `"tcp:7297"`} {
		if !strings.Contains(s, want) {
			t.Errorf("result lacks %q:\n%s", want, s)
		}
	}
	std := mustStd(t, out)
	if !strings.Contains(std, `"tag:server"`) || !strings.Contains(std, `"tcp:22"`) {
		t.Errorf("existing entries lost:\n%s", std)
	}
	// Idempotent: a second pass changes nothing.
	out2, changes2, err := EnsureExePolicy(out)
	if err != nil || len(changes2) != 0 || string(out2) != string(out) {
		t.Fatalf("second pass: changes=%v err=%v", changes2, err)
	}
}

func TestEnsureExePolicyEmptyFile(t *testing.T) {
	out, changes, err := EnsureExePolicy([]byte("{}\n"))
	if err != nil || len(changes) != 2 {
		t.Fatalf("changes=%v err=%v", changes, err)
	}
	std := mustStd(t, out)
	if !strings.Contains(std, `"tagOwners"`) || !strings.Contains(std, `"grants"`) {
		t.Fatalf("missing sections:\n%s", std)
	}
}

func TestEnsureExePolicyACLCovers(t *testing.T) {
	p := `{"tagOwners": {"tag:exe": ["autogroup:admin"]},
	"acls": [{"action": "accept", "src": ["autogroup:admin"], "dst": ["tag:exe:*"]}]}`
	_, changes, err := EnsureExePolicy([]byte(p))
	if err != nil || len(changes) != 0 {
		t.Fatalf("an ACL already allowing tag:exe:* should be enough; changes=%v err=%v", changes, err)
	}
}

func TestEnsureExePolicyWrongPortStillAdds(t *testing.T) {
	p := `{"tagOwners": {"tag:exe": ["autogroup:admin"]},
	"grants": [{"src": ["autogroup:admin"], "dst": ["tag:exe"], "ip": ["tcp:22"]}]}`
	_, changes, err := EnsureExePolicy([]byte(p))
	if err != nil || len(changes) != 1 {
		t.Fatalf("want the grant added; changes=%v err=%v", changes, err)
	}
}

func TestDiffShowsOnlyChanges(t *testing.T) {
	d := Diff([]byte("a\nb\nc\n"), []byte("a\nb\nx\nc\n"))
	if strings.TrimSpace(d) != "3 + x" {
		t.Fatalf("diff = %q", d)
	}
}
