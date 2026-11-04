package rexapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Sidebar is the Rex app's host list together with the app and the Rex
// server around it. Every change runs the same sequence: quit the app (when
// asked, so the sidebar shows the change now), re-read the list, back it up,
// edit it, write it, mirror the change on the server, and reopen the app.
// Once Sidebar has quit the app it reopens it, even when a later step fails.
type Sidebar struct {
	Prefs     Prefs
	App       App
	Server    Server
	BackupDir string
}

// Prefs is where the app keeps its host list (the RexSync.servers default).
type Prefs interface {
	ReadRaw(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, data []byte) error
}

// App is the running Rex app.
type App interface {
	Quit(ctx context.Context) error
	Open(ctx context.Context) error
}

// Server is the Rex server: the one at addr for Status, the local one otherwise.
type Server interface {
	Status(ctx context.Context, addr string) (hostID, instanceID string, err error)
	Put(ctx context.Context, id, label, endpoint, icon, hostID string) error
	RemoveEndpoint(ctx context.Context, endpoint string) ([]string, error)
}

// DefaultSidebar is this Mac's Rex.
func DefaultSidebar() *Sidebar {
	return &Sidebar{Prefs: defaultsPrefs{}, App: macApp{}, Server: rexServer{}, BackupDir: BackupDir()}
}

// AddOptions controls AddHost.
type AddOptions struct {
	Label, Endpoint, Icon string
	Via                   string // reach the host here for server.status, instead of Endpoint
	Restart               bool   // quit and reopen Rex so the sidebar shows it now
}

// AddHost adds a host to this Mac's Rex sidebar and server list.
func AddHost(ctx context.Context, o AddOptions) error { return DefaultSidebar().Add(ctx, o) }

// RemoveHost removes endpoint from this Mac's Rex sidebar and server list.
func RemoveHost(ctx context.Context, endpoint string, restart bool) ([]string, error) {
	return DefaultSidebar().Remove(ctx, endpoint, restart)
}

// Read reads and parses the list.
func (s *Sidebar) Read(ctx context.Context) (*List, error) {
	raw, err := s.Prefs.ReadRaw(ctx)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// Add adds a host to the sidebar and the server's list.
func (s *Sidebar) Add(ctx context.Context, o AddOptions) error {
	if o.Icon == "" {
		o.Icon = "server.rack"
	}
	addr := o.Via
	if addr == "" {
		addr = o.Endpoint
	}
	hostID, instID, err := s.Server.Status(ctx, addr)
	if err != nil {
		return err
	}
	id := strings.ToUpper(newUUID()) // app ids are upper-case UUIDs; the server's copy is host:<lower-case>
	add := func(l *List) error { return l.Add(id, o.Label, o.Endpoint, o.Icon, hostID, instID) }
	// Check first, so a duplicate fails before Rex restarts.
	l, err := s.Read(ctx)
	if err != nil {
		return err
	}
	if err := add(l); err != nil {
		return err
	}
	return s.edit(ctx, o.Restart, func(l *List) (bool, error) {
		if err := add(l); err != nil {
			return false, err
		}
		return true, nil
	}, func() error {
		return s.Server.Put(ctx, "host:"+strings.ToLower(id), o.Label, o.Endpoint, o.Icon, hostID)
	})
}

// Remove removes endpoint from the sidebar and the server's list. It returns
// the removed sidebar entry ids (none if it wasn't in the sidebar).
func (s *Sidebar) Remove(ctx context.Context, endpoint string, restart bool) ([]string, error) {
	l, err := s.Read(ctx)
	if err != nil {
		return nil, err
	}
	var gone []string
	server := func() error {
		// Drop the server's copies too, or they'd linger.
		_, err := s.Server.RemoveEndpoint(ctx, endpoint)
		return err
	}
	if !l.Has(endpoint) {
		return nil, server() // nothing in the sidebar: no restart
	}
	err = s.edit(ctx, restart, func(l *List) (bool, error) {
		gone = l.Remove(endpoint)
		return len(gone) > 0, nil
	}, server)
	return gone, err
}

// edit is the one way the list changes. change edits the freshly read list
// and says whether to write it; mirror then updates the server. If mirror
// fails after the write, the sidebar change is kept, not rolled back.
func (s *Sidebar) edit(ctx context.Context, restart bool, change func(*List) (bool, error), mirror func() error) (err error) {
	if restart {
		if err := s.App.Quit(ctx); err != nil {
			return err
		}
		defer func() {
			if oerr := s.App.Open(ctx); oerr != nil {
				err = errors.Join(err, fmt.Errorf("reopening Rex: %w", oerr))
			}
		}()
	}
	raw, err := s.Prefs.ReadRaw(ctx) // re-read: the app may have saved on quit
	if err != nil {
		return err
	}
	if _, err := Backup(s.BackupDir, raw); err != nil {
		return err
	}
	l, err := Parse(raw)
	if err != nil {
		return err
	}
	write, err := change(l)
	if err != nil {
		return err
	}
	if !write {
		return mirror()
	}
	if err := s.Prefs.Write(ctx, l.Encode()); err != nil {
		return err
	}
	// The sidebar is what people use, so a change that reached it stays even
	// if the server's copy can't follow; the error says which list is behind.
	if err := mirror(); err != nil {
		return fmt.Errorf("the Rex sidebar is updated, but the Rex server's host list (rex hosts list) isn't: %w", err)
	}
	return nil
}

// The real adapters.

type defaultsPrefs struct{}

func (defaultsPrefs) ReadRaw(ctx context.Context) ([]byte, error)  { return ReadRaw(ctx) }
func (defaultsPrefs) Write(ctx context.Context, data []byte) error { return Write(ctx, data) }

type macApp struct{}

func (macApp) Quit(ctx context.Context) error { return Quit(ctx) }
func (macApp) Open(ctx context.Context) error { return Open(ctx) }

type rexServer struct{}

func (rexServer) Status(ctx context.Context, addr string) (string, string, error) {
	return ServerStatus(ctx, addr)
}
func (rexServer) Put(ctx context.Context, id, label, endpoint, icon, hostID string) error {
	return ServerPut(ctx, id, label, endpoint, icon, hostID)
}
func (rexServer) RemoveEndpoint(ctx context.Context, endpoint string) ([]string, error) {
	return ServerRemoveEndpoint(ctx, endpoint)
}
