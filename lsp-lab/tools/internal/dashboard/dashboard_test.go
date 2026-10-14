package dashboard

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"learn_go_by_example/tools/internal/monitor"
)

func TestParseMB(t *testing.T) {
	tests := map[string]float64{"1.5GiB / 4GiB": 1536, "512MiB / 3GiB": 512, "2048KiB / 1GiB": 2, "10MB / 1GB": 10}
	for in, want := range tests {
		if got, ok := parseMB(in); !ok || got != want {
			t.Errorf("parseMB(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
}

func TestNiceCeil(t *testing.T) {
	for in, want := range map[float64]float64{0: 1, 7: 10, 120: 200, 1100: 2000, 2400: 2500} {
		if got := niceCeil(in); got != want {
			t.Errorf("niceCeil(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestDownsampleKeepsSpikes(t *testing.T) {
	t0 := time.Now()
	var pts []monitor.Point
	for i := range 1000 {
		v := 1.0
		if i == 537 {
			v = 99
		}
		pts = append(pts, monitor.Point{T: t0.Add(time.Duration(i) * time.Second), V: v})
	}
	out := downsample(pts, 50)
	if len(out) > 51 {
		t.Fatalf("len = %d", len(out))
	}
	found := false
	for _, p := range out {
		found = found || p.V == 99
	}
	if !found {
		t.Error("spike was dropped")
	}
}

func TestPageRenders(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "devbox")
	os.MkdirAll(src, 0o755)
	var b strings.Builder
	b.WriteString("ts,pid,ppid,state,rss_mb,cpu_pct,threads,fds,tcp_established,heap_used_mb,heap_committed_mb,uptime_s,workspace\n")
	now := time.Now()
	for i := range 10 {
		ts := now.Add(time.Duration(i-10) * time.Minute).Format(time.RFC3339)
		fmt.Fprintf(&b, "%s,42,1,S,%d,5,90,50,1,%d,512,%d,/workspace\n", ts, 900+i*10, 100+i, 600+i*60)
	}
	os.WriteFile(filepath.Join(src, "samples-20261014-100000.csv"), []byte(b.String()), 0o644)
	os.WriteFile(filepath.Join(src, "alerts.log"), []byte(now.Format(time.DateTime)+" ALERT HEAP-LEAK pid=42 heap floor rising\n"), 0o644)
	AddMarker(root, "rename <Shape>")

	s := &Server{opt: Options{OutRoot: root, Refresh: 5 * time.Second}, errors: map[string]string{}}
	rec := httptest.NewRecorder()
	s.page(rec, httptest.NewRequest("GET", "/?w=1h", nil))
	body := rec.Body.String()
	for _, want := range []string{"devbox", "HEAP-LEAK", "rename &lt;Shape&gt;", "<path class=\"line\"", "Resident memory - pid 42", "/workspace"} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	if rec.Code != 200 {
		t.Errorf("status %d", rec.Code)
	}
}
