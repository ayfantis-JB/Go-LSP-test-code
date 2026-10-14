// Package store exercises generics: type parameters, constraints and
// inference, which are common sources of LSP edge cases.
package store

import (
	"cmp"
	"slices"
	"sync"
)

// Number is a constraint union.
type Number interface {
	~int | ~int64 | ~float64
}

// Store is a concurrency-safe generic map.
type Store[K cmp.Ordered, V any] struct {
	mu    sync.RWMutex
	items map[K]V
}

func New[K cmp.Ordered, V any]() *Store[K, V] {
	return &Store[K, V]{items: make(map[K]V)}
}

func (s *Store[K, V]) Put(key K, value V) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[key] = value
}

func (s *Store[K, V]) Get(key K) (V, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.items[key]
	return v, ok
}

// Keys returns the keys in sorted order.
func (s *Store[K, V]) Keys() []K {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]K, 0, len(s.items))
	for k := range s.items {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func (s *Store[K, V]) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}

// Map transforms a slice; the type arguments are inferred at call sites.
func Map[T, U any](in []T, f func(T) U) []U {
	out := make([]U, 0, len(in))
	for _, v := range in {
		out = append(out, f(v))
	}
	return out
}

func Sum[N Number](values ...N) N {
	var total N
	for _, v := range values {
		total += v
	}
	return total
}
