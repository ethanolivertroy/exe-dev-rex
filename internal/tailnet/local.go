// Package tailnet talks to Tailscale: the local tailscaled (through the
// tailscale CLI) and the Tailscale API.
package tailnet

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ethanolivertroy/exe-dev-rex/internal/run"
)

// Status is the part of `tailscale status --json` exe-rex uses.
type Status struct {
	BackendState   string
	MagicDNSSuffix string
	Self           Peer
	Peer           map[string]Peer
}

// Peer is one node in the status.
type Peer struct {
	ID           string
	HostName     string
	DNSName      string
	TailscaleIPs []string
	Tags         []string
	Online       bool
	CurAddr      string // the direct address in use; "" when relayed or idle
	Relay        string // the home DERP relay region
}

// Local reads this Mac's tailscaled status. Version-mismatch warnings on
// stderr are ignored.
func Local(ctx context.Context) (*Status, error) {
	out, err := run.Output(ctx, "tailscale", "status", "--json")
	if err != nil && out == "" {
		return nil, err
	}
	var s Status
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		return nil, fmt.Errorf("tailscale status --json: %w", err)
	}
	return &s, nil
}

// Find returns the peer whose host name (or first DNS label) is name.
func (s *Status) Find(name string) (Peer, bool) {
	for _, p := range s.Peer {
		if p.HostName == name || strings.SplitN(p.DNSName, ".", 2)[0] == name {
			return p, true
		}
	}
	return Peer{}, false
}

// IPv4 returns the peer's tailnet IPv4 address, or "".
func (p Peer) IPv4() string {
	for _, a := range p.TailscaleIPs {
		if strings.Contains(a, ".") {
			return a
		}
	}
	return ""
}

// HasTag reports whether the peer carries tag.
func (p Peer) HasTag(tag string) bool {
	for _, t := range p.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// PingResult is how this Mac reaches a peer.
type PingResult struct {
	Direct  bool
	Via     string // an ip:port, or DERP(region)
	Latency string
}

var pongRe = regexp.MustCompile(`pong from \S+ \(\S+\) via (\S+) in (\S+)`)

// Ping runs `tailscale ping`, which keeps trying until it has a direct path
// (or gives up after a few pings) and reports the path of the last reply.
func Ping(ctx context.Context, host string) (PingResult, error) {
	out, _ := run.Combined(ctx, "tailscale", "ping", "-c", "5", host)
	return parsePing(out, host)
}

func parsePing(out, host string) (PingResult, error) {
	m := pongRe.FindAllStringSubmatch(out, -1)
	if m == nil {
		return PingResult{}, fmt.Errorf("no reply from %s", host)
	}
	last := m[len(m)-1]
	return PingResult{Direct: !strings.HasPrefix(last[1], "DERP("), Via: last[1], Latency: last[2]}, nil
}
