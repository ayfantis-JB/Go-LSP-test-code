package monitor

import (
	"testing"
	"time"
)

func TestSlopePerHour(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	var pts []Point
	for i := range 10 {
		pts = append(pts, Point{t0.Add(time.Duration(i) * time.Minute), 100 + float64(i)*5}) // +5 MB/min
	}
	s, r2 := SlopePerHour(pts)
	if s < 299 || s > 301 || r2 < 0.99 {
		t.Errorf("slope=%v r2=%v, want 300 MB/h, r2≈1", s, r2)
	}
}

func TestFloorsIgnoresSawtooth(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0).Truncate(5 * time.Minute)
	var pts []Point
	// Sawtooth between 100 and 500 MB whose floor rises 10 MB per bucket.
	for b := range 4 {
		for i, v := range []float64{500, 300, 100} {
			pts = append(pts, Point{t0.Add(time.Duration(b)*5*time.Minute + time.Duration(i)*time.Minute), v + float64(b)*10})
		}
	}
	fl := Floors(pts, 5*time.Minute)
	if len(fl) != 4 || fl[0].V != 100 || fl[3].V != 130 {
		t.Errorf("floors = %v", fl)
	}
}

func TestTrim(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	pts := []Point{{now.Add(-2 * time.Hour), 1}, {now.Add(-30 * time.Minute), 2}, {now, 3}}
	if got := Trim(pts, now, time.Hour); len(got) != 2 || got[0].V != 2 {
		t.Errorf("Trim = %v", got)
	}
}
