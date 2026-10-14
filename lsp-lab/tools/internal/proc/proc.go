// Package proc samples OS processes without cgo: /proc on Linux, ps/lsof on macOS.
package proc

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Sample is one observation of a process.
type Sample struct {
	PID, PPID      int
	State          string
	RSSMB          float64
	CPUSeconds     float64 // cumulative user+system time
	Uptime         time.Duration
	Cmd            string
	Threads        int // -1 if unknown
	FDs            int // -1 if unknown
	TCPEstablished int // -1 if unknown
}

// List returns all processes whose command line matches re, excluding this process.
func List(re *regexp.Regexp) ([]Sample, error) {
	all, err := list()
	if err != nil {
		return nil, err
	}
	self := os.Getpid()
	var out []Sample
	for _, s := range all {
		if s.PID == self || !re.MatchString(s.Cmd) {
			continue
		}
		enrich(&s)
		out = append(out, s)
	}
	return out, nil
}

// Command returns the command line of pid.
func Command(pid int) (string, error) {
	all, err := list()
	if err != nil {
		return "", err
	}
	for _, s := range all {
		if s.PID == pid {
			return s.Cmd, nil
		}
	}
	return "", fmt.Errorf("no such pid %d", pid)
}

// ParseDuration parses ps etime/time values: [[DD-]HH:]MM:SS[.ss].
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	var days int
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, fmt.Errorf("bad days in %q", s)
		}
		days, s = n, rest
	}
	var secs float64
	for _, part := range strings.Split(s, ":") {
		v, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return 0, fmt.Errorf("bad duration %q", s)
		}
		secs = secs*60 + v
	}
	return time.Duration(days)*24*time.Hour + time.Duration(secs*float64(time.Second)), nil
}

// WorkspaceRoot extracts --workspace-root from a JetBrains server command line.
func WorkspaceRoot(cmd string) string {
	if m := workspaceRe.FindStringSubmatch(cmd); m != nil {
		return m[1]
	}
	return ""
}

var workspaceRe = regexp.MustCompile(`--workspace-root[= ](\S+)`)
