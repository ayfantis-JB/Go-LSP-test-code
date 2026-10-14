package proc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// clockTicks is USER_HZ; 100 on every mainstream Linux build (sysconf needs cgo).
const clockTicks = 100

func list() ([]Sample, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	bootUptime := systemUptime()
	pageMB := float64(os.Getpagesize()) / (1 << 20)
	var res []Sample
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		s, ok := readStat(pid, bootUptime, pageMB)
		if ok {
			res = append(res, s)
		}
	}
	return res, nil
}

func readStat(pid int, bootUptime, pageMB float64) (Sample, bool) {
	dir := "/proc/" + strconv.Itoa(pid)
	stat, err := os.ReadFile(dir + "/stat")
	if err != nil {
		return Sample{}, false
	}
	// Fields after "(comm)"; comm may contain spaces and parens.
	i := strings.LastIndexByte(string(stat), ')')
	if i < 0 {
		return Sample{}, false
	}
	f := strings.Fields(string(stat[i+2:]))
	if len(f) < 22 {
		return Sample{}, false
	}
	num := func(k int) float64 { v, _ := strconv.ParseFloat(f[k], 64); return v }
	cmdline, _ := os.ReadFile(dir + "/cmdline")
	cmd := strings.TrimSpace(strings.ReplaceAll(string(cmdline), "\x00", " "))
	if cmd == "" {
		cmd = string(stat[strings.IndexByte(string(stat), '(')+1 : i])
	}
	return Sample{
		PID:        pid,
		PPID:       int(num(1)),
		State:      f[0],
		CPUSeconds: (num(11) + num(12)) / clockTicks,
		Uptime:     time.Duration((bootUptime - num(19)/clockTicks) * float64(time.Second)),
		RSSMB:      num(21) * pageMB,
		Cmd:        cmd,
		Threads:    int(num(17)),
	}, true
}

func systemUptime() float64 {
	b, _ := os.ReadFile("/proc/uptime")
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return v
}

func enrich(s *Sample) {
	dir := "/proc/" + strconv.Itoa(s.PID) + "/fd"
	entries, err := os.ReadDir(dir)
	if err != nil {
		s.FDs, s.TCPEstablished = -1, -1
		return
	}
	states := tcpStates()
	s.FDs = len(entries)
	for _, e := range entries {
		link, err := os.Readlink(filepath.Join(dir, e.Name()))
		if err == nil && strings.HasPrefix(link, "socket:[") && states[link[8:len(link)-1]] == "01" {
			s.TCPEstablished++
		}
	}
}

// tcpStates maps socket inode -> TCP state (hex; 01 = ESTABLISHED).
func tcpStates() map[string]string {
	states := map[string]string{}
	for _, p := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lines := strings.Split(string(b), "\n")
		for _, l := range lines[1:] {
			if f := strings.Fields(l); len(f) > 9 {
				states[f[9]] = f[3]
			}
		}
	}
	return states
}
