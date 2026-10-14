package dashboard

import (
	"fmt"
	"html/template"
	"math"
	"strings"
	"time"

	"learn_go_by_example/tools/internal/monitor"
)

// Categorical slots in fixed order; the CSS defines --s1..--s8 per theme.
const slots = 8

const (
	chartW, chartH         = 560.0, 200.0
	padL, padR, padT, padB = 52.0, 14.0, 14.0, 26.0
	maxLinePts             = 360 // points drawn per series
	maxHoverPts            = 72  // hover targets per series
)

type legendItem struct {
	Name, Last string
	Slot       int
}

// Chart is a rendered line chart plus its legend.
type Chart struct {
	Title  string
	SVG    template.HTML
	Legend []legendItem
	Empty  bool
}

// slotOf keeps a series' color stable across charts of the same source:
// it follows the entity (its rank in the source's sorted names), never the
// order a particular chart happens to list it in.
func slotOf(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i%slots + 1
		}
	}
	return slots
}

func lineChart(title, unit string, series []Series, names []string, from, to time.Time, markers []Marker) Chart {
	c := Chart{Title: title}
	var maxV float64
	n := 0
	for _, s := range series {
		for _, p := range s.Pts {
			maxV = math.Max(maxV, p.V)
			n++
		}
	}
	if n == 0 {
		c.Empty = true
		return c
	}
	if len(series) == 1 {
		c.Title = title + " - " + series[0].Name
	}
	yMax := niceCeil(maxV * 1.1)
	span := to.Sub(from).Seconds()
	x := func(t time.Time) float64 { return padL + (chartW-padL-padR)*t.Sub(from).Seconds()/span }
	y := func(v float64) float64 { return chartH - padB - (chartH-padT-padB)*v/yMax }

	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %.0f %.0f" role="img" aria-label="%s">`, chartW, chartH, template.HTMLEscapeString(c.Title))
	// Recessive grid and y labels.
	for i := 0; i <= 4; i++ {
		v := yMax * float64(i) / 4
		fmt.Fprintf(&b, `<line class="grid" x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f"/>`, padL, chartW-padR, y(v), y(v))
		fmt.Fprintf(&b, `<text class="axis" x="%.1f" y="%.1f" text-anchor="end">%s</text>`, padL-6, y(v)+4, fmtVal(v, unit))
	}
	for i, frac := range []float64{0, 0.5, 1} {
		t := from.Add(time.Duration(frac * float64(to.Sub(from))))
		anchor := [...]string{"start", "middle", "end"}[i]
		fmt.Fprintf(&b, `<text class="axis" x="%.1f" y="%.1f" text-anchor="%s">%s</text>`, x(t), chartH-6, anchor, t.Format("15:04:05"))
	}
	// Markers from `lab mark`.
	for _, m := range markers {
		mx := x(m.T)
		// Near the right edge the label flips to the left of the line.
		tx, anchor := mx+4, "start"
		if mx > chartW-padR-140 {
			tx, anchor = mx-4, "end"
		}
		fmt.Fprintf(&b, `<g class="marker"><title>%s %s</title><line x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f"/>`+
			`<text x="%.1f" y="%.1f" text-anchor="%s">%s</text></g>`,
			m.T.Format("15:04:05"), template.HTMLEscapeString(m.Text), mx, mx, padT, chartH-padB, tx, padT+10, anchor,
			template.HTMLEscapeString(truncate(m.Text, 24)))
	}
	for _, s := range series {
		slot := slotOf(names, s.Name)
		pts := downsample(s.Pts, maxLinePts)
		var path strings.Builder
		for i, p := range pts {
			cmd := "L"
			if i == 0 {
				cmd = "M"
			}
			fmt.Fprintf(&path, "%s%.1f %.1f", cmd, x(p.T), y(p.V))
		}
		fmt.Fprintf(&b, `<path class="line" style="stroke:var(--s%d)" d="%s"/>`, slot, path.String())
		lastPt := pts[len(pts)-1]
		fmt.Fprintf(&b, `<circle class="end" style="fill:var(--s%d)" cx="%.1f" cy="%.1f" r="4"/>`, slot, x(lastPt.T), y(lastPt.V))
		// Native hover tooltips: hit targets larger than the mark.
		for _, p := range downsample(s.Pts, maxHoverPts) {
			fmt.Fprintf(&b, `<circle class="hit" cx="%.1f" cy="%.1f" r="7"><title>%s  %s  %s</title></circle>`,
				x(p.T), y(p.V), template.HTMLEscapeString(s.Name), fmtVal(p.V, unit), p.T.Format("15:04:05"))
		}
		c.Legend = append(c.Legend, legendItem{s.Name, fmtVal(lastPt.V, unit), slot})
	}
	b.WriteString(`</svg>`)
	c.SVG = template.HTML(b.String())
	if len(series) < 2 {
		c.Legend = nil // the title names a single series
	}
	return c
}

// downsample keeps at most n points, preferring the peak of each bucket so
// spikes survive (they are what you are hunting for).
func downsample(pts []monitor.Point, n int) []monitor.Point {
	if len(pts) <= n {
		return pts
	}
	out := make([]monitor.Point, 0, n+1)
	step := float64(len(pts)) / float64(n)
	for i := 0; i < n; i++ {
		lo, hi := int(float64(i)*step), int(float64(i+1)*step)
		best := pts[lo]
		for _, p := range pts[lo:hi] {
			if p.V > best.V {
				best = p
			}
		}
		out = append(out, best)
	}
	if last := pts[len(pts)-1]; out[len(out)-1].T != last.T {
		out = append(out, last)
	}
	return out
}

func niceCeil(v float64) float64 {
	if v <= 0 {
		return 1
	}
	mag := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if v <= m*mag {
			return m * mag
		}
	}
	return 10 * mag
}

func fmtVal(v float64, unit string) string {
	switch {
	case v >= 100 || v == math.Trunc(v):
		return fmt.Sprintf("%.0f %s", v, unit)
	default:
		return fmt.Sprintf("%.1f %s", v, unit)
	}
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
