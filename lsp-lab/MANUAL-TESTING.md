# Manual testing guide

How to spin up the lab, code by hand against the Go LSP in each container,
and watch memory, CPU and hanging processes while you do it.

## 0. One-time setup

```
go build -o ~/go/bin/lab ./tools/lab     # optional: lets you type `lab …` instead of `go run ./tools/lab …`
lab up                                    # builds the images, generates the SSH key
```

Add the generated SSH config once (it is regenerated in `lab-out/ssh_config` on every session):

```
# first line of ~/.ssh/config
Include /path/to/this/repo/lab-out/ssh_config
```

## 1. Start a session (one terminal, leave it running)

```
lab session --up          # --up starts/updates the containers first; add --down to stop them on exit
```

This single command:

| What | Where it shows up |
|---|---|
| Starts devbox, runner and app and waits until they are ready | `[lab]` lines |
| Runs `lab monitor` on the **host** (VS Code on your Mac) and **inside every container** (where the Go LSP server runs when VS Code is connected remotely) | `[host]`, `[devbox]`, `[runner]`, `[app]` alert lines; CSVs in `lab-out/<source>/` |
| Records `docker stats` for every container | dashboard, `lab-out/<host>/containers-*.csv` |
| Follows the server and extension logs on the host and in each container. Only warnings and errors are shown, with stack traces. | `[host log]`, `[devbox log]` … |
| Serves the dashboard | **http://127.0.0.1:7070** |
| Prints a status line every minute | `[status]` |
| Sends a macOS notification on every ALERT | Notification Center |
| On Ctrl-C: stops the in-container monitors and prints a trend summary per source | end of output |

Open **http://127.0.0.1:7070** next to your editor. It refreshes every 5 seconds and has 5m, 15m, 1h, 6h and 24h windows. Hover any line for exact values.

## 2. Connect an editor to a container

Pick one per test run:

| Target | How |
|---|---|
| devbox (bind-mounted repo, write code here) | VS Code → *Dev Containers: Reopen in Container* → `lsp-lab devbox`, or *Remote-SSH* → `lsp-lab-devbox` |
| runner (baked copy of the code) | `lsp-lab runner` / `lsp-lab-runner` |
| app (baked copy, running HTTP service on :8080) | `lsp-lab app` / `lsp-lab-app` |
| host (no container) | open this folder in VS Code locally |

The extension isn't on the marketplace, so the first time you connect, install it in the container. Use **Install in Dev Container / SSH** on the extension, or run `lab install-ext /path/goland-server.vsix`.

As soon as the server starts, the session prints `[devbox] … START pid=…` and the dashboard gets a devbox process table and charts.

## 3. Mark every test step

Before each manual action, from any terminal on the host or in devbox:

```
lab mark "go to definition across packages"
lab mark "rename Shape -> Figure"
lab mark "lab stress 300 + completion on n."
```

Markers appear as dashed lines on **every** chart, so a memory step or CPU spike lines up with the action that caused it.

## 4. What to test: suggested checklist

| # | Step | Files |
|---|---|---|
| 1 | Open the workspace, wait for indexing to finish. **This is your baseline**: note the RSS and heap. | - |
| 2 | Go to definition / references across packages | `lsp/cmd/shapes/main.go` → `lsp/shapes`, `lsp/store` |
| 3 | Go to implementation on an interface | `Shape` in `lsp/shapes/shapes.go` |
| 4 | Completion on promoted methods, generics, type switch cases | `lsp/shapes`, `lsp/store` |
| 5 | Diagnostics and quick fixes | `lsp/diagnostics/broken.go` (each `// want:` line) |
| 6 | Rename across packages, then undo | `TotalArea`, `Store` |
| 7 | Build constraints: open the `_linux.go` and `_darwin.go` files | `tools/internal/proc` |
| 8 | GOEXPERIMENT handling: is `encoding/json/v2` resolved? | `gobyexample/json/json.go` |
| 9 | Run tests from code lens / gutter | `lsp/*/…_test.go`, `gobyexample/testing-and-benchmarking` |
| 10 | Browse the 85 Go by Example programs, editing as you go | `gobyexample/` |
| 11 | Write new code from scratch | devbox `~/scratch` (its own module) |
| 12 | Load test: `lab stress 300`, then type in `lsp/stress/main/main.go` | generated, `lab stress-clean` afterwards |
| 13 | Disconnect: close the VS Code window and keep watching for 2 minutes | - |

## 5. Reading the results

### Memory leaks

* **Heap floor rising.** Watch the *JVM heap used* chart. It's a sawtooth, and the low points after each GC are what matter. If the low points keep rising while you repeat the same action, that's a leak. The monitor checks this automatically and raises `HEAP-LEAK`.
* **RSS rising while the heap stays flat.** That points to native or off-heap growth, or to memory the JVM doesn't return to the OS. `RSS-GROWTH` fires at 100 MB/h or more over at least 10 minutes. To check native memory, add `"intellij.additionalJvmArgs": ["-XX:NativeMemoryTracking=summary"]` to the VS Code settings and take snapshots.
* **Confirming a suspected leak:**
  ```
  lab snapshot <pid>                     # host; in a container: lab in devbox snapshot <pid>
  # repeat the suspect action ~20 times
  lab snapshot <pid>
  lab histo-diff lab-out/<src>/snap-A lab-out/<src>/snap-B   # which classes grew
  lab snapshot --heapdump <pid>          # .hprof for a profiler, if needed
  ```
* **Container memory.** The *Container memory* chart includes VS Code Server and the language server. `CONTAINER-MEM` fires at 90% of the container's limit (devbox has 4 GB, the others 3 GB).

### Hanging processes

* **`HOT`**: CPU at 95% or more for a full minute, for example a runaway analysis or a busy loop. A thread dump is saved automatically to `lab-out/<src>/snap-…-hot/threads.txt`.
* **`JVM-UNRESPONSIVE`**: the JVM didn't answer `jcmd` within 15 seconds (safepoint stall or deadlock). A snapshot is taken automatically.
* **`ORPHAN`**: the server is detached (its parent is PID 1) and has had **0 clients** for a minute. Since it's started with `--idle-timeout 10s`, it should have exited. Check this after step 13.
* **`DUPLICATE`**: two servers for the same workspace, for example after reconnecting.
* **`RESTART-LOOP`**: 4 or more server starts in 5 minutes, meaning it keeps crashing. Check the `[… log]` lines.
* **`STATE-Z`, `STATE-D`, `STATE-T`**: zombie, uninterruptible sleep, or stopped process.
* **UI feels stuck but no alert?** Take a thread dump manually with `lab snapshot <pid>` and look for `BLOCKED` or long-running analysis threads.

### Errors

Errors and stack traces stream into the session as they happen. The dashboard's *Log errors* panel groups them by signature over the last hour. For a longer window, run `lab errors --since 24h` (host) or `lab in devbox errors --since 24h`.

## 6. After the session

Ctrl-C prints a per-source summary (RSS and heap trend in MB/h, CPU, thread and fd peaks, verdict). Everything stays in `lab-out/`:

| File | Content |
|---|---|
| `lab-out/<source>/samples-*.csv` | Every sample: RSS, heap, CPU, threads, fds, clients |
| `lab-out/<host>/containers-*.csv` | docker stats |
| `lab-out/<source>/alerts.log` | All alerts |
| `lab-out/<source>/snap-*` | Snapshots (automatic and manual) |
| `lab-out/markers.log` | Your markers |

`lab summary lab-out/<source>/samples-….csv` re-runs the report on any file. `lab dashboard` serves the dashboard without starting a session, so you can review afterwards.

### Monitoring without the session

| Need | Command |
|---|---|
| Only the host | `lab monitor --docker --notify` and `lab logs` |
| Only one container | `lab in devbox monitor` and `lab in devbox logs` |
| Dashboard only | `lab dashboard` |
