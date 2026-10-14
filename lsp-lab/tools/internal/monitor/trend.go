package monitor

import (
	"math"
	"sort"
	"time"
)

// Point is a timestamped value.
type Point struct {
	T time.Time
	V float64
}

// SlopePerHour fits a least-squares line and returns units/hour and R².
func SlopePerHour(pts []Point) (slope, r2 float64) {
	n := float64(len(pts))
	if n < 3 {
		return 0, 0
	}
	var mx, my float64
	for _, p := range pts {
		mx += float64(p.T.Unix())
		my += p.V
	}
	mx, my = mx/n, my/n
	var sxx, sxy, syy float64
	for _, p := range pts {
		dx, dy := float64(p.T.Unix())-mx, p.V-my
		sxx += dx * dx
		sxy += dx * dy
		syy += dy * dy
	}
	if sxx == 0 {
		return 0, 0
	}
	if syy > 0 {
		r2 = sxy * sxy / (sxx * syy)
	}
	return sxy / sxx * 3600, r2
}

// Floors keeps the minimum of each time bucket. For a JVM heap this
// approximates the post-GC floor, which is what grows under a real leak.
func Floors(pts []Point, bucket time.Duration) []Point {
	mins := map[int64]Point{}
	for _, p := range pts {
		k := p.T.UnixNano() / int64(bucket)
		if cur, ok := mins[k]; !ok || p.V < cur.V {
			mins[k] = p
		}
	}
	out := make([]Point, 0, len(mins))
	for _, p := range mins {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].T.Before(out[j].T) })
	return out
}

// Trim drops points older than window relative to now.
func Trim(pts []Point, now time.Time, window time.Duration) []Point {
	i := sort.Search(len(pts), func(i int) bool { return now.Sub(pts[i].T) <= window })
	return pts[i:]
}

// Span is the time between the first and last point.
func Span(pts []Point) time.Duration {
	if len(pts) < 2 {
		return 0
	}
	return pts[len(pts)-1].T.Sub(pts[0].T)
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
