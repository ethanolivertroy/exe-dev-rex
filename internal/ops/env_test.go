package ops

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethanolivertroy/exe-dev-rex/internal/exedev"
	"github.com/ethanolivertroy/exe-dev-rex/internal/rexapp"
	"github.com/ethanolivertroy/exe-dev-rex/internal/tailnet"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
)

// world is exe.dev, the tailnet and Rex in memory. It implements ExeDev,
// Tailnet and Rex, so a test drives the real workflow against it.
type world struct {
	mu sync.Mutex

	// exe.dev
	vms        []exedev.VMInfo
	integ      exedev.IntegrationState
	createErr  error
	deleteErr  error
	createArgs []string
	created    []string // tag of each Create
	deleted    []string
	forgot     []string
	stages     []string // markers Stage returns in turn; the last one repeats
	scriptOut  string   // what removeDeviceScript prints
	scriptErr  error

	// tailnet
	peers     map[string]tailnet.Peer
	joinAfter int // Local calls before a newly created VM shows up
	locals    int
	pending   map[string]tailnet.Peer // created, not joined yet

	// Rex
	added   []rexapp.AddOptions
	removed []string
	gone    []string // what RemoveHost reports removed
}

func newWorld() *world {
	return &world{
		integ: exedev.IntegrationState{Exists: true, Attachments: []string{"tag:rex-host"}, Ephemeral: true},
		peers: map[string]tailnet.Peer{}, pending: map[string]tailnet.Peer{},
	}
}

func (w *world) env() *Env {
	return &Env{ExeDev: w, Tailnet: w, Rex: w, SetupScript: "#!/bin/sh\n",
		Sleep: func(ctx context.Context, _ time.Duration) {
			select {
			case <-ctx.Done():
			case <-time.After(time.Millisecond):
			}
		}}
}

func (w *world) ListVMs(context.Context) ([]exedev.VMInfo, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.vms), nil
}
func (w *world) TailscaleIntegration(context.Context) (exedev.IntegrationState, error) {
	return w.integ, nil
}
func (w *world) Create(_ context.Context, name, tag, _ string, args []string) (exedev.Created, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.createArgs = slices.Clone(args)
	if w.createErr != nil {
		return exedev.Created{}, w.createErr
	}
	w.created = append(w.created, tag)
	w.vms = append(w.vms, exedev.VMInfo{Name: name, Tags: []string{tag}})
	w.pending[name] = tailnet.Peer{HostName: name, TailscaleIPs: []string{"100.64.0.9", "fd7a::9"}}
	return exedev.Created{Name: name, SSHDest: name + ".exe.xyz", HTTPSURL: "https://" + name + ".exe.xyz"}, nil
}
func (w *world) Delete(_ context.Context, name string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.deleteErr != nil {
		return w.deleteErr
	}
	w.deleted = append(w.deleted, name)
	return nil
}
func (w *world) VM(name, _ string) (VMShell, error) { return &fakeVM{w: w, name: name}, nil }
func (w *world) ForgetHostKey(name string)          { w.forgot = append(w.forgot, name) }

func (w *world) Local(context.Context) (*tailnet.Status, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.locals++
	if w.locals > w.joinAfter {
		for n, p := range w.pending {
			w.peers[n] = p
			delete(w.pending, n)
		}
	}
	return &tailnet.Status{BackendState: "Running", MagicDNSSuffix: "tail1.ts.net", Peer: clonePeers(w.peers)}, nil
}

func clonePeers(m map[string]tailnet.Peer) map[string]tailnet.Peer {
	out := map[string]tailnet.Peer{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (w *world) Reachable(_ context.Context, addr string, _ time.Duration) error {
	if strings.HasPrefix(addr, "100.64.0.9:") {
		return nil
	}
	return errors.New("connection refused")
}
func (w *world) AddHost(_ context.Context, o rexapp.AddOptions) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.added = append(w.added, o)
	return nil
}
func (w *world) RemoveHost(_ context.Context, endpoint string, _ bool) ([]string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.removed = append(w.removed, endpoint)
	return w.gone, nil
}

type fakeVM struct {
	w    *world
	name string
	n    int
}

func (v *fakeVM) Host() string { return v.name + ".exe.xyz" }
func (v *fakeVM) Close()       {}
func (v *fakeVM) Stage(context.Context) (string, error) {
	v.w.mu.Lock()
	defer v.w.mu.Unlock()
	if len(v.w.stages) == 0 {
		return "none", nil
	}
	m := v.w.stages[min(v.n, len(v.w.stages)-1)]
	v.n++
	return m, nil
}
func (v *fakeVM) Script(context.Context, string) (string, error) {
	v.w.mu.Lock()
	defer v.w.mu.Unlock()
	if v.w.scriptErr == nil && strings.Contains(v.w.scriptOut, "LOGGED-OUT") {
		delete(v.w.peers, v.name) // an ephemeral node leaves on logout
	}
	return v.w.scriptOut, v.w.scriptErr
}

// sinkUI records the Events a workflow emits.
func sinkUI(t *testing.T) (*ui.UI, func() []ui.Event) {
	t.Helper()
	t.Setenv("EXE_NEW_TRACE", "")            // no trace file
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no config file
	var mu sync.Mutex
	var got []ui.Event
	u := ui.NewSink(func(e ui.Event) { mu.Lock(); got = append(got, e); mu.Unlock() })
	return u, func() []ui.Event { mu.Lock(); defer mu.Unlock(); return slices.Clone(got) }
}

func texts(evs []ui.Event, k ui.Kind) []string {
	var out []string
	for _, e := range evs {
		if e.Kind == k {
			out = append(out, e.Text)
		}
	}
	return out
}

func TestCreateVM(t *testing.T) {
	w := newWorld()
	w.stages = []string{"tailscale", "rex-install", "setup-done"}
	w.joinAfter = 3
	u, events := sinkUI(t)

	res, err := w.env().CreateVM(context.Background(), u, "alpha", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.created, []string{"rex-host"}) {
		t.Errorf("created with tags %q, want the integration's tag", w.created)
	}
	want := rexapp.AddOptions{Label: "alpha - exe.dev", Endpoint: "alpha.tail1.ts.net", Via: "100.64.0.9:7297", Restart: true}
	if len(w.added) != 1 || w.added[0] != want {
		t.Errorf("AddHost %+v, want %+v", w.added, want)
	}
	if res.Endpoint != want.Endpoint || res.SSH != "alpha.exe.xyz" || res.HTTPSURL != "https://alpha.exe.xyz" {
		t.Errorf("result %+v", res)
	}
	// Every stage ends with its ✓, in order, whichever markers were seen.
	stages := texts(events(), ui.KindOK)
	wantStages := []string{"Created the VM", "VM booted", "Joined your tailnet",
		"Rex installed and running", "Rex answers over the tailnet", "Added to the Rex sidebar"}
	if !slices.Equal(stages, wantStages) {
		t.Errorf("stages\n got %q\nwant %q", stages, wantStages)
	}
}

func TestCreateVMRefuses(t *testing.T) {
	for name, tc := range map[string]struct {
		vm    string
		integ exedev.IntegrationState
		want  string
	}{
		"bad name":      {"Not_OK", exedev.IntegrationState{Exists: true, Attachments: []string{"tag:rex-host"}}, "lowercase"},
		"no integ":      {"alpha", exedev.IntegrationState{}, "run: exe-rex install"},
		"pending integ": {"alpha", exedev.IntegrationState{Exists: true, Pending: true}, "run: exe-rex install"},
		"no tag":        {"alpha", exedev.IntegrationState{Exists: true}, "isn't attached to a tag"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld()
			w.integ = tc.integ
			u, _ := sinkUI(t)
			_, err := w.env().CreateVM(context.Background(), u, tc.vm, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
			if len(w.created) > 0 {
				t.Fatal("must not create a VM")
			}
		})
	}
}

func TestCreateVMFailsCleanly(t *testing.T) {
	w := newWorld()
	w.createErr = errors.New("quota exceeded")
	u, _ := sinkUI(t)
	_, err := w.env().CreateVM(context.Background(), u, "alpha", nil)
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("err = %v", err)
	}
	if len(w.added) > 0 {
		t.Fatal("nothing goes in the sidebar when exe.dev fails")
	}
}

// existing is a world with VM alpha on the tailnet and in the sidebar.
func existing() *world {
	w := newWorld()
	w.vms = []exedev.VMInfo{{Name: "alpha", SSHDest: "alpha.exe.xyz"}}
	w.peers["alpha"] = tailnet.Peer{HostName: "alpha", TailscaleIPs: []string{"100.64.0.9"}}
	w.gone = []string{"alpha - exe.dev"}
	return w
}

func TestRemoveVMEphemeral(t *testing.T) {
	w := existing()
	w.scriptOut = "EXE-REX-LOGGED-OUT\n"
	u, _ := sinkUI(t)
	res, err := w.env().RemoveVM(context.Background(), u, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if res.Tailscale != "logged-out" || !res.Sidebar {
		t.Errorf("result %+v", res)
	}
	if !slices.Equal(w.removed, []string{"alpha.tail1.ts.net"}) || !slices.Equal(w.deleted, []string{"alpha"}) ||
		!slices.Equal(w.forgot, []string{"alpha"}) {
		t.Errorf("removed=%q deleted=%q forgot=%q", w.removed, w.deleted, w.forgot)
	}
}

func TestRemoveVMPersistentWithoutDevicesCore(t *testing.T) {
	w := existing()
	w.scriptErr = errors.New("exit status 1: Tailscale API said HTTP 403")
	u, events := sinkUI(t)
	res, err := w.env().RemoveVM(context.Background(), u, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if res.Tailscale != "left" || !slices.Equal(w.deleted, []string{"alpha"}) {
		t.Fatalf("the VM still goes, its device is left: %+v deleted=%q", res, w.deleted)
	}
	warns := texts(events(), ui.KindWarn)
	if len(warns) != 1 || !strings.Contains(warns[0], "auth keys only") {
		t.Errorf("warnings %q", warns)
	}
	if !slices.ContainsFunc(texts(events(), ui.KindInfo), func(s string) bool { return strings.Contains(s, "Left to do:") }) {
		t.Error("tells the user what's left to do")
	}
}

func TestRemoveVMUnknown(t *testing.T) {
	w := existing()
	u, _ := sinkUI(t)
	if _, err := w.env().RemoveVM(context.Background(), u, "nope"); err == nil {
		t.Fatal("want an error for a VM that doesn't exist")
	}
	if len(w.removed)+len(w.deleted) > 0 {
		t.Fatal("must change nothing")
	}
}

func TestRemoveVMDeleteFails(t *testing.T) {
	w := existing()
	w.scriptOut = "EXE-REX-DELETED\n"
	w.deleteErr = errors.New("ssh: timeout")
	u, _ := sinkUI(t)
	_, err := w.env().RemoveVM(context.Background(), u, "alpha")
	if err == nil || !strings.Contains(err.Error(), "couldn't delete alpha") {
		t.Fatalf("err = %v", err)
	}
	if len(w.forgot) > 0 {
		t.Fatal("keep the host key while the VM still exists")
	}
}
