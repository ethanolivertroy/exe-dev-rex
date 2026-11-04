// Package rexapp edits the Rex Mac app's own host list (the sidebar), and the
// Rex server's host list that `rex hosts` shows.
//
// The app shows only its prefs key RexSync.servers (domain
// com.superlogical.rex), JSON stored as plist data, and never shows hosts
// added with `rex hosts add`. This is an unsupported internal format (seen in
// Rex Beta 0.1.0 build 1758), so Parse refuses anything that doesn't look
// exactly like it. Tested on that build: a running app doesn't reload the
// list, and doesn't overwrite it on quit, so edits show at the next launch.
package rexapp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"howett.net/plist"

	"github.com/ethanolivertroy/exe-dev-rex/internal/run"
)

const (
	Domain = "com.superlogical.rex"
	Key    = "RexSync.servers"
)

var knownFields = map[string]bool{"icon": true, "id": true, "lastHostID": true,
	"lastInstanceID": true, "locators": true, "name": true, "preferredURL": true}

// List is the app's host list.
type List struct {
	top     map[string]any
	Records []map[string]any
}

// Host is one entry, for display.
type Host struct {
	ID, Name string
	Locators []string
}

// ReadRaw returns the stored JSON (an empty list if the key is missing).
func ReadRaw(ctx context.Context) ([]byte, error) {
	out, err := run.Output(ctx, "defaults", "export", Domain, "-")
	if err != nil {
		return nil, err
	}
	var prefs map[string]any
	if _, err := plist.Unmarshal([]byte(out), &prefs); err != nil {
		return nil, fmt.Errorf("reading Rex's preferences: %w", err)
	}
	switch v := prefs[Key].(type) {
	case nil:
		return []byte(`{"formatVersion":1,"records":[]}`), nil
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("unexpected %s type %T - not touching it", Key, v)
	}
}

// Parse checks the format and returns the list.
func Parse(raw []byte) (*List, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var top map[string]any
	if err := dec.Decode(&top); err != nil {
		return nil, fmt.Errorf("unexpected %s format: %w", Key, err)
	}
	if fmt.Sprint(top["formatVersion"]) != "1" {
		return nil, fmt.Errorf("unexpected %s formatVersion %v - not touching it", Key, top["formatVersion"])
	}
	recs, ok := top["records"].([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected %s format (no records) - not touching it", Key)
	}
	l := &List{top: top}
	for _, r := range recs {
		m, ok := r.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("unexpected record %v - not touching it", r)
		}
		for k := range m {
			if !knownFields[k] {
				return nil, fmt.Errorf("unexpected record field %q - not touching it", k)
			}
		}
		for _, k := range []string{"id", "name", "locators"} {
			if _, ok := m[k]; !ok {
				return nil, fmt.Errorf("record without %q - not touching it", k)
			}
		}
		if _, ok := m["locators"].([]any); !ok {
			return nil, fmt.Errorf("record locators isn't a list - not touching it")
		}
		l.Records = append(l.Records, m)
	}
	return l, nil
}

// Read reads and parses the app's list.
func Read(ctx context.Context) (*List, error) {
	raw, err := ReadRaw(ctx)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

func locators(r map[string]any) []string {
	var out []string
	for _, x := range r["locators"].([]any) {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

// Hosts lists the entries.
func (l *List) Hosts() []Host {
	var out []Host
	for _, r := range l.Records {
		out = append(out, Host{ID: fmt.Sprint(r["id"]), Name: fmt.Sprint(r["name"]), Locators: locators(r)})
	}
	return out
}

// Has reports whether an entry uses endpoint.
func (l *List) Has(endpoint string) bool {
	for _, h := range l.Hosts() {
		for _, x := range h.Locators {
			if x == endpoint {
				return true
			}
		}
	}
	return false
}

// Add appends an entry. hostID and instanceID come from the host's server.status.
func (l *List) Add(id, label, endpoint, icon, hostID, instanceID string) error {
	for _, h := range l.Hosts() {
		if strings.EqualFold(h.Name, label) || l.Has(endpoint) {
			return fmt.Errorf("already in the app: %s / %s", label, endpoint)
		}
	}
	r := map[string]any{"icon": icon, "id": id, "locators": []any{endpoint}, "name": label,
		"preferredURL": "http://" + endpoint + ":7297"}
	if hostID != "" {
		r["lastHostID"] = hostID
	}
	if instanceID != "" {
		r["lastInstanceID"] = instanceID
	}
	l.Records = append(l.Records, r)
	return nil
}

// Remove drops the entries that use endpoint and returns their ids.
func (l *List) Remove(endpoint string) []string {
	var keep []map[string]any
	var gone []string
	for _, r := range l.Records {
		hit := false
		for _, x := range locators(r) {
			hit = hit || x == endpoint
		}
		if hit {
			gone = append(gone, fmt.Sprint(r["id"]))
		} else {
			keep = append(keep, r)
		}
	}
	l.Records = keep
	return gone
}

// Encode returns the compact JSON to store.
func (l *List) Encode() []byte {
	top := map[string]any{}
	for k, v := range l.top {
		top[k] = v
	}
	recs := make([]any, len(l.Records))
	for i, r := range l.Records {
		recs[i] = r
	}
	top["records"] = recs
	b, _ := json.Marshal(top)
	return b
}

// Write stores data as the app's list (through cfprefsd, like `defaults write`).
func Write(ctx context.Context, data []byte) error {
	_, err := run.Output(ctx, "defaults", "write", Domain, Key, "-data", hex.EncodeToString(data))
	return err
}

// BackupDir is where the app's host list is backed up before each change.
func BackupDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "rex", "app-hosts-backups")
}

// Backup saves raw in dir, named by the time.
func Backup(dir string, raw []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, time.Now().Format("20060102-150405.000")+".json")
	return path, os.WriteFile(path, raw, 0o644)
}

// AppName is the Rex app's name, such as "Rex Beta".
func AppName() string {
	m, _ := filepath.Glob("/Applications/Rex*.app")
	sort.Strings(m)
	if len(m) == 0 {
		return "Rex Beta"
	}
	return strings.TrimSuffix(filepath.Base(m[0]), ".app")
}

// Running reports whether the Rex app is running.
func Running() bool {
	return exec.Command("pgrep", "-qf", AppName()+".app/Contents/MacOS/Rex").Run() == nil
}

// Quit quits the Rex app and waits for it to exit. Sessions survive: they live
// in Rex Server, a separate process.
func Quit(ctx context.Context) error {
	if !Running() {
		return nil
	}
	_ = exec.CommandContext(ctx, "osascript", "-e", fmt.Sprintf("quit app %q", AppName())).Run()
	for i := 0; i < 40; i++ {
		if !Running() {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("Rex didn't quit")
}

// Open launches the Rex app.
func Open(ctx context.Context) error {
	return exec.CommandContext(ctx, "open", "-a", AppName()).Run()
}

// ServerStatus reads host_id and instance_id from the Rex server at addr.
func ServerStatus(ctx context.Context, addr string) (hostID, instanceID string, err error) {
	out, err := run.Output(ctx, "rex", "--timeout", "5s", "-S", addr, "api", "call", "server.status")
	if err != nil {
		return "", "", fmt.Errorf("can't reach Rex at %s: %w", addr, err)
	}
	var s struct {
		HostID     string `json:"host_id"`
		InstanceID string `json:"instance_id"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		return "", "", err
	}
	return s.HostID, s.InstanceID, nil
}

// Reachable reports whether a Rex server answers at addr within timeout.
func Reachable(ctx context.Context, addr string, timeout time.Duration) error {
	_, err := run.Output(ctx, "rex", "--timeout", timeout.String(), "-S", addr, "session", "ls")
	return err
}

// ServerHost is an entry in the Rex server's host list.
type ServerHost struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Endpoints []string `json:"endpoints"`
}

// ServerHosts lists the local Rex server's hosts.
func ServerHosts(ctx context.Context) ([]ServerHost, error) {
	out, err := run.Output(ctx, "rex", "hosts", "list", "--json")
	if err != nil {
		return nil, err
	}
	var r struct {
		Hosts []struct{ Host ServerHost }
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return nil, err
	}
	var hs []ServerHost
	for _, h := range r.Hosts {
		hs = append(hs, h.Host)
	}
	return hs, nil
}

// ServerPut adds a host to the local Rex server's list.
func ServerPut(ctx context.Context, id, label, endpoint, icon, hostID string) error {
	from, _ := run.Output(ctx, "scutil", "--get", "LocalHostName")
	if strings.TrimSpace(from) == "" {
		from, _ = os.Hostname()
	}
	body, _ := json.Marshal(map[string]any{"host": map[string]any{"id": id, "label": label,
		"endpoints": []string{endpoint}, "icon": icon, "host_id": hostID, "added_from": strings.TrimSpace(from)}})
	_, err := run.Output(ctx, "rex", "api", "call", "hosts.put", string(body))
	return err
}

// ServerRemoveEndpoint removes the local Rex server's hosts that use endpoint.
func ServerRemoveEndpoint(ctx context.Context, endpoint string) ([]string, error) {
	hs, err := ServerHosts(ctx)
	if err != nil {
		return nil, err
	}
	var gone []string
	for _, h := range hs {
		for _, e := range h.Endpoints {
			if e == endpoint {
				body, _ := json.Marshal(map[string]string{"id": h.ID})
				if _, err := run.Output(ctx, "rex", "api", "call", "hosts.remove", string(body)); err != nil {
					return gone, err
				}
				gone = append(gone, h.ID)
				break
			}
		}
	}
	return gone, nil
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
