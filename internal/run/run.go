// Package run wraps the external commands exe-rex drives (ssh, tailscale, rex,
// defaults, osascript).
package run

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Error is a failed command with its stderr.
type Error struct {
	Cmd    string
	Err    error
	Stderr string
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return fmt.Sprintf("%s: %s", e.Cmd, msg)
}

func (e *Error) Unwrap() error { return e.Err }

// Output runs name with args and returns stdout. On failure the error carries stderr.
func Output(ctx context.Context, name string, args ...string) (string, error) {
	return Input(ctx, "", name, args...)
}

// Input is Output with stdin.
func Input(ctx context.Context, stdin, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	if err := cmd.Run(); err != nil {
		return out.String(), &Error{Cmd: name + " " + strings.Join(short(args), " "), Err: err, Stderr: errb.String()}
	}
	return out.String(), nil
}

// Combined runs the command and returns stdout and stderr together.
func Combined(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return string(b), &Error{Cmd: name + " " + strings.Join(short(args), " "), Err: err, Stderr: string(b)}
	}
	return string(b), nil
}

// short keeps error messages readable when an argument is a long JSON blob.
func short(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if len(a) > 60 {
			a = a[:57] + "..."
		}
		out[i] = a
	}
	return out
}
