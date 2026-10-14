// Package shapes exercises interface/implementation navigation,
// struct embedding and method sets.
package shapes

import (
	"fmt"
	"math"
)

// Shape is implemented by Circle, Rect and Square.
// Try "Go to Implementation" on Shape or on Area.
type Shape interface {
	Area() float64
	Perimeter() float64
}

// Named is a second interface so a type can satisfy several at once.
type Named interface {
	Name() string
}

type Circle struct {
	Radius float64
}

func (c Circle) Area() float64      { return math.Pi * c.Radius * c.Radius }
func (c Circle) Perimeter() float64 { return 2 * math.Pi * c.Radius }
func (c Circle) Name() string       { return "circle" }

type Rect struct {
	Width, Height float64
}

func (r Rect) Area() float64      { return r.Width * r.Height }
func (r Rect) Perimeter() float64 { return 2 * (r.Width + r.Height) }
func (r Rect) Name() string       { return "rect" }

// Square embeds Rect: Area and Perimeter are promoted, Name is overridden.
type Square struct {
	Rect
}

func NewSquare(side float64) Square {
	return Square{Rect{Width: side, Height: side}}
}

func (s Square) Name() string { return "square" }

// Describe uses a type switch, which is useful for testing completion
// inside case clauses.
func Describe(s Shape) string {
	name := "shape"
	if n, ok := s.(Named); ok {
		name = n.Name()
	}
	switch v := s.(type) {
	case Circle:
		return fmt.Sprintf("%s r=%.1f area=%.2f", name, v.Radius, v.Area())
	case Square:
		return fmt.Sprintf("%s side=%.1f area=%.2f", name, v.Width, v.Area())
	case Rect:
		return fmt.Sprintf("%s %.1fx%.1f area=%.2f", name, v.Width, v.Height, v.Area())
	default:
		return fmt.Sprintf("%s area=%.2f", name, s.Area())
	}
}

// TotalArea is referenced from cmd/shapes and cmd/server ("Find References").
func TotalArea(shapes ...Shape) float64 {
	total := 0.0
	for _, s := range shapes {
		total += s.Area()
	}
	return total
}
