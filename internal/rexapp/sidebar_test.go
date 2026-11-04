package rexapp

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

// mac is the Rex app, its prefs and the Rex server in memory. Set fail to
// the name of a step ("quit", "read", "write", "put", "remove", "open") to
// make it fail.
type mac struct {
	raw    []byte
	open   bool
	log    []string
	server []string // endpoints in the server's list
	fail   string
}

func (m *mac) step(name string) error {
	m.log = append(m.log, name)
	if m.fail == name {
		return errors.New(name + " failed")
	}
	return nil
}

func (m *mac) ReadRaw(context.Context) ([]byte, error) { return m.raw, m.step("read") }
func (m *mac) Write(_ context.Context, data []byte) error {
	if err := m.step("write"); err != nil {
		return err
	}
	m.raw = data
	return nil
}
func (m *mac) Quit(context.Context) error {
	if err := m.step("quit"); err != nil {
		return err
	}
	m.open = false
	return nil
}
func (m *mac) Open(context.Context) error {
	if err := m.step("open"); err != nil {
		return err
	}
	m.open = true
	return nil
}
func (m *mac) Status(context.Context, string) (string, string, error) {
	return "h9", "i9", m.step("status")
}
func (m *mac) Put(_ context.Context, _, _, endpoint, _, _ string) error {
	if err := m.step("put"); err != nil {
		return err
	}
	m.server = append(m.server, endpoint)
	return nil
}
func (m *mac) RemoveEndpoint(_ context.Context, endpoint string) ([]string, error) {
	if err := m.step("remove"); err != nil {
		return nil, err
	}
	m.server = slices.DeleteFunc(m.server, func(e string) bool { return e == endpoint })
	return []string{"host:x"}, nil
}

func sidebar(t *testing.T) (*Sidebar, *mac) {
	m := &mac{raw: []byte(sample), open: true}
	return &Sidebar{Prefs: m, App: m, Server: m, BackupDir: t.TempDir()}, m
}

func backups(t *testing.T, s *Sidebar) int {
	es, err := os.ReadDir(s.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	return len(es)
}

func TestSidebarAdd(t *testing.T) {
	s, m := sidebar(t)
	if err := s.Add(context.Background(), AddOptions{Label: "vm - exe.dev", Endpoint: "vm.tail1.ts.net", Restart: true}); err != nil {
		t.Fatal(err)
	}
	l, _ := Parse(m.raw)
	if !l.Has("vm.tail1.ts.net") || !slices.Equal(m.server, []string{"vm.tail1.ts.net"}) || !m.open {
		t.Fatalf("sidebar=%+v server=%q open=%v", l.Hosts(), m.server, m.open)
	}
	if backups(t, s) != 1 {
		t.Fatal("back up before writing")
	}
	// It quits before the read it writes back, so the app can't overwrite it.
	if i, j := slices.Index(m.log, "quit"), slices.Index(m.log, "write"); i < 0 || i > j {
		t.Fatalf("order %q", m.log)
	}
}

func TestSidebarAddDuplicateDoesNotRestart(t *testing.T) {
	s, m := sidebar(t)
	err := s.Add(context.Background(), AddOptions{Label: "BOX", Endpoint: "elsewhere", Restart: true})
	if err == nil {
		t.Fatal("duplicate label accepted")
	}
	if slices.Contains(m.log, "quit") {
		t.Fatalf("Rex restarted for nothing: %q", m.log)
	}
}

// Whatever fails after Rex quits, Rex comes back.
func TestSidebarReopensRexAfterAFailure(t *testing.T) {
	for _, step := range []string{"write", "put"} {
		t.Run("add/"+step, func(t *testing.T) {
			s, m := sidebar(t)
			m.fail = step
			err := s.Add(context.Background(), AddOptions{Label: "vm", Endpoint: "vm.tail1.ts.net", Restart: true})
			if err == nil || !strings.Contains(err.Error(), step) {
				t.Fatalf("err = %v", err)
			}
			if !m.open {
				t.Fatalf("Rex left closed: %q", m.log)
			}
		})
	}
	for _, step := range []string{"write", "remove"} {
		t.Run("remove/"+step, func(t *testing.T) {
			s, m := sidebar(t)
			m.fail = step
			if _, err := s.Remove(context.Background(), "box.tail1.ts.net", true); err == nil {
				t.Fatal("want the error")
			}
			if !m.open {
				t.Fatalf("Rex left closed: %q", m.log)
			}
		})
	}
}

func TestSidebarReportsReopenFailure(t *testing.T) {
	s, m := sidebar(t)
	m.fail = "open"
	_, err := s.Remove(context.Background(), "box.tail1.ts.net", true)
	if err == nil || !strings.Contains(err.Error(), "reopening Rex") {
		t.Fatalf("err = %v", err)
	}
}

func TestSidebarRemove(t *testing.T) {
	s, m := sidebar(t)
	m.server = []string{"box.tail1.ts.net"}
	gone, err := s.Remove(context.Background(), "box.tail1.ts.net", true)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := Parse(m.raw)
	if !slices.Equal(gone, []string{"AAA"}) || len(l.Hosts()) != 0 || len(m.server) != 0 || !m.open {
		t.Fatalf("gone=%q left=%+v server=%q open=%v", gone, l.Hosts(), m.server, m.open)
	}
}

func TestSidebarRemoveNotInSidebar(t *testing.T) {
	s, m := sidebar(t)
	m.server = []string{"ghost"}
	gone, err := s.Remove(context.Background(), "ghost", true)
	if err != nil || len(gone) != 0 {
		t.Fatalf("gone=%q err=%v", gone, err)
	}
	if slices.Contains(m.log, "quit") || len(m.server) != 0 {
		t.Fatalf("drop the server's copy without restarting Rex: %q server=%q", m.log, m.server)
	}
}

// A host that reached the sidebar stays there when the server can't take it.
func TestSidebarKeepsEntryWhenServerFails(t *testing.T) {
	s, m := sidebar(t)
	m.fail = "put"
	err := s.Add(context.Background(), AddOptions{Label: "vm", Endpoint: "vm.tail1.ts.net", Restart: true})
	if err == nil || !strings.Contains(err.Error(), "sidebar is updated") || !strings.Contains(err.Error(), "rex hosts list") {
		t.Fatalf("err = %v", err)
	}
	if l, _ := Parse(m.raw); !l.Has("vm.tail1.ts.net") {
		t.Fatal("the sidebar entry was rolled back")
	}
}
