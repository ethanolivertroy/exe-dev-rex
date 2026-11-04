package ops

import (
	"context"
	"time"

	exedevrex "github.com/ethanolivertroy/exe-dev-rex"
	"github.com/ethanolivertroy/exe-dev-rex/internal/exedev"
	"github.com/ethanolivertroy/exe-dev-rex/internal/rexapp"
	"github.com/ethanolivertroy/exe-dev-rex/internal/tailnet"
)

// Env is everything CreateVM and RemoveVM reach outside this process: exe.dev
// (over SSH), this Mac's tailscaled, and Rex. Default is the real thing;
// tests swap in fakes and drive the whole workflow.
type Env struct {
	ExeDev  ExeDev
	Tailnet Tailnet
	Rex     Rex
	// Sleep waits between polls; it returns early when ctx ends.
	Sleep func(ctx context.Context, d time.Duration)
	// SetupScript is the first-boot script new VMs run (exe-setup.sh).
	SetupScript string
}

// ExeDev is the exe.dev side: the VM list, the Tailscale integration, and
// creating, reaching and deleting VMs.
type ExeDev interface {
	ListVMs(ctx context.Context) ([]exedev.VMInfo, error)
	TailscaleIntegration(ctx context.Context) (exedev.IntegrationState, error)
	Create(ctx context.Context, name, tag, script string, extra []string) (exedev.Created, error)
	Delete(ctx context.Context, name string) error
	// VM opens SSH to a VM; dest is its SSH destination ("" for name.exe.xyz).
	VM(name, dest string) (VMShell, error)
	ForgetHostKey(name string)
}

// VMShell runs things on one VM.
type VMShell interface {
	Host() string
	Script(ctx context.Context, script string) (string, error)
	// Stage is exe-setup.sh's newest EXE-STAGE marker.
	Stage(ctx context.Context) (string, error)
	Close()
}

// Tailnet is this Mac's view of the tailnet.
type Tailnet interface {
	Local(ctx context.Context) (*tailnet.Status, error)
}

// Rex is the Rex app and server on this Mac, and Rex on the VMs.
type Rex interface {
	Reachable(ctx context.Context, addr string, timeout time.Duration) error
	AddHost(ctx context.Context, o rexapp.AddOptions) error
	RemoveHost(ctx context.Context, endpoint string, restart bool) ([]string, error)
}

// Default is the real environment.
func Default() *Env {
	return &Env{ExeDev: realExeDev{}, Tailnet: realTailnet{}, Rex: realRex{},
		Sleep: sleep, SetupScript: exedevrex.SetupScript}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

type realExeDev struct{}

func (realExeDev) ListVMs(ctx context.Context) ([]exedev.VMInfo, error) { return exedev.ListVMs(ctx) }
func (realExeDev) TailscaleIntegration(ctx context.Context) (exedev.IntegrationState, error) {
	return exedev.TailscaleIntegration(ctx)
}
func (realExeDev) Create(ctx context.Context, name, tag, script string, extra []string) (exedev.Created, error) {
	return exedev.Create(ctx, name, tag, script, extra)
}
func (realExeDev) Delete(ctx context.Context, name string) error {
	_, err := exedev.Cmd(ctx, "rm", "--json", name)
	return err
}
func (realExeDev) VM(name, dest string) (VMShell, error) {
	vm, err := exedev.NewVM(name)
	if err != nil {
		return nil, err
	}
	vm.Dest = dest
	return vm, nil
}
func (realExeDev) ForgetHostKey(name string) { exedev.ForgetHostKey(name) }

type realTailnet struct{}

func (realTailnet) Local(ctx context.Context) (*tailnet.Status, error) { return tailnet.Local(ctx) }

type realRex struct{}

func (realRex) Reachable(ctx context.Context, addr string, timeout time.Duration) error {
	return rexapp.Reachable(ctx, addr, timeout)
}
func (realRex) AddHost(ctx context.Context, o rexapp.AddOptions) error { return rexapp.AddHost(ctx, o) }
func (realRex) RemoveHost(ctx context.Context, endpoint string, restart bool) ([]string, error) {
	return rexapp.RemoveHost(ctx, endpoint, restart)
}
