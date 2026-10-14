package proc

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func run(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

func list() ([]Sample, error) {
	out, err := run(10*time.Second, "ps", "-axo", "pid=,ppid=,stat=,rss=,etime=,time=,command=")
	if err != nil {
		return nil, err
	}
	var res []Sample
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 7 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		ppid, _ := strconv.Atoi(f[1])
		rss, _ := strconv.ParseFloat(f[3], 64)
		up, _ := ParseDuration(f[4])
		cpu, _ := ParseDuration(f[5])
		res = append(res, Sample{
			PID: pid, PPID: ppid, State: f[2], RSSMB: rss / 1024,
			Uptime: up, CPUSeconds: cpu.Seconds(), Cmd: strings.Join(f[6:], " "),
			Threads: -1, FDs: -1, TCPEstablished: -1,
		})
	}
	return res, sc.Err()
}

func enrich(s *Sample) {
	pid := strconv.Itoa(s.PID)
	if out, err := run(5*time.Second, "ps", "-M", "-p", pid); err == nil {
		s.Threads = max(0, bytes.Count(out, []byte("\n"))-1)
	}
	// lsof exits 1 when some info is unavailable; the output is still usable.
	out, _ := run(10*time.Second, "lsof", "-nP", "-p", pid, "-F", "ftT")
	if len(out) == 0 {
		return
	}
	s.FDs, s.TCPEstablished = 0, bytes.Count(out, []byte("TST=ESTABLISHED"))
	for line := range bytes.Lines(out) {
		if len(line) > 1 && line[0] == 'f' && line[1] >= '0' && line[1] <= '9' {
			s.FDs++
		}
	}
}
