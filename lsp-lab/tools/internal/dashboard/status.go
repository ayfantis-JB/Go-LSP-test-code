package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// StatusLines summarises the newest sample of every source in one line each,
// for the periodic heartbeat printed by `lab session`.
func StatusLines(outRoot string, since time.Duration) []string {
	from := time.Now().Add(-since)
	dirs, _ := os.ReadDir(outRoot)
	var lines []string
	for _, e := range dirs {
		if !e.IsDir() {
			continue
		}
		src := loadSource(filepath.Join(outRoot, e.Name()), from)
		if src.SamplesFile == "" {
			continue
		}
		var parts []string
		for _, p := range src.Procs {
			heap := ""
			if p.Heap != "" {
				heap = " heap " + p.Heap + "MB"
			}
			parts = append(parts, fmt.Sprintf("pid %s rss %sMB%s cpu %s%% clients %s", p.PID, p.RSS, heap, p.CPU, p.Clients))
		}
		if len(parts) == 0 {
			parts = []string{"no Go LSP server running"}
		}
		lines = append(lines, fmt.Sprintf("%-8s %s", src.Name, strings.Join(parts, " | ")))
	}
	c := loadContainers(outRoot, from)
	if len(c.Latest) > 0 {
		var parts []string
		for _, r := range c.Latest {
			parts = append(parts, fmt.Sprintf("%s %s (%s%%)", r.Name, strings.SplitN(r.Mem, " /", 2)[0], r.MemPct))
		}
		lines = append(lines, "docker   "+strings.Join(parts, "  "))
	}
	sort.Strings(lines)
	return lines
}
