// Command lab runs the Go LSP test lab: containers, monitoring, logs and load.
//
//	go run ./tools/lab help
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"

	"learn_go_by_example/tools/internal/jvm"
	"learn_go_by_example/tools/internal/lab"
	"learn_go_by_example/tools/internal/logs"
	"learn_go_by_example/tools/internal/monitor"
	"learn_go_by_example/tools/internal/proc"
	"learn_go_by_example/tools/internal/report"
	"learn_go_by_example/tools/internal/stress"
)

type command struct {
	name, args, help string
	run              func(args []string) error
}

var commands []command

func init() {
	commands = []command{
		{"up", "[--no-build]", "build and start devbox, runner, app; prints ssh config", cmdUp},
		{"down", "", "stop containers (volumes kept)", compose("down")},
		{"rebuild", "", "rebuild images without cache and restart", cmdRebuild},
		{"nuke", "", "stop and delete containers AND volumes (scratch, caches, vscode-server)", compose("down", "-v")},
		{"ps", "", "container status", compose("ps")},
		{"container-logs", "", "follow container stdout (runner output, app requests)", compose("logs", "-f")},
		{"shell", "[service]", "interactive shell in a container (default devbox)", cmdShell},
		{"in", "service lab-command...", "run a lab command inside a container, e.g. `in devbox monitor`", cmdIn},
		{"ssh-key", "", "generate the lab SSH key (also done by up)", cmdSSHKey},
		{"ssh-config", "", "print ~/.ssh/config entries for Remote-SSH / Gateway", cmdSSHConfig},
		{"install-ext", "VSIX", "install a VSIX into every container's VS Code Server", cmdInstallExt},
		{"install-ext-local", "VSIX", "(inside a container) install a VSIX into VS Code Server", cmdInstallExtLocal},
		{"test", "", "go vet + go test (skips the broken diagnostics package)", cmdTest},
		{"run-examples", "[--stay] [dir]", "vet, test and run every main package", cmdRunExamples},
		{"monitor", "[flags]", "watch LSP processes: leaks, hangs, orphans (see -h)", cmdMonitor},
		{"logs", "[flags]", "follow server/extension logs, errors highlighted", cmdLogs},
		{"errors", "[--since 24h]", "group and count errors in the logs", cmdErrors},
		{"summary", "[csv]", "trend report for a samples CSV (default: latest)", cmdSummary},
		{"snapshot", "[--heapdump] PID", "thread dump, heap info, class histogram", cmdSnapshot},
		{"histo-diff", "A B", "class growth between two histograms or snapshot dirs", cmdHistoDiff},
		{"stress", "[N]", "generate N interlinked packages under lsp/stress (default 100)", cmdStress},
		{"stress-clean", "", "remove generated stress packages", cmdStressClean},
		{"help", "", "show this help", func([]string) error { usage(); return nil }},
	}
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	for _, c := range commands {
		if c.name == os.Args[1] {
			if err := c.run(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "lab:", err)
				os.Exit(1)
			}
			return
		}
	}
	fmt.Fprintf(os.Stderr, "lab: unknown command %q\n\n", os.Args[1])
	usage()
	os.Exit(2)
}

func usage() {
	fmt.Println("usage: go run ./tools/lab <command> [args]   (inside containers: lab <command>)")
	fmt.Println()
	for _, c := range commands {
		fmt.Printf("  %-18s %-24s %s\n", c.name, c.args, c.help)
	}
}

func root() string {
	r, err := lab.Root()
	if err != nil {
		fmt.Fprintln(os.Stderr, "lab:", err)
		os.Exit(1)
	}
	return r
}

// outDir is $LAB_OUT, else <module root>/lab-out/<hostname>.
func outDir() string {
	if d := os.Getenv("LAB_OUT"); d != "" {
		return d
	}
	host, _ := os.Hostname()
	if r, err := lab.Root(); err == nil {
		return filepath.Join(r, "lab-out", host)
	}
	return filepath.Join("lab-out", host)
}

func interruptible() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func compose(args ...string) func([]string) error {
	return func([]string) error { return lab.Compose(root(), args...) }
}

func cmdUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	noBuild := fs.Bool("no-build", false, "start without rebuilding images")
	fs.Parse(args)
	return lab.Up(root(), os.Stdout, !*noBuild)
}

func cmdRebuild([]string) error {
	if err := lab.Compose(root(), "build", "--no-cache"); err != nil {
		return err
	}
	return lab.Up(root(), os.Stdout, false)
}

func cmdShell(args []string) error {
	svc := "devbox"
	if len(args) > 0 {
		svc = args[0]
	}
	return lab.Compose(root(), "exec", svc, "bash", "-l")
}

func cmdIn(args []string) error {
	if len(args) < 2 {
		return errors.New("usage: in <service> <lab-command> [args]")
	}
	return lab.Compose(root(), append([]string{"exec", args[0], "lab"}, args[1:]...)...)
}

func cmdSSHKey([]string) error { return lab.EnsureKey(root(), os.Stdout) }

func cmdSSHConfig([]string) error {
	r := root()
	if err := lab.EnsureKey(r, os.Stderr); err != nil {
		return err
	}
	lab.WriteSSHConfig(r, os.Stdout)
	return nil
}

func cmdInstallExt(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: install-ext path/to/extension.vsix")
	}
	return lab.InstallExt(args[0], os.Stdout)
}

func cmdInstallExtLocal(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: install-ext-local path/to/extension.vsix")
	}
	return lab.InstallExtLocal(args[0], os.Stdout)
}

func cmdTest([]string) error {
	r := root()
	pkgs, err := lab.Packages(r)
	if err != nil {
		return err
	}
	for _, sub := range []string{"vet", "test"} {
		if err := goCmd(r, append([]string{sub}, pkgs...)...); err != nil {
			return err
		}
	}
	return nil
}

func cmdRunExamples(args []string) error {
	fs := flag.NewFlagSet("run-examples", flag.ExitOnError)
	stay := fs.Bool("stay", false, "keep running after the examples (container mode)")
	fs.Parse(args)
	dir := fs.Arg(0)
	if dir == "" {
		dir = root()
	}
	err := lab.RunExamples(dir, os.Stdout)
	if *stay {
		if err != nil {
			fmt.Println("run-examples:", err)
		}
		fmt.Println("runner: done, staying up for remote attach")
		ctx, cancel := interruptible()
		defer cancel()
		<-ctx.Done()
		return nil
	}
	return err
}

func cmdMonitor(args []string) error {
	d := monitor.Defaults()
	fs := flag.NewFlagSet("monitor", flag.ExitOnError)
	cfg := d
	fs.StringVar(&cfg.OutDir, "out", outDir(), "output directory")
	fs.StringVar(&cfg.Pattern, "pattern", d.Pattern, "regexp matched against process command lines")
	fs.DurationVar(&cfg.Interval, "interval", d.Interval, "sampling interval")
	fs.IntVar(&cfg.HeapEvery, "heap-every", d.HeapEvery, "query JVM heap every N samples")
	fs.Float64Var(&cfg.HotCPU, "hot-cpu", d.HotCPU, "CPU% treated as busy")
	fs.DurationVar(&cfg.HotFor, "hot-for", d.HotFor, "sustained busy time before HOT alert")
	fs.DurationVar(&cfg.OrphanGrace, "orphan-grace", d.OrphanGrace, "detached with 0 clients before ORPHAN alert")
	fs.Float64Var(&cfg.LeakMBPerHour, "leak-mb-per-hour", d.LeakMBPerHour, "growth rate treated as a leak")
	fs.DurationVar(&cfg.LeakWindow, "leak-window", d.LeakWindow, "history kept for trend detection")
	fs.DurationVar(&cfg.LeakMinWindow, "leak-min-window", d.LeakMinWindow, "history required before RSS trend alerts")
	fs.IntVar(&cfg.MaxFDs, "max-fds", d.MaxFDs, "open fd alert threshold (0 = off)")
	fs.IntVar(&cfg.MaxThreads, "max-threads", d.MaxThreads, "thread alert threshold (0 = off)")
	fs.BoolVar(&cfg.Docker, "docker", false, "also record docker stats for the compose project")
	fs.StringVar(&cfg.ComposeProject, "compose-project", d.ComposeProject, "compose project for --docker")
	fs.BoolVar(&cfg.Notify, "notify", false, "macOS notification on ALERT")
	fs.BoolVar(&cfg.Quiet, "quiet", false, "only print alerts")
	fs.Parse(args)
	cfg.HeapEvery = max(1, cfg.HeapEvery)

	w, err := monitor.New(cfg, os.Stdout)
	if err != nil {
		return err
	}
	ctx, cancel := interruptible()
	defer cancel()
	err = w.Run(ctx)
	fmt.Printf("\nsamples: %s\nsummary: go run ./tools/lab summary %s\n", w.CSVPath(), w.CSVPath())
	return err
}

type multiFlag []string

func (m *multiFlag) String() string     { return fmt.Sprint(*m) }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func cmdLogs(args []string) error {
	var opt logs.FollowOptions
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	fs.BoolVar(&opt.All, "all", false, "print every line, not only warnings/errors")
	fs.BoolVar(&opt.Short, "short", false, "hide stack-trace continuation lines")
	fs.BoolVar(&opt.FromStart, "from-start", false, "print existing content first")
	fs.DurationVar(&opt.MaxAge, "max-age", 48*time.Hour, "ignore files untouched for this long")
	fs.Var((*multiFlag)(&opt.Extra), "path", "extra log file glob (repeatable)")
	fs.Parse(args)
	ctx, cancel := interruptible()
	defer cancel()
	return logs.Follow(ctx, opt, os.Stdout)
}

func cmdErrors(args []string) error {
	fs := flag.NewFlagSet("errors", flag.ExitOnError)
	since := fs.Duration("since", 24*time.Hour, "look back this far")
	top := fs.Int("top", 20, "number of groups to show")
	width := fs.Int("width", 160, "signature width")
	var extra multiFlag
	fs.Var(&extra, "path", "extra log file glob (repeatable)")
	fs.Parse(args)

	groups, err := logs.Errors(time.Now().Add(-*since), extra, *width)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		fmt.Println("no errors found")
		return nil
	}
	fmt.Printf("%6s  %-19s  %-19s  %s\n", "COUNT", "FIRST", "LAST", "SIGNATURE")
	for _, g := range groups[:min(*top, len(groups))] {
		fmt.Printf("%6d  %-19s  %-19s  %s\n%50sin: %v\n", g.Count, orDash(g.First), orDash(g.Last), g.Signature, "", g.Sources)
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func cmdSummary(args []string) error {
	path := ""
	if len(args) > 0 {
		path = args[0]
	} else {
		var err error
		if path, err = report.Latest(outDir()); err != nil {
			return err
		}
	}
	rows, err := report.Summarize(path)
	if err != nil {
		return err
	}
	report.Print(os.Stdout, path, rows)
	return nil
}

func cmdSnapshot(args []string) error {
	fs := flag.NewFlagSet("snapshot", flag.ExitOnError)
	heapdump := fs.Bool("heapdump", false, "also write a .hprof (large)")
	fs.Parse(args)
	pid, err := strconv.Atoi(fs.Arg(0))
	if err != nil {
		return errors.New("usage: snapshot [--heapdump] PID")
	}
	cmd, err := proc.Command(pid)
	if err != nil {
		return err
	}
	dir, err := jvm.Snapshot(outDir(), pid, jvm.FindJcmd(cmd), "manual", jvm.SnapshotOptions{Histogram: true, HeapDump: *heapdump})
	if err != nil {
		return err
	}
	fmt.Println(dir)
	entries, _ := os.ReadDir(dir)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		if fi, err := e.Info(); err == nil {
			fmt.Printf("  %-22s %10d bytes\n", e.Name(), fi.Size())
		}
	}
	return nil
}

func cmdHistoDiff(args []string) error {
	fs := flag.NewFlagSet("histo-diff", flag.ExitOnError)
	top := fs.Int("top", 25, "rows to show")
	fs.Parse(args)
	if fs.NArg() != 2 {
		return errors.New("usage: histo-diff [--top N] OLD NEW")
	}
	old, err := jvm.ReadHistogram(fs.Arg(0))
	if err != nil {
		return err
	}
	cur, err := jvm.ReadHistogram(fs.Arg(1))
	if err != nil {
		return err
	}
	fmt.Printf("%14s %12s %14s  %s\n", "Δ bytes", "Δ instances", "now bytes", "class")
	for _, d := range jvm.DiffHistograms(old, cur)[:min(*top, len(cur))] {
		fmt.Printf("%14d %12d %14d  %s\n", d.DeltaBytes, d.DeltaInst, d.NowBytes, d.Class)
	}
	return nil
}

func cmdStress(args []string) error {
	n := 100
	if len(args) > 0 {
		var err error
		if n, err = strconv.Atoi(args[0]); err != nil {
			return fmt.Errorf("N must be a number: %w", err)
		}
	}
	r := root()
	mod, err := lab.Module(r)
	if err != nil {
		return err
	}
	dir, err := stress.Generate(r, mod, n)
	if err != nil {
		return err
	}
	fmt.Printf("generated %d packages in %s\n", n, dir)
	return goCmd(r, "run", "./"+stress.Dir+"/main")
}

func cmdStressClean([]string) error {
	return os.RemoveAll(filepath.Join(root(), stress.Dir))
}

func goCmd(dir string, args ...string) error {
	return lab.Go(dir, os.Stdout, args...)
}
