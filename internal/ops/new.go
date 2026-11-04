package ops

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sync"
	"time"

	"github.com/ethanolivertroy/exe-dev-rex/internal/config"
	"github.com/ethanolivertroy/exe-dev-rex/internal/exedev"
	"github.com/ethanolivertroy/exe-dev-rex/internal/rexapp"
	"github.com/ethanolivertroy/exe-dev-rex/internal/trace"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
)

var vmName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// stage is one step of the VM's setup, as the progress bar shows it.
type stage struct {
	lo, hi int
	expect time.Duration
	msg    string
	done   string
}

// The stages after "Created VM", in order. expect values are estimates;
// the ✓ lines show the real durations.
var vmStages = []stage{
	{15, 25, 15 * time.Second, "Booting the VM", "VM booted"},
	{25, 45, 10 * time.Second, "Joining your tailnet", "Joined your tailnet"},
	{45, 80, 10 * time.Second, "Installing and starting Rex", "Rex installed and running"},
	{80, 90, 10 * time.Second, "Reaching Rex over the tailnet", "Rex answers over the tailnet"},
}

// lastStage is the stage that ends when Rex answers.
var lastStage = len(vmStages) - 1

// stageFor maps exe-setup.sh's newest EXE-STAGE marker to the stage it starts.
func stageFor(marker string) int {
	switch marker {
	case "rex-install", "rex-start":
		return 2
	case "setup-done":
		return 3
	default: // none, rex-install-bg, tailscale, or a marker this version doesn't know
		return 1
	}
}

// NewResult is what CreateVM made.
type NewResult struct {
	Name     string  `json:"name"`
	Endpoint string  `json:"endpoint"`      // Rex host address, <name>.<tailnet>:7297 is implied
	Label    string  `json:"sidebar_label"` // its name in the Rex sidebar
	HTTPSURL string  `json:"https_url,omitempty"`
	SSH      string  `json:"ssh"` // ssh destination
	Seconds  float64 `json:"seconds"`
}

// ValidVMName reports whether exe.dev accepts name.
func ValidVMName(name string) bool { return vmName.MatchString(name) }

// CreateVM is exe-rex new without the questions: it creates name (with extra
// ssh exe.dev new args, plus the config file's default size for anything
// extra doesn't set), waits for Rex on it and adds it to the Rex sidebar.
// All output goes to u.
func CreateVM(ctx context.Context, u *ui.UI, name string, extra []string) (NewResult, error) {
	return Default().CreateVM(ctx, u, name, extra)
}

// CreateVM is CreateVM in e.
func (e *Env) CreateVM(ctx context.Context, u *ui.UI, name string, extra []string) (res NewResult, err error) {
	return e.createVM(ctx, u, name, extra, true)
}

// CreateVMExact creates a VM using only the supplied size flags. The TUI has
// already resolved saved defaults; blank fields must remain service defaults.
func CreateVMExact(ctx context.Context, u *ui.UI, name string, extra []string) (NewResult, error) {
	return Default().createVM(ctx, u, name, extra, false)
}

func (e *Env) createVM(ctx context.Context, u *ui.UI, name string, extra []string, defaults bool) (res NewResult, err error) {
	started := time.Now()
	if !vmName.MatchString(name) {
		return res, invalid("VM names are lowercase letters, digits and dashes")
	}
	if defaults {
		if cfg, err := config.Load(); err == nil {
			extra = append(cfg.New.Args(extra), extra...)
		} else {
			u.Warn("ignoring the config file: %v", err)
		}
	}

	tr := trace.Open("exe-new", name)
	defer tr.Close()
	tr.Printf("start name=%s args=%q", name, extra)

	// The tag lives where tailscale-wif is attached (exe-rex install sets it);
	// read it while checking the tailnet.
	type integ struct {
		s   exedev.IntegrationState
		err error
	}
	ich := make(chan integ, 1)
	go func() { s, err := e.ExeDev.TailscaleIntegration(ctx); ich <- integ{s, err} }()
	st, err := e.Tailnet.Local(ctx)
	if err != nil {
		return res, err
	}
	ig := <-ich
	if ig.err != nil {
		return res, fmt.Errorf("reading the %s integration: %w", exedev.Integration, ig.err)
	}
	if !ig.s.Exists || ig.s.Pending {
		return res, fmt.Errorf("the %s integration isn't set up; run: exe-rex install", exedev.Integration)
	}
	tag := ig.s.VMTag()
	tr.Printf("integration attachments=%v tag=%q", ig.s.Attachments, tag)
	if tag == "" && !ig.s.AttachedAll {
		return res, fmt.Errorf("%s isn't attached to a tag; run: exe-rex install", exedev.Integration)
	}
	ep := name + "." + st.MagicDNSSuffix

	u.Header("creating " + u.Bold(name))
	bar := u.NewBar()
	bar.OnStage = func(m string) { tr.Printf("stage: %s", m) }
	fail := func(msg string) error {
		bar.Stop()
		tr.Printf("fail: %s", msg)
		u.Status("error", msg, -1, "")
		u.Notify("exe.dev: "+name+" failed", msg)
		return errors.New(msg)
	}

	// 1. Create the VM.
	bar.Begin(0, 15, 10*time.Second, "Creating the VM")
	created, err := e.ExeDev.Create(ctx, name, tag, e.SetupScript, extra)
	tr.Printf("new --json: %s", created.Raw)
	if err != nil {
		return res, fail(fmt.Sprintf("exe.dev couldn't create %s: %v", name, err))
	}
	bar.Done("Created the VM")

	// 2. Watch the VM set up. Pollers report through channels, so the bar keeps
	// moving while SSH or Rex calls are slow.
	vm, err := e.ExeDev.VM(name, created.SSHDest)
	if err != nil {
		return res, fail(err.Error())
	}
	defer vm.Close()
	pctx, stop := context.WithCancel(ctx)
	defer stop()
	markers := make(chan string, 1)
	ready := make(chan string, 1) // the IP once Rex answers
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // the VM's newest stage marker
		defer wg.Done()
		for pctx.Err() == nil {
			c, cancel := context.WithTimeout(pctx, 8*time.Second)
			m, err := vm.Stage(c)
			cancel()
			if err == nil {
				select {
				case markers <- m:
				default: // the main loop hasn't read the last one; it'll get the next
				}
			}
			e.Sleep(pctx, time.Second)
		}
	}()
	go func() { // the tailnet peer, then Rex on its IP
		defer wg.Done()
		ip := ""
		for ip == "" && pctx.Err() == nil {
			if s, err := e.Tailnet.Local(pctx); err == nil {
				if p, ok := s.Find(name); ok {
					ip = p.IPv4()
				}
			}
			if ip == "" {
				e.Sleep(pctx, 500*time.Millisecond)
			}
		}
		if ip == "" {
			return
		}
		tr.Printf("peer seen ip=%s", ip)
		for n := 1; pctx.Err() == nil; n++ {
			if err := e.Rex.Reachable(pctx, ip+":7297", 1500*time.Millisecond); err == nil {
				tr.Printf("rex attempt %d via %s ok", n, ip)
				ready <- ip
				return
			} else {
				tr.Printf("rex attempt %d via %s failed: %v", n, ip, err)
			}
			e.Sleep(pctx, 500*time.Millisecond)
		}
	}()

	cur := -1
	enter := func(want int) {
		for cur < want {
			if cur >= 0 {
				bar.Done(vmStages[cur].done)
			}
			cur++
			s := vmStages[cur]
			bar.Begin(s.lo, s.hi, s.expect, s.msg)
		}
	}
	enter(0)
	deadline := time.After(5 * time.Minute)
	var ip string
wait:
	for {
		select {
		case m := <-markers:
			if i := stageFor(m); i > cur {
				enter(i)
			}
		case ip = <-ready:
			break wait
		case <-deadline:
			stop()
			return res, fail("Rex on " + name + " didn't come up in 5 minutes; see /tmp/exe-setup.log on the VM")
		case <-ctx.Done():
			stop()
			return res, fail("interrupted")
		}
	}
	stop()
	wg.Wait()
	enter(lastStage)
	bar.Done(vmStages[lastStage].done)

	// For the trace: once the VM has joined, its name should resolve.
	if tr != nil {
		addrs, err := net.DefaultResolver.LookupHost(ctx, ep)
		tr.Printf("dns %s: %v %v", ep, addrs, err)
		if err := e.Rex.Reachable(ctx, ep, 3*time.Second); err != nil {
			tr.Printf("rex via name failed: %v", err)
		} else {
			tr.Printf("rex via name ok")
		}
	}

	// 3. The Rex app's sidebar (restarts Rex, about a second).
	bar.Begin(90, 100, 3*time.Second, "Adding "+name+" to the Rex sidebar (Rex restarts)")
	if err := e.Rex.AddHost(ctx, rexapp.AddOptions{
		Label: name + " - exe.dev", Endpoint: ep, Via: ip + ":7297", Restart: true,
	}); err != nil {
		return res, fail("couldn't add " + name + " to the Rex app: " + err.Error())
	}
	bar.Done("Added to the Rex sidebar")
	bar.Stop()
	total := time.Since(started)
	t := fmt.Sprintf("%.0fs", total.Seconds())
	if total >= time.Minute {
		t = fmt.Sprintf("%d:%02d", int(total.Seconds())/60, int(total.Seconds())%60)
	}

	u.Rule()
	u.Line(fmt.Sprintf("  %s %s is ready %s", u.Green("●"), u.Bold(name), u.Dim("in "+t)))
	u.Line("")
	u.Field("Rex", name+" - exe.dev "+u.Dim("(in the sidebar)"))
	if created.HTTPSURL != "" {
		u.Field("Web", created.HTTPSURL)
	}
	u.Field("SSH", "ssh "+vm.Host())
	u.Field("Remove", u.Dim("exe-rex rm "+name))
	u.Line("")
	tr.Printf("done total=%s", t)
	u.Status("done", fmt.Sprintf("%s is ready in Rex (%s)", name, t), -1, "")
	u.Notify("exe.dev: "+name+" is ready", "Ready in the Rex sidebar after "+t)
	return NewResult{Name: name, Endpoint: ep, Label: name + " - exe.dev", HTTPSURL: created.HTTPSURL,
		SSH: vm.Host(), Seconds: total.Seconds()}, nil
}
