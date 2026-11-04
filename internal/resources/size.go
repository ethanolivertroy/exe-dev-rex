// Package resources defines the resource inputs shared by settings and the TUI.
package resources

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var sizePattern = regexp.MustCompile(`^([0-9]+)\s*(G|GB|M|MB)?$`)
var cpuPattern = regexp.MustCompile(`^[0-9]+$`)

// ParseSize returns MiB; bare numbers mean GB. Empty means service default (0).
func ParseSize(s string) (int64, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return 0, nil
	}
	match := sizePattern.FindStringSubmatch(s)
	bad := errors.New("enter a positive whole size: 4, 4GB, 8G or 512MB; blank uses exe.dev defaults")
	if match == nil {
		return 0, bad
	}
	n, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil || n < 1 {
		return 0, bad
	}
	if match[2] != "M" && match[2] != "MB" {
		if n > math.MaxInt64/1024 {
			return 0, bad
		}
		n *= 1024
	}
	return n, nil
}

func ValidateSize(s string) error { _, err := ParseSize(s); return err }

func ValidateCPU(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if !cpuPattern.MatchString(s) || err != nil || n < 1 {
		return errors.New("enter a positive whole number, e.g. 1 or 2; blank uses exe.dev defaults")
	}
	return nil
}

// SizeArgument removes optional whitespace and normalizes units for exe.dev.
func SizeArgument(s string) string { return strings.Join(strings.Fields(strings.ToUpper(s)), "") }
