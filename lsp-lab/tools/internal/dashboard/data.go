// Package dashboard serves a live, server-rendered view of everything the lab
// monitors write to lab-out/: per-source process charts, container usage,
// alerts, test markers and log errors. No JavaScript; the page refreshes itself.
package dashboard

import (
	"bufio"
	"encoding/csv"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"learn_go_by_example/tools/internal/monitor"
)

// Series is one line on a chart.
type Series struct {
	Name string
	Pts  []monitor.Point
}

// ProcRow is the latest sample of one process.
type ProcRow struct {
	PID, PPID, State, Workspace string
	RSS, Heap, CPU              string
	Threads, FDs, Clients       string
	Uptime                      string
	Seen                        time.Time
}

// Source is one monitor output directory (host or a container).
type Source struct {
	Name           string
	RSS, Heap, CPU []Series
	Procs          []ProcRow
	Alerts         []Alert
	SamplesFile    string
}

// Alert is one alerts.log line.
type Alert struct {
	T                   time.Time
	Source, Level, Code string
	Msg                 string
}

// Marker is a user annotation from `lab mark`.
type Marker struct {
	T    time.Time
	Text string
}

// MarkersFile is where `lab mark` appends, relative to the lab-out root.
const MarkersFile = "markers.log"

// AddMarker appends a marker line.
func AddMarker(outRoot, text string) (Marker, error) {
	m := Marker{time.Now(), text}
	if err := os.MkdirAll(outRoot, 0o755); err != nil {
		return m, err
	}
	f, err := os.OpenFile(filepath.Join(outRoot, MarkersFile), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return m, err
	}
	defer f.Close()
	_, err = f.WriteString(m.T.Format(time.RFC3339) + "\t" + strings.ReplaceAll(text, "\n", " ") + "\n")
	return m, err
}

func readMarkers(outRoot string, from time.Time) []Marker {
	var res []Marker
	eachLine(filepath.Join(outRoot, MarkersFile), func(l string) {
		ts, text, ok := strings.Cut(l, "\t")
		t, err := time.Parse(time.RFC3339, ts)
		if ok && err == nil && !t.Before(from) {
			res = append(res, Marker{t, text})
		}
	})
	return res
}

func eachLine(path string, fn func(string)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		fn(sc.Text())
	}
}

func latest(dir, pattern string) string {
	m, _ := filepath.Glob(filepath.Join(dir, pattern))
	if len(m) == 0 {
		return ""
	}
	sort.Strings(m)
	return m[len(m)-1]
}

func readCSV(path string) (map[string]int, [][]string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	recs, _ := r.ReadAll() // a monitor may be mid-write: keep what parsed
	if len(recs) == 0 {
		return nil, nil
	}
	col := map[string]int{}
	for i, h := range recs[0] {
		col[h] = i
	}
	return col, recs[1:]
}

func field(rec []string, col map[string]int, name string) string {
	if i, ok := col[name]; ok && i < len(rec) {
		return rec[i]
	}
	return ""
}

// loadSource reads the newest samples CSV and the alerts of one source dir.
func loadSource(dir string, from time.Time) Source {
	s := Source{Name: filepath.Base(dir), SamplesFile: latest(dir, "samples-*.csv")}
	col, recs := readCSV(s.SamplesFile)
	rss, heap, cpu := map[string][]monitor.Point{}, map[string][]monitor.Point{}, map[string][]monitor.Point{}
	last := map[string]ProcRow{}
	for _, r := range recs {
		t, err := time.Parse(time.RFC3339, field(r, col, "ts"))
		if err != nil || t.Before(from) {
			continue
		}
		pid := field(r, col, "pid")
		name := "pid " + pid
		add := func(m map[string][]monitor.Point, key string) {
			if v, err := strconv.ParseFloat(field(r, col, key), 64); err == nil {
				m[name] = append(m[name], monitor.Point{T: t, V: v})
			}
		}
		add(rss, "rss_mb")
		add(heap, "heap_used_mb")
		add(cpu, "cpu_pct")
		up, _ := strconv.Atoi(field(r, col, "uptime_s"))
		last[pid] = ProcRow{
			PID: pid, PPID: field(r, col, "ppid"), State: field(r, col, "state"), Workspace: field(r, col, "workspace"),
			RSS: field(r, col, "rss_mb"), Heap: orKeep(field(r, col, "heap_used_mb"), last[pid].Heap), CPU: field(r, col, "cpu_pct"),
			Threads: field(r, col, "threads"), FDs: field(r, col, "fds"), Clients: field(r, col, "tcp_established"),
			Uptime: (time.Duration(up) * time.Second).String(), Seen: t,
		}
	}
	s.RSS, s.Heap, s.CPU = toSeries(rss), toSeries(heap), toSeries(cpu)
	for _, p := range last {
		s.Procs = append(s.Procs, p)
	}
	sort.Slice(s.Procs, func(i, j int) bool { return s.Procs[i].PID < s.Procs[j].PID })
	s.Alerts = readAlerts(filepath.Join(dir, "alerts.log"), s.Name, from)
	return s
}

func orKeep(v, prev string) string {
	if v == "" {
		return prev
	}
	return v
}

func toSeries(m map[string][]monitor.Point) []Series {
	res := make([]Series, 0, len(m))
	for name, pts := range m {
		res = append(res, Series{name, pts})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Name < res[j].Name })
	return res
}

var alertRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) (INFO|WARN|ALERT) (\S+) (.*)$`)

func readAlerts(path, source string, from time.Time) []Alert {
	var res []Alert
	eachLine(path, func(l string) {
		m := alertRe.FindStringSubmatch(l)
		if m == nil {
			return
		}
		t, err := time.ParseInLocation(time.DateTime, m[1], time.Local)
		if err == nil && !t.Before(from) {
			res = append(res, Alert{t, source, m[2], m[3], m[4]})
		}
	})
	return res
}

// Containers holds docker stats series from the host monitor.
type Containers struct {
	Mem, CPU []Series
	Latest   []ContainerRow
}

// ContainerRow is the latest docker stats line of one container.
type ContainerRow struct{ Name, CPU, Mem, MemPct, PIDs string }

var sizeRe = regexp.MustCompile(`^([\d.]+)\s*([KMGT]i?B|B)`)

// parseMB turns docker's "1.21GiB / 4GiB" into MB of the first value.
func parseMB(s string) (float64, bool) {
	m := sizeRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	switch strings.TrimSuffix(strings.TrimSuffix(m[2], "B"), "i") {
	case "K":
		v /= 1024
	case "G":
		v *= 1024
	case "T":
		v *= 1024 * 1024
	case "":
		v /= 1024 * 1024
	}
	return v, true
}

func loadContainers(outRoot string, from time.Time) Containers {
	var c Containers
	dirs, _ := os.ReadDir(outRoot)
	path := ""
	for _, d := range dirs {
		if p := latest(filepath.Join(outRoot, d.Name()), "containers-*.csv"); p > path {
			path = p
		}
	}
	col, recs := readCSV(path)
	mem, cpu := map[string][]monitor.Point{}, map[string][]monitor.Point{}
	last := map[string]ContainerRow{}
	for _, r := range recs {
		t, err := time.Parse(time.RFC3339, field(r, col, "ts"))
		if err != nil || t.Before(from) {
			continue
		}
		name := strings.TrimPrefix(strings.TrimSuffix(field(r, col, "name"), "-1"), "lsp-lab-")
		if v, ok := parseMB(field(r, col, "mem_usage")); ok {
			mem[name] = append(mem[name], monitor.Point{T: t, V: v})
		}
		if v, err := strconv.ParseFloat(field(r, col, "cpu_pct"), 64); err == nil {
			cpu[name] = append(cpu[name], monitor.Point{T: t, V: v})
		}
		last[name] = ContainerRow{name, field(r, col, "cpu_pct"), field(r, col, "mem_usage"), field(r, col, "mem_pct"), field(r, col, "pids")}
	}
	c.Mem, c.CPU = toSeries(mem), toSeries(cpu)
	for _, row := range last {
		c.Latest = append(c.Latest, row)
	}
	sort.Slice(c.Latest, func(i, j int) bool { return c.Latest[i].Name < c.Latest[j].Name })
	return c
}
