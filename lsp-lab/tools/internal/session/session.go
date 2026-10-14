// Package session runs a complete manual-testing session: containers up,
// monitors on the host and inside every container, log followers, the
// dashboard, a heartbeat, and a trend summary on exit.
package session

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"learn_go_by_example/tools/internal/dashboard"
	"learn_go_by_example/tools/internal/lab"
	"learn_go_by_example/tools/internal/logs"
	"learn_go_by_example/tools/internal/monitor"
	"learn_go_by_example/tools/internal/report"
	"learn_go_by_example/tools/internal/term"
)

// Options configures a session.
type Options struct {
	Root      string
	Up, Build bool // start containers first (and rebuild images)
	Down      bool // stop containers on exit
	Logs      bool // follow server logs on the host and in containers
	Addr      string
	Interval  time.Duration
	Heartbeat time.Duration
	Notify    bool
	Services  []string
}

// Run blocks until ctx is cancelled (Ctrl-C).
func Run(ctx context.Context, opt Options, out io.Writer) error {
	started := time.Now()
	outRoot := filepath.Join(opt.Root, "lab-out")
	p := &printer{out: out}
	say := p.writer("lab", term.Bold, false)

	if opt.Up {
		if err := lab.Up(opt.Root, io.Discard, opt.Build); err != nil {
			return err
		}
	}
	if err := waitReady(ctx, opt, say); err != nil {
		return err
	}
	cfgPath, err := writeSSHConfig(opt.Root, outRoot)
	if err != nil {
		return err
	}
	fmt.Fprintf(say, "ssh config: %s  (add `Include %s` at the top of ~/.ssh/config)\n", cfgPath, cfgPath)
	stopInContainers(opt) // leftovers from a session that was killed

	var wg sync.WaitGroup
	run := func(fn func()) { wg.Add(1); go func() { defer wg.Done(); fn() }() }

	host, _ := os.Hostname()
	cfg := monitor.Defaults()
	cfg.OutDir, cfg.Interval, cfg.Docker, cfg.Quiet, cfg.Notify = filepath.Join(outRoot, host), opt.Interval, true, true, opt.Notify
	w, err := monitor.New(cfg, p.writer("host", term.Yellow, false))
	if err != nil {
		return err
	}
	run(func() { w.Run(ctx) })

	for _, svc := range opt.Services {
		args := []string{"compose", "exec", "-T", svc, "lab", "monitor", "--quiet", "--interval", opt.Interval.String()}
		mw := p.writer(svc, term.Yellow, opt.Notify)
		run(func() { supervise(ctx, opt.Root, mw, args) })
	}
	if opt.Logs {
		lw := p.writer("host log", term.Dim, false)
		run(func() { logs.Follow(ctx, logs.FollowOptions{Short: true, MaxAge: 48 * time.Hour}, lw) })
		for _, svc := range opt.Services {
			args := []string{"compose", "exec", "-T", svc, "lab", "logs", "--short"}
			lw := p.writer(svc+" log", term.Dim, false)
			run(func() { supervise(ctx, opt.Root, lw, args) })
		}
	}
	dashOpt := dashboard.Options{Addr: opt.Addr, OutRoot: outRoot, ComposeDir: opt.Root, Services: opt.Services}
	run(func() {
		err := dashboard.Serve(ctx, dashOpt, func(url string) {
			fmt.Fprintf(say, "dashboard: %s\n", url)
		})
		if err != nil {
			fmt.Fprintf(say, "dashboard: %v\n", err)
		}
	})
	run(func() { heartbeat(ctx, outRoot, opt.Heartbeat, p.writer("status", term.Dim, false)) })

	fmt.Fprintln(say, "monitoring host + "+strings.Join(opt.Services, ", ")+"; mark test steps with `go run ./tools/lab mark \"…\"`; Ctrl-C to stop")
	<-ctx.Done()
	fmt.Fprintln(say, "stopping monitors…")
	stopInContainers(opt)
	wg.Wait()
	printSummaries(out, outRoot, started)
	if opt.Down {
		return lab.Compose(opt.Root, "down")
	}
	fmt.Fprintln(out, "containers are still running (`go run ./tools/lab down` to stop them)")
	return nil
}

// waitReady blocks until every service accepts `docker compose exec`.
func waitReady(ctx context.Context, opt Options, say io.Writer) error {
	deadline := time.Now().Add(2 * time.Minute)
	for _, svc := range opt.Services {
		for {
			cmd := exec.CommandContext(ctx, "docker", "compose", "exec", "-T", svc, "true")
			cmd.Dir = opt.Root
			if cmd.Run() == nil {
				break
			}
			if time.Now().After(deadline) || ctx.Err() != nil {
				return fmt.Errorf("%s is not running (start it with `go run ./tools/lab up` or `session --up`)", svc)
			}
			time.Sleep(2 * time.Second)
		}
	}
	fmt.Fprintln(say, "containers ready:", strings.Join(opt.Services, ", "))
	return nil
}

func writeSSHConfig(root, outRoot string) (string, error) {
	if err := lab.EnsureKey(root, io.Discard); err != nil {
		return "", err
	}
	var b bytes.Buffer
	lab.WriteSSHConfig(root, &b)
	path := filepath.Join(outRoot, "ssh_config")
	if err := os.MkdirAll(outRoot, 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, b.Bytes(), 0o644)
}

// supervise runs a docker command and restarts it if it dies (e.g. the
// container restarted) until ctx is cancelled.
func supervise(ctx context.Context, dir string, w io.Writer, args []string) {
	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Dir, cmd.Stdout, cmd.Stderr = dir, w, w
		err := cmd.Run()
		if ctx.Err() != nil {
			return
		}
		fmt.Fprintf(w, "exited (%v); restarting in 5s\n", err)
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

// stopInContainers interrupts the in-container monitors: killing the docker
// CLI does not stop a process started with `docker exec`.
func stopInContainers(opt Options) {
	var wg sync.WaitGroup
	for _, svc := range opt.Services {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "docker", "compose", "exec", "-T", svc, "lab", "stop")
			cmd.Dir = opt.Root
			cmd.Run()
		}()
	}
	wg.Wait()
}

func heartbeat(ctx context.Context, outRoot string, every time.Duration, w io.Writer) {
	if every <= 0 {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, l := range dashboard.StatusLines(outRoot, 2*every) {
				fmt.Fprintln(w, l)
			}
		}
	}
}

func printSummaries(out io.Writer, outRoot string, since time.Time) {
	dirs, _ := os.ReadDir(outRoot)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		path, err := report.Latest(filepath.Join(outRoot, d.Name()))
		if err != nil {
			continue
		}
		if fi, err := os.Stat(path); err != nil || fi.ModTime().Before(since) {
			continue
		}
		fmt.Fprintf(out, "\n%s\n", term.Paint(term.Bold, "== "+d.Name()))
		rows, err := report.Summarize(path)
		if err != nil {
			fmt.Fprintln(out, "no Go LSP server seen during this session")
			continue
		}
		report.Print(out, path, rows)
	}
}

// printer serialises lines from many goroutines, each tagged with its source.
type printer struct {
	mu  sync.Mutex
	out io.Writer
}

func (p *printer) writer(tag, color string, notify bool) io.Writer {
	pr, pw := io.Pipe()
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		label := term.Paint(color, fmt.Sprintf("%-10s", "["+tag+"]"))
		for sc.Scan() {
			line := sc.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}
			p.mu.Lock()
			fmt.Fprintln(p.out, label, line)
			p.mu.Unlock()
			if notify && strings.Contains(line, " ALERT ") {
				monitor.Notify("lab "+tag, line)
			}
		}
	}()
	return pw
}
