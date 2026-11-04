package tui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/ethanolivertroy/exe-dev-rex/internal/config"
	"github.com/ethanolivertroy/exe-dev-rex/internal/exedev"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ops"
	"github.com/ethanolivertroy/exe-dev-rex/internal/resources"
	"github.com/ethanolivertroy/exe-dev-rex/internal/ui"
)

type newAnswers struct{ name, cpu, memory, disk string }

// wizard holds the settings screen's answers.
type wizard struct {
	node              string // ephemeral or persistent
	devicesCore       bool
	tag               string
	palette, links    bool
	cpu, memory, disk string
}

func (m *model) setForm(kind string, f *huh.Form) tea.Cmd {
	m.form, m.formKind, m.mode = f.WithShowHelp(true), kind, modeForm
	m.sizeForm()
	return m.form.Init()
}

func (m *model) openNew() tea.Cmd {
	c := m.cfg.New
	m.newVM = newAnswers{name: "vm-" + time.Now().Format("0102-1504"), memory: c.Memory, disk: c.Disk}
	if c.CPU > 0 {
		m.newVM.cpu = strconv.Itoa(c.CPU)
	}
	return m.newForm(true)
}

func (m *model) newForm(refresh bool) tea.Cmd {
	groups := []*huh.Group{huh.NewGroup(
		huh.NewInput().Title("Name for the new VM").Value(&m.newVM.name).
			Description("Lowercase letters, digits and dashes; start with a letter or digit.").Validate(func(s string) error {
			if !ops.ValidVMName(strings.TrimSpace(s)) {
				return errors.New("use lowercase letters, digits and dashes")
			}
			return nil
		}),
	).Title("New exe.dev VM · Name")}
	groups = append(groups, m.resourceGroups(&m.newVM.cpu, &m.newVM.memory, &m.newVM.disk, "New exe.dev VM")...)
	init := m.setForm("new", huh.NewForm(groups...))
	if refresh {
		return tea.Batch(init, m.fetchCapacity())
	}
	return init
}

const devicesCoreWarning = `exe-rex rm can then delete persistent devices itself. Be aware: any process
on any of your exe.dev VMs (an agent you run there, say) can then get a
Tailscale API token that can remove, expire or retag ANY machine on your
tailnet, not just these VMs. Without it you delete devices in the console.`

func (m *model) openWizard() tea.Cmd {
	w := wizard{node: "ephemeral", devicesCore: true, tag: "rex-host",
		palette: m.cfg.PaletteOn(), links: m.cfg.LinksOn(), memory: m.cfg.New.Memory, disk: m.cfg.New.Disk}
	if m.cfg.New.CPU > 0 {
		w.cpu = strconv.Itoa(m.cfg.New.CPU)
	}
	if m.loaded && m.ov.Tag != "" {
		w.tag = m.ov.Tag
		if !m.ov.Ephemeral {
			w.node = "persistent"
		}
	}
	m.wiz = w
	wz := &m.wiz
	groups := []*huh.Group{
		huh.NewGroup(
			huh.NewSelect[string]().Title("How should VMs join your tailnet?").
				Options(huh.NewOption("Ephemeral (recommended)", "ephemeral"), huh.NewOption("Persistent", "persistent")).
				Value(&wz.node).
				DescriptionFunc(func() string {
					if wz.node == "persistent" {
						return "Kept on your tailnet while a VM is off. Removing a VM then needs the\ndevices:core scope (next question) or a click in the Tailscale console."
					}
					return "Tailscale cleans up VMs you delete; the credential needs only auth keys.\nA VM paused 30–60+ min rejoins by itself, but that restarts Rex on it\n(sessions survive, programs running in them stop)."
				}, &wz.node),
		).Title("Settings 1/4 · Node type"),
		huh.NewGroup(
			huh.NewConfirm().Title("Let exe-rex rm delete Tailscale devices automatically (devices:core)?").
				Description(devicesCoreWarning).Affirmative("Yes, allow").Negative("No, I'll use the console").
				Value(&wz.devicesCore),
		).Title("Settings 1/4 · Device cleanup").WithHideFunc(func() bool { return wz.node != "persistent" }),
		huh.NewGroup(
			huh.NewInput().Title("exe.dev tag for exe-rex's VMs").Value(&wz.tag).
				Description("Only VMs with this tag get the Tailscale identity. Changing it re-tags VMs already on your tailnet.").
				Validate(func(s string) error {
					if !exedev.ValidTag(strings.TrimSpace(s)) {
						return errors.New("lowercase letters, digits and dashes")
					}
					return nil
				}),
		).Title("Settings 2/4 · VM tag"),
		huh.NewGroup(
			huh.NewConfirm().Title("Add New/Remove exe.dev VM… to the Rex command palette?").Value(&wz.palette),
			huh.NewConfirm().Title("Link exe-new, exe-rm and rex-app-host to exe-rex?").
				Description("Shorter names for the same commands.").Value(&wz.links),
		).Title("Settings 3/4 · Shortcuts"),
	}
	groups = append(groups, m.resourceGroups(&wz.cpu, &wz.memory, &wz.disk, "Settings 4/4 · Defaults for future VMs")...)
	return tea.Batch(m.setForm("wizard", huh.NewForm(groups...)), m.fetchCapacity())
}

func (m *model) askToken() tea.Cmd {
	m.token = ""
	return m.setForm("token", huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Tailscale API access token").
			Description("Some of these changes need one. Create it at\nhttps://login.tailscale.com/admin/settings/keys → Generate access token\n(1 day expiry is plenty). It's used in memory only and never saved;\nrevoke it afterwards.").
			EchoMode(huh.EchoModePassword).Value(&m.token).Validate(func(s string) error {
			if !strings.HasPrefix(strings.TrimSpace(s), "tskey-api-") {
				return errors.New("Tailscale API tokens start with tskey-api-")
			}
			return nil
		}),
	)))
}

func (m *model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	f, cmd := m.form.Update(msg)
	if ff, ok := f.(*huh.Form); ok {
		m.form = ff
	}
	switch m.form.State {
	case huh.StateAborted:
		m.form, m.mode = nil, modeTable
		if m.formKind == "token" {
			m.token = ""
		}
		return m, nil
	case huh.StateCompleted:
		kind := m.formKind
		m.form = nil
		switch kind {
		case "new":
			return m, m.reviewNew()
		case "wizard", "token":
			return m, m.plan()
		}
	}
	return m, cmd
}

func (m *model) createVM() tea.Cmd {
	a := m.newVM
	name := strings.TrimSpace(a.name)
	var extra []string
	if v := strings.TrimSpace(a.cpu); v != "" {
		extra = append(extra, "--cpu="+v)
	}
	if v := strings.TrimSpace(a.memory); v != "" {
		extra = append(extra, "--memory="+resources.SizeArgument(v))
	}
	if v := strings.TrimSpace(a.disk); v != "" {
		extra = append(extra, "--disk="+resources.SizeArgument(v))
	}
	return m.begin("new", name, "Creating "+name, func(ctx context.Context, u *ui.UI) (bool, error) {
		_, err := ops.CreateVMExact(ctx, u, name, extra)
		return false, err
	})
}

func (m *model) installOptions(dry bool) ops.InstallOptions {
	w := m.wiz
	return ops.InstallOptions{Dry: dry, Yes: !dry, Palette: w.palette, Links: w.links,
		Persistent: w.node == "persistent", AuthKeysOnly: w.node == "persistent" && !w.devicesCore,
		VMTag: strings.TrimSpace(w.tag), BinDir: m.cfg.BinDir(), Token: m.token}
}

// plan runs the installer as a dry run; its output is the review screen.
func (m *model) plan() tea.Cmd {
	o := m.installOptions(true)
	return m.begin("plan", "", "Checking what would change", func(ctx context.Context, u *ui.UI) (bool, error) {
		return ops.RunInstall(ctx, u, o)
	})
}

// apply runs the installer for real with the reviewed answers.
func (m *model) apply() tea.Cmd {
	o := m.installOptions(false)
	w := m.wiz
	return m.begin("apply", "", "Applying settings", func(ctx context.Context, u *ui.UI) (bool, error) {
		cfg, err := config.Load()
		if err != nil {
			return false, err
		}
		for k, v := range map[string]string{"new.cpu": w.cpu, "new.memory": w.memory, "new.disk": w.disk} {
			if err := config.Set(&cfg, k, strings.TrimSpace(v)); err != nil {
				return false, err
			}
		}
		if err := cfg.Save(); err != nil {
			return false, err
		}
		return ops.RunInstall(ctx, u, o)
	})
}

func (m *model) sizeForm() {
	if m.form == nil {
		return
	}
	if m.width > 0 {
		m.form.WithWidth(max(20, min(88, m.width-4)))
	}
	if m.height > 0 {
		m.form.WithHeight(max(8, m.height-14))
	}
}
