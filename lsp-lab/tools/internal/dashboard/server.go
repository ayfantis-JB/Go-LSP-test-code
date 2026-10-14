package dashboard

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"learn_go_by_example/tools/internal/logs"
)

// Options configures the dashboard.
type Options struct {
	Addr       string   // e.g. 127.0.0.1:7070
	OutRoot    string   // lab-out
	ComposeDir string   // where docker-compose.yml lives; "" disables container error summaries
	Services   []string // containers to ask for `lab errors`
	Refresh    time.Duration
}

// Server renders the dashboard.
type Server struct {
	opt    Options
	mu     sync.Mutex
	errors map[string]string // source -> grouped error text
	errAt  time.Time
}

var windows = []struct {
	Label string
	D     time.Duration
}{{"5m", 5 * time.Minute}, {"15m", 15 * time.Minute}, {"1h", time.Hour}, {"6h", 6 * time.Hour}, {"24h", 24 * time.Hour}}

// Serve listens until ctx is cancelled. ready receives the URL once listening.
func Serve(ctx context.Context, opt Options, ready func(url string)) error {
	if opt.Refresh == 0 {
		opt.Refresh = 5 * time.Second
	}
	s := &Server{opt: opt, errors: map[string]string{}}
	ln, err := net.Listen("tcp", opt.Addr)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.page)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	go s.refreshErrors(ctx)
	if ready != nil {
		ready("http://" + ln.Addr().String())
	}
	if err := srv.Serve(ln); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// refreshErrors regroups log errors every 30s; scanning logs per request would be too slow.
func (s *Server) refreshErrors(ctx context.Context) {
	for {
		res := map[string]string{}
		host, _ := os.Hostname()
		if groups, err := logs.Errors(time.Now().Add(-time.Hour), nil, 140); err == nil {
			res[host] = formatGroups(groups)
		}
		if s.opt.ComposeDir != "" {
			for _, svc := range s.opt.Services {
				c, cancel := context.WithTimeout(ctx, 20*time.Second)
				cmd := exec.CommandContext(c, "docker", "compose", "exec", "-T", svc, "lab", "errors", "--since", "1h", "--top", "10", "--width", "140")
				cmd.Dir = s.opt.ComposeDir
				out, err := cmd.CombinedOutput()
				cancel()
				if err != nil {
					out = append(out, fmt.Sprintf("\n(%v)", err)...)
				}
				res[svc] = string(bytes.TrimSpace(out))
			}
		}
		s.mu.Lock()
		s.errors, s.errAt = res, time.Now()
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

func formatGroups(groups []logs.Group) string {
	if len(groups) == 0 {
		return "no errors found"
	}
	var b bytes.Buffer
	for _, g := range groups[:min(10, len(groups))] {
		fmt.Fprintf(&b, "%5d  last %s  %s\n", g.Count, g.Last, g.Signature)
	}
	return b.String()
}

type sourceView struct {
	Source
	Label  string // "host · <hostname>" for the host, else the container name
	Charts []Chart
}

type pageData struct {
	Now        time.Time
	Window     string
	Windows    []string
	Refresh    int
	Alerts     []Alert
	Markers    []Marker
	Containers Containers
	CChart     []Chart
	Sources    []sourceView
	Errors     []errorView
	ErrAt      time.Time
}

type errorView struct{ Source, Text string }

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	window := time.Hour
	label := "1h"
	for _, wd := range windows {
		if r.URL.Query().Get("w") == wd.Label {
			window, label = wd.D, wd.Label
		}
	}
	now := time.Now()
	from := now.Add(-window)
	d := pageData{Now: now, Window: label, Refresh: int(s.opt.Refresh.Seconds())}
	for _, wd := range windows {
		d.Windows = append(d.Windows, wd.Label)
	}
	d.Markers = readMarkers(s.opt.OutRoot, from)

	dirs, _ := os.ReadDir(s.opt.OutRoot)
	for _, e := range dirs {
		if !e.IsDir() {
			continue
		}
		src := loadSource(filepath.Join(s.opt.OutRoot, e.Name()), from)
		if src.SamplesFile == "" && len(src.Alerts) == 0 {
			continue
		}
		d.Alerts = append(d.Alerts, src.Alerts...)
		names := seriesNames(src.RSS, src.Heap, src.CPU)
		label := src.Name
		if host, _ := os.Hostname(); src.Name == host {
			label = "host · " + host
		}
		d.Sources = append(d.Sources, sourceView{Source: src, Label: label, Charts: []Chart{
			lineChart("Resident memory", "MB", src.RSS, names, from, now, d.Markers),
			lineChart("JVM heap used", "MB", src.Heap, names, from, now, d.Markers),
			lineChart("CPU", "%", src.CPU, names, from, now, d.Markers),
		}})
	}
	sort.Slice(d.Alerts, func(i, j int) bool { return d.Alerts[i].T.After(d.Alerts[j].T) })
	d.Alerts = d.Alerts[:min(40, len(d.Alerts))]

	d.Containers = loadContainers(s.opt.OutRoot, from)
	cnames := seriesNames(d.Containers.Mem, d.Containers.CPU)
	d.CChart = []Chart{
		lineChart("Container memory", "MB", d.Containers.Mem, cnames, from, now, d.Markers),
		lineChart("Container CPU", "%", d.Containers.CPU, cnames, from, now, d.Markers),
	}

	s.mu.Lock()
	for src, text := range s.errors {
		d.Errors = append(d.Errors, errorView{src, text})
	}
	d.ErrAt = s.errAt
	s.mu.Unlock()
	sort.Slice(d.Errors, func(i, j int) bool { return d.Errors[i].Source < d.Errors[j].Source })

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTmpl.Execute(w, d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func seriesNames(groups ...[]Series) []string {
	set := map[string]bool{}
	for _, g := range groups {
		for _, s := range g {
			set[s.Name] = true
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

var pageTmpl = template.Must(template.New("page").Funcs(template.FuncMap{
	"clock": func(t time.Time) string { return t.Format("15:04:05") },
}).Parse(pageHTML))
