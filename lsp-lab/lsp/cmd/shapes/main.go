// Command shapes runs once and exits; used by the "runner" container.
package main

import (
	"fmt"

	"learn_go_by_example/lsp/shapes"
	"learn_go_by_example/lsp/store"
)

func main() {
	all := []shapes.Shape{
		shapes.Circle{Radius: 1.5},
		shapes.Rect{Width: 2, Height: 3},
		shapes.NewSquare(2),
	}

	byName := store.New[string, shapes.Shape]()
	for _, s := range all {
		fmt.Println(shapes.Describe(s))
		if n, ok := s.(shapes.Named); ok {
			byName.Put(n.Name(), s)
		}
	}

	names := store.Map(byName.Keys(), func(k string) string { return "<" + k + ">" })
	fmt.Println("names:", names)
	fmt.Printf("total area: %.2f\n", shapes.TotalArea(all...))
}
