package report

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSummarizeFlagsLeakAndOrphan(t *testing.T) {
	var b strings.Builder
	b.WriteString("ts,pid,ppid,state,rss_mb,cpu_pct,threads,fds,tcp_established,heap_used_mb,heap_committed_mb,uptime_s,workspace\n")
	t0 := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	for i := range 60 { // one hour, one sample per minute
		ts := t0.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		rss := 1000 + float64(i)*5                   // +300 MB/h
		heap := 100 + float64(i)*2 + float64(i%3)*50 // sawtooth over a rising floor
		fmt.Fprintf(&b, "%s,42,1,S,%.0f,3,100,50,0,%.0f,512,%d,/w\n", ts, rss, heap, i*60)
	}
	path := filepath.Join(t.TempDir(), "samples-x.csv")
	os.WriteFile(path, []byte(b.String()), 0o644)

	rows, err := Summarize(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	r := rows[0]
	if r.RSSSlope < 290 || r.RSSSlope > 310 {
		t.Errorf("RSS slope = %.0f, want ~300", r.RSSSlope)
	}
	for _, want := range []string{"RSS growing", "HEAP LEAK?", "ended detached w/o clients"} {
		if !slices.Contains(r.Verdicts, want) {
			t.Errorf("verdicts %v missing %q", r.Verdicts, want)
		}
	}
}
