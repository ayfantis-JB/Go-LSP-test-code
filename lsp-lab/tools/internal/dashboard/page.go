package dashboard

// pageHTML is the dashboard template. Colors are role tokens; the categorical
// slots --s1..--s8 follow the validated reference palette (light and dark steps).
const pageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="{{.Refresh}}">
<title>LSP Lab Monitor</title>
<style>
:root {
  color-scheme: light;
  --bg: #f6f6f4; --surface: #fcfcfb; --border: #e3e2de;
  --text: #0b0b0b; --text-2: #52514e; --muted: #8a8984; --grid: #ebeae6;
  --s1: #2a78d6; --s2: #eb6834; --s3: #1baf7a; --s4: #eda100;
  --s5: #e87ba4; --s6: #008300; --s7: #6250d6; --s8: #e34948;
  --critical: #c62828; --warning: #a86b00; --info: #52514e;
  --critical-bg: #fdecea; --warning-bg: #fff4dc;
}
@media (prefers-color-scheme: dark) {
  :root:where(:not([data-theme="light"])) {
    color-scheme: dark;
    --bg: #111110; --surface: #1a1a19; --border: #2e2e2c;
    --text: #f0efec; --text-2: #c3c2b7; --muted: #8f8e88; --grid: #262624;
    --s1: #3987e5; --s2: #d95926; --s3: #199e70; --s4: #c98500;
    --s5: #d55181; --s6: #008300; --s7: #9085e9; --s8: #e66767;
    --critical: #ff8a80; --warning: #f0b84a; --info: #c3c2b7;
    --critical-bg: #3a1716; --warning-bg: #33280f;
  }
}
:root[data-theme="dark"] {
  color-scheme: dark;
  --bg: #111110; --surface: #1a1a19; --border: #2e2e2c;
  --text: #f0efec; --text-2: #c3c2b7; --muted: #8f8e88; --grid: #262624;
  --s1: #3987e5; --s2: #d95926; --s3: #199e70; --s4: #c98500;
  --s5: #d55181; --s6: #008300; --s7: #9085e9; --s8: #e66767;
  --critical: #ff8a80; --warning: #f0b84a; --info: #c3c2b7;
  --critical-bg: #3a1716; --warning-bg: #33280f;
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--text);
  font: 14px/1.45 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif; }
main { max-width: 1240px; margin: 0 auto; padding: 20px 16px 48px; }
header { display: flex; flex-wrap: wrap; align-items: baseline; gap: 8px 20px; margin-bottom: 16px; }
h1 { font-size: 20px; margin: 0; }
h2 { font-size: 16px; margin: 0 0 10px; }
h3 { font-size: 13px; margin: 0 0 4px; color: var(--text-2); font-weight: 600; }
.meta, .hint { color: var(--muted); font-size: 12px; }
code { font: 12px ui-monospace, SFMono-Regular, Menlo, monospace; }
nav a { color: var(--text-2); text-decoration: none; padding: 2px 8px; border-radius: 6px; border: 1px solid var(--border); font-size: 12px; }
nav a.on { background: var(--text); color: var(--bg); border-color: var(--text); }
section { background: var(--surface); border: 1px solid var(--border); border-radius: 10px; padding: 14px 16px; margin-bottom: 16px; }
.charts { display: grid; grid-template-columns: repeat(auto-fit, minmax(440px, 1fr)); gap: 16px 20px; margin-top: 12px; }
@media (max-width: 520px) { .charts { grid-template-columns: 1fr; } }
svg { width: 100%; height: auto; display: block; overflow: visible; }
.grid { stroke: var(--grid); stroke-width: 1; }
.axis { fill: var(--muted); font-size: 11px; }
.line { fill: none; stroke-width: 2; stroke-linejoin: round; stroke-linecap: round; }
.end { stroke: var(--surface); stroke-width: 2; }
.hit { fill: transparent; }
.hit:hover { fill: var(--text-2); fill-opacity: .25; }
.marker line { stroke: var(--text-2); stroke-width: 1; stroke-dasharray: 3 3; }
.marker text { fill: var(--text-2); font-size: 10px; }
.legend { display: flex; flex-wrap: wrap; gap: 4px 14px; margin-top: 4px; font-size: 12px; color: var(--text-2); }
.sw { display: inline-block; width: 10px; height: 10px; border-radius: 3px; margin-right: 5px; vertical-align: -1px; }
.empty { color: var(--muted); font-size: 12px; padding: 24px 0; text-align: center; border: 1px dashed var(--border); border-radius: 8px; }
.table { overflow-x: auto; }
table { border-collapse: collapse; width: 100%; font-size: 12px; }
th { text-align: left; color: var(--muted); font-weight: 500; border-bottom: 1px solid var(--border); padding: 4px 8px; white-space: nowrap; }
td { padding: 4px 8px; border-bottom: 1px solid var(--grid); white-space: nowrap; font-variant-numeric: tabular-nums; }
td.wrap { white-space: normal; }
.lv { font-weight: 600; font-size: 11px; padding: 1px 6px; border-radius: 4px; }
.lv-ALERT { color: var(--critical); background: var(--critical-bg); }
.lv-WARN { color: var(--warning); background: var(--warning-bg); }
.lv-INFO { color: var(--info); }
pre { margin: 6px 0 0; font: 11.5px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; color: var(--text-2); white-space: pre-wrap; word-break: break-word; }
.cols { display: grid; grid-template-columns: repeat(auto-fit, minmax(360px, 1fr)); gap: 16px; }
</style>
</head>
<body>
<main>
<header>
  <h1>LSP lab monitor</h1>
  <span class="meta">updated {{clock .Now}} · refreshes every {{.Refresh}}s</span>
  <nav>{{range .Windows}}<a href="?w={{.}}"{{if eq . $.Window}} class="on"{{end}}>{{.}}</a> {{end}}</nav>
  <span class="hint">annotate a test step: <code>go run ./tools/lab mark "rename Shape"</code></span>
</header>

<div class="cols">
<section>
  <h2>Alerts</h2>
  {{if .Alerts}}<div class="table"><table>
    <tr><th>Time</th><th>Source</th><th>Level</th><th>Code</th><th>Message</th></tr>
    {{range .Alerts}}<tr><td>{{clock .T}}</td><td>{{.Source}}</td>
      <td><span class="lv lv-{{.Level}}">{{if eq .Level "ALERT"}}▲ {{else if eq .Level "WARN"}}● {{end}}{{.Level}}</span></td>
      <td>{{.Code}}</td><td class="wrap">{{.Msg}}</td></tr>{{end}}
  </table></div>{{else}}<div class="empty">No alerts in the last {{.Window}}</div>{{end}}
</section>
<section>
  <h2>Markers</h2>
  {{if .Markers}}<div class="table"><table><tr><th>Time</th><th>Step</th></tr>
    {{range .Markers}}<tr><td>{{clock .T}}</td><td class="wrap">{{.Text}}</td></tr>{{end}}
  </table></div>{{else}}<div class="empty">No markers yet. Run <code>lab mark "…"</code> before each manual test step; markers show as dashed lines on every chart.</div>{{end}}
</section>
</div>

<section>
  <h2>Containers</h2>
  {{if .Containers.Latest}}<div class="table"><table>
    <tr><th>Container</th><th>CPU %</th><th>Memory</th><th>Mem % of limit</th><th>PIDs</th></tr>
    {{range .Containers.Latest}}<tr><td>{{.Name}}</td><td>{{.CPU}}</td><td>{{.Mem}}</td><td>{{.MemPct}}</td><td>{{.PIDs}}</td></tr>{{end}}
  </table></div>{{else}}<div class="empty">No container stats - is the host monitor running with --docker?</div>{{end}}
  <div class="charts">{{range .CChart}}{{template "chart" .}}{{end}}</div>
</section>

{{range .Sources}}
<section>
  <h2>{{.Label}} · Go LSP server processes</h2>
  {{if .Procs}}<div class="table"><table>
    <tr><th>PID</th><th>PPID</th><th>State</th><th>RSS MB</th><th>Heap MB</th><th>CPU %</th><th>Threads</th><th>FDs</th><th>Clients</th><th>Uptime</th><th>Last seen</th><th>Workspace</th></tr>
    {{range .Procs}}<tr><td>{{.PID}}</td><td>{{.PPID}}</td><td>{{.State}}</td><td>{{.RSS}}</td><td>{{.Heap}}</td><td>{{.CPU}}</td>
      <td>{{.Threads}}</td><td>{{.FDs}}</td><td>{{.Clients}}</td><td>{{.Uptime}}</td><td>{{clock .Seen}}</td><td>{{.Workspace}}</td></tr>{{end}}
  </table></div>{{else}}<div class="empty">No Go LSP server seen here in the last {{$.Window}}. Open a Go file in VS Code connected to {{.Name}}.</div>{{end}}
  {{if .Procs}}<div class="charts">{{range .Charts}}{{template "chart" .}}{{end}}</div>{{end}}
</section>
{{end}}

<section>
  <h2>Log errors (last hour)</h2>
  <span class="meta">regrouped every 30s{{if not .ErrAt.IsZero}}, last at {{clock .ErrAt}}{{end}}</span>
  {{range .Errors}}<h3 style="margin-top:12px">{{.Source}}</h3><pre>{{.Text}}</pre>{{else}}<div class="empty">collecting…</div>{{end}}
</section>
</main>
</body>
</html>
{{define "chart"}}<div>
  <h3>{{.Title}}</h3>
  {{if .Empty}}<div class="empty">no data in this window</div>{{else}}{{.SVG}}
  {{if .Legend}}<div class="legend">{{range .Legend}}<span><span class="sw" style="background:var(--s{{.Slot}})"></span>{{.Name}} · {{.Last}}</span>{{end}}</div>{{end}}{{end}}
</div>{{end}}`
