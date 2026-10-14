// Package report turns a monitor samples CSV into a per-process trend summary.
package report

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"learn_go_by_example/tools/internal/monitor"
)

// Row is the summary for one process.
type Row struct {
	PID                 string
	Span                time.Duration
	RSSStart, RSSEnd    float64
	RSSMax              float64
	RSSSlope, HeapSlope float64 // MB/hour
	CPUMax              float64
	ThreadsMax, FDsMax  int
	Verdicts            []string
}

// Latest returns the newest samples-*.csv in dir.
func Latest(dir string) (string, error) {
	m, _ := filepath.Glob(filepath.Join(dir, "samples-*.csv"))
	if len(m) == 0 {
		return "", fmt.Errorf("no samples in %s; run `lab monitor` first", dir)
	}
	sort.Strings(m)
	return m[len(m)-1], nil
}

// Summarize reads a samples CSV written by monitor.Watcher.
func Summarize(path string) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(recs) < 2 {
		return nil, fmt.Errorf("%s has no samples", path)
	}
	col := map[string]int{}
	for i, h := range recs[0] {
		col[h] = i
	}
	type acc struct {
		rss, heap   []monitor.Point
		cpu         float64
		thr, fds    int
		lastPPID    string
		lastClients string
	}
	per := map[string]*acc{}
	var order []string
	num := func(r []string, k string) float64 { v, _ := strconv.ParseFloat(r[col[k]], 64); return v }
	for _, r := range recs[1:] {
		t, err := time.Parse(time.RFC3339, r[col["ts"]])
		if err != nil {
			continue
		}
		pid := r[col["pid"]]
		a := per[pid]
		if a == nil {
			a = &acc{}
			per[pid] = a
			order = append(order, pid)
		}
		a.rss = append(a.rss, monitor.Point{T: t, V: num(r, "rss_mb")})
		if r[col["heap_used_mb"]] != "" {
			a.heap = append(a.heap, monitor.Point{T: t, V: num(r, "heap_used_mb")})
		}
		a.cpu = max(a.cpu, num(r, "cpu_pct"))
		a.thr = max(a.thr, int(num(r, "threads")))
		a.fds = max(a.fds, int(num(r, "fds")))
		a.lastPPID, a.lastClients = r[col["ppid"]], r[col["tcp_established"]]
	}

	var rows []Row
	for _, pid := range order {
		a := per[pid]
		span := monitor.Span(a.rss)
		row := Row{PID: pid, Span: span, RSSStart: a.rss[0].V, RSSEnd: a.rss[len(a.rss)-1].V,
			CPUMax: a.cpu, ThreadsMax: a.thr, FDsMax: a.fds}
		for _, p := range a.rss {
			row.RSSMax = max(row.RSSMax, p.V)
		}
		var rssR2, heapR2 float64
		row.RSSSlope, rssR2 = monitor.SlopePerHour(a.rss)
		bucket := max(time.Minute, span/10)
		row.HeapSlope, heapR2 = monitor.SlopePerHour(monitor.Floors(a.heap, bucket))
		if span >= 10*time.Minute && row.RSSSlope > 100 && rssR2 > 0.6 {
			row.Verdicts = append(row.Verdicts, "RSS growing")
		}
		if row.HeapSlope > 50 && heapR2 > 0.7 {
			row.Verdicts = append(row.Verdicts, "HEAP LEAK?")
		}
		if a.cpu >= 95 {
			row.Verdicts = append(row.Verdicts, "cpu spikes")
		}
		if a.lastPPID == "1" && a.lastClients == "0" {
			row.Verdicts = append(row.Verdicts, "ended detached w/o clients")
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Print writes rows as a table.
func Print(w io.Writer, path string, rows []Row) {
	fmt.Fprintf(w, "summary of %s\n\n", path)
	fmt.Fprintf(w, "%7s %9s %24s %9s %15s %8s %8s %8s  %s\n", "PID", "SPAN", "RSS start→end (max) MB", "RSS MB/h", "HEAP floor MB/h", "CPU max", "THR max", "FDS max", "VERDICT")
	for _, r := range rows {
		verdict := strings.Join(r.Verdicts, ", ")
		if verdict == "" {
			verdict = "ok"
		}
		rss := fmt.Sprintf("%.0f→%.0f (%.0f)", r.RSSStart, r.RSSEnd, r.RSSMax)
		fmt.Fprintf(w, "%7s %9s %24s %+9.0f %+15.0f %8.0f %8d %8d  %s\n", r.PID, r.Span.Round(time.Second), rss,
			r.RSSSlope, r.HeapSlope, r.CPUMax, r.ThreadsMax, r.FDsMax, verdict)
	}
}
