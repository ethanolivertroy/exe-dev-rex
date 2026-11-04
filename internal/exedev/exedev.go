// Package exedev drives exe.dev through its SSH interface (ssh exe.dev ...)
// and SSHes into its VMs.
package exedev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ethanolivertroy/exe-dev-rex/internal/run"
)

// Integration is the exe.dev integration exe-rex uses for Tailscale WIF.
const Integration = "tailscale-wif"

// Every `ssh exe.dev` in one exe-rex run shares a single SSH connection: the
// handshake is ~1s of each call's ~1.3s, so later calls take ~0.4s.
var (
	cmOnce sync.Once
	cmDir  string
)

func sshOpts() []string {
	cmOnce.Do(func() {
		// Unix socket paths are limited to ~104 bytes on macOS, so keep it short.
		if d, err := os.MkdirTemp("/tmp", "exe-rex-"); err == nil {
			cmDir = d
		}
	})
	o := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}
	if cmDir != "" {
		o = append(o, "-o", "ControlMaster=auto", "-o", "ControlPath="+filepath.Join(cmDir, "x"), "-o", "ControlPersist=60")
	}
	return append(o, "exe.dev")
}

// CloseShared ends the shared exe.dev connection, if one was opened.
func CloseShared() {
	if cmDir == "" {
		return
	}
	_ = exec.Command("ssh", "-o", "ControlPath="+filepath.Join(cmDir, "x"), "-O", "exit", "exe.dev").Run()
	_ = os.RemoveAll(cmDir)
}

// Cmd runs `ssh exe.dev args...` and returns its stdout.
func Cmd(ctx context.Context, args ...string) (string, error) {
	return run.Output(ctx, "ssh", append(sshOpts(), args...)...)
}

// CmdInput is Cmd with stdin.
func CmdInput(ctx context.Context, stdin string, args ...string) (string, error) {
	return run.Input(ctx, stdin, "ssh", append(sshOpts(), args...)...)
}

// VMInfo is one VM in `ls --json`.
type VMInfo struct {
	Name    string   `json:"vm_name"`
	SSHDest string   `json:"ssh_dest"`
	Status  string   `json:"status"`
	Tags    []string `json:"tags"`
	// CreatedAt and HTTPSURL are filled by ListVMs only.
	CreatedAt time.Time `json:"created_at"`
	HTTPSURL  string    `json:"https_url"`
}

// ListVMs lists your VMs (`ssh exe.dev ls --json`).
func ListVMs(ctx context.Context) ([]VMInfo, error) {
	out, err := Cmd(ctx, "ls", "--json")
	if err != nil {
		return nil, err
	}
	var r struct {
		VMs []VMInfo `json:"vms"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return nil, fmt.Errorf("ssh exe.dev ls --json: %w", err)
	}
	return r.VMs, nil
}

// VMs lists the names of your VMs.
func VMs(ctx context.Context) ([]string, error) {
	vms, err := ListVMs(ctx)
	var names []string
	for _, v := range vms {
		names = append(names, v.Name)
	}
	return names, err
}

// IntegrationState is what `integrations list --json` says about tailscale-wif.
type IntegrationState struct {
	Exists      bool
	Pending     bool // created, but no Tailscale Client ID yet
	AttachedAll bool // attached to auto:all
	Attachments []string
	Audience    string
	Issuer      string
	Subject     string
	ClientID    string
	Ephemeral   bool // VMs join as ephemeral nodes (metadata ephemeral, default true)
}

// VMTag returns the exe.dev VM tag the integration is attached to ("" if it
// isn't attached to a tag). exe-rex tags the VMs it creates with it, so this
// attachment is the one place the tag is configured.
func (s IntegrationState) VMTag() string {
	for _, a := range s.Attachments {
		if t, ok := strings.CutPrefix(a, "tag:"); ok {
			return t
		}
	}
	return ""
}

type integration struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Attachments []string          `json:"attachments"`
	Audience    string            `json:"audience"`
	Issuer      string            `json:"issuer"`
	Subject     string            `json:"subject"`
	Metadata    map[string]string `json:"metadata"`
}

// TailscaleIntegration reads the tailscale-wif integration's state.
func TailscaleIntegration(ctx context.Context) (IntegrationState, error) {
	out, err := Cmd(ctx, "integrations", "list", "--json")
	if err != nil {
		return IntegrationState{}, err
	}
	var all []integration
	if err := json.Unmarshal([]byte(out), &all); err != nil {
		return IntegrationState{}, fmt.Errorf("ssh exe.dev integrations list --json: %w", err)
	}
	for _, i := range all {
		if i.Name != Integration {
			continue
		}
		return IntegrationState{
			Exists:      true,
			Pending:     strings.HasSuffix(i.Audience, "/pending") || i.Metadata["client_id"] == "",
			AttachedAll: slices.Contains(i.Attachments, "auto:all"),
			Attachments: i.Attachments,
			Audience:    i.Audience,
			Issuer:      i.Issuer,
			Subject:     i.Subject,
			ClientID:    i.Metadata["client_id"],
			Ephemeral:   i.Metadata["ephemeral"] != "false",
		}, nil
	}
	return IntegrationState{}, nil
}

// CreateTailscaleIntegration creates tailscale-wif with a placeholder audience,
// attached to attach (such as tag:rex-host), and returns its Issuer and Subject.
func CreateTailscaleIntegration(ctx context.Context, attach string) (issuer, subject string, err error) {
	if _, err := Cmd(ctx, "integrations", "add", "wif", "--name", Integration,
		"--audience", "api.tailscale.com/pending", "--consumer", "tailscale", "--attach", attach); err != nil {
		return "", "", err
	}
	// Read them back as JSON rather than parsing add's human-readable output.
	st, err := TailscaleIntegration(ctx)
	if err != nil {
		return "", "", err
	}
	if st.Issuer == "" || st.Subject == "" {
		return "", "", fmt.Errorf("exe.dev created %s but didn't report its Issuer and Subject", Integration)
	}
	return st.Issuer, st.Subject, nil
}

// Created is what `new --json` reports. exe.dev doesn't document a successful
// response, so the fields are read leniently and Raw keeps all of it.
type Created struct {
	Name, SSHDest, HTTPSURL string
	Raw                     string
}

// Create runs `ssh exe.dev new --json` for a VM tagged tag, with script as
// its first-boot script (sent on stdin, so nothing is quoted or escaped).
func Create(ctx context.Context, name, tag, script string, extra []string) (Created, error) {
	args := append([]string{"new", "--name=" + name, "--json", "--setup-script=/dev/stdin"}, extra...)
	if tag != "" {
		args = append(args, "--tag="+tag)
	}
	out, err := CmdInput(ctx, script, args...)
	c := Created{Name: name, Raw: strings.TrimSpace(out)}
	var r map[string]any
	if json.Unmarshal([]byte(out), &r) == nil {
		if msg, ok := r["error"].(string); ok && msg != "" {
			return c, errors.New(strings.TrimSpace(msg))
		}
		if v, ok := r["vm"].(map[string]any); ok { // in case it's wrapped
			r = v
		}
		str := func(k string) string { s, _ := r[k].(string); return s }
		if n := str("vm_name"); n != "" {
			c.Name = n
		}
		c.SSHDest, c.HTTPSURL = str("ssh_dest"), str("https_url")
	}
	if err != nil {
		return c, err
	}
	return c, nil
}

// AttachIntegration attaches tailscale-wif to spec (vm:NAME, tag:NAME or auto:all).
func AttachIntegration(ctx context.Context, spec string) error {
	_, err := Cmd(ctx, "integrations", "attach", Integration, spec)
	return err
}

// DetachIntegration detaches tailscale-wif from spec.
func DetachIntegration(ctx context.Context, spec string) error {
	_, err := Cmd(ctx, "integrations", "detach", Integration, spec)
	return err
}

// TagVM adds tag to an existing VM.
func TagVM(ctx context.Context, vm, tag string) error {
	_, err := Cmd(ctx, "tag", vm, tag)
	return err
}

// ValidTag reports whether t is usable as an exe.dev VM tag. exe.dev doesn't
// document its rules, so this is the conservative subset VM names use.
func ValidTag(t string) bool { return tagRe.MatchString(t) }

var tagRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// SetTailscaleClient points tailscale-wif at a Tailscale trust credential and
// says how VMs join: exe-setup.sh reads client_id, tags and ephemeral from the
// integration's /metadata. edit replaces all metadata, so every key is passed.
func SetTailscaleClient(ctx context.Context, audience, clientID string, ephemeral bool) error {
	_, err := Cmd(ctx, "integrations", "edit", Integration, "--audience", audience,
		"--metadata=client_id="+clientID, "--metadata=tags=tag:exe",
		fmt.Sprintf("--metadata=ephemeral=%t", ephemeral))
	return err
}

// RemoveTailscaleIntegration deletes tailscale-wif.
func RemoveTailscaleIntegration(ctx context.Context) error {
	_, err := Cmd(ctx, "integrations", "remove", Integration)
	return err
}

// DefaultSetupScript returns exe.dev's default first-boot script ("" if none).
func DefaultSetupScript(ctx context.Context) (string, error) {
	out, err := Cmd(ctx, "defaults", "read", "dev.exe", "new.setup-script")
	if err != nil && out == "" {
		return "", nil // no default set
	}
	if strings.TrimSpace(out) == "(not set)" {
		return "", nil
	}
	return out, nil
}

// DeleteDefaultSetupScript removes the account-wide first-boot script.
func DeleteDefaultSetupScript(ctx context.Context) error {
	_, err := Cmd(ctx, "defaults", "delete", "dev.exe", "new.setup-script")
	return err
}

// SetDefaultSetupScript makes script the first-boot script for new VMs.
func SetDefaultSetupScript(ctx context.Context, script string) error {
	if _, err := CmdInput(ctx, script, "defaults", "write", "dev.exe", "new.setup-script"); err != nil {
		return err
	}
	got, err := DefaultSetupScript(ctx)
	if err != nil {
		return err
	}
	if strings.TrimRight(got, "\n") != strings.TrimRight(script, "\n") {
		return fmt.Errorf("exe.dev didn't store the setup script")
	}
	return nil
}

// VM is SSH access to one VM, sharing one connection between calls.
type VM struct {
	Name string
	Dest string // SSH destination; "" means Name.exe.xyz
	dir  string
}

// NewVM prepares SSH to name.exe.xyz. Close it to end the shared connection.
func NewVM(name string) (*VM, error) {
	// Unix socket paths are limited to ~104 bytes on macOS, so keep this short.
	dir, err := os.MkdirTemp("/tmp", "exe-rex-")
	if err != nil {
		return nil, err
	}
	return &VM{Name: name, dir: dir}, nil
}

// Host is the VM's SSH host name.
func (v *VM) Host() string {
	if v.Dest != "" {
		return v.Dest
	}
	return v.Name + ".exe.xyz"
}

func (v *VM) opts() []string {
	return []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5",
		// A fresh VM's host key isn't in known_hosts yet, and BatchMode can't prompt.
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ControlMaster=auto", "-o", "ControlPath=" + filepath.Join(v.dir, "cm"), "-o", "ControlPersist=120"}
}

// Run runs a shell command on the VM.
func (v *VM) Run(ctx context.Context, command string) (string, error) {
	return run.Output(ctx, "ssh", append(v.opts(), v.Host(), command)...)
}

// Script runs a bash script on the VM (bash -s).
func (v *VM) Script(ctx context.Context, script string) (string, error) {
	return run.Input(ctx, script, "ssh", append(v.opts(), v.Host(), "bash -s")...)
}

// Close ends the shared SSH connection.
func (v *VM) Close() {
	_ = exec.Command("ssh", "-o", "ControlPath="+filepath.Join(v.dir, "cm"), "-O", "exit", v.Host()).Run()
	os.RemoveAll(v.dir)
}

// Stage is the newest EXE-STAGE marker exe-setup.sh wrote, or "none" once SSH
// works but the script hasn't logged one yet.
func (v *VM) Stage(ctx context.Context) (string, error) {
	out, err := v.Run(ctx, `grep -ao "EXE-STAGE [a-z-]*" /tmp/exe-setup.log 2>/dev/null | tail -1 | cut -d" " -f2; true`)
	if err != nil {
		return "", err
	}
	if s := strings.TrimSpace(out); s != "" {
		return s, nil
	}
	return "none", nil
}

// ForgetHostKey removes the VM from ~/.ssh/known_hosts.
func ForgetHostKey(name string) {
	_ = exec.Command("ssh-keygen", "-R", name+".exe.xyz").Run()
}
