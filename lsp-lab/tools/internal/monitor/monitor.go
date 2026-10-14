// Package monitor watches the Go LSP server processes and raises alerts for
// leaks, hangs, orphans, duplicates and crash loops.
package monitor

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"learn_go_by_example/tools/internal/jvm"
	"learn_go_by_example/tools/internal/proc"
)

// DefaultPattern matches the JetBrains Go LSP server launched by the VS Code extension.
const DefaultPattern = `intellij-server|goland-server`

// Config holds the thresholds; zero values are replaced by Defaults.
type Config struct {
	OutDir         string
	Pattern        string
	Interval       time.Duration
	HeapEvery      int           // query JVM heap every N samples
	HotCPU         float64       // CPU% considered busy
	HotFor         time.Duration // sustained busy time before HOT
	OrphanGrace    time.Duration // detached with 0 clients before ORPHAN
	LeakMBPerHour  float64
	LeakWindow     time.Duration // history kept for trends
	LeakMinWindow  time.Duration // history required before RSS trend alerts
	MaxFDs         int
	MaxThreads     int
	Docker         bool
	ComposeProject string
	Notify         bool
	Quiet          bool
}

// Defaults returns the standard thresholds.
func Defaults() Config {
	return Config{
		Pattern: DefaultPattern, Interval: 5 * time.Second, HeapEvery: 6,
		HotCPU: 95, HotFor: time.Minute, OrphanGrace: time.Minute,
		LeakMBPerHour: 100, LeakWindow: time.Hour, LeakMinWindow: 10 * time.Minute,
		MaxFDs: 4000, MaxThreads: 600, ComposeProject: "lsp-lab",
	}
}

type tracked struct {
	pid       int
	firstSeen time.Time
	cmd       string
	jcmd      string
	lastCPU   float64
	lastT     time.Time
	hotSince  time.Time
	idleSince time.Time
	rss, heap []Point
	alerted   map[string]bool
	last      proc.Sample
	lastCPUPc float64
	lastHeap  string
}

// Watcher samples processes on every tick.
type Watcher struct {
	cfg     Config
	re      *regexp.Regexp
	procs   map[int]*tracked
	starts  []time.Time
	tick    int
	out     io.Writer
	csvPath string
	csv     *csv.Writer
	csvFile *os.File
	alerts  *os.File
	docker  *csv.Writer
}

var csvHeader = []string{"ts", "pid", "ppid", "state", "rss_mb", "cpu_pct", "threads", "fds",
	"tcp_established", "heap_used_mb", "heap_committed_mb", "uptime_s", "workspace"}

// New creates the output files and a ready-to-run Watcher.
func New(cfg Config, out io.Writer) (*Watcher, error) {
	re, err := regexp.Compile(cfg.Pattern)
	if err != nil {
		return nil, fmt.Errorf("bad pattern: %w", err)
	}
	if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		return nil, err
	}
	stamp := time.Now().Format("20060102-150405")
	w := &Watcher{cfg: cfg, re: re, procs: map[int]*tracked{}, out: out,
		csvPath: filepath.Join(cfg.OutDir, "samples-"+stamp+".csv")}
	if w.csvFile, err = os.Create(w.csvPath); err != nil {
		return nil, err
	}
	w.csv = csv.NewWriter(w.csvFile)
	w.csv.Write(csvHeader)
	if w.alerts, err = os.OpenFile(filepath.Join(cfg.OutDir, "alerts.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err != nil {
		return nil, err
	}
	if cfg.Docker {
		f, err := os.Create(filepath.Join(cfg.OutDir, "containers-"+stamp+".csv"))
		if err != nil {
			return nil, err
		}
		w.docker = csv.NewWriter(f)
		w.docker.Write([]string{"ts", "name", "cpu_pct", "mem_usage", "mem_pct", "pids"})
	}
	return w, nil
}

// CSVPath is where samples are written.
func (w *Watcher) CSVPath() string { return w.csvPath }

// Run samples until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) error {
	defer w.close()
	fmt.Fprintf(w.out, "writing to %s\n", w.cfg.OutDir)
	t := time.NewTicker(w.cfg.Interval)
	defer t.Stop()
	for {
		if err := w.Sample(); err != nil {
			fmt.Fprintf(w.out, "sample: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func (w *Watcher) close() {
	w.csv.Flush()
	w.csvFile.Close()
	w.alerts.Close()
	if w.docker != nil {
		w.docker.Flush()
	}
}

type level string

const (
	info  level = "INFO"
	warn  level = "WARN"
	alarm level = "ALERT"
)

// alert prints and records an alert. With p != nil it fires once per process per code.
func (w *Watcher) alert(lv level, code, msg string, p *tracked) {
	if p != nil {
		if p.alerted[code] {
			return
		}
		p.alerted[code] = true
	}
	line := fmt.Sprintf("%s %s %s %s", time.Now().Format(time.DateTime), lv, code, msg)
	color := map[level]string{info: dim, warn: yellow, alarm: red}[lv]
	fmt.Fprintln(w.out, paint(color, line))
	fmt.Fprintln(w.alerts, line)
	if w.cfg.Notify && lv == alarm && runtime.GOOS == "darwin" {
		script := fmt.Sprintf("display notification %q with title %q", truncate(msg, 200), "lspmon "+code)
		exec.Command("osascript", "-e", script).Run()
	}
}

// Sample takes one round of measurements and evaluates the rules.
func (w *Watcher) Sample() error {
	now := time.Now()
	samples, err := proc.List(w.re)
	if err != nil {
		return err
	}
	seen := map[int]bool{}
	byWorkspace := map[string][]int{}
	for _, s := range samples {
		seen[s.PID] = true
		p := w.procs[s.PID]
		if p == nil {
			p = &tracked{pid: s.PID, firstSeen: now, cmd: s.Cmd, jcmd: jvm.FindJcmd(s.Cmd), alerted: map[string]bool{}}
			w.procs[s.PID] = p
			w.starts = append(w.starts, now)
			w.alert(info, "START", fmt.Sprintf("pid=%d uptime=%s %s", s.PID, s.Uptime.Round(time.Second), truncate(s.Cmd, 160)), nil)
		}

		cpuPct := 0.0
		if !p.lastT.IsZero() && now.After(p.lastT) {
			cpuPct = max(0, (s.CPUSeconds-p.lastCPU)/now.Sub(p.lastT).Seconds()*100)
		}
		p.lastCPU, p.lastT = s.CPUSeconds, now

		heapUsed, heapCommitted := "", ""
		if p.jcmd != "" && w.tick%w.cfg.HeapEvery == 0 {
			h, err := jvm.HeapInfo(p.jcmd, s.PID)
			switch {
			case errors.Is(err, jvm.ErrTimeout):
				w.alert(alarm, "JVM-UNRESPONSIVE", fmt.Sprintf("pid=%d jcmd GC.heap_info timed out (safepoint stall / deadlock?)", s.PID), nil)
				w.snapshot(p, "unresponsive")
			case err == nil:
				heapUsed, heapCommitted = ftoa(h.UsedMB), ftoa(h.CommittedMB)
				p.heap = append(p.heap, Point{now, h.UsedMB})
				p.lastHeap = heapUsed
			}
		}
		p.rss = Trim(append(p.rss, Point{now, s.RSSMB}), now, w.cfg.LeakWindow)
		p.heap = Trim(p.heap, now, w.cfg.LeakWindow)
		p.last, p.lastCPUPc = s, cpuPct

		ws := proc.WorkspaceRoot(s.Cmd)
		if ws != "" {
			byWorkspace[ws] = append(byWorkspace[ws], s.PID)
		}
		w.csv.Write([]string{now.Format(time.RFC3339), strconv.Itoa(s.PID), strconv.Itoa(s.PPID), s.State,
			ftoa(s.RSSMB), ftoa(cpuPct), strconv.Itoa(s.Threads), strconv.Itoa(s.FDs), strconv.Itoa(s.TCPEstablished),
			heapUsed, heapCommitted, strconv.Itoa(int(s.Uptime.Seconds())), ws})
		w.check(p, s, now, cpuPct)
	}

	for ws, pids := range byWorkspace {
		if len(pids) > 1 {
			w.alert(warn, "DUPLICATE", fmt.Sprintf("%d servers for workspace %s: pids=%v", len(pids), ws, pids), w.procs[pids[0]])
		}
	}
	for pid, p := range w.procs {
		if seen[pid] {
			continue
		}
		delete(w.procs, pid)
		lv := info
		if now.Sub(p.firstSeen) < 2*time.Minute {
			lv = warn
		}
		w.alert(lv, "EXIT", fmt.Sprintf("pid=%d observed for %s, last rss=%.0fMB", pid, now.Sub(p.firstSeen).Round(time.Second), p.last.RSSMB), nil)
	}
	w.starts = trimTimes(w.starts, now, 5*time.Minute)
	if len(w.starts) >= 4 && w.tick > 1 {
		w.alert(alarm, "RESTART-LOOP", fmt.Sprintf("%d server starts in the last 5 minutes", len(w.starts)), nil)
		w.starts = nil
	}
	w.csv.Flush()
	if w.docker != nil {
		w.sampleDocker()
	}
	if !w.cfg.Quiet {
		w.render(now)
	}
	w.tick++
	return w.csv.Error()
}

func (w *Watcher) check(p *tracked, s proc.Sample, now time.Time, cpu float64) {
	c := w.cfg
	// Hang: sustained CPU burn.
	if cpu >= c.HotCPU {
		if p.hotSince.IsZero() {
			p.hotSince = now
		}
		if now.Sub(p.hotSince) >= c.HotFor && !p.alerted["HOT"] {
			w.alert(alarm, "HOT", fmt.Sprintf("pid=%d CPU >= %.0f%% for %s (busy loop / runaway analysis?)", p.pid, c.HotCPU, now.Sub(p.hotSince).Round(time.Second)), p)
			w.snapshot(p, "hot")
		}
	} else {
		if p.alerted["HOT"] {
			w.alert(info, "HOT-CLEARED", fmt.Sprintf("pid=%d CPU back to %.0f%%", p.pid, cpu), nil)
			delete(p.alerted, "HOT")
		}
		p.hotSince = time.Time{}
	}
	// Process state: Z zombie, D uninterruptible sleep, T stopped.
	if st := s.State[:1]; st == "Z" || st == "D" || st == "T" {
		w.alert(alarm, "STATE-"+st, fmt.Sprintf("pid=%d in state %s", p.pid, s.State), p)
	}
	// Orphan: the multi-client server detached from VS Code and has no clients left.
	if s.PPID == 1 && s.TCPEstablished == 0 {
		if p.idleSince.IsZero() {
			p.idleSince = now
		}
		if now.Sub(p.idleSince) >= c.OrphanGrace {
			w.alert(alarm, "ORPHAN", fmt.Sprintf("pid=%d detached server with 0 clients for %s (should exit after --idle-timeout); rss=%.0fMB",
				p.pid, now.Sub(p.idleSince).Round(time.Second), s.RSSMB), p)
		}
	} else {
		p.idleSince = time.Time{}
	}
	if c.MaxFDs > 0 && s.FDs >= c.MaxFDs {
		w.alert(warn, "FDS", fmt.Sprintf("pid=%d has %d open fds", p.pid, s.FDs), p)
	}
	if c.MaxThreads > 0 && s.Threads >= c.MaxThreads {
		w.alert(warn, "THREADS", fmt.Sprintf("pid=%d has %d threads", p.pid, s.Threads), p)
	}
	// Leaks: steady growth of RSS and of the JVM heap floor.
	if Span(p.rss) >= c.LeakMinWindow {
		if slope, r2 := SlopePerHour(p.rss); slope >= c.LeakMBPerHour && r2 >= 0.6 {
			w.alert(warn, "RSS-GROWTH", fmt.Sprintf("pid=%d RSS %+.0fMB/h (R²=%.2f) now %.0fMB", p.pid, slope, r2, s.RSSMB), p)
		}
	}
	if fl := Floors(p.heap, 5*time.Minute); len(fl) >= 3 {
		if slope, r2 := SlopePerHour(fl); slope >= c.LeakMBPerHour/2 && r2 >= 0.7 {
			w.alert(alarm, "HEAP-LEAK", fmt.Sprintf("pid=%d heap floor %+.0fMB/h over %d buckets (R²=%.2f); run: lab snapshot %d",
				p.pid, slope, len(fl), r2, p.pid), p)
		}
	}
}

func (w *Watcher) snapshot(p *tracked, reason string) {
	dir, err := jvm.Snapshot(w.cfg.OutDir, p.pid, p.jcmd, reason, jvm.SnapshotOptions{})
	if err != nil {
		w.alert(warn, "SNAPSHOT", fmt.Sprintf("pid=%d snapshot failed: %v", p.pid, err), nil)
		return
	}
	w.alert(info, "SNAPSHOT", fmt.Sprintf("pid=%d wrote %s", p.pid, dir), nil)
}

type dockerStat struct {
	Name, CPUPerc, MemUsage, MemPerc, PIDs string
}

func (w *Watcher) sampleDocker() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ids, err := exec.CommandContext(ctx, "docker", "ps", "-q", "--filter", "label=com.docker.compose.project="+w.cfg.ComposeProject).Output()
	if err != nil || len(strings.Fields(string(ids))) == 0 {
		return
	}
	args := append([]string{"stats", "--no-stream", "--format", "{{json .}}"}, strings.Fields(string(ids))...)
	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		var s dockerStat
		if json.Unmarshal([]byte(line), &s) != nil {
			continue
		}
		memPct := strings.TrimSuffix(s.MemPerc, "%")
		w.docker.Write([]string{time.Now().Format(time.RFC3339), s.Name, strings.TrimSuffix(s.CPUPerc, "%"), s.MemUsage, memPct, s.PIDs})
		if v, err := strconv.ParseFloat(memPct, 64); err == nil && v >= 90 {
			w.alert(alarm, "CONTAINER-MEM", fmt.Sprintf("%s at %s of its memory limit", s.Name, s.MemPerc), nil)
		}
	}
	w.docker.Flush()
}

func (w *Watcher) render(now time.Time) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", paint(bold, fmt.Sprintf("lspmon %s  every %s  csv=%s", now.Format(time.DateTime), w.cfg.Interval, w.csvPath)))
	fmt.Fprintf(&b, "%7s %6s %-4s %8s %8s %6s %5s %5s %4s %10s  %s\n", "PID", "PPID", "ST", "RSS MB", "HEAP MB", "CPU%", "THR", "FDS", "TCP", "UPTIME", "WORKSPACE")
	pids := make([]int, 0, len(w.procs))
	for pid := range w.procs {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	for _, pid := range pids {
		p := w.procs[pid]
		s := p.last
		ws := proc.WorkspaceRoot(p.cmd)
		if ws == "" {
			ws = truncate(p.cmd, 50)
		}
		fmt.Fprintf(&b, "%7d %6d %-4s %8.0f %8s %6.1f %5d %5d %4d %10s  %s\n", pid, s.PPID, s.State, s.RSSMB, p.lastHeap,
			p.lastCPUPc, s.Threads, s.FDs, s.TCPEstablished, s.Uptime.Round(time.Second), ws)
	}
	if len(pids) == 0 {
		b.WriteString(paint(dim, "  (no matching processes - open a Go file in VS Code)") + "\n")
	}
	fmt.Fprintln(w.out, b.String())
}

func trimTimes(ts []time.Time, now time.Time, window time.Duration) []time.Time {
	out := ts[:0]
	for _, t := range ts {
		if now.Sub(t) < window {
			out = append(out, t)
		}
	}
	return out
}

func ftoa(v float64) string { return strconv.FormatFloat(round1(v), 'f', -1, 64) }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
