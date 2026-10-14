package store

import (
	"slices"
	"testing"
)

func TestStore(t *testing.T) {
	s := New[string, int]()
	s.Put("b", 2)
	s.Put("a", 1)

	if v, ok := s.Get("a"); !ok || v != 1 {
		t.Fatalf("Get(a) = %v, %v", v, ok)
	}
	if got := s.Keys(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("Keys() = %v", got)
	}
}

func TestMapAndSum(t *testing.T) {
	doubled := Map([]int{1, 2, 3}, func(i int) int { return i * 2 })
	if got := Sum(doubled...); got != 12 {
		t.Fatalf("Sum = %v, want 12", got)
	}
}
