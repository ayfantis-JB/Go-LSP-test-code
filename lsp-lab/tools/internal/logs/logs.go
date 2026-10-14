// Package logs finds, follows and summarises the Go LSP server and VS Code
// extension-host logs, on the host and inside VS Code Server containers.
package logs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"learn_go_by_example/tools/internal/term"
)

// Globs are relative to $HOME: host VS Code (macOS, Linux) and VS Code Server.
var Globs = []string{
	"Library/Application Support/Code/User/workspaceStorage/*/JetBrains.goland-server/system/log/*.log",
	"Library/Application Support/Code/logs/*/window*/exthost/JetBrains.goland-server/*.log",
	".config/Code/User/workspaceStorage/*/JetBrains.goland-server/system/log/*.log",
	".config/Code/logs/*/window*/exthost/JetBrains.goland-server/*.log",
	".vscode-server/data/User/workspaceStorage/*/JetBrains.goland-server/system/log/*.log",
	".vscode-server/data/logs/*/exthost*/JetBrains.goland-server/*.log",
}

// Case-sensitive on purpose: "Logger.error(" in stack frames must not count.
var (
	severityRe = regexp.MustCompile(`\b(SEVERE|ERROR|FATAL|WARN|WARNING)\b|\[(error|warning)\]`)
	errorRe    = regexp.MustCompile(`\b(SEVERE|ERROR|FATAL)\b|\[error\]`)
	traceRe    = regexp.MustCompile(`^(\s+at |\s+\.\.\.|Caused by:|\t|\s{2,}\S|[\w.$]+(Exception|Error)\b)`)
	tsRe       = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2})[,.]?\d*`)
	uptimeRe   = regexp.MustCompile(`\[\s*\d+\]`)
	noiseRe    = regexp.MustCompile(`@[0-9a-f]{5,}|0x[0-9a-f]+|\b\d+\b`)
)

// Files returns the existing log files, optionally only those modified within maxAge.
func Files(extra []string, maxAge time.Duration) []string {
	home, _ := os.UserHomeDir()
	set := map[string]bool{}
	add := func(pattern string) {
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			fi, err := os.Stat(m)
			if err != nil || fi.IsDir() || (maxAge > 0 && time.Since(fi.ModTime()) > maxAge) {
				continue
			}
			set[m] = true
		}
	}
	for _, g := range Globs {
		add(filepath.Join(home, g))
	}
	for _, g := range extra {
		if strings.HasPrefix(g, "~/") {
			g = filepath.Join(home, g[2:])
		}
		add(g)
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// Label is a short tag: the workspace folder name (from workspace.json) or the parent dir.
func Label(path string) string {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	for dir := filepath.Dir(path); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if filepath.Base(filepath.Dir(dir)) != "workspaceStorage" {
			continue
		}
		var ws struct{ Folder string }
		if b, err := os.ReadFile(filepath.Join(dir, "workspace.json")); err == nil && json.Unmarshal(b, &ws) == nil && ws.Folder != "" {
			return filepath.Base(ws.Folder) + ":" + stem
		}
		break
	}
	return filepath.Base(filepath.Dir(path)) + ":" + stem
}

// FollowOptions controls Follow.
type FollowOptions struct {
	All       bool // print every line, not only warnings/errors
	Short     bool // hide stack-trace continuation lines
	FromStart bool
	MaxAge    time.Duration
	Extra     []string
}

// Follow tails all log files (picking up new ones) until ctx is cancelled.
func Follow(ctx context.Context, opt FollowOptions, out io.Writer) error {
	pos := map[string]int64{}
	inTrace := map[string]bool{}
	first := true
	fmt.Fprintln(out, term.Paint(term.Dim, "following server/extension logs (Ctrl-C to stop)"))
	for {
		for _, f := range Files(opt.Extra, opt.MaxAge) {
			fi, err := os.Stat(f)
			if err != nil {
				continue
			}
			off, known := pos[f]
			if !known {
				// Files appearing after start are read from the beginning.
				if !opt.FromStart && first {
					off = fi.Size()
				}
				fmt.Fprintln(out, term.Paint(term.Dim, "+ "+f))
			}
			if fi.Size() < off { // rotated or truncated
				off = 0
			}
			pos[f] = off
			if fi.Size() == off {
				continue
			}
			pos[f] = emit(f, off, opt, inTrace, out)
		}
		first = false
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}
}

func emit(path string, off int64, opt FollowOptions, inTrace map[string]bool, out io.Writer) int64 {
	f, err := os.Open(path)
	if err != nil {
		return off
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return off
	}
	tag := term.Paint(term.Dim, "["+Label(path)+"]")
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		if err != nil { // keep a partial last line for the next round
			return off
		}
		off += int64(len(line))
		line = strings.TrimRight(line, "\r\n")
		switch {
		case severityRe.MatchString(line):
			inTrace[path] = errorRe.MatchString(line)
			color := term.Yellow
			if inTrace[path] {
				color = term.Red
			}
			fmt.Fprintln(out, tag, term.Paint(color, line))
		case inTrace[path] && traceRe.MatchString(line):
			if !opt.Short {
				fmt.Fprintln(out, tag, term.Paint(term.Dim, "  "+line))
			}
		default:
			inTrace[path] = false
			if opt.All {
				fmt.Fprintln(out, tag, line)
			}
		}
	}
}

// Group is one error signature with its occurrences.
type Group struct {
	Signature   string
	Count       int
	First, Last string
	Sources     []string
}

// Errors groups error lines since the given time by a normalised signature.
func Errors(since time.Time, extra []string, width int) ([]Group, error) {
	groups := map[string]*Group{}
	sources := map[string]map[string]bool{}
	for _, path := range Files(extra, 0) {
		fi, err := os.Stat(path)
		if err != nil || fi.ModTime().Before(since) {
			continue
		}
		if err := scanErrors(path, since, width, groups, sources); err != nil {
			return nil, err
		}
	}
	res := make([]Group, 0, len(groups))
	for sig, g := range groups {
		for s := range sources[sig] {
			g.Sources = append(g.Sources, s)
		}
		sort.Strings(g.Sources)
		res = append(res, *g)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Count > res[j].Count })
	return res, nil
}

func scanErrors(path string, since time.Time, width int, groups map[string]*Group, sources map[string]map[string]bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	label := Label(path)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		line := sc.Text()
		if !errorRe.MatchString(line) {
			continue
		}
		ts := ""
		if m := tsRe.FindStringSubmatch(line); m != nil {
			ts = m[1]
			if t, err := time.ParseInLocation(time.DateTime, strings.Replace(ts, "T", " ", 1), time.Local); err == nil && t.Before(since) {
				continue
			}
		}
		sig := Signature(line, width)
		g := groups[sig]
		if g == nil {
			g = &Group{Signature: sig}
			groups[sig] = g
			sources[sig] = map[string]bool{}
		}
		g.Count++
		if ts != "" && (g.First == "" || ts < g.First) {
			g.First = ts
		}
		if ts > g.Last {
			g.Last = ts
		}
		sources[sig][label] = true
	}
	return sc.Err()
}

// Signature normalises a log line so repeats group together: it drops the
// timestamp, uptime counter, hex ids and numbers.
func Signature(line string, width int) string {
	s := tsRe.ReplaceAllString(line, "")
	s = uptimeRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(noiseRe.ReplaceAllString(s, "#"))
	if width > 0 && len(s) > width {
		s = s[:width]
	}
	return s
}
