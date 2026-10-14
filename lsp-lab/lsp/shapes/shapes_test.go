package shapes

import (
	"math"
	"testing"
)

func TestArea(t *testing.T) {
	tests := []struct {
		name  string
		shape Shape
		want  float64
	}{
		{"circle", Circle{Radius: 1}, math.Pi},
		{"rect", Rect{Width: 2, Height: 3}, 6},
		{"square", NewSquare(4), 16},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.shape.Area(); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("Area() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTotalArea(t *testing.T) {
	if got := TotalArea(Rect{1, 1}, NewSquare(2)); got != 5 {
		t.Errorf("TotalArea() = %v, want 5", got)
	}
}

func BenchmarkDescribe(b *testing.B) {
	c := Circle{Radius: 2}
	for b.Loop() {
		_ = Describe(c)
	}
}
