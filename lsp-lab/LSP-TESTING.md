# Go LSP test lab

Test bed for the JetBrains **Go LSP** VS Code extension (`jetbrains.goland-server`,
a JVM `intellij-server` process).

Open **this folder** (the one with `go.mod`) as the VS Code / GoLand workspace, so
the Go module and `.devcontainer/` configs are picked up.

**House rules:** every piece of code is Go (YAML/JSON config is fine), and gopls
is not used anywhere. All lab tooling lives in this module under `tools/`, so the
language server is exercised on the tooling too.

```
go run ./tools/lab up        # build + start devbox, runner, app (generates SSH key, prints ssh config)
go run ./tools/lab monitor   # live process monitor: leaks, hangs, orphans
go run ./tools/lab logs      # follow server/extension logs, errors highlighted
go run ./tools/lab help      # everything else
go run ./tools/lab session --up   # manual testing: containers + all monitors + dashboard (see MANUAL-TESTING.md)
```

Tip: `go build -o ~/go/bin/lab ./tools/lab` and then just `lab <command>`. Inside the containers, `lab` is already on `PATH`.

## Layout

| Path | What |
|---|---|
| `tools/lab` | CLI: containers, monitoring, logs, stress, examples |
| `tools/labinit` | Container entrypoint: SSH keys, sshd, scratch seeding, drops privileges to `dev` |
| `tools/internal/proc` | Process sampling, using `/proc` on Linux and `ps`/`lsof` on macOS (`_linux.go` / `_darwin.go` / `_other.go`) |
| `tools/internal/jvm` | `jcmd`: heap info, snapshots, class histograms and their diff |
| `tools/internal/monitor` | Alert rules, trend math (least squares, heap floors), CSV, docker stats |
| `tools/internal/logs` | Log discovery, follow, error grouping |
| `tools/internal/report` | Trend summary for a samples CSV |
| `tools/internal/stress` | Generator for N chained packages (`text/template`) |
| `tools/internal/lab` | docker compose, SSH, VSIX install, run-examples |
| `lsp/` | Sample code for exercising LSP features (see below) |
| `gobyexample/` | All Go by Example programs |
| `tools/internal/dashboard` | Dashboard: CSV loading, SVG charts, page template |
| `tools/internal/session` | `lab session` orchestration |
| `containers/` | Dockerfile, sshd config, scratch module template |

## 1. Monitoring

| Command | What it does |
|---|---|
| `lab monitor [--docker] [--notify]` | Samples every 5s: RSS, JVM heap (via the server's bundled `jcmd`), CPU, threads, fds, client TCP connections. Writes `lab-out/<host>/samples-*.csv` and `alerts.log`; `--docker` also writes `containers-*.csv`. `-h` lists all thresholds. |
| `lab logs [--all] [--short]` | Follows `intellij-server.log` for every workspace plus the extension-host `Go LSP.log`. Shows WARN/ERROR/SEVERE with stack traces, tagged with the workspace name. |
| `lab errors --since 168h` | Groups and counts errors by signature, with first/last seen time. |
| `lab summary [csv]` | Per-process trend report: RSS MB/h, heap-floor MB/h, max CPU/threads/fds, verdict. |
| `lab snapshot [--heapdump] PID` | Thread dump, heap info, VM flags, native memory, class histogram, lsof. |
| `lab histo-diff OLD NEW` | Shows which classes grew between two snapshots, for leak hunting. |
| `lab stress 300` / `lab stress-clean` | Generates 300 chained packages under `lsp/stress` (6000 promoted methods on `Node`) to load the server. |
| `lab session [--up] [--down]` | **Manual testing:** everything above, for the host and every container at once, plus the dashboard. See [MANUAL-TESTING.md](MANUAL-TESTING.md). |
| `lab dashboard` | Live dashboard at http://127.0.0.1:7070: process and container charts, alerts, markers, grouped log errors. Server-rendered Go, no JavaScript. |
| `lab mark "step"` | Annotates a manual test step; shows as a dashed line on every chart. |
| `lab in devbox monitor` / `lab in devbox logs` | The same commands, run inside a container. Output goes to `lab-out/<service>/`. |

Alerts:

| Code | Meaning |
|---|---|
| `ORPHAN` | Server reparented to PID 1 with **0 clients** for longer than `--orphan-grace` (1m). It should have exited after `--idle-timeout`. |
| `HOT` | CPU ≥ 95% for 1m. A thread dump is taken automatically. |
| `JVM-UNRESPONSIVE` | `jcmd` timed out (safepoint stall or deadlock). Snapshot taken. |
| `HEAP-LEAK` | The post-GC heap floor (minimum per 5-minute bucket) rises steadily, using a linear fit with R² ≥ 0.7. |
| `RSS-GROWTH` | Steady RSS growth ≥ 100 MB/h over at least 10 minutes. |
| `DUPLICATE` | More than one server for the same `--workspace-root`. |
| `RESTART-LOOP` | 4 or more server starts in 5 minutes. |
| `STATE-Z/D/T`, `FDS`, `THREADS`, `CONTAINER-MEM` | Zombie, uninterruptible or stopped process; fd or thread count too high; container at ≥ 90% of its memory limit. |

To get native-memory numbers in snapshots, add
`"intellij.additionalJvmArgs": ["-XX:NativeMemoryTracking=summary"]` to VS Code settings.

## 2–3. Containers (`docker-compose.yml`, project `lsp-lab`)

| Service | Purpose | Code | SSH | Other |
|---|---|---|---|---|
| `devbox` | **Write & run code.** | `/workspace` = this repo (bind mount, rw). `~/scratch` = separate module in a volume. | `127.0.0.1:2221` | dlv port 2345, 4 GB limit |
| `runner` | `lab run-examples`: vet, tests and every example on start, then stays up. `lab in runner run-examples /src` re-runs them. | `/src`, baked into the image | `2222` | 3 GB limit |
| `app` | Long-running HTTP service (`lsp/cmd/server`). | `/src`, baked into the image | `2223` | `curl localhost:8080/shapes`, `-X POST 'localhost:8080/circle?r=2'` |

All three images include Go 1.26.5, dlv, staticcheck, sshd, `lab`, htop, lsof and `SYS_PTRACE`, so `jcmd` works against the server inside the container. There is no gopls.
Named volumes keep the module cache, scratch and `~/.vscode-server`. `lab nuke` wipes them, which you need to test a clean extension install.
Other commands: `lab down`, `lab rebuild`, `lab ps`, `lab container-logs`, `lab shell runner`.

## 4. Remote development

* **VS Code Dev Containers:** *Reopen in Container* and pick `lsp-lab devbox | runner | app` (`.devcontainer/*`). You can also use *Attach to Running Container*.
* **VS Code Remote-SSH / JetBrains Gateway:** `lab ssh-config >> ~/.ssh/config`, then connect to `lsp-lab-devbox`, `lsp-lab-runner` or `lsp-lab-app` (user `dev`, key `containers/ssh/id_ed25519`).

The Go LSP extension came from a VSIX, so it isn't installed in containers automatically. Either click *Install in Dev Container / SSH* on the extension in the Extensions view, or connect once and run `lab install-ext /path/goland-server.vsix`.

## 5. Code for exercising LSP features

| Where | Exercises |
|---|---|
| `lsp/shapes` | interface → implementations, embedding and promoted methods, type switch, cross-package references |
| `lsp/store` | generics: constraints, unions, inference |
| `lsp/cmd/shapes`, `lsp/cmd/server` | multi-package imports, rename across packages, net/http |
| `lsp/diagnostics/broken.go` | **intentionally broken.** Each `// want:` line should show a diagnostic or quick fix. It is excluded from `lab test` and from the runner. |
| `tools/...` | real-world Go: build constraints (`proc_linux.go` / `proc_darwin.go`), `syscall`, `os/exec`, `text/template`, flag sets, tests |
| `~/scratch` (devbox) | a second module that exists only inside the container |
| `gobyexample/` | all 85 [Go by Example](https://gobyexample.com) programs, verbatim (CC BY 3.0, see `gobyexample/README.md`). The runner executes each one, with arguments, stdin, expected exit codes and server probes from `tools/internal/lab/examples.go`. `json` needs `GOEXPERIMENT=jsonv2` on Go 1.26, which the lab and containers set. |
